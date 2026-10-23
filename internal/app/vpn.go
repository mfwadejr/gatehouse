package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"gatehouse/internal/fw"
	"gatehouse/internal/netcfg"
	"gatehouse/internal/netd"
	"gatehouse/internal/vpn"
)

// The VPN side of the daemon. Everything about a tunnel lives in the configuration database;
// the network helper is only ever handed a complete plan, so a half-finished edit can never
// leave a tunnel half configured.

const vpnEndpointSetting = "vpn.endpoint"

type wgRow struct {
	ID         int64
	Name       string
	Label      string
	Mode       string
	Port       int
	Address    string
	PrivateKey string // plain
	PublicKey  string
	Endpoint   string
	DNS        string
	MTU        int
	Enabled    bool
	ZoneID     int64
	RouteAll   bool
	KillSwitch bool
	Check      string
	CreatedAt  int64
}

type wgPeerRow struct {
	ID        int64
	WGID      int64
	Name      string
	PublicKey string
	PSK       string // plain
	Address   string
	Allowed   string
	Endpoint  string
	Keepalive int
	Enabled   bool
	CreatedAt int64
}

type ovpnRow struct {
	ID         int64
	Name       string
	Label      string
	Mode       string
	Enabled    bool
	Proto      string
	Port       int
	Subnet     string
	Endpoint   string
	RouteAll   bool
	DNS        string
	ZoneID     int64
	KillSwitch bool
	Check      string
	CACert     string
	CAKey      string // plain
	SrvCert    string
	SrvKey     string // plain
	TLSCrypt   string // plain
	CRL        string
	CRLNumber  int64
	Profile    string // plain
	AuthUser   string
	AuthPass   string // plain
	CreatedAt  int64
}

type ovpnUserRow struct {
	ID        int64
	OVPNID    int64
	Name      string
	Serial    string
	Cert      string
	CreatedAt int64
	RevokedAt int64
}

func (a *App) openSealed(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return a.Box.Open(s)
}

