package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"nopewarden/internal/api"
)

// Port names. A person can give each network port a friendly name ("T-Mobile gateway", "Spare 1").
// Only the label is stored; the real port, DHCP, DNS and the firewall never see it.

var (
	errPortUnknown   = errors.New("that port was not found on this device")
	errPortLabelLong = errors.New("keep the name to 30 characters or fewer")
)

const portLabelKey = "net.port_labels"
const maxPortLabel = 30

func (a *App) portLabels() map[string]string {
	m := map[string]string{}
	json.Unmarshal([]byte(a.Config.Setting(portLabelKey, "{}")), &m)
	return m
}

// labelledPorts is the port list with each port's friendly name, if it has one.
func (a *App) labelledPorts() []map[string]any {
	ps := portsLister()
	labels := a.portLabels()
	for _, p := range ps {
		if n, _ := p["name"].(string); labels[n] != "" {
			p["label"] = labels[n]
		}
	}
	return ps
}

// One name for a port and the internet connection on it. A person who names the connection T-Mobile
// and a person who names the port T-Mobile mean the same thing, so the latest rename wins from
// either place. This only applies when exactly one connection uses the port; a port that carries
// several (one per VLAN) keeps its own name, and so do the connections on it.

// wanOnPort returns the one internet connection that uses a port.
func (a *App) wanOnPort(port string) (id int64, name string, one bool) {
	rows, err := a.Config.Query(`SELECT id,name FROM wan_links WHERE iface=?`, port)
	if err != nil {
		return 0, "", false
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		rows.Scan(&id, &name)
		n++
	}
	return id, name, n == 1
}

func shortLabel(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > maxPortLabel {
		r = r[:maxPortLabel]
	}
	return string(r)
}

// storeLabel keeps or removes the label for one port.
func (a *App) storeLabel(port, label string) error {
	m := a.portLabels()
	if label == "" || label == port {
		delete(m, port)
	} else {
		m[port] = label
	}
	b, _ := json.Marshal(m)
	return a.Config.SetSetting(portLabelKey, string(b))
}

// nameConnectionFromPort gives the connection on a port the name the port was just given.
func (a *App) nameConnectionFromPort(port, label string) (renamed bool, err error) {
	id, name, one := a.wanOnPort(port)
	if !one || label == "" || label == port || name == label {
		return false, nil
	}
	if _, err := a.Config.Exec(`UPDATE wan_links SET name=? WHERE id=?`, label, id); err != nil {
		return false, friendlyDup(err, "another internet connection already has that name")
	}
	return true, nil
}

// namePortFromConnection gives the port a connection uses the connection's name.
func (a *App) namePortFromConnection(port, name string) {
	if _, _, one := a.wanOnPort(port); !one || name == "" || name == port {
		return
	}
	a.storeLabel(port, shortLabel(name))
}

// syncPortNames runs once after an update that brings this rule: a port that has no name takes its
// connection's name, and when the two were named differently the connection's name is used.
func (a *App) syncPortNames() {
	if a.Config.Setting("net.names_synced", "") == "1" {
		return
	}
	rows, err := a.Config.Query(`SELECT name,iface FROM wan_links ORDER BY priority,id`)
	if err != nil {
		return
	}
	type pair struct{ name, iface string }
	var ps []pair
	for rows.Next() {
		var p pair
		rows.Scan(&p.name, &p.iface)
		ps = append(ps, p)
	}
	rows.Close()
	labels := a.portLabels()
	var did []string
	for _, p := range ps {
		if _, _, one := a.wanOnPort(p.iface); !one || p.name == p.iface {
			continue
		}
		if want := shortLabel(p.name); labels[p.iface] != want {
			a.storeLabel(p.iface, want)
			did = append(did, p.iface+" is now called "+want)
		}
	}
	a.Config.SetSetting("net.names_synced", "1")
	if len(did) > 0 {
		a.event("NopeWarden", "info", "Port names now follow connection names", strings.Join(did, "; ")+". Rename either one and the other follows.")
	}
}

func (a *App) registerPortNameRoutes() {
	a.syncPortNames()
	a.API.HandleAdmin("PUT /api/net/ports/label", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Name  string `json:"name"`
			Label string `json:"label"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		found := false
		for _, p := range portsLister() {
			if n, _ := p["name"].(string); n == in.Name {
				found = true
			}
		}
		if !found {
			fail(w, errPortUnknown)
			return
		}
		label := strings.Join(strings.Fields(in.Label), " ")
		if len([]rune(label)) > maxPortLabel {
			fail(w, errPortLabelLong)
			return
		}
		renamed, err := a.nameConnectionFromPort(in.Name, label)
		if err != nil {
			fail(w, err)
			return
		}
		if err := a.storeLabel(in.Name, label); err != nil {
			fail(w, err)
			return
		}
		if renamed {
			a.publishNet()
			a.publishFW()
		}
		ok(w)
	})
}
