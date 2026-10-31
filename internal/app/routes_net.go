package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gatehouse/internal/bridgemode"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
	"gatehouse/internal/netcfg"
	"gatehouse/internal/netd"
	"gatehouse/internal/speedtest"
	"gatehouse/internal/wanmon"
)

// NetController is the privileged network side; privsep.Client implements it.
type NetController interface {
	NetApply(p netd.Plan, window time.Duration) (string, time.Time, error)
	NetConfirm(token string) error
	NetRollback() error
	NetReset() error
	NetPending() (string, time.Time, bool)
	WanStatus() (map[string]wanmon.State, error)
	ApplyVPN(p netd.VPNPlan) error
	VPNStatus() (netd.VPNStatus, error)
	FWEvents(since uint64, max int) ([]netd.FWEvent, uint64, error)
	SetSystem(hostname, zone string) error
	WifiScan(iface string) ([]netd.WifiNetwork, error)
	UPnPApply(ms []netd.UPnPMapping) error
	UpdateStart(path string) error
	SystemCheck() (netd.SystemUpdates, error)
	SystemUpdateStart() error
	Power(action string) error
	SpeedTest(p speedtest.Params) (speedtest.Result, error)
	WifiHealth(ifaces []string, keepAwake bool) ([]netd.WifiState, error)
	WifiPowerSave(iface string, keepAwake bool) error
	WifiReport(iface string) (string, error)
	Diag(kind, target string) (string, error)
	SSHApply(p netd.SSHPlan) error
	SSHLog(since int64) ([]string, error)
	HAApply(p netd.HAPlan) error
	NDPIApply(p netd.NDPIPlan) error
	NDPIInstalled() bool
	FlowAcct(on bool) error
	ConntrackDump() (string, error)
	SQMApply(p netd.SQMPlan) (string, error)
	OverrideApply(p netd.OverridePlan) error
}

// IfaceRow is a LAN-side port or VLAN as the console edits it.
type IfaceRow struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Parent  string `json:"parent"`
	VLAN    int    `json:"vlan"`
	ZoneID  int64  `json:"zone_id"`
	IPv4    string `json:"ipv4"`
	DHCP    bool   `json:"dhcp"`
	Gateway string `json:"gateway"`
	DNS     string `json:"dns"`
	IPv6    string `json:"ipv6"`
	PD      bool   `json:"pd"`
	MTU     int    `json:"mtu"`
	Enabled bool   `json:"enabled"`
	// Members are the ports of a bridge.
	Members []string `json:"members"`
}