func (a *App) listWG(q querier) ([]wgRow, error) {
	rows, err := q.Query(`SELECT id,name,label,mode,listen_port,address,private_key,public_key,endpoint_host,dns,mtu,enabled,COALESCE(zone_id,0),route_all,killswitch,check_target,created_at FROM vpn_wg ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []wgRow
	var sealed []string
	for rows.Next() {
		var r wgRow
		var s string
		var en, ra, ks int
		if err := rows.Scan(&r.ID, &r.Name, &r.Label, &r.Mode, &r.Port, &r.Address, &s, &r.PublicKey, &r.Endpoint, &r.DNS, &r.MTU, &en, &r.ZoneID, &ra, &ks, &r.Check, &r.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		r.Enabled, r.RouteAll, r.KillSwitch = en == 1, ra == 1, ks == 1
		out = append(out, r)
		sealed = append(sealed, s)
	}
	rows.Close()
	for i := range out {
		if out[i].PrivateKey, err = a.openSealed(sealed[i]); err != nil {
			return nil, fmt.Errorf("the key of %s cannot be read", out[i].Name)
		}
	}
	return out, nil
}

func (a *App) listWGPeers(q querier, wgID int64) ([]wgPeerRow, error) {
	rows, err := q.Query(`SELECT id,wg_id,name,public_key,psk,address,allowed_ips,endpoint,keepalive,enabled,created_at FROM vpn_wg_peers WHERE wg_id=? ORDER BY id`, wgID)
	if err != nil {
		return nil, err
	}
	var out []wgPeerRow
	var sealed []string
	for rows.Next() {
		var r wgPeerRow
		var s string
		var en int
		if err := rows.Scan(&r.ID, &r.WGID, &r.Name, &r.PublicKey, &s, &r.Address, &r.Allowed, &r.Endpoint, &r.Keepalive, &en, &r.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		r.Enabled = en == 1
		out = append(out, r)
		sealed = append(sealed, s)
	}
	rows.Close()
	for i := range out {
		if out[i].PSK, err = a.openSealed(sealed[i]); err != nil {
			return nil, fmt.Errorf("a shared secret of %s cannot be read", out[i].Name)
		}
	}
	return out, nil
}

func (a *App) listOVPN(q querier) ([]ovpnRow, error) {
	rows, err := q.Query(`SELECT id,name,label,mode,enabled,proto,port,subnet,endpoint_host,route_all,dns,COALESCE(zone_id,0),killswitch,check_target,
		ca_cert,ca_key,srv_cert,srv_key,tls_crypt,crl,crl_number,profile,auth_user,auth_pass,created_at FROM vpn_ovpn ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []ovpnRow
	var sealed [][5]string
	for rows.Next() {
		var r ovpnRow
		var en, ra, ks int
		var s [5]string
		if err := rows.Scan(&r.ID, &r.Name, &r.Label, &r.Mode, &en, &r.Proto, &r.Port, &r.Subnet, &r.Endpoint, &ra, &r.DNS, &r.ZoneID, &ks, &r.Check,
			&r.CACert, &s[0], &r.SrvCert, &s[1], &s[2], &r.CRL, &r.CRLNumber, &s[3], &r.AuthUser, &s[4], &r.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		r.Enabled, r.RouteAll, r.KillSwitch = en == 1, ra == 1, ks == 1
		out = append(out, r)
		sealed = append(sealed, s)
	}
	rows.Close()
	for i := range out {
		var err error
		o := &out[i]
		for j, dst := range []*string{&o.CAKey, &o.SrvKey, &o.TLSCrypt, &o.Profile, &o.AuthPass} {
			if *dst, err = a.openSealed(sealed[i][j]); err != nil {
				return nil, fmt.Errorf("the keys of %s cannot be read", o.Name)
			}
		}
	}
	return out, nil
}

func (a *App) listOVPNUsers(q querier, id int64) ([]ovpnUserRow, error) {
	rows, err := q.Query(`SELECT id,ovpn_id,name,serial,cert,created_at,revoked_at FROM vpn_ovpn_users WHERE ovpn_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ovpnUserRow
	for rows.Next() {
		var r ovpnUserRow
		if err := rows.Scan(&r.ID, &r.OVPNID, &r.Name, &r.Serial, &r.Cert, &r.CreatedAt, &r.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// tunnelTables gives every outbound tunnel its own routing table, in a stable order.
func tunnelTables(wgs []wgRow, ovs []ovpnRow) (map[string]int, error) {
	var names []string
	for _, w := range wgs {
		if w.Mode == "client" {
			names = append(names, w.Name)
		}
	}
	for _, o := range ovs {
		if o.Mode == "client" {
			names = append(names, o.Name)
		}
	}
	sort.Strings(names)
	if len(names) > netcfg.MaxTunnelTable-299 {
		return nil, fmt.Errorf("Gatehouse supports up to %d outbound VPN connections", netcfg.MaxTunnelTable-299)
	}
	out := map[string]int{}
	for i, n := range names {
		out[n] = 300 + i
	}
	return out, nil
}

// addVPNToPlan puts every WireGuard interface and OpenVPN client device into the network plan.
func (a *App) addVPNToPlan(q querier, p *netd.Plan) error {
	wgs, err := a.listWG(q)
	if err != nil {
		return err
	}
	ovs, err := a.listOVPN(q)
	if err != nil {
		return err
	}
	tables, err := tunnelTables(wgs, ovs)
	if err != nil {
		return err
	}
	srcs, err := fw.TunnelSources(q)
	if err != nil {
		return err
	}
	for _, w := range wgs {
		peers, err := a.listWGPeers(q, w.ID)
		if err != nil {
			return err
		}
		nw := netcfg.WG{Name: w.Name, Address: w.Address, PrivateKey: w.PrivateKey, MTU: w.MTU, Enabled: w.Enabled, Mode: w.Mode}
		if w.Mode == "server" {
			nw.Port = w.Port
		} else {
			nw.Table, nw.Sources = tables[w.Name], srcs[w.Name]
		}
		for _, pr := range peers {
			np := netcfg.WGPeer{Name: pr.Name, PublicKey: pr.PublicKey, PSK: pr.PSK, Endpoint: pr.Endpoint, Keepalive: pr.Keepalive, Enabled: pr.Enabled}
			for _, ip := range strings.Split(pr.Allowed, ",") {
				if ip = strings.TrimSpace(ip); ip != "" {
					np.AllowedIPs = append(np.AllowedIPs, ip)
				}
			}
			nw.Peers = append(nw.Peers, np)
		}
		p.Config.WireGuard = append(p.Config.WireGuard, nw)
	}
	for _, o := range ovs {
		if o.Mode == "client" {
			p.Config.Tunnels = append(p.Config.Tunnels, netcfg.Tunnel{Name: o.Name, Table: tables[o.Name], Sources: srcs[o.Name], Enabled: o.Enabled})
		}
	}
	return nil
}

// baseFingerprint identifies the plan without any VPN parts, so a VPN change can tell whether
// there are other network edits waiting that it must not apply by accident.
func baseFingerprint(p *netd.Plan) string {
	q := *p
	q.Config.WireGuard, q.Config.Tunnels = nil, nil
	return fingerprint(&q)
}

// lanRoutes lists the subnets of the local networks, which remote devices may be given.
func (a *App) lanRoutes(q querier) []string {
	rows, err := q.Query(`SELECT ipv4_cidr FROM interfaces WHERE enabled=1 AND ipv4_cidr<>'' ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		if p, err := netip.ParsePrefix(strings.TrimSpace(c)); err == nil && p.Addr().Is4() {
			out = append(out, p.Masked().String())
		}
	}
	return out
}

func (a *App) zoneIsFull(q querier, id int64) bool {
	rows, err := q.Query(`SELECT kind FROM zones WHERE id=?`, id)
	if err != nil {
		return false
	}
	defer rows.Close()
	var k string
	if rows.Next() {
		rows.Scan(&k)
	}
	return k == fw.KindTrusted
}

// routerAddr is the router's own address inside a tunnel network.
func routerAddr(cidr string) string {
	if p, err := netip.ParsePrefix(cidr); err == nil {
		return p.Addr().String()
	}
	return ""
}

// buildVPNPlan is what the network helper keeps running for OpenVPN and what it watches.
func (a *App) buildVPNPlan(q querier) (*netd.VPNPlan, error) {
	wgs, err := a.listWG(q)
	if err != nil {
		return nil, err
	}
	ovs, err := a.listOVPN(q)
	if err != nil {
		return nil, err
	}
	plan := &netd.VPNPlan{}
	lan := a.lanRoutes(q)
	for _, w := range wgs {
		if w.Enabled && w.Mode == "client" && w.Check != "" {
			plan.Checks = append(plan.Checks, netd.Check{Name: w.Name, Target: w.Check, Exit: true})
		}
	}
	for _, o := range ovs {
		if !o.Enabled {
			continue
		}
		inst := netd.OVPNInstance{Name: o.Name, Role: o.Mode, Files: map[string]string{}}
		if o.Mode == "server" {
			routes := []string(nil)
			if a.zoneIsFull(q, o.ZoneID) {
				routes = lan
			}
			srvAddr, err := vpn.ServerAddr(o.Subnet)
			if err != nil {
				return nil, err
			}
			dns := o.DNS
			if dns == "" {
				dns = srvAddr
			}
			conf, err := vpn.OVPNServer{Dev: o.Name, Proto: o.Proto, Port: o.Port, Subnet: o.Subnet, DNS: dns, RouteAll: o.RouteAll,
				Routes: routes, Dir: netd.ServerKeyDir(o.Name)}.Config()
			if err != nil {
				return nil, err
			}
			inst.Conf = conf
			inst.Files["ca.crt"], inst.Files["server.crt"], inst.Files["server.key"] = o.CACert, o.SrvCert, o.SrvKey
			inst.Files["tls-crypt.key"], inst.Files["crl.pem"] = o.TLSCrypt, o.CRL
		} else {
			imp, err := vpn.ImportProfile(o.Profile)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", o.Label, err)
			}
			auth := ""
			if imp.NeedsAuth && o.AuthUser != "" {
				auth = netd.ClientAuthPath(o.Name)
				inst.Files["auth"] = o.AuthUser + "\n" + o.AuthPass + "\n"
			}
			inst.Conf = vpn.OVPNClientConfig(imp, o.Name, auth)
			if o.Check != "" {
				plan.Checks = append(plan.Checks, netd.Check{Name: o.Name, Target: o.Check, Exit: true})
			}
		}
		plan.OpenVPN = append(plan.OpenVPN, inst)
	}
	return plan, nil
}

// applyFirewall compiles and installs the stored firewall, confirming it at once.
func (a *App) applyFirewall(author string) error {
	if a.FW == nil {
		return errNoHelper
	}
	m, script, err := a.currentRuleset()
	if err != nil {
		return err
	}
	tok, _, err := a.FW.Apply(script, confirmWindow, author)
	if err != nil {
		return err
	}
	if err := a.FW.Confirm(tok); err != nil {
		return err
	}
	summary := strings.Join(fw.Describe(m), " ")
	if len(summary) > 500 {
		summary = summary[:500]
	}
	a.Config.Exec(`INSERT INTO fw_revisions(created_at,author,summary,ruleset,status) VALUES(?,?,?,?,'confirmed')`, time.Now().Unix(), author, summary, script)
	a.publishFW()
	return nil
}

var errWaitingChanges = errors.New("there are network changes waiting on the Internet page. Apply or discard those first, then try again")

// applyVPN puts the stored VPN configuration into effect. VPN changes never touch the
// connections that carry the console, so they are applied and confirmed immediately.
func (a *App) applyVPN(author string) error {
	if a.Net == nil || a.FW == nil {
		return errNoHelper
	}
	p, err := a.buildPlan(a.Config)
	if err == nil {
		err = a.validatePlan(p)
	}
	if err != nil {
		return err
	}
	if _, _, pending := a.Net.NetPending(); pending {
		return netcfg.ErrPending
	}
	if base := a.Config.Setting("net.applied_base", ""); base != "" && base != baseFingerprint(p) {
		return errWaitingChanges
	}
	vp, err := a.buildVPNPlan(a.Config)
	if err != nil {
		return err
	}
	tok, _, err := a.Net.NetApply(*p, confirmWindow)
	if err != nil {
		return err
	}
	if err := a.Net.NetConfirm(tok); err != nil {
		return err
	}
	a.Config.SetSetting("net.applied", fingerprint(p))
	a.Config.SetSetting("net.applied_base", baseFingerprint(p))
	if err := a.applyFirewall(author); err != nil {
		return err
	}
	if err := a.Net.ApplyVPN(*vp); err != nil {
		return err
	}
	a.vpnMarkApplied()
	a.publishNet()
	a.refreshVPN()
	return nil
}

// mutateVPN edits the stored configuration, keeps it only if the network and firewall still
// validate, then applies it.
func (a *App) mutateVPN(author string, fn func(tx *sql.Tx) error) error {
	tx, err := a.Config.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	p, err := a.buildPlan(tx)
	if err == nil {
		err = a.validatePlan(p)
	}
	if err != nil {
		return err
	}
	m, err := fw.Load(tx)
	if err == nil {
		err = m.Validate()
	}
	if err != nil {
		return err
	}
	if _, err := a.buildVPNPlan(tx); err != nil {
		return err
	}
	if err := a.checkTunnelOverlap(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if a.Net == nil || a.FW == nil {
		return nil
	}
	if err := a.applyVPN(author); err != nil {
		return fmt.Errorf("saved, but it could not be switched on yet: %v", err)
	}
	return nil
}

// checkTunnelOverlap refuses two tunnels that claim the same address, so it is always clear
// which one a device uses.
func (a *App) checkTunnelOverlap(q querier) error {
	srcs, err := fw.TunnelSources(q)
	if err != nil {
		return err
	}
	type pt struct {
		t string
		p netip.Prefix
	}
	var all []pt
	for t, list := range srcs {
		for _, s := range list {
			if p, err := netip.ParsePrefix(s); err == nil {
				all = append(all, pt{t, p})
			}
		}
	}
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if all[i].t != all[j].t && all[i].p.Overlaps(all[j].p) {
				return fmt.Errorf("%s is sent through two different VPNs (%s and %s). A device or network can use only one", all[i].p, all[i].t, all[j].t)
			}
		}
	}
	return nil
}

