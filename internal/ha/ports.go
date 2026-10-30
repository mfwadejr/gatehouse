package ha

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Port is a wired network port on one of the two machines.
type Port struct {
	Name     string `json:"name"`
	Speed    int    `json:"speed"` // megabits per second; -1 when no cable is plugged in
	Link     bool   `json:"link"`
	Wireless bool   `json:"wifi"`
}

// PortRole is a job one of the primary's ports does: carrying the internet, or a network.
type PortRole struct {
	ID    string `json:"id"`    // for example "internet:Fibre" or "network:Home"
	Label string `json:"label"` // plain words for the screen
	Port  string `json:"port"`  // the primary's port
	Speed int    `json:"speed"`
}

// Match says which of the backup's ports takes over a role.
type Match struct {
	Role    string `json:"role"`
	Label   string `json:"label"`
	Primary string `json:"primary"`
	Backup  string `json:"backup"`
	Warning string `json:"warning,omitempty"`
}

// MapPorts pairs every port role on the primary with a port on the backup. keep holds choices the
// person made by hand (role id to backup port); they are used when valid. It refuses a backup that
// has fewer wired ports than the primary uses, and warns, with the plain consequence, about a
// backup port that is slower than the one it replaces or has no cable plugged in to measure.
func MapPorts(roles []PortRole, backup []Port, keep map[string]string) ([]Match, error) {
	var wired []Port
	for _, p := range backup {
		if !p.Wireless {
			wired = append(wired, p)
		}
	}
	if len(roles) == 0 {
		return nil, errors.New("the primary has no ports in use to match")
	}
	if len(wired) < len(roles) {
		return nil, fmt.Errorf("this machine has %d wired port%s but the primary uses %d. The backup needs at least as many, otherwise a failover would leave something unconnected. Add a network card (a USB network adapter works) and try again", len(wired), plural(len(wired)), len(roles))
	}
	rs := append([]PortRole(nil), roles...)
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Speed != rs[j].Speed {
			return rs[i].Speed > rs[j].Speed
		}
		return rs[i].ID < rs[j].ID
	})
	ws := append([]Port(nil), wired...)
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].Speed != ws[j].Speed {
			return ws[i].Speed > ws[j].Speed
		}
		return ws[i].Name < ws[j].Name
	})
	byName := map[string]Port{}
	for _, p := range wired {
		byName[p.Name] = p
	}
	used := map[string]bool{}
	chosen := map[string]string{}
	for _, r := range rs { // hand choices first
		if b, ok := keep[r.ID]; ok {
			if _, real := byName[b]; !real {
				return nil, fmt.Errorf("%s is not a wired port on this machine", b)
			}
			if used[b] {
				return nil, fmt.Errorf("%s is chosen twice", b)
			}
			used[b], chosen[r.ID] = true, b
		}
	}
	for _, r := range rs {
		if chosen[r.ID] != "" {
			continue
		}
		for _, w := range ws {
			if !used[w.Name] {
				used[w.Name], chosen[r.ID] = true, w.Name
				break
			}
		}
	}
	var out []Match
	for _, r := range roles {
		b := byName[chosen[r.ID]]
		m := Match{Role: r.ID, Label: r.Label, Primary: r.Port, Backup: b.Name}
		switch {
		case !b.Link || b.Speed <= 0:
			m.Warning = "No cable is plugged into " + b.Name + " yet, so its speed is unknown. Plug it into the same switch as " + r.Port + " on the primary."
		case r.Speed > 0 && b.Speed < r.Speed:
			m.Warning = fmt.Sprintf("%s runs at %s but %s on the primary runs at %s. After a failover %s would be limited to the slower speed.", b.Name, speed(b.Speed), r.Port, speed(r.Speed), strings.ToLower(r.Label))
		case b.Speed < 1000:
			m.Warning = fmt.Sprintf("%s runs at %s, below 1 Gbps. Fast connections would be held back after a failover.", b.Name, speed(b.Speed))
		}
		out = append(out, m)
	}
	return out, nil
}

// Rename turns the primary's port names into this machine's, for every name in the map; names not
// in the map are left alone.
func Rename(name string, ms []Match) string {
	for _, m := range ms {
		if m.Primary == name {
			return m.Backup
		}
	}
	return name
}

func speed(mbps int) string {
	if mbps >= 1000 && mbps%1000 == 0 {
		return fmt.Sprintf("%d Gbps", mbps/1000)
	}
	if mbps >= 1000 {
		return fmt.Sprintf("%.1f Gbps", float64(mbps)/1000)
	}
	return fmt.Sprintf("%d Mbps", mbps)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
