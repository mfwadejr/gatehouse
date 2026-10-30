package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/netcfg"
)

// Silent warm standby (see package ha): the option is one setting that is copied to both units.

const haWarmSetting = "standby.warm"

func (a *App) haWarmOn() bool { return a.Config.Setting(haWarmSetting, "0") == "1" }

type haWanSplit struct {
	Down   []string          // ports taken down while standby, as before
	Warm   []string          // ports kept connected but silent
	Static []string          // "port address" for fixed addresses to announce
	ByName map[string]string // connection name -> port, for the warm ones
}

// haWanSplit sorts the internet connections: plain ones (automatic or fixed address) can be warm;
// a login (PPPoE) and a wireless connection keep the earlier behaviour.
func (a *App) haWanSplit(warm bool) haWanSplit {
	sp := haWanSplit{ByName: map[string]string{}}
	wans, err := a.listWans(a.Config)
	if err != nil {
		return sp
	}
	for _, w := range wans {
		if !w.Enabled {
			continue
		}
		dev := netcfg.WanDevice(netcfg.WAN{Name: w.Name, Iface: w.Iface, Type: w.Type, VLAN: w.VLAN})
		_, wireless := statErr(filepath.Join("/sys/class/net", w.Iface, "wireless"))
		if warm && (w.Type == "dhcp" || w.Type == "static") && !wireless {
			sp.Warm = append(sp.Warm, dev)
			sp.ByName[w.Name] = dev
			if w.Type == "static" {
				if ip, _, ok := strings.Cut(w.Addr, "/"); ok && ip != "" {
					sp.Static = append(sp.Static, dev+" "+ip)
				}
			}
			continue
		}
		sp.Down = append(sp.Down, dev)
	}
	return sp
}

func statErr(p string) (os.FileInfo, bool) {
	st, err := os.Stat(p)
	return st, err == nil
}

// haWanMACs reads the hardware address on each warm connection's port (what the provider sees).
// Only the active unit reports, because the standby's own addresses are not what is on the wire.
var haReadMAC = func(dev string) string {
	b, _ := os.ReadFile(filepath.Join("/sys/class/net", dev, "address"))
	return strings.TrimSpace(string(b))
}

func (a *App) haOwnWanMACs() map[string]string {
	st := a.haLoad()
	if st == nil || !a.haWarmOn() || !a.haActive(st) {
		return nil
	}
	out := map[string]string{}
	for name, dev := range a.haWanSplit(true).ByName {
		if m := haReadMAC(dev); m != "" {
			out[name] = m
		}
	}
	return out
}

// haPartnerMACs are "port address" lines for the ports this unit will take over, copied from the
// partner's report. Empty when the partner cannot be asked.
func (a *App) haPartnerMACs(st *haLocal) []string {
	if !a.haWarmOn() || st.PeerFP == "" {
		return nil
	}
	ps, err := a.peerStatus(st, 3*time.Second)
	if err != nil {
		return nil
	}
	var out []string
	mine := a.haWanSplit(true).ByName
	for name, mac := range ps.WanMACs {
		if dev, ok := mine[name]; ok {
			out = append(out, dev+" "+mac)
		}
	}
	sort.Strings(out)
	return out
}

// haWarmRefresh re-applies the standby's services when the active unit's hardware addresses change.
func (a *App) haWarmRefresh(st *haLocal) {
	if !a.haWarmOn() || !st.Activated || a.haActive(st) {
		return
	}
	want := strings.Join(a.haPartnerMACs(st), ",")
	if want == "" || want == a.warmMACs {
		return
	}
	if err := a.haInstall(st); err == nil {
		a.warmMACs = want
	}
}

func (a *App) haWarmLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if st := a.haLoad(); st != nil && st.Activated {
			a.haWarmRefresh(st)
		}
	}
}

func (a *App) registerHAWarmRoutes() {
	a.API.HandleAdmin("PUT /api/ha/warm", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			On bool `json:"on"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		st := a.haLoad()
		if st == nil {
			api.Error(w, 400, "this unit is not paired")
			return
		}
		if st.Activated && !a.haActive(st) {
			api.Error(w, 409, "change this on the active unit; it is copied to the other one")
			return
		}
		prev := a.setting(haWarmSetting, "0")
		v := "0"
		if in.On {
			v = "1"
		}
		a.Config.SetSetting(haWarmSetting, v)
		if st.Activated {
			if err := a.haInstall(st); err != nil {
				a.Config.SetSetting(haWarmSetting, prev)
				api.Error(w, 500, err.Error())
				return
			}
		}
		haKick() // the other unit takes the setting with the next copy
		a.event(c.User.Username, "info", map[bool]string{true: "The spare unit's internet connection is now kept ready", false: "The spare unit's internet connection is no longer kept ready"}[in.On], "")
		ok(w)
	})
}
