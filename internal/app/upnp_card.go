package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// One UPnP card. The three old switches (on each internet connection, on each network, and the
// "only the devices I list" option) are one choice made in one place. What the person picks is
// remembered even while UPnP is off, so turning it off and on again asks nothing twice.

type upnpPick struct {
	Mode        string   `json:"mode"` // devices or network
	Networks    []int64  `json:"networks"`
	Connections []string `json:"connections"`
}

type cardNet struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Ticked bool   `json:"ticked"`
}
type cardConn struct {
	Name   string `json:"name"`
	Ticked bool   `json:"ticked"`
}

func (a *App) loadUpnpPick(zones []fw.ZoneRow, conns map[string]bool) upnpPick {
	var p upnpPick
	if s := a.setting("upnp.pick", ""); s != "" {
		json.Unmarshal([]byte(s), &p)
	} else {
		// Settings from before this card carry over as ticks.
		for _, z := range zones {
			if z.UPnP {
				p.Networks = append(p.Networks, z.ID)
			}
		}
		for n, on := range conns {
			if on {
				p.Connections = append(p.Connections, n)
			}
		}
		sort.Strings(p.Connections)
	}
	if p.Mode == "" {
		p.Mode = "network"
		if a.setting("upnp.restrict", "0") == "1" {
			p.Mode = "devices"
		}
	}
	return p
}

func (a *App) upnpWans() ([]string, map[string]bool) {
	names := []string{}
	on := map[string]bool{}
	if rows, err := a.Config.Query(`SELECT name,upnp FROM wan_links WHERE enabled=1 ORDER BY priority,id`); err == nil {
		for rows.Next() {
			var n string
			var u int
			rows.Scan(&n, &u)
			names = append(names, n)
			on[n] = u == 1
		}
		rows.Close()
	}
	return names, on
}

// upnpMissing says in words what is still needed before UPnP can be switched on.
func upnpMissing(mode string, networks int, conns int, devices int) []string {
	var miss []string
	if mode == "devices" && devices == 0 {
		miss = append(miss, "Add at least one device that may open ports.")
	}
	if mode == "network" && networks == 0 {
		miss = append(miss, "Tick at least one network.")
	}
	if conns == 0 {
		miss = append(miss, "Tick at least one internet connection.")
	}
	return miss
}