// ensureVPNZone returns the network that VPN devices join: full access to everything at home,
// or internet only.
func (a *App) ensureVPNZone(tx *sql.Tx, full bool) (int64, error) {
	name, kind := "VPN", fw.KindTrusted
	desc := "Devices connected from outside, with access to everything at home"
	if !full {
		name, kind = "VPN internet only", fw.KindPrivate
		desc = "Devices connected from outside that only use this router for internet"
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM zones WHERE name=?`, name).Scan(&id); err == nil {
		return id, nil
	}
	return fw.SaveZone(tx, fw.ZoneRow{Name: name, Kind: kind, Internet: true, Management: false, Gateway: true, DNSForce: false, Description: desc})
}

func (a *App) usedPrefixes(q querier) []netip.Prefix {
	var out []netip.Prefix
	add := func(s string) {
		if p, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil {
			out = append(out, p.Masked())
		}
	}
	for _, c := range a.lanRoutes(q) {
		add(c)
	}
	if rows, err := q.Query(`SELECT address FROM vpn_wg UNION ALL SELECT subnet FROM vpn_ovpn`); err == nil {
		for rows.Next() {
			var s string
			rows.Scan(&s)
			add(s)
		}
		rows.Close()
	}
	return out
}

func freeName(q querier, table, prefix string) (string, error) {
	taken := map[string]bool{}
	if rows, err := q.Query(`SELECT name FROM ` + table); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			taken[n] = true
		}
		rows.Close()
	}
	for i := 0; i < 10; i++ {
		if n := fmt.Sprintf("%s%d", prefix, i); !taken[n] {
			return n, nil
		}
	}
	return "", errors.New("the limit of 10 VPNs of this type has been reached")
}

func (a *App) freeWGPort(q querier) int {
	used := map[int]bool{}
	for _, t := range []string{"SELECT listen_port FROM vpn_wg", "SELECT port FROM vpn_ovpn"} {
		if rows, err := q.Query(t); err == nil {
			for rows.Next() {
				var p int
				rows.Scan(&p)
				used[p] = true
			}
			rows.Close()
		}
	}
	for p := 51820; p < 51900; p++ {
		if !used[p] {
			return p
		}
	}
	return 51820
}

func cleanLabel(s, def string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