// WanRow is an internet connection as the console edits it. The password is never sent back.
type WanRow struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Iface       string `json:"iface"`
	Type        string `json:"type"`
	VLAN        int    `json:"vlan"`
	Addr        string `json:"addr"`
	Gateway     string `json:"gateway"`
	DNS         string `json:"dns"`
	PPPoEUser   string `json:"pppoe_user"`
	PPPoEPass   string `json:"pppoe_pass,omitempty"` // write-only
	HasPass     bool   `json:"has_pass"`
	V6Mode      string `json:"v6_mode"`
	V6Addr      string `json:"v6_addr"`
	V6Gateway   string `json:"v6_gateway"`
	Priority    int    `json:"priority"`
	Weight      int    `json:"weight"`
	CheckKind   string `json:"check_kind"`
	CheckTarget string `json:"check_target"`
	CheckEvery  int    `json:"check_interval"`
	CheckFails  int    `json:"check_failures"`
	MTU         int    `json:"mtu"`
	UPnP        bool   `json:"upnp"`
	Enabled     bool   `json:"enabled"`
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func (a *App) listIfaces(q querier) ([]IfaceRow, error) {
	rows, err := q.Query(`SELECT id,name,kind,parent,vlan_id,COALESCE(zone_id,0),ipv4_cidr,ipv6_cidr,ipv6_pd,mtu,enabled,dhcp,gateway,dns,members FROM interfaces ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IfaceRow{}
	for rows.Next() {
		var r IfaceRow
		var pd, en, dh int
		var mem string
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Parent, &r.VLAN, &r.ZoneID, &r.IPv4, &r.IPv6, &pd, &r.MTU, &en, &dh, &r.Gateway, &r.DNS, &mem); err != nil {
			return nil, err
		}
		r.Members = []string{}
		if mem != "" {
			r.Members = strings.Split(mem, ",")
		}
		r.PD, r.Enabled, r.DHCP = pd == 1, en == 1, dh == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (a *App) listWans(q querier) ([]WanRow, error) {
	rows, err := q.Query(`SELECT id,name,iface,type,vlan_id,ipv4_addr,ipv4_gw,dns,pppoe_user,pppoe_pass,ipv6_mode,ipv6_addr,ipv6_gw,
		priority,weight,check_kind,check_target,check_interval,check_failures,mtu,upnp,enabled FROM wan_links ORDER BY priority,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WanRow{}
	for rows.Next() {
		var r WanRow
		var pass string
		var up, en int
		if err := rows.Scan(&r.ID, &r.Name, &r.Iface, &r.Type, &r.VLAN, &r.Addr, &r.Gateway, &r.DNS, &r.PPPoEUser, &pass, &r.V6Mode,
			&r.V6Addr, &r.V6Gateway, &r.Priority, &r.Weight, &r.CheckKind, &r.CheckTarget, &r.CheckEvery, &r.CheckFails, &r.MTU, &up, &en); err != nil {
			return nil, err
		}
		r.PPPoEPass = pass // sealed; callers decide whether to expose
		r.HasPass = pass != ""
		r.UPnP, r.Enabled = up == 1, en == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// buildPlan turns the stored configuration into what the helper installs.
// Connection i (enabled, ordered by priority then id) uses routing table 100+i and mark MarkBase+i,
// matching the firewall's view of the internet-side interfaces.
func (a *App) buildPlan(q querier) (*netd.Plan, error) {
	return a.buildPlanFrom(q, fw.BridgeFrom(q))
}

// buildPlanFrom builds the plan from the saved networks; a live bridge setup replaces them.
func (a *App) buildPlanFrom(q querier, bridge *bridgemode.Config) (*netd.Plan, error) {
	ifs, err := a.listIfaces(q)
	if err != nil {
		return nil, err
	}
	wans, err := a.listWans(q)
	if err != nil {
		return nil, err
	}
	p := &netd.Plan{Mode: "failover"}
	if rows, err := q.Query(`SELECT value FROM settings WHERE key='wan.mode'`); err == nil {
		if rows.Next() {
			rows.Scan(&p.Mode)
		}
		rows.Close()
	}
	for _, i := range ifs {
		p.Config.Interfaces = append(p.Config.Interfaces, netcfg.Interface{Name: i.Name, Kind: i.Kind, Parent: i.Parent,
			VLAN: i.VLAN, IPv4: i.IPv4, DHCP: i.DHCP, Gateway: i.Gateway, DNS: i.DNS, Members: i.Members, IPv6: i.IPv6, PD: i.PD, MTU: i.MTU, Enabled: i.Enabled})
	}
	n := 0
	for _, w := range wans {
		pass := ""
		if w.PPPoEPass != "" {
			if pass, err = a.Box.Open(w.PPPoEPass); err != nil {
				return nil, fmt.Errorf("the saved password for %s cannot be read; enter it again", w.Name)
			}
		}
		nw := netcfg.WAN{Name: w.Name, Iface: w.Iface, Type: w.Type, VLAN: w.VLAN, Addr: w.Addr, Gateway: w.Gateway, DNS: w.DNS,
			PPPoEUser: w.PPPoEUser, PPPoEPass: pass, V6Mode: w.V6Mode, V6Addr: w.V6Addr, V6Gateway: w.V6Gateway,
			Priority: w.Priority, MTU: w.MTU, Enabled: w.Enabled}
		p.Config.WANs = append(p.Config.WANs, nw)
		if !w.Enabled {
			continue
		}
		p.Links = append(p.Links, netd.LinkSpec{Name: w.Name, Dev: netcfg.WanDevice(nw), Priority: w.Priority, Weight: max(w.Weight, 1),
			Table: 100 + n, Mark: uint32(fw.MarkBase + n), Kind: w.CheckKind, Target: w.CheckTarget,
			Interval: w.CheckEvery, Failures: w.CheckFails, Recover: w.CheckFails})
		n++
	}
	if err := a.addVPNToPlan(q, p); err != nil {
		return nil, err
	}
	if err := a.addGroupsToPlan(q, p); err != nil {
		return nil, err
	}
	if err := a.addWifiToPlan(q, p); err != nil {
		return nil, err
	}
	applyBridgeToPlan(p, bridge)
	return p, nil
}

// fingerprint identifies a plan, so the console can tell whether anything changed since the last apply.
func fingerprint(p *netd.Plan) string {
	q := *p
	q.Seconds = 0
	b, _ := json.Marshal(q)
	// A change in what Gatehouse writes for a tunnel device (not in the settings themselves) must
	// still show as "waiting to be applied", or an update could never reach a machine that
	// already has tunnels. Change tunnelFiles when the generated tunnel file changes.
	if len(p.Config.Tunnels) > 0 {
		b = append(b, []byte("|tunnel-files:"+tunnelFiles)...)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// tunnelFiles names the version of the file written for each tunnel device.
const tunnelFiles = "2-keep-static"

func (a *App) validatePlan(p *netd.Plan) error {
	if err := p.Config.Validate(); err != nil {
		return err
	}
	if _, err := netcfg.Generate(&p.Config); err != nil {
		return err
	}
	if p.Mode != "failover" && p.Mode != "balance" {
		return errors.New("connection sharing must be failover or balance")
	}
	return nil
}

// mutateNet runs fn, then keeps the change only if the whole network configuration still validates.
func (a *App) mutateNet(fn func(tx *sql.Tx) error) error {
	tx, err := a.Config.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	p, err := a.buildPlan(tx)
	if err != nil {
		return err
	}
	if err := a.validatePlan(p); err != nil {
		return err
	}
	return tx.Commit()
}

func b2iN(b bool) int { return b2i(b) }

func (a *App) netStatus() map[string]any {
	st := map[string]any{"helper": a.Net != nil}
	p, err := a.buildPlan(a.Config)
	if err != nil {
		st["error"] = err.Error()
		return st
	}
	st["dirty"] = (len(p.Config.Interfaces) > 0 || len(p.Config.WANs) > 0) && fingerprint(p) != a.Config.Setting("net.applied", "")
	st["rev"] = fingerprint(p)
	if a.Net != nil {
		if tok, dl, ok := a.Net.NetPending(); ok {
			st["pending"] = map[string]any{"token": tok, "deadline": dl.Unix()}
		}
	}
	return st
}

func (a *App) publishNet() { a.Bus.Publish("net.status", a.netStatus()) }

// ports lists network ports a person can plug a cable into: built-in, PCIe and USB adapters.
// Virtual devices (bridges, VPNs, containers, loopback) are left out. Wireless cards are listed with wifi set.
func ports() []map[string]any {
	ents, _ := os.ReadDir("/sys/class/net")
	out := []map[string]any{}
	for _, e := range ents {
		n := e.Name()
		base := "/sys/class/net/" + n
		if n == "lo" {
			continue
		}
		real, err := filepath.EvalSymlinks(base)
		if err != nil || strings.Contains(real, "/devices/virtual/") {
			continue
		}
		_, werr := os.Stat(base + "/wireless")
		if t, _ := os.ReadFile(base + "/type"); strings.TrimSpace(string(t)) != "1" {
			continue // not Ethernet
		}
		ifi, err := net.InterfaceByName(n)
		if err != nil {
			continue
		}
		carrier, _ := os.ReadFile(base + "/carrier")
		speed, _ := os.ReadFile(base + "/speed")
		out = append(out, map[string]any{"name": n, "mac": ifi.HardwareAddr.String(),
			"link": strings.TrimSpace(string(carrier)) == "1", "speed": strings.TrimSpace(string(speed)),
			"usb": strings.Contains(real, "/usb"), "wifi": werr == nil, "addrs": ifaceAddrs(n)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	return out
}

// bridgePortsFree checks that every port chosen for a bridge belongs nowhere else: not to the
// internet, not to another bridge, and not to a network of its own. The people editing see the
// reason in words instead of a networking error.
func bridgePortsFree(tx *sql.Tx, self int64, members []string) error {
	seen := map[string]bool{}
	for _, m := range members {
		if seen[m] {
			return fmt.Errorf("%s is listed twice", m)
		}
		seen[m] = true
		var n int
		tx.QueryRow(`SELECT COUNT(*) FROM wan_links WHERE iface=?`, m).Scan(&n)
		if n > 0 {
			return fmt.Errorf("%s is used for the internet, so it cannot also be on this network", m)
		}
	}
	rows, err := tx.Query(`SELECT id,name,kind,members,ipv4_cidr,COALESCE(zone_id,0) FROM interfaces WHERE id<>?`, self)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, zone int64
		var name, kind, mem, cidr string
		if err := rows.Scan(&id, &name, &kind, &mem, &cidr, &zone); err != nil {
			return err
		}
		for _, o := range strings.Split(mem, ",") {
			if o != "" && seen[o] {
				return fmt.Errorf("%s already belongs to another network (%s); take it out of there first", o, name)
			}
		}
		if kind != "vlan" && kind != "bridge" && seen[name] && (cidr != "" || zone != 0) {
			return fmt.Errorf("%s already has a network of its own; remove or move that network first", name)
		}
	}
	return rows.Err()
}

func (a *App) registerNetRoutes() {
	s := a.API
	s.Handle("GET /api/net/ports", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, ports())
	})
	s.Handle("GET /api/net/status", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		out := map[string]any{"status": a.netStatus()}
		if a.Net != nil {
			if st, err := a.Net.WanStatus(); err == nil {
				out["links"] = st
			}
		}
		api.JSON(w, 200, out)
	})
	s.Handle("GET /api/net/interfaces", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		v, err := a.listIfaces(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, v)
	})
	s.HandleAdmin("PUT /api/net/interfaces", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in IfaceRow
		if !api.Decode(w, r, &in) {
			return
		}
		var id int64
		err := a.mutateNet(func(tx *sql.Tx) error {
			var zone any
			if in.ZoneID > 0 {
				zone = in.ZoneID
			}
			if in.Kind == "bridge" {
				if err := bridgePortsFree(tx, in.ID, in.Members); err != nil {
					return err
				}
			}
			if in.ID == 0 {
				res, err := tx.Exec(`INSERT INTO interfaces(name,kind,parent,vlan_id,zone_id,ipv4_cidr,ipv6_cidr,ipv6_pd,mtu,enabled,dhcp,gateway,dns,members) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
					in.Name, in.Kind, in.Parent, in.VLAN, zone, in.IPv4, in.IPv6, b2iN(in.PD), in.MTU, b2iN(in.Enabled), b2iN(in.DHCP), in.Gateway, in.DNS, strings.Join(in.Members, ","))
				if err != nil {
					return friendlyDup(err, "a port or VLAN with that name already exists")
				}
				id, _ = res.LastInsertId()
				return nil
			}
			id = in.ID
			_, err := tx.Exec(`UPDATE interfaces SET name=?,kind=?,parent=?,vlan_id=?,zone_id=?,ipv4_cidr=?,ipv6_cidr=?,ipv6_pd=?,mtu=?,enabled=?,dhcp=?,gateway=?,dns=?,members=? WHERE id=?`,
				in.Name, in.Kind, in.Parent, in.VLAN, zone, in.IPv4, in.IPv6, b2iN(in.PD), in.MTU, b2iN(in.Enabled), b2iN(in.DHCP), in.Gateway, in.DNS, strings.Join(in.Members, ","), in.ID)
			return friendlyDup(err, "a port or VLAN with that name already exists")
		})
		if err != nil {
			fail(w, err)
			return
		}
		a.publishNet()
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	a.networkCleanupRoutes(s)
	// Removing a network takes its DHCP range with it, and its firewall network when this was
	// the last port, so nothing is left behind (a "Wi-Fi" network with no port, an address range
	// for a port that is gone).
	s.HandleAdmin("DELETE /api/net/interfaces/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		var imp *removalImpact
		if err := a.mutateNet(func(tx *sql.Tx) error {
			var e error
			if imp, e = ifaceImpact(tx, id); e != nil {
				return e
			}
			if _, e = tx.Exec(`DELETE FROM interfaces WHERE id=?`, id); e != nil {
				return e
			}
			deleteScopes(tx, imp.Scopes)
			if imp.RemoveZone {
				return fw.DeleteZone(tx, imp.ZoneID)
			}
			return nil
		}); err != nil {
			fail(w, err)
			return
		}
		if len(imp.Scopes) > 0 {
			a.DHCP.Reload()
			a.publishLeases()
		}
		if imp.RemoveZone {
			a.markDirty()
		}
		a.publishNet()
		api.JSON(w, 200, imp)
	})
	s.Handle("GET /api/net/wans", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		v, err := a.listWans(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		for i := range v {
			v[i].PPPoEPass = ""
		}
		api.JSON(w, 200, map[string]any{"wans": v, "mode": a.Config.Setting("wan.mode", "failover")})
	})
	s.HandleAdmin("PUT /api/net/wans", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in WanRow
		if !api.Decode(w, r, &in) {
			return
		}
		defaults(&in)
		var id int64
		err := a.mutateNet(func(tx *sql.Tx) error {
			sealed := ""
			if in.PPPoEPass != "" {
				sealed = a.Box.Seal(in.PPPoEPass)
			} else if in.ID != 0 {
				tx.QueryRow(`SELECT pppoe_pass FROM wan_links WHERE id=?`, in.ID).Scan(&sealed)
			}
			if in.Type != "pppoe" {
				sealed = ""
			}
			args := []any{in.Name, in.Iface, in.Type, in.VLAN, in.Addr, in.Gateway, in.DNS, in.PPPoEUser, sealed, in.V6Mode, in.V6Addr, in.V6Gateway,
				in.Priority, in.Weight, in.CheckKind, in.CheckTarget, in.CheckEvery, in.CheckFails, in.MTU, b2iN(in.UPnP), b2iN(in.Enabled)}
			if in.ID == 0 {
				res, err := tx.Exec(`INSERT INTO wan_links(name,iface,type,vlan_id,ipv4_addr,ipv4_gw,dns,pppoe_user,pppoe_pass,ipv6_mode,ipv6_addr,ipv6_gw,
					priority,weight,check_kind,check_target,check_interval,check_failures,mtu,upnp,enabled) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
				if err != nil {
					return friendlyDup(err, "an internet connection with that name already exists")
				}
				id, _ = res.LastInsertId()
				return nil
			}
			id = in.ID
			_, err := tx.Exec(`UPDATE wan_links SET name=?,iface=?,type=?,vlan_id=?,ipv4_addr=?,ipv4_gw=?,dns=?,pppoe_user=?,pppoe_pass=?,ipv6_mode=?,ipv6_addr=?,ipv6_gw=?,
				priority=?,weight=?,check_kind=?,check_target=?,check_interval=?,check_failures=?,mtu=?,upnp=?,enabled=? WHERE id=?`, append(args, in.ID)...)
			return friendlyDup(err, "an internet connection with that name already exists")
		})
		if err != nil {
			fail(w, err)
			return
		}
		a.publishNet()
		a.publishFW()
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("DELETE /api/net/wans/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := a.mutateNet(func(tx *sql.Tx) error { _, e := tx.Exec(`DELETE FROM wan_links WHERE id=?`, id); return e }); err != nil {
			fail(w, err)
			return
		}
		a.publishNet()
		a.publishFW()
		ok(w)
	})
	s.HandleAdmin("PUT /api/net/mode", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Mode string `json:"mode"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Mode != "failover" && in.Mode != "balance" {
			fail(w, errors.New("choose failover or balance"))
			return
		}
		a.Config.SetSetting("wan.mode", in.Mode)
		a.publishNet()
		ok(w)
	})
	s.HandleAdmin("POST /api/net/preview", a.netPreview)
	s.HandleAdmin("POST /api/net/apply", a.netApply)
	s.HandleAdmin("POST /api/net/confirm", a.netConfirm)
	s.HandleAdmin("POST /api/net/rollback", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		if err := a.Net.NetRollback(); err != nil {
			fail(w, err)
			return
		}
		a.publishNet()
		ok(w)
	})
}

func friendlyDup(err error, msg string) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return errors.New(msg)
	}
	return err
}

// defaults fills in sensible values so the console can send the minimum.
func defaults(w *WanRow) {
	if w.Type == "" {
		w.Type = "dhcp"
	}
	if w.V6Mode == "" {
		w.V6Mode = "none"
	}
	if w.Priority == 0 {
		w.Priority = 1
	}
	if w.Weight == 0 {
		w.Weight = 1
	}
	if w.CheckKind == "" {
		w.CheckKind = "icmp"
	}
	if w.CheckTarget == "" {
		w.CheckTarget = "1.1.1.1"
	}
	if w.CheckEvery == 0 {
		w.CheckEvery = 5
	}
	if w.CheckFails == 0 {
		w.CheckFails = 3
	}
}

// netWarnings are plain-language cautions that do not block an apply.
func (a *App) netWarnings(p *netd.Plan, clientIP string) []string {
	var out []string
	if len(p.Links) == 0 {
		out = append(out, "No internet connection is set up, so devices will not reach the internet.")
	}
	ci := ifaceOf(clientIP)
	for _, i := range p.Config.Interfaces {
		if ci != "" && ci == i.Name && !i.Enabled {
			out = append(out, "You are connected through "+i.Name+", which this turns off. You will be disconnected; the change is undone if you do not confirm.")
		}
	}
	for _, w := range p.Config.WANs {
		if w.Type == "static" && w.Enabled {
			if ip, _, ok := strings.Cut(w.Addr, "/"); ok {
				if shared, why := netcfg.CGNAT(ip); shared {
					out = append(out, w.Name+": "+why)
				}
			}
		}
	}
	return out
}

func (a *App) netPreview(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	p, err := a.buildPlan(a.Config)
	if err == nil {
		err = a.validatePlan(p)
	}
	if err != nil {
		api.JSON(w, 200, map[string]any{"ok": false, "problems": strings.Split(err.Error(), "; ")})
		return
	}
	api.JSON(w, 200, map[string]any{"ok": true, "changed": fingerprint(p) != a.Config.Setting("net.applied", ""),
		"warnings": a.netWarnings(p, c.IP)})
}

func (a *App) netApply(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	if a.Net == nil {
		fail(w, errNoHelper)
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	if _, _, pending := a.Net.NetPending(); pending {
		api.Error(w, http.StatusConflict, netcfg.ErrPending.Error())
		return
	}
	if a.Config.Setting("fw.stepup", "0") == "1" && c.User.TOTPEnabled {
		if err := a.Auth.VerifyStepUp(c.User.ID, in.Code); err != nil {
			api.Error(w, http.StatusForbidden, "enter a current code from your authenticator to apply network changes")
			return
		}
	}
	p, err := a.buildPlan(a.Config)
	if err == nil {
		err = a.validatePlan(p)
	}
	if err != nil {
		fail(w, err)
		return
	}
	a.restorePoint()
	tok, deadline, err := a.Net.NetApply(*p, confirmWindow)
	if err != nil {
		if errors.Is(err, netcfg.ErrPending) {
			api.Error(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	a.netApplyState.mu.Lock()
	a.netApplyState.connID, a.netApplyState.print, a.netApplyState.base = api.ConnID(r), fingerprint(p), baseFingerprint(p)
	a.netApplyState.mu.Unlock()
	a.publishNet()
	w.Header().Set("Connection", "close")
	api.JSON(w, 200, map[string]any{"token": tok, "deadline": deadline.Unix(), "seconds": int(confirmWindow / time.Second)})
}

func (a *App) netConfirm(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	if a.Net == nil {
		fail(w, errNoHelper)
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	a.netApplyState.mu.Lock()
	same := a.netApplyState.connID != 0 && a.netApplyState.connID == api.ConnID(r)
	print, base := a.netApplyState.print, a.netApplyState.base
	a.netApplyState.mu.Unlock()
	if same {
		api.Error(w, http.StatusConflict, "confirm from a new connection; the old one can outlive a broken change")
		return
	}
	if err := a.Net.NetConfirm(in.Token); err != nil {
		fail(w, err)
		return
	}
	a.Config.SetSetting("net.applied", print)
	a.Config.SetSetting("net.applied_base", base)
	a.publishNet()
	ok(w)
}
