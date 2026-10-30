package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/ha"
	"gatehouse/internal/netcfg"
	"gatehouse/internal/netd"
	"gatehouse/internal/update"
)

// High availability. Two units hold the same settings. The primary is where changes are made; the
// standby gets a copy of the settings (with its own port names filled in) and never answers DHCP or
// holds the shared addresses until the active unit goes quiet.
//
// Everything about the pairing lives in a local file, never in the settings that are copied, so a
// copy of the settings can never overwrite which unit is which.

type haLocal struct {
	Pairing   ha.Pairing  `json:"pairing"`
	Role      ha.Role     `json:"role"`
	Self      ha.Identity `json:"self"`
	PeerFP    string      `json:"peer_fp"`
	PeerPort  int         `json:"peer_port"`
	MgmtIface string      `json:"mgmt_iface"`
	Mapping   []ha.Match  `json:"mapping"`
	Seq       int64       `json:"seq"`
	SyncedAt  int64       `json:"synced_at"`
	LastHash  string      `json:"last_hash"`
	Prefer    ha.Role     `json:"prefer,omitempty"`    // which unit (by how it was paired) is the Primary; empty means the first one
	HeartDev  string      `json:"heart_dev,omitempty"` // this unit's port for the optional cable to the partner
	Activated bool        `json:"activated"`           // the shared-address services are installed
	Applied   bool        `json:"applied"`             // the first copy of the settings has been applied here
	LastError string      `json:"last_error,omitempty"`
}

var haRT struct {
	mu   sync.Mutex
	kick chan struct{}
}

func init() { haRT.kick = make(chan struct{}, 1) }

// haKick asks the sync loop to send the settings soon.
func haKick() {
	select {
	case haRT.kick <- struct{}{}:
	default:
	}
}

func (a *App) haPath() string { return filepath.Join(a.Cfg.DataDir, "ha.json") }

func (a *App) haLoad() *haLocal {
	haRT.mu.Lock()
	defer haRT.mu.Unlock()
	b, err := os.ReadFile(a.haPath())
	if err != nil {
		return nil
	}
	var s haLocal
	if json.Unmarshal(b, &s) != nil || s.Role == "" {
		return nil
	}
	return &s
}

func (a *App) haSave(s *haLocal) error {
	haRT.mu.Lock()
	defer haRT.mu.Unlock()
	if s == nil {
		os.Remove(a.haPath())
		return nil
	}
	b, _ := json.Marshal(s)
	tmp := a.haPath() + ".part"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.haPath())
}

func (a *App) haUpdate(fn func(*haLocal)) {
	if s := a.haLoad(); s != nil {
		fn(s)
		a.haSave(s)
	}
}

// mgmtIface is the port the two units talk over: the shared address whose network holds the
// primary's own address.
func mgmtIface(p ha.Pairing) string {
	pa, _ := netip.ParseAddr(p.Primary)
	for _, v := range p.VIPs {
		if pf, err := netip.ParsePrefix(v.CIDR); err == nil && pf.Contains(pa) {
			return v.Iface
		}
	}
	return ""
}

// isPreferred says whether this unit is the Primary: the one that takes the higher priority and the
// one the failback prompt offers to give the role back to.
func (s *haLocal) isPreferred() bool {
	if s.Prefer == "" {
		return s.Role == ha.Primary
	}
	return s.Prefer == s.Role
}

func (s *haLocal) preferRole() ha.Role {
	if s.Prefer == "" {
		return ha.Primary
	}
	return s.Prefer
}

func (s *haLocal) ownAddr() string {
	if s.Role == ha.Standby {
		return s.Pairing.Standby
	}
	return s.Pairing.Primary
}

// local is the pairing as this unit sees it: the shared addresses sit on this unit's own port names.
func (s *haLocal) local() ha.Pairing {
	p := s.Pairing
	p.VIPs = append([]ha.VIP(nil), p.VIPs...)
	if s.Role == ha.Standby {
		for i := range p.VIPs {
			p.VIPs[i].Iface = renamePort(p.VIPs[i].Iface, s.Mapping)
		}
	}
	return p
}

func (s *haLocal) peerAddr() string {
	if s.Role == ha.Standby {
		return s.Pairing.Primary
	}
	return s.Pairing.Standby
}