func (a *App) upnpCard() map[string]any {
	st := a.upnpStatus()
	zones, _ := fw.Zones(a.Config)
	wanNames, wanOn := a.upnpWans()
	pick := a.loadUpnpPick(zones, wanOn)
	tickN := map[int64]bool{}
	for _, id := range pick.Networks {
		tickN[id] = true
	}
	tickC := map[string]bool{}
	for _, n := range pick.Connections {
		tickC[n] = true
	}
	nets := []cardNet{}
	on := false
	for _, z := range zones {
		if z.Kind == fw.KindWan {
			continue
		}
		nets = append(nets, cardNet{z.ID, z.Name, tickN[z.ID]})
		if z.UPnP {
			on = true
		}
	}
	conns := []cardConn{}
	anyWan := false
	for _, n := range wanNames {
		conns = append(conns, cardConn{n, tickC[n]})
		if wanOn[n] {
			anyWan = true
		}
	}
	isOn := on && anyWan
	nick := 0
	for _, n := range nets {
		if n.Ticked {
			nick++
		}
	}
	tickedConns := 0
	for _, c := range conns {
		if c.Ticked {
			tickedConns++
		}
	}
	miss := upnpMissing(pick.Mode, nick, tickedConns, len(st.Allowed))
	line := ""
	if isOn {
		who := "every device on " + strconv.Itoa(nick) + " network" + plural(nick)
		if pick.Mode == "devices" {
			who = strconv.Itoa(len(st.Allowed)) + " device" + plural(len(st.Allowed))
		}
		line = "On for " + who + " through " + strings.Join(pick.Connections, ", ") + ". " + strconv.Itoa(len(st.Mappings)) + " port" + plural(len(st.Mappings)) + " open now."
	}
	return map[string]any{
		"on": isOn, "mode": pick.Mode, "networks": nets, "connections": conns, "devices": st.Allowed,
		"missing": miss, "line": line, "status": st,
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

type upnpCardIn struct {
	On          bool           `json:"on"`
	Mode        string         `json:"mode"`
	Networks    []int64        `json:"networks"`
	Connections []string       `json:"connections"`
	Devices     []upnpAllowRow `json:"devices"`
	LeaseHours  int            `json:"max_lease_hours"`
}

func (a *App) saveUpnpCard(in upnpCardIn) error {
	if in.Mode != "devices" && in.Mode != "network" {
		return errors.New("choose how you want to allow it: specific devices, or a whole network")
	}
	macs := map[string]string{}
	for _, x := range in.Devices {
		m, err := net.ParseMAC(strings.TrimSpace(x.MAC))
		if err != nil || len(m) != 6 {
			return errors.New("\"" + x.MAC + "\" is not a device address")
		}
		if len(x.Note) > 80 {
			x.Note = x.Note[:80]
		}
		macs[strings.ToLower(m.String())] = x.Note
	}
	if in.LeaseHours == 0 {
		in.LeaseHours, _ = strconv.Atoi(a.setting("upnp.max_lease_hours", "24"))
	}
	if in.LeaseHours < 1 || in.LeaseHours > 168 {
		return errors.New("choose between 1 hour and 7 days")
	}
	zones, _ := fw.Zones(a.Config)
	wanNames, _ := a.upnpWans()
	validZone, validWan := map[int64]bool{}, map[string]bool{}
	for _, z := range zones {
		if z.Kind != fw.KindWan {
			validZone[z.ID] = true
		}
	}
	for _, n := range wanNames {
		validWan[n] = true
	}
	nets, conns := []int64{}, []string{}
	for _, id := range in.Networks {
		if validZone[id] {
			nets = append(nets, id)
		}
	}
	for _, n := range in.Connections {
		if validWan[n] {
			conns = append(conns, n)
		}
	}
	if in.On {
		if miss := upnpMissing(in.Mode, len(nets), len(conns), len(macs)); len(miss) > 0 {
			return errors.New("UPnP cannot be switched on yet. " + strings.Join(miss, " "))
		}
	}
	listenOn := map[int64]bool{}
	if in.On {
		if in.Mode == "network" {
			for _, id := range nets {
				listenOn[id] = true
			}
		} else {
			// Specific devices can be on any of your networks; the list does the restricting.
			for id := range validZone {
				listenOn[id] = true
			}
		}
	}
	useConn := map[string]bool{}
	if in.On {
		for _, n := range conns {
			useConn[n] = true
		}
	}
	err := fw.Mutate(a.Config, func(tx *sql.Tx) error {
		for id := range validZone {
			if _, err := tx.Exec(`UPDATE zones SET upnp=? WHERE id=?`, b2i(listenOn[id]), id); err != nil {
				return err
			}
		}
		for _, n := range wanNames {
			if _, err := tx.Exec(`UPDATE wan_links SET upnp=? WHERE name=?`, b2i(useConn[n]), n); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM upnp_allow`); err != nil {
			return err
		}
		for m, n := range macs {
			if _, err := tx.Exec(`INSERT INTO upnp_allow(mac,note) VALUES(?,?)`, m, n); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	pb, _ := json.Marshal(upnpPick{Mode: in.Mode, Networks: nets, Connections: conns})
	a.Config.SetSetting("upnp.pick", string(pb))
	a.Config.SetSetting("upnp.restrict", strconv.Itoa(b2i(in.Mode == "devices")))
	a.Config.SetSetting("upnp.max_lease_hours", strconv.Itoa(in.LeaseHours))
	a.upnpInit()
	a.upnp.tbl.SetPolicy(a.upnpPolicy())
	a.markDirty()
	return nil
}

func (a *App) registerUPnPCardRoutes() {
	s := a.API
	s.Handle("GET /api/upnp/card", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.upnpCard())
	})
	s.HandleAdmin("PUT /api/upnp/card", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in upnpCardIn
		if !api.Decode(w, r, &in) {
			return
		}
		if err := a.saveUpnpCard(in); err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, a.upnpCard())
	})
}
