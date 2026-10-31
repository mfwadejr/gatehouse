package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"gatehouse/internal/api"
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
	ps := ports()
	labels := a.portLabels()
	for _, p := range ps {
		if n, _ := p["name"].(string); labels[n] != "" {
			p["label"] = labels[n]
		}
	}
	return ps
}

func (a *App) registerPortNameRoutes() {
	a.API.HandleAdmin("PUT /api/net/ports/label", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Name  string `json:"name"`
			Label string `json:"label"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		found := false
		for _, p := range ports() {
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
		m := a.portLabels()
		if label == "" || label == in.Name {
			delete(m, in.Name)
		} else {
			m[in.Name] = label
		}
		b, _ := json.Marshal(m)
		if err := a.Config.SetSetting(portLabelKey, string(b)); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
}