// heartCarrier says whether a port has a cable plugged in and connected. Tests replace it.
var heartCarrier = func(dev string) bool {
	b, err := os.ReadFile(filepath.Join("/sys/class/net", dev, "carrier"))
	return err == nil && strings.TrimSpace(string(b)) == "1"
}

// heart is this unit's own address on the cable and the partner's, when a cable is set up.
func (s *haLocal) heart() (own netip.Prefix, peer netip.Addr, ok bool) {
	if s.HeartDev == "" {
		return
	}
	o, p, err := ha.HeartOwn(s.Pairing.ID, s.Role)
	return o, p, err == nil
}

func (s *haLocal) peerHost() string {
	port := s.PeerPort
	if port == 0 {
		port = ha.LinkPort
	}
	// The cable is used while it is connected; otherwise the shared network carries the same talk.
	if _, peer, ok := s.heart(); ok && heartCarrier(s.HeartDev) {
		return net.JoinHostPort(peer.String(), strconv.Itoa(port))
	}
	return net.JoinHostPort(s.peerAddr(), strconv.Itoa(port))
}

// haActive says whether this unit holds the shared addresses right now.
func (a *App) haActive(s *haLocal) bool {
	if a.haActiveFn != nil {
		return a.haActiveFn()
	}
	for _, v := range s.local().VIPs {
		pf, err := netip.ParsePrefix(v.CIDR)
		if err != nil {
			continue
		}
		ifi, err := net.InterfaceByName(v.Iface)
		if err != nil {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, ad := range addrs {
			if n, ok := ad.(*net.IPNet); ok && n.IP.String() == pf.Addr().String() {
				return true
			}
		}
	}
	return false
}

// haStandbyNow is true on a paired unit that does not hold the shared addresses.
func (a *App) haStandbyNow() bool {
	s := a.haLoad()
	return s != nil && s.Activated && !a.haActive(s)
}

// ---- what the ports do, for the port matching ----

func speedOf(p map[string]any) int {
	n, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(p["speed"])))
	if n <= 0 || p["link"] != true {
		return -1
	}
	return n
}

func (a *App) haPortList() []ha.Port {
	var out []ha.Port
	for _, p := range portsLister() {
		n, _ := p["name"].(string)
		w, _ := p["wifi"].(bool)
		l, _ := p["link"].(bool)
		out = append(out, ha.Port{Name: n, Speed: speedOf(p), Link: l, Wireless: w})
	}
	return out
}

func (a *App) haRoles() []ha.PortRole {
	by := map[string]ha.Port{}
	for _, p := range a.haPortList() {
		by[p.Name] = p
	}
	var out []ha.PortRole
	add := func(id, label, port string) {
		if port == "" {
			return
		}
		sp := by[port].Speed
		if sp < 0 {
			sp = 0
		}
		out = append(out, ha.PortRole{ID: id, Label: label, Port: port, Speed: sp})
	}
	if ifs, err := a.listIfaces(a.Config); err == nil {
		for _, i := range ifs {
			if !i.Enabled {
				continue
			}
			switch {
			case i.Kind == "bridge" && len(i.Members) > 0:
				add("network:"+i.Name, "Network "+i.Name, i.Members[0])
			case i.Kind == "vlan":
				// a VLAN rides on a port already counted by its parent
			case i.Kind == "phys":
				add("network:"+i.Name, "Network "+i.Name, i.Name)
			}
		}
	}
	if wans, err := a.listWans(a.Config); err == nil {
		for _, w := range wans {
			if w.Enabled {
				add("internet:"+w.Name, "Internet "+w.Name, w.Iface)
			}
		}
	}
	return out
}

// haWanDevs are the internet ports that stay down while this unit is standby.
func (a *App) haWanDevs() []string {
	var out []string
	if wans, err := a.listWans(a.Config); err == nil {
		for _, w := range wans {
			if w.Enabled {
				out = append(out, netcfg.WanDevice(netcfg.WAN{Name: w.Name, Iface: w.Iface, Type: w.Type, VLAN: w.VLAN}))
			}
		}
	}
	return out
}

// ---- the services ----

