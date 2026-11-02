package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/netcfg"
)

// Throughput of each internet connection, sampled from the kernel's interface counters.

const (
	trafficEvery   = time.Second
	trafficSamples = 900 // fifteen minutes
)

type trafficSeries struct {
	Dev    string    `json:"dev"`
	Rx     []float64 `json:"rx"` // bits per second, oldest first
	Tx     []float64 `json:"tx"`
	lastRx uint64
	lastTx uint64
	lastAt time.Time
}

type wanTraffic struct {
	mu     sync.Mutex
	series map[string]*trafficSeries
	seq    uint64 // samples taken so far; lets a caller ask for only what is new
}

func readCounter(dev, name string) (uint64, bool) {
	b, err := os.ReadFile(filepath.Join("/sys/class/net", dev, "statistics", name))
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return n, err == nil
}

func push(s []float64, v float64) []float64 {
	if len(s) >= trafficSamples {
		s = s[1:]
	}
	return append(s, v)
}

// sample records one reading for every connection. A counter that went backwards (the device
// was recreated) records a gap of zero instead of a spike.
func (t *wanTraffic) sample(devs map[string]string, now time.Time, read func(dev, name string) (uint64, bool)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	if t.series == nil {
		t.series = map[string]*trafficSeries{}
	}
	for name := range t.series {
		if _, ok := devs[name]; !ok {
			delete(t.series, name)
		}
	}
	for name, dev := range devs {
		s := t.series[name]
		if s == nil || s.Dev != dev {
			s = &trafficSeries{Dev: dev}
			t.series[name] = s
		}
		rx, ok1 := read(dev, "rx_bytes")
		tx, ok2 := read(dev, "tx_bytes")
		if !ok1 || !ok2 {
			s.Rx, s.Tx, s.lastAt = push(s.Rx, 0), push(s.Tx, 0), time.Time{}
			continue
		}
		var r, x float64
		if !s.lastAt.IsZero() && rx >= s.lastRx && tx >= s.lastTx {
			if dt := now.Sub(s.lastAt).Seconds(); dt > 0 {
				r, x = float64(rx-s.lastRx)*8/dt, float64(tx-s.lastTx)*8/dt
			}
		}
		s.Rx, s.Tx = push(s.Rx, r), push(s.Tx, x)
		s.lastRx, s.lastTx, s.lastAt = rx, tx, now
	}
}

func (t *wanTraffic) snapshot() map[string]trafficSeries { l, _ := t.tail(0); return l }

// tail returns the newest n samples of each connection (all of them when n is 0) and the running
// sample count, so a console that already holds the older ones asks for only a few.
func (t *wanTraffic) tail(n int) (map[string]trafficSeries, uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cut := func(v []float64) []float64 {
		if n > 0 && len(v) > n {
			v = v[len(v)-n:]
		}
		return append([]float64{}, v...)
	}
	out := map[string]trafficSeries{}
	for name, s := range t.series {
		out[name] = trafficSeries{Dev: s.Dev, Rx: cut(s.Rx), Tx: cut(s.Tx)}
	}
	return out, t.seq
}

func (a *App) wanDevices() map[string]string {
	wans, err := a.listWans(a.Config)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, w := range wans {
		if !w.Enabled {
			continue
		}
		out[w.Name] = netcfg.WanDevice(netcfg.WAN{Name: w.Name, Iface: w.Iface, Type: w.Type, VLAN: w.VLAN})
	}
	return out
}

func (a *App) collectTraffic(ctx context.Context) {
	t := time.NewTicker(trafficEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			a.traffic.sample(a.wanDevices(), now, readCounter)
			a.sampleNetworks(now)
		}
	}
}

// sampleNetworks records the speed of each network (all its ports added together), once a second.
func (a *App) sampleNetworks(now time.Time) {
	ports := map[string][]string{}
	for iface, zone := range a.zoneByIface() {
		ports[zone] = append(ports[zone], iface)
	}
	devs := map[string]string{}
	for zone := range ports {
		devs[zone] = zone
	}
	a.netTraffic.sample(devs, now, func(zone, name string) (uint64, bool) {
		var sum uint64
		ok := false
		for _, p := range ports[zone] {
			if n, good := readCounter(p, name); good {
				sum, ok = sum+n, true
			}
		}
		return sum, ok
	})
}

// latest is the newest reading of each series: bits per second received and sent on the ports.
func (t *wanTraffic) latest() map[string][2]float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string][2]float64{}
	for name, s := range t.series {
		if n := len(s.Rx); n > 0 {
			out[name] = [2]float64{s.Rx[n-1], s.Tx[n-1]}
		}
	}
	return out
}

func (a *App) registerTrafficRoutes() {
	a.API.Handle("GET /api/net/zone-speed", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		// A port's received bytes are what the devices on it sent, so the names swap here.
		out := map[string]map[string]float64{}
		for zone, v := range a.netTraffic.latest() {
			out[zone] = map[string]float64{"down": v[1], "up": v[0]}
		}
		api.JSON(w, 200, map[string]any{"zones": out})
	})
	a.registerViaRoutes()
	a.API.Handle("GET /api/net/throughput", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		n, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		links, seq := a.traffic.tail(n)
		api.JSON(w, 200, map[string]any{"interval": int(trafficEvery / time.Second), "seq": seq, "links": links})
	})
}
