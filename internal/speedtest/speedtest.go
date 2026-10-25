// Package speedtest measures one internet connection: how fast it downloads and uploads, how long
// an answer takes, and how much that delay grows when the line is busy. The test is bound to one
// network device, so a connection that is only standing by is measured through itself and nothing
// else. It talks to a public server that needs no account (Cloudflare's speed test by default) and
// sends nothing but the test data.
package speedtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultServer is the public test server used when none is chosen.
const DefaultServer = "https://speed.cloudflare.com"

// Params says what to measure.
type Params struct {
	Dev    string `json:"dev"`    // network device to leave through
	Server string `json:"server"` // base address of the test server
	Quick  bool   `json:"quick"`  // a short test that uses little data, for connections with a limit
}

// Result is one finished test.
type Result struct {
	DownBps   float64 `json:"down_bps"`
	UpBps     float64 `json:"up_bps"`
	DelayMS   float64 `json:"delay_ms"`  // answer time on an idle line
	JitterMS  float64 `json:"jitter_ms"` // how much that time varies
	LoadedMS  float64 `json:"loaded_ms"` // answer time while the line is busy downloading
	Grade     string  `json:"grade"`     // A to F, from how much the delay grows under load
	Server    string  `json:"server"`    // where the test ran (the data centre's name when known)
	BytesUsed int64   `json:"bytes_used"`
	Seconds   float64 `json:"seconds"`
}

type plan struct {
	conns   int
	down    time.Duration
	up      time.Duration
	skip    time.Duration // ramp-up that is not counted
	probes  int
	maxBody int64
}

func planFor(quick bool) plan {
	if quick {
		return plan{conns: 1, down: 3 * time.Second, up: 3 * time.Second, skip: 500 * time.Millisecond, probes: 5, maxBody: 4 << 20}
	}
	return plan{conns: 4, down: 8 * time.Second, up: 8 * time.Second, skip: 1500 * time.Millisecond, probes: 12, maxBody: 50 << 20}
}

// ValidDevice accepts only something that looks like a network device name.
func ValidDevice(dev string) bool {
	if dev == "" || len(dev) > 15 {
		return false
	}
	for _, r := range dev {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.:-", r)) {
			return false
		}
	}
	return true
}

func client(dev string, conns int) *http.Client {
	d := &net.Dialer{Timeout: 8 * time.Second}
	if dev != "" {
		d.Control = bindToDevice(dev)
	}
	tr := &http.Transport{
		DialContext:         d.DialContext,
		Proxy:               nil, // never through a proxy: the test must use the line itself
		MaxIdleConnsPerHost: conns + 2,
		TLSHandshakeTimeout: 8 * time.Second,
		DisableCompression:  true,
	}
	return &http.Client{Transport: tr}
}

type counter struct{ n atomic.Int64 }

type countReader struct {
	c    *counter
	ctx  context.Context
	left int64
}

func (r *countReader) Read(p []byte) (int, error) {
	if r.ctx.Err() != nil || r.left <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.left {
		n = r.left
	}
	for i := int64(0); i < n; i++ {
		p[i] = 0
	}
	r.left -= n
	r.c.n.Add(n)
	return int(n), nil
}

// probe measures the time to the first answer byte of a tiny request on a warm connection.
func probe(ctx context.Context, c *http.Client, server string) (time.Duration, string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", server+"/__down?bytes=0", nil)
	t0 := time.Now()
	var first time.Duration
	resp, err := c.Do(req)
	if err != nil {
		return 0, "", err
	}
	first = time.Since(t0)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, "", fmt.Errorf("the test server answered %d", resp.StatusCode)
	}
	return first, resp.Header.Get("cf-meta-colo"), nil
}

func median(d []float64) float64 {
	if len(d) == 0 {
		return 0
	}
	s := append([]float64{}, d...)
	sort.Float64s(s)
	return s[len(s)/2]
}

func jitter(d []float64) float64 {
	if len(d) < 2 {
		return 0
	}
	var sum float64
	for i := 1; i < len(d); i++ {
		x := d[i] - d[i-1]
		if x < 0 {
			x = -x
		}
		sum += x
	}
	return sum / float64(len(d)-1)
}

// Grade rates how much the delay grows when the line is busy, the thing that makes calls and games
// stutter while someone downloads: A is under 30 ms extra, F is over 400.
func Grade(idle, loaded float64) string {
	extra := loaded - idle
	if loaded == 0 {
		return ""
	}
	switch {
	case extra < 30:
		return "A"
	case extra < 60:
		return "B"
	case extra < 200:
		return "C"
	case extra < 400:
		return "D"
	}
	return "F"
}

