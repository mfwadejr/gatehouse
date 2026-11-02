package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"gatehouse/internal/api"
	"gatehouse/internal/bridgemode"
)

// Device mode: the one place that says whether this box is a Router or a Bridge, with its ports in
// plain words. The two real setups live where they always did (internet connection and networks for
// a router, the bridge setup for a bridge); this file only reads them together, and guards the
// changes the walkthrough makes so a mode change cannot cut the person off or break a pair.

var (
	errModePaired = errors.New("this box is in a high-availability pair, so its mode cannot be changed here. Take it out of the pair first (Settings, Advanced, High availability); paired boxes are cabled for failover and a change would break that")
	errModeNoWAN  = errors.New("no internet connection is saved yet. Add one on the Internet page first")
)

// modeGate refuses mode and port changes while the box is part of a high-availability pair.
func (a *App) modeGate() error {
	if a.haLoad() != nil {
		return errModePaired
	}
	return nil
}

func (a *App) portName(n string) string {
	if l := a.portLabels()[n]; l != "" {
		return l + " (" + n + ")"
	}
	return n
}

func (a *App) portNames(ns []string) string {
	var out []string
	for _, n := range ns {
		out = append(out, a.portName(n))
	}
	return strings.Join(out, ", ")
}

// modeHeadline describes the current mode and its ports in one sentence.
func (a *App) modeHeadline() (mode, headline string) {
	if b := a.bridgeLive(); b != nil && b.Enabled {
		h := "Bridge. " + a.portName(b.Outside) + " goes to your other router; " + a.portNames(b.Inside) + " go to your devices"
		if b.OOB != "" {
			h += "; " + a.portName(b.OOB) + " is for managing this box"
		}
		return "bridge", h + "."
	}
	var wan string
	if ws, err := a.listWans(a.Config); err == nil {
		for _, w := range ws {
			if w.Enabled {
				wan = "Router. The internet is on " + a.portName(w.Iface) + " (" + w.Name + ")"
				break
			}
		}
	}
	if wan == "" {
		wan = "Router. No internet connection is set up yet"
	}
	var lans []string
	if ifs, err := a.listIfaces(a.Config); err == nil {
		for _, i := range ifs {
			if i.Enabled {
				lans = append(lans, i.Name)
			}
		}
	}
	if len(lans) > 0 {
		wan += "; your devices are on " + a.portNames(lans)
	}
	return "router", wan + "."
}

// checkInternetPort says in words whether iface can become the internet port.
func (a *App) checkInternetPort(iface, clientIP string) error {
	if err := a.modeGate(); err != nil {
		return err
	}
	if b := a.bridgeLive(); b != nil && b.Enabled {
		return errors.New("this box is in bridge mode. Switch to router mode first")
	}
	var info *bridgemode.PortInfo
	for _, p := range a.bridgePorts() {
		if p.Name == iface {
			p := p
			info = &p
		}
	}
	switch {
	case info == nil:
		return errors.New(iface + " is not a port on this machine")
	}
	for _, cp := range a.currentPorts(clientIP) {
		if cp == iface {
			return errors.New("you are connected through " + a.portName(iface) + " right now, so it cannot become the internet port. Leave it plugged in and choose another")
		}
	}
	if info.UsedFor == "network" {
		return errors.New(a.portName(iface) + " carries one of your networks. Move that network to another port first (Networks page)")
	}
	return nil
}

func (a *App) registerModeRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/mode", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		mode, head := a.modeHeadline()
		st := a.bridgeStatus()
		out := map[string]any{"mode": mode, "headline": head, "paired": a.haLoad() != nil,
			"ports": a.labelledPorts(), "current": a.currentPorts(c.IP), "live": st["live"], "pending": st["pending"], "undone": st["undone"],
			"bridge": a.bridgeLive(), "draft": a.bridgeDraft()}
		var wan any
		if ws, err := a.listWans(a.Config); err == nil {
			for _, x := range ws {
				if x.Enabled {
					wan = map[string]any{"name": x.Name, "iface": x.Iface}
					break
				}
			}
		}
		out["wan"] = wan
		lans := []string{}
		if ifs, err := a.listIfaces(a.Config); err == nil {
			for _, i := range ifs {
				if i.Enabled {
					lans = append(lans, i.Name)
				}
			}
		}
		out["lans"] = lans
		api.JSON(w, 200, out)
	})
	// Move the internet connection to another port. It is only saved; the person applies it with
	// the usual 60-second confirm-or-undo.
	s.HandleAdmin("PUT /api/mode/internet-port", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Iface string `json:"iface"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if err := a.checkInternetPort(in.Iface, c.IP); err != nil {
			fail(w, err)
			return
		}
		ws, err := a.listWans(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		var id int64
		for _, x := range ws {
			if x.Enabled {
				if x.Iface == in.Iface {
					if id == 0 {
						api.JSON(w, 200, map[string]any{"changed": false})
						return
					}
					fail(w, errors.New(a.portName(in.Iface)+" is already the internet port of \""+x.Name+"\""))
					return
				}
				if id == 0 {
					id = x.ID
				}
			} else if x.Iface == in.Iface {
				fail(w, errors.New(a.portName(in.Iface)+" is already used by the switched-off connection \""+x.Name+"\". Remove or move it on the Internet page first"))
				return
			}
		}
		if id == 0 {
			fail(w, errModeNoWAN)
			return
		}
		if err := a.mutateNet(func(tx *sql.Tx) error {
			_, e := tx.Exec(`UPDATE wan_links SET iface=? WHERE id=?`, in.Iface, id)
			return e
		}); err != nil {
			fail(w, err)
			return
		}
		a.publishNet()
		a.publishFW()
		api.JSON(w, 200, map[string]any{"changed": true})
	})
}