func (a *App) haInstall(s *haLocal) error {
	if a.Net == nil {
		return errNoHelper
	}
	ka, err := ha.KeepalivedConfPrefer(s.local(), s.Role, s.ownAddr(), s.isPreferred())
	if err != nil {
		return err
	}
	ct, err := ha.ConntrackdConf(s.local(), s.Role, s.MgmtIface)
	if err != nil {
		return err
	}
	plan := netd.HAPlan{Enabled: true, Keepalived: ka, Conntrackd: ct, WanDevs: a.haWanDevs()}
	if a.haWarmOn() {
		sp := a.haWanSplit(true)
		plan.WanDevs, plan.WarmDevs, plan.WanStatic, plan.WanMACs = sp.Down, sp.Warm, sp.Static, a.haPartnerMACs(s)
	}
	if own, _, ok := s.heart(); ok {
		if ct, err = ha.HeartConntrackdConf(s.local(), s.Role, s.HeartDev); err != nil {
			return err
		}
		plan.Conntrackd, plan.HeartDev, plan.HeartCIDR = ct, s.HeartDev, own.String()
	}
	return a.Net.HAApply(plan)
}

// haSetFirewallPeer records the partner's address for the firewall's partner rules.
func (a *App) haSetFirewallPeer(s *haLocal) {
	if s == nil {
		a.Config.Exec(`DELETE FROM settings WHERE key='ha.peer'`)
	} else {
		a.Config.SetSetting("ha.peer", s.peerAddr())
	}
	a.publishFW()
}

// ---- the link ----

func (a *App) haLink() *ha.Link {
	return &ha.Link{
		Peer: func() string {
			if s := a.haLoad(); s != nil {
				return s.PeerFP
			}
			return ""
		},
		OnJoin: func(fp string, port int) error {
			s := a.haLoad()
			if s == nil || s.Role != ha.Primary {
				return errors.New("this unit is not waiting for a backup")
			}
			if s.PeerFP != "" && s.PeerFP != fp {
				return errors.New("a backup is already paired with this unit; remove it first")
			}
			s.PeerFP, s.PeerPort = fp, port
			if err := a.haSave(s); err != nil {
				return err
			}
			haKick()
			return nil
		},
		OnSnapshot: a.haReceive,
		OnPrefer:   a.haOnPrefer,
		OnUpdate:   a.haOnUpdate,
		Status:     a.haLocalStatus,
		OnHandover: func() error {
			st := a.haLoad()
			if st == nil || !st.Activated || !a.haActive(st) {
				return errors.New("this unit is not the active one")
			}
			// Restarting the shared-address service on the active unit makes it step aside; it comes back as the standby.
			return a.haInstall(st)
		},
		OnLeave: func() error {
			if st := a.haLoad(); st != nil {
				return a.haLeave(st)
			}
			return nil
		},
		Overview: func() ha.Overview { return ha.Overview{Roles: a.haRoles(), Version: a.Cfg.Version} },
	}
}

func (a *App) haLocalStatus() ha.Status {
	s := a.haLoad()
	st := ha.Status{Version: a.Cfg.Version, Time: time.Now().Unix(), WanMACs: a.haOwnWanMACs(), Update: update.ReadState(a.updateState())}
	if r, ok := update.ReadResult(a.updateState()); ok {
		st.ResultTo, st.Result = r.To, "failed"
		if r.OK {
			st.Result = "ok"
		}
	}
	if a.haApplyMu.TryLock() {
		a.haApplyMu.Unlock()
	} else {
		st.Applying = true
	}
	if s != nil {
		st.Role, st.Seq, st.Active, st.Prefer = string(s.Role), s.Seq, a.haActive(s), string(s.preferRole())
	}
	return st
}