// rate runs the workers for the time given and returns the speed after the ramp-up is skipped.
func rate(ctx context.Context, dur, skip time.Duration, c *counter, start func(ctx context.Context)) (float64, int64, error) {
	rctx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()
	done := make(chan struct{})
	go func() { start(rctx); close(done) }()
	t0 := time.Now()
	var at int64
	var atT time.Time
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
loop:
	for {
		select {
		case <-rctx.Done():
			break loop
		case now := <-tick.C:
			if atT.IsZero() && now.Sub(t0) >= skip {
				at, atT = c.n.Load(), now
			}
		}
	}
	end := time.Now()
	total := c.n.Load()
	<-done
	if atT.IsZero() || end.Sub(atT) < 500*time.Millisecond {
		return 0, total, errors.New("the test server did not send data in time")
	}
	return float64(total-at) * 8 / end.Sub(atT).Seconds(), total, nil
}

// Run measures the connection. It stops early when ctx ends.
func Run(ctx context.Context, p Params) (Result, error) {
	server := strings.TrimRight(strings.TrimSpace(p.Server), "/")
	if server == "" {
		server = DefaultServer
	}
	if !strings.HasPrefix(server, "https://") && !strings.HasPrefix(server, "http://") {
		return Result{}, errors.New("the test server address must start with https://")
	}
	if p.Dev != "" && !ValidDevice(p.Dev) {
		return Result{}, errors.New("that is not a network device")
	}
	pl := planFor(p.Quick)
	c := client(p.Dev, pl.conns+1)
	defer c.CloseIdleConnections()
	start := time.Now()
	res := Result{Server: strings.TrimPrefix(strings.TrimPrefix(server, "https://"), "http://")}

	// Delay on an idle line. The first request pays for the connection and is not counted.
	var idle []float64
	for i := 0; i <= pl.probes; i++ {
		d, colo, err := probe(ctx, c, server)
		if err != nil {
			return res, friendly(err)
		}
		if colo != "" {
			res.Server = "Cloudflare " + colo
		}
		if i > 0 {
			idle = append(idle, float64(d)/float64(time.Millisecond))
		}
	}
	res.DelayMS, res.JitterMS = median(idle), jitter(idle)

	// Download, with delay probes running alongside.
	var dc counter
	var loaded []float64
	var lmu sync.Mutex
	pc := client(p.Dev, 2)
	defer pc.CloseIdleConnections()
	down, dtotal, err := rate(ctx, pl.down, pl.skip, &dc, func(rc context.Context) {
		var wg sync.WaitGroup
		for i := 0; i < pl.conns; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for rc.Err() == nil {
					req, _ := http.NewRequestWithContext(rc, "GET", fmt.Sprintf("%s/__down?bytes=%d", server, pl.maxBody), nil)
					resp, err := c.Do(req)
					if err != nil {
						return
					}
					buf := make([]byte, 64<<10)
					for {
						n, err := resp.Body.Read(buf)
						dc.n.Add(int64(n))
						if err != nil {
							break
						}
					}
					resp.Body.Close()
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(pl.skip)
			for rc.Err() == nil {
				if d, _, err := probe(rc, pc, server); err == nil {
					lmu.Lock()
					loaded = append(loaded, float64(d)/float64(time.Millisecond))
					lmu.Unlock()
				}
				select {
				case <-rc.Done():
				case <-time.After(250 * time.Millisecond):
				}
			}
		}()
		wg.Wait()
	})
	if err != nil {
		return res, friendly(err)
	}
	res.DownBps, res.LoadedMS = down, median(loaded)
	res.Grade = Grade(res.DelayMS, res.LoadedMS)

	// Upload.
	var uc counter
	up, utotal, err := rate(ctx, pl.up, pl.skip, &uc, func(rc context.Context) {
		var wg sync.WaitGroup
		for i := 0; i < pl.conns; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for rc.Err() == nil {
					req, _ := http.NewRequestWithContext(rc, "POST", server+"/__up", &countReader{c: &uc, ctx: rc, left: pl.maxBody / 2})
					req.ContentLength = pl.maxBody / 2
					req.Header.Set("Content-Type", "application/octet-stream")
					resp, err := c.Do(req)
					if err != nil {
						return
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}()
		}
		wg.Wait()
	})
	if err != nil {
		return res, friendly(err)
	}
	res.UpBps = up
	res.BytesUsed = dtotal + utotal
	res.Seconds = time.Since(start).Seconds()
	return res, nil
}

// friendly turns a network error into a sentence a person can act on.
func friendly(err error) error {
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New("the test was stopped")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("the test took too long and was stopped")
	case errors.As(err, &ne) && ne.Timeout():
		return errors.New("the test server did not answer in time. Check that this connection has internet")
	}
	msg := err.Error()
	if strings.Contains(msg, "no such host") {
		return errors.New("the test server's name could not be looked up through this connection")
	}
	if strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "no route") {
		return errors.New("this connection has no route to the internet right now")
	}
	return fmt.Errorf("the test could not run: %s", msg)
}
