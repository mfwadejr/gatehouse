package devices

import (
	"encoding/csv"
	"io"
	"os"
	"strings"
	"sync"
)

// The maker of a network card is the first half of its address. The list is the IEEE's public
// registry. It comes with Debian's ieee-data package, which Gatehouse installs and which updates
// refresh, so there is nothing to download here. Without the file, makers are simply not shown.

// OUIPath is where the registry file is read from.
var OUIPath = "/usr/share/ieee-data/oui.csv"

var (
	ouiOnce sync.Once
	ouiMap  map[string]string
	ouiPath string
)

func loadOUI() {
	ouiMap = map[string]string{}
	f, err := os.Open(OUIPath)
	if err != nil {
		return
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) < 3 || len(rec[1]) != 6 {
			continue
		}
		ouiMap[strings.ToLower(rec[1])] = shortVendor(rec[2])
	}
}

// ReloadOUI forgets the loaded list, so the next lookup reads the file again.
func ReloadOUI() { ouiOnce = sync.Once{} }

var vendorSuffix = []string{
	", inc.", ", inc", " inc.", " inc", ", ltd.", ", ltd", " ltd.", " ltd", " co., ltd.", " co.,ltd.", " co., ltd", " co.,ltd", " co. ltd", " corporation", " corp.", " corp",
	" limited", " llc", " l.l.c.", " gmbh", " s.a.", " s.p.a.", " b.v.", " ag", " a/s", " ab", " oy", " plc", " pte", " pty", " technologies", " technology",
	" international", " electronics", " (shenzhen)", " (china)", " company", " co.", ",",
}

// shortVendor trims the legal ending from a registered name, so "Apple, Inc." reads "Apple".
func shortVendor(s string) string {
	s = strings.TrimSpace(s)
	for changed := true; changed; {
		changed = false
		l := strings.ToLower(s)
		for _, suf := range vendorSuffix {
			if strings.HasSuffix(l, suf) && len(s) > len(suf) {
				s = strings.TrimSpace(s[:len(s)-len(suf)])
				changed = true
				break
			}
		}
	}
	if len(s) > 28 {
		if i := strings.LastIndex(s[:28], " "); i > 6 {
			s = s[:i]
		}
	}
	return s
}

// IsPrivateMAC says whether an address was made up by the device itself. Phones and laptops do this
// per network, so no maker can be named for them.
func IsPrivateMAC(mac string) bool {
	mac = strings.ToLower(strings.TrimSpace(mac))
	if len(mac) < 2 {
		return false
	}
	var b byte
	for _, c := range mac[:2] {
		b <<= 4
		switch {
		case c >= '0' && c <= '9':
			b |= byte(c - '0')
		case c >= 'a' && c <= 'f':
			b |= byte(c-'a') + 10
		}
	}
	return b&0x02 != 0
}

// Vendor names the maker of a device from its address. A made-up address says so instead.
func Vendor(mac string) string {
	mac = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(mac, ":", ""), "-", ""))
	if len(mac) < 6 {
		return ""
	}
	if IsPrivateMAC(mac) {
		return "Private address"
	}
	ouiOnce.Do(loadOUI)
	return ouiMap[mac[:6]]
}
