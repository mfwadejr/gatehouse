package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/bits"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Country matching uses the address lists the five regional internet registries publish. Nothing
// is bought or licensed: each registry says which country it handed every block of addresses to.

var rirFiles = []string{
	"https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest",
	"https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest",
	"https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest",
	"https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest",
	"https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest",
}

const geoMaxAge = 7 * 24 * time.Hour

func (a *App) geoFile() string { return filepath.Join(a.Cfg.DataDir, "geo", "prefixes.txt") }

// geoAge says how old the country data is; ok is false when there is none.
func (a *App) geoAge() (time.Duration, bool) {
	st, err := os.Stat(a.geoFile())
	if err != nil {
		return 0, false
	}
	return time.Since(st.ModTime()), true
}

// cidrsOf splits a run of count IPv4 addresses starting at start into aligned networks.
func cidrsOf(start netip.Addr, count uint64) []string {
	var out []string
	b := start.As4()
	cur := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
	for count > 0 && cur < 1<<32 {
		size := uint64(1) << uint(bits.TrailingZeros64(cur|1<<32))
		for size > count {
			size >>= 1
		}
		ip := netip.AddrFrom4([4]byte{byte(cur >> 24), byte(cur >> 16), byte(cur >> 8), byte(cur)})
		out = append(out, fmt.Sprintf("%s/%d", ip, 32-bits.TrailingZeros64(size)))
		cur += size
		count -= size
	}
	return out
}

// parseDelegations reads one registry file and passes each allocated block on.
func parseDelegations(r io.Reader, emit func(cc string, fam int, prefix string)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) < 7 || len(f[1]) != 2 || (f[6] != "allocated" && f[6] != "assigned") {
			continue
		}
		cc := strings.ToUpper(f[1])
		switch f[2] {
		case "ipv4":
			start, err := netip.ParseAddr(f[3])
			n, err2 := strconv.ParseUint(f[4], 10, 64)
			if err != nil || err2 != nil || !start.Is4() {
				continue
			}
			for _, p := range cidrsOf(start, n) {
				emit(cc, 4, p)
			}
		case "ipv6":
			bitsN, err := strconv.Atoi(f[4])
			start, err2 := netip.ParseAddr(f[3])
			if err != nil || err2 != nil || bitsN < 1 || bitsN > 64 {
				continue
			}
			emit(cc, 6, netip.PrefixFrom(start, bitsN).Masked().String())
		}
	}
	return sc.Err()
}

// geoRefresh downloads the five files and writes one combined list. The old list stays in use
// if anything goes wrong.
func (a *App) geoRefresh(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(a.geoFile()), 0o750); err != nil {
		return err
	}
	tmp := a.geoFile() + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	w := bufio.NewWriter(out)
	n := 0
	emit := func(cc string, fam int, prefix string) { fmt.Fprintf(w, "%s %d %s\n", cc, fam, prefix); n++ }
	client := &http.Client{Timeout: 4 * time.Minute}
	for _, u := range rirFiles {
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		resp, err := client.Do(req)
		if err != nil {
			out.Close()
			return fmt.Errorf("could not download the country lists: %w", err)
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			out.Close()
			return fmt.Errorf("could not download the country lists: %s answered %s", req.URL.Host, resp.Status)
		}
		err = parseDelegations(resp.Body, emit)
		resp.Body.Close()
		if err != nil {
			out.Close()
			return err
		}
	}
	if n < 1000 {
		out.Close()
		return errors.New("the country lists came back nearly empty; keeping the old ones")
	}
	if err := w.Flush(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, a.geoFile())
}

// geoPrefixes lists the networks of one country and family.
func (a *App) geoPrefixes(want map[string]bool) map[string][]string {
	res := map[string][]string{}
	f, err := os.Open(a.geoFile())
	if err != nil {
		return res
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.SplitN(sc.Text(), " ", 3)
		if len(p) != 3 {
			continue
		}
		key := strings.ToLower(p[0]) + "_" + p[1]
		if want[key] {
			res[key] = append(res[key], p[2])
		}
	}
	return res
}

// geoLoop keeps the country data fresh while any rule uses it.
func (a *App) geoMaybeRefresh(ctx context.Context, needed bool) {
	if !needed {
		return
	}
	age, ok := a.geoAge()
	if ok && age < geoMaxAge {
		return
	}
	if !a.dyn.geoBusy.CompareAndSwap(false, true) {
		return
	}
	defer a.dyn.geoBusy.Store(false)
	if err := a.geoRefresh(ctx); err != nil {
		log.Printf("geo: %v", err)
		a.dyn.geoErr.Store(err.Error())
		return
	}
	a.dyn.geoErr.Store("")
	a.Config.SetSetting("geo.updated", strconv.FormatInt(time.Now().Unix(), 10))
	a.event("system", "info", "Country address lists updated", "Downloaded from the regional internet registries.")
	a.fwSyncAll()
}

// geoIndex answers "which country is this address in" from the same registry lists the country
// rules use. It reads the list once and again whenever the file is replaced.
type geoIndex struct {
	mu    sync.Mutex
	mtime time.Time
	rows  []geoRange // sorted by start; ranges do not overlap
}

type geoRange struct {
	start, end netip.Addr
	cc         string
}

func (a *App) geoLoad() {
	g := &a.geoIdx
	st, err := os.Stat(a.geoFile())
	if err != nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if st.ModTime().Equal(g.mtime) && g.rows != nil {
		return
	}
	f, err := os.Open(a.geoFile())
	if err != nil {
		return
	}
	defer f.Close()
	var rows []geoRange
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.SplitN(sc.Text(), " ", 3)
		if len(p) != 3 {
			continue
		}
		pf, err := netip.ParsePrefix(p[2])
		if err != nil {
			continue
		}
		pf = pf.Masked()
		rows = append(rows, geoRange{pf.Addr(), lastAddr(pf), p[0]})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].start.Compare(rows[j].start) < 0 })
	g.rows, g.mtime = rows, st.ModTime()
}

// lastAddr is the final address of a network.
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 1 << (7 - uint(i%8))
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

// country gives the two-letter country code of a public address, or "" when it is not known.
func (a *App) country(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
		return ""
	}
	addr = addr.Unmap()
	a.geoLoad()
	g := &a.geoIdx
	g.mu.Lock()
	defer g.mu.Unlock()
	i := sort.Search(len(g.rows), func(i int) bool { return g.rows[i].start.Compare(addr) > 0 }) - 1
	if i >= 0 && g.rows[i].end.Compare(addr) >= 0 && g.rows[i].start.Is4() == addr.Is4() {
		return g.rows[i].cc
	}
	return ""
}