// haServe keeps the link listener running while a pairing exists.
func (a *App) haServe(ctx context.Context) {
	var cancel context.CancelFunc
	var running string
	stop := func() {
		if cancel != nil {
			cancel()
			cancel, running = nil, ""
		}
	}
	defer stop()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		s := a.haLoad()
		want := ""
		if s != nil {
			want = s.Self.FP
		}
		if want != running {
			stop()
			if s != nil {
				l := a.haLink()
				l.Self, l.Secret = s.Self, s.Pairing.Secret
				addr := a.haListen
				if addr == "" {
					addr = net.JoinHostPort("", strconv.Itoa(ha.LinkPort))
				}
				if ln, err := net.Listen("tcp", addr); err == nil {
					c, cn := context.WithCancel(ctx)
					cancel, running = cn, want
					a.haBound.Store(ln.Addr().String())
					go l.Serve(c, ln)
				} else {
					a.haUpdate(func(x *haLocal) { x.LastError = "the link port could not be opened: " + err.Error() })
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ---- keeping the settings the same ----

func (a *App) configHash() string {
	tmp := filepath.Join(a.backupDir(), ".hash-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	os.MkdirAll(a.backupDir(), 0o700)
	defer os.Remove(tmp)
	if _, err := a.Config.Exec(`VACUUM INTO ?`, tmp); err != nil {
		return ""
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", fnvSum(b))
}

func fnvSum(b []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}

// haPush sends the settings to the partner. Only the active unit sends.
func (a *App) haPush(force bool) error {
	s := a.haLoad()
	if s == nil || s.PeerFP == "" {
		return errors.New("not paired yet")
	}
	if !a.haActive(s) && !force {
		return nil
	}
	hash := a.configHash()
	if !force && hash != "" && hash == s.LastHash && time.Since(time.Unix(s.SyncedAt, 0)) < 5*time.Minute {
		return nil
	}
	path, err := a.saveBackup("ha-sync")
	if err != nil {
		return err
	}
	defer os.Remove(path)
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	seq := s.Seq + 1
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	hdr := map[string]string{"X-GH-Seq": strconv.FormatInt(seq, 10), "X-GH-Version": a.Cfg.Version}
	// A standby that is running the network holds port names of its own. The Primary needs to know
	// how they map back, so the settings it takes in fit its ports.
	if s.Role == ha.Standby && len(s.Mapping) > 0 {
		hdr["X-GH-Map"] = encodeMap(s.Mapping)
	}
	_, err = ha.Call(ctx, s.peerHost(), s.Self, s.PeerFP, "POST", "/ha/snapshot", body, hdr, "")
	if err != nil {
		a.haUpdate(func(x *haLocal) { x.LastError = "the partner did not take the settings: " + err.Error() })
		return err
	}
	a.haUpdate(func(x *haLocal) { x.Seq, x.SyncedAt, x.LastHash, x.LastError = seq, time.Now().Unix(), hash, "" })
	return nil
}

func (a *App) haSyncLoop(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-haRT.kick:
			time.Sleep(2 * time.Second)
		}
		if s := a.haLoad(); s != nil && s.PeerFP != "" {
			a.DHCP.Silent.Store(a.haStandbyNow())
			if a.haActive(s) || s.SyncedAt == 0 && s.Role == ha.Primary {
				a.haPush(false)
			}
		} else {
			a.DHCP.Silent.Store(false)
		}
	}
}

// haReceive takes a copy of the settings from the partner.
func (a *App) haReceive(body []byte, seq int64, version, mapping string) error {
	s := a.haLoad()
	if s == nil {
		return errors.New("this unit is not paired")
	}
	if s.Role != ha.Standby && s.Activated && a.haActive(s) {
		return errors.New("this unit is the active one; it does not take settings")
	}
	dir := a.backupDir()
	os.MkdirAll(dir, 0o700)
	f, err := os.CreateTemp(dir, ".ha-in-*"+bkExt)
	if err != nil {
		return err
	}
	f.Write(body)
	f.Close()
	defer os.Remove(f.Name())
	if _, err := a.restoreBackup(f.Name(), "", "partner unit"); err != nil {
		a.haUpdate(func(x *haLocal) { x.LastError = "the copied settings were not taken: " + err.Error() })
		return err
	}
	ms := s.Mapping
	if s.Role == ha.Primary {
		ms = swapMap(decodeMap(mapping))
	}
	if err := a.haLocalize(s, ms); err != nil {
		return err
	}
	a.haUpdate(func(x *haLocal) { x.Seq, x.SyncedAt, x.LastError = seq, time.Now().Unix(), "" })
	a.afterHACopy(s)
	return nil
}

// haLocalize makes copied settings fit this unit: its port names, its own address, and the partner
// rule in the firewall. It runs inside one transaction.
func (a *App) haLocalize(s *haLocal, ms []ha.Match) error {
	tx, err := a.Config.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(ms) > 0 {
		if err := remapPorts(tx, ms); err != nil {
			return err
		}
	}
	if s.MgmtIface != "" {
		pf := ""
		for _, v := range s.local().VIPs {
			if v.Iface == s.MgmtIface {
				if p, err := netip.ParsePrefix(v.CIDR); err == nil {
					pf = fmt.Sprintf("%s/%d", s.ownAddr(), p.Bits())
				}
			}
		}
		if pf != "" {
			if _, err := tx.Exec(`UPDATE interfaces SET ipv4_cidr=? WHERE name=?`, pf, s.MgmtIface); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.Config.SetSetting("ha.peer", s.peerAddr())
	a.haSetHeartSetting(s)
	return nil
}

// haSetHeartSetting tells the firewall about the cable. It is kept out of the copied settings.
func (a *App) haSetHeartSetting(s *haLocal) {
	if _, peer, ok := s.heart(); ok && s != nil {
		a.Config.SetSetting("ha.heart", s.HeartDev+"|"+peer.String())
		return
	}
	a.Config.Exec(`DELETE FROM settings WHERE key='ha.heart'`)
}

func renamePort(n string, ms []ha.Match) string {
	base, rest, dotted := strings.Cut(n, ".")
	base = ha.Rename(base, ms)
	if dotted {
		return base + "." + rest
	}
	return base
}

// remapPorts renames the primary's ports to this unit's in every place a port name is saved.
func remapPorts(tx *sql.Tx, ms []ha.Match) error {
	type row struct {
		id           int64
		name, parent string
		members      string
	}
	rows, err := tx.Query(`SELECT id,name,parent,members FROM interfaces`)
	if err != nil {
		return err
	}
	var all []row
	for rows.Next() {
		var r row
		rows.Scan(&r.id, &r.name, &r.parent, &r.members)
		all = append(all, r)
	}
	rows.Close()
	for _, r := range all {
		var mem []string
		json.Unmarshal([]byte(r.members), &mem)
		for i := range mem {
			mem[i] = renamePort(mem[i], ms)
		}
		mb, _ := json.Marshal(mem)
		if r.members == "" {
			mb = []byte("")
		}
		if _, err := tx.Exec(`UPDATE interfaces SET name=?, parent=?, members=? WHERE id=?`, renamePort(r.name, ms), renamePort(r.parent, ms), string(mb), r.id); err != nil {
			return fmt.Errorf("renaming ports: %w", err)
		}
	}
	for _, t := range []string{"wan_links", "dhcp_scopes", "wifi_radios"} {
		rows, err := tx.Query(`SELECT rowid,iface FROM ` + t)
		if err != nil {
			return err
		}
		type pr struct {
			id int64
			n  string
		}
		var ps []pr
		for rows.Next() {
			var p pr
			rows.Scan(&p.id, &p.n)
			ps = append(ps, p)
		}
		rows.Close()
		for _, p := range ps {
			if nn := renamePort(p.n, ms); nn != p.n {
				if _, err := tx.Exec(`UPDATE `+t+` SET iface=? WHERE rowid=?`, nn, p.id); err != nil {
					return fmt.Errorf("renaming ports: %w", err)
				}
			}
		}
	}
	return nil
}

// encodeMap, decodeMap and swapMap carry the port matching to the Primary, and turn it around.
// Only the port names travel.
func encodeMap(ms []ha.Match) string {
	out := make([]ha.Match, 0, len(ms))
	for _, m := range ms {
		out = append(out, ha.Match{Primary: m.Primary, Backup: m.Backup})
	}
	b, _ := json.Marshal(out)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeMap(s string) []ha.Match {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	var ms []ha.Match
	if json.Unmarshal(b, &ms) != nil {
		return nil
	}
	return ms
}

func swapMap(ms []ha.Match) []ha.Match {
	out := make([]ha.Match, 0, len(ms))
	for _, m := range ms {
		out = append(out, ha.Match{Primary: m.Backup, Backup: m.Primary})
	}
	return out
}
