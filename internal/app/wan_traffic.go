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
	trafficEvery   = 2 * time.Second
	trafficSamples = 300 // ten minutes
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

func (t *wanTraffic) snapshot() map[string]trafficSeries {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string]trafficSeries{}
	for n, s := range t.series {
		out[n] = trafficSeries{Dev: s.Dev, Rx: append([]float64{}, s.Rx...), Tx: append([]float64{}, s.Tx...)}
	}
	return out
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
		}
	}
}

func (a *App) registerTrafficRoutes() {
	a.API.Handle("GET /api/net/throughput", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"interval": int(trafficEvery / time.Second), "links": a.traffic.snapshot()})
	})
}
