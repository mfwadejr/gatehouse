package app

import (
	"context"
	"time"
)

// upstreamProbeEvery is how often every upstream DNS server gets a quick test query, so speed and
// health stay current even when a server is not being used for real lookups.
const upstreamProbeEvery = time.Minute

// internetLooksDown is true only when links are being checked and none of them is up; then a
// failed test would say nothing about the server.
func (a *App) internetLooksDown() bool {
	if a.Net == nil {
		return false
	}
	st, err := a.Net.WanStatus()
	if err != nil {
		return false
	}
	checked := false
	for name, s := range st {
		if len(name) > 6 && name[:6] == "group:" || !s.Checked {
			continue
		}
		checked = true
		if s.Up {
			return false
		}
	}
	return checked
}

// upstreamProbeLoop tests every upstream shortly after start, after any change to the list, and
// once a minute.
func (a *App) upstreamProbeLoop(ctx context.Context) {
	t := time.NewTicker(upstreamProbeEvery)
	defer t.Stop()
	first := time.NewTimer(10 * time.Second)
	defer first.Stop()
	probe := func() {
		if a.DNS == nil || a.internetLooksDown() {
			return
		}
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		a.DNS.Pool().Probe(pctx)
		a.Bus.Publish("dns.upstreams", a.DNS.Pool().Status())
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			probe()
		case <-a.probeKick:
			time.Sleep(2 * time.Second)
			probe()
		case <-t.C:
			probe()
		}
	}
}
