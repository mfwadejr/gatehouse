package dhcp

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gatehouse/internal/store"
)

// Scope is one DHCP scope as the engine uses it.
type Scope struct {
	ID      int64
	Name    string
	Iface   string
	Mode    string // serve, relay, off
	Net     *net.IPNet
	Start   net.IP
	End     net.IP
	Router  net.IP
	DNS     []net.IP
	Domain  string
	NTP     []net.IP
	Lease   time.Duration
	Relay   net.IP
	Enabled bool
}

// Ingress says where a request arrived.
type Ingress struct {
	Iface    string
	ServerIP net.IP // our address on that interface
}

const (
	offerHold    = 60 * time.Second
	declineHold  = time.Hour
	minLease     = 2 * time.Minute
	maxScanRange = 1 << 17
)

// Engine decides what to answer. It owns no sockets.
type Engine struct {
	db  *store.DB
	now func() time.Time

	mu     sync.Mutex // serialises allocation
	scopes atomic.Pointer[[]*Scope]
	snap   atomic.Pointer[Snapshot]

	// OnChange is called after the lease table changes (for live widgets).
	OnChange func()
	// OnEvent receives notable events: "ack", "nak", "decline", "release".
	OnEvent func(kind, mac string, ip net.IP, scope string)
}

func NewEngine(db *store.DB) *Engine {
	e := &Engine{db: db, now: time.Now}
	empty := []*Scope{}
	e.scopes.Store(&empty)
	e.snap.Store(&Snapshot{})
	return e
}

func splitIPs(s string) []net.IP {
	var out []net.IP
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if ip := net.ParseIP(strings.TrimSpace(f)).To4(); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

// Reload reads the scopes from the database and refreshes the name snapshot.
func (e *Engine) Reload() error {
	rows, err := e.db.Query(`SELECT id,name,iface,mode,subnet,range_start,range_end,router,dns,domain,ntp,lease_seconds,relay_addr,enabled FROM dhcp_scopes ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var list []*Scope
	for rows.Next() {
		var s Scope
		var subnet, rs, re, router, dnsS, ntp, relay string
		var lease, en int
		if err := rows.Scan(&s.ID, &s.Name, &s.Iface, &s.Mode, &subnet, &rs, &re, &router, &dnsS, &s.Domain, &ntp, &lease, &relay, &en); err != nil {
			return err
		}
		_, n, err := net.ParseCIDR(subnet)
		if err != nil || n.IP.To4() == nil {
			continue
		}
		s.Net = n
		s.Start, s.End = ip4(rs), ip4(re)
		s.Router = ip4(router)
		s.DNS, s.NTP = splitIPs(dnsS), splitIPs(ntp)
		s.Relay = ip4(relay)
		s.Lease = time.Duration(lease) * time.Second
		if s.Lease < minLease {
			s.Lease = 24 * time.Hour
		}
		s.Enabled = en == 1
		list = append(list, &s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	e.scopes.Store(&list)
	e.rebuildSnapshot()
	return nil
}

// Scopes returns the loaded scopes.
func (e *Engine) Scopes() []*Scope { return *e.scopes.Load() }

// ScopeFor picks the scope that answers a request.
func (e *Engine) ScopeFor(req *Packet, in Ingress) *Scope {
	relayed := req.GIAddr != nil && !req.GIAddr.Equal(net.IPv4zero)
	for _, s := range e.Scopes() {
		if !s.Enabled || s.Mode == "off" {
			continue
		}
		if relayed {
			if s.Mode == "relay" && s.Net.Contains(req.GIAddr) && (s.Relay == nil || s.Relay.Equal(req.GIAddr)) {
				return s
			}
			continue
		}
		if s.Mode == "serve" && s.Iface == in.Iface && in.ServerIP != nil && s.Net.Contains(in.ServerIP) {
			return s
		}
	}
	return nil
}

func cleanHostname(b []byte) string {
	var out []byte
	for _, c := range strings.ToLower(string(b)) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			out = append(out, byte(c))
		case c == '.' || c == '_' || c == ' ':
			if c == '.' {
				return string(out) // keep the first label only
			}
			out = append(out, '-')
		}
		if len(out) == 63 {
			break
		}
	}
	return strings.Trim(string(out), "-")
}

func macKey(h net.HardwareAddr) string { return strings.ToLower(h.String()) }

type lease struct {
	ip      net.IP
	mac     string
	expires time.Time
	state   string
}

func (e *Engine) reservationFor(scope int64, mac string) net.IP {
	var ip string
	if err := e.db.QueryRow(`SELECT ip FROM dhcp_reservations WHERE scope_id=? AND lower(mac)=?`, scope, mac).Scan(&ip); err != nil {
		return nil
	}
	return ip4(ip)
}

// reservedForOther reports whether ip is reserved for a different MAC.
func (e *Engine) reservedForOther(scope int64, ip net.IP, mac string) bool {
	var m string
	err := e.db.QueryRow(`SELECT lower(mac) FROM dhcp_reservations WHERE scope_id=? AND ip=?`, scope, ip.String()).Scan(&m)
	return err == nil && m != mac
}

func (e *Engine) leaseAt(scope int64, ip net.IP) *lease {
	var l lease
	var exp int64
	var s string
	err := e.db.QueryRow(`SELECT mac,expires,state FROM dhcp_leases WHERE scope_id=? AND ip=?`, scope, ip.String()).Scan(&l.mac, &exp, &l.state)
	if err != nil {
		return nil
	}
	l.ip, l.expires = ip, time.Unix(exp, 0)
	_ = s
	return &l
}

func (e *Engine) heldByOther(scope int64, ip net.IP, mac string) bool {
	if e.reservedForOther(scope, ip, mac) {
		return true
	}
	l := e.leaseAt(scope, ip)
	if l == nil {
		return false
	}
	if l.state == "released" || !l.expires.After(e.now()) {
		return false
	}
	return l.mac != mac
}

func inRange(s *Scope, ip net.IP) bool {
	if s.Start == nil || s.End == nil || ip == nil {
		return false
	}
	n := ipToU32(ip)
	return n >= ipToU32(s.Start) && n <= ipToU32(s.End) && s.Net.Contains(ip)
}

var errNoAddress = errors.New("dhcp: no free address")

// choose picks the address to offer.
func (e *Engine) choose(s *Scope, mac string, requested net.IP, serverIP net.IP) (net.IP, error) {
	if ip := e.reservationFor(s.ID, mac); ip != nil {
		return ip, nil
	}
	reserved := func(ip net.IP) bool { return ip.Equal(serverIP) || (s.Router != nil && ip.Equal(s.Router)) }
	// An address this device already holds.
	var cur string
	if err := e.db.QueryRow(`SELECT ip FROM dhcp_leases WHERE scope_id=? AND lower(mac)=? AND state IN ('active','offered') ORDER BY expires DESC LIMIT 1`, s.ID, mac).Scan(&cur); err == nil {
		if ip := ip4(cur); inRange(s, ip) && !reserved(ip) && !e.heldByOther(s.ID, ip, mac) {
			return ip, nil
		}
	}
	if requested != nil && inRange(s, requested) && !reserved(requested) && !e.heldByOther(s.ID, requested, mac) {
		if l := e.leaseAt(s.ID, requested); l == nil || l.state != "declined" || !l.expires.After(e.now()) {
			return requested, nil
		}
	}
	lo, hi := ipToU32(s.Start), ipToU32(s.End)
	if hi < lo || hi-lo > maxScanRange {
		return nil, errNoAddress
	}
	for n := lo; n <= hi; n++ {
		ip := u32ToIP(n)
		if reserved(ip) || e.heldByOther(s.ID, ip, mac) {
			continue
		}
		if l := e.leaseAt(s.ID, ip); l != nil && l.state == "declined" && l.expires.After(e.now()) {
			continue
		}
		return ip, nil
	}
	return nil, errNoAddress
}

func (e *Engine) putLease(s *Scope, ip net.IP, mac, host, cid, state string, dur time.Duration) error {
	now := e.now()
	_, err := e.db.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,hostname,client_id,start,expires,state) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(scope_id,ip) DO UPDATE SET mac=excluded.mac,hostname=CASE WHEN excluded.hostname<>'' THEN excluded.hostname ELSE dhcp_leases.hostname END,
		client_id=excluded.client_id,start=excluded.start,expires=excluded.expires,state=excluded.state`,
		s.ID, ip.String(), mac, host, cid, now.Unix(), now.Add(dur).Unix(), state)
	return err
}

func (e *Engine) reply(s *Scope, in Ingress, req *Packet, typ byte) *Packet {
	r := &Packet{Op: 2, HType: req.HType, HLen: req.HLen, XID: req.XID, Flags: req.Flags,
		GIAddr: req.GIAddr, CHAddr: req.CHAddr,
		CIAddr: net.IPv4zero.To4(), YIAddr: net.IPv4zero.To4(), SIAddr: net.IPv4zero.To4()}
	r.Set(OptMsgType, []byte{typ})
	server := in.ServerIP
	if server == nil && s.Router != nil {
		server = s.Router
	}
	if server != nil {
		r.Set(OptServerID, server.To4())
	}
	return r
}

func (e *Engine) addConfig(r *Packet, s *Scope, in Ingress, lease time.Duration) {
	if lease > 0 {
		secs := uint32(lease / time.Second)
		r.SetU32(OptLeaseTime, secs)
		r.SetU32(OptRenewal, secs/2)
		r.SetU32(OptRebinding, secs*7/8)
	}
	r.Set(OptSubnetMask, []byte(net.IP(s.Net.Mask).To4()))
	bc := make(net.IP, 4)
	base := s.Net.IP.To4()
	for i := range bc {
		bc[i] = base[i] | ^s.Net.Mask[i]
	}
	r.Set(OptBroadcast, bc)
	router := s.Router
	if router == nil {
		router = in.ServerIP
	}
	if router != nil {
		r.SetIPs(OptRouter, []net.IP{router})
	}
	dnsList := s.DNS
	if len(dnsList) == 0 && in.ServerIP != nil {
		dnsList = []net.IP{in.ServerIP}
	}
	r.SetIPs(OptDNS, dnsList)
	if s.Domain != "" {
		r.Set(OptDomain, []byte(s.Domain))
	}
	r.SetIPs(OptNTP, s.NTP)
}

func (e *Engine) event(kind, mac string, ip net.IP, scope string) {
	if e.OnEvent != nil {
		e.OnEvent(kind, mac, ip, scope)
	}
}

// Handle answers one request. A nil reply means stay silent.
func (e *Engine) Handle(req *Packet, in Ingress) (*Packet, error) {
	if req.Op != 1 || req.HType != 1 || len(req.CHAddr) != 6 {
		return nil, nil
	}
	s := e.ScopeFor(req, in)
	if s == nil {
		return nil, nil
	}
	mac := macKey(req.CHAddr)
	host := cleanHostname(req.Options[OptHostname])
	cid := fmt.Sprintf("%x", req.Options[OptClientID])
	e.mu.Lock()
	defer e.mu.Unlock()
	switch req.Type() {
	case Discover:
		ip, err := e.choose(s, mac, req.IPOption(OptRequestedIP), in.ServerIP)
		if err != nil {
			return nil, err
		}
		if err := e.putLease(s, ip, mac, host, cid, "offered", offerHold); err != nil {
			return nil, err
		}
		r := e.reply(s, in, req, Offer)
		r.YIAddr = ip
		e.addConfig(r, s, in, s.Lease)
		return r, nil

	case Request:
		if sid := req.IPOption(OptServerID); sid != nil && in.ServerIP != nil && !sid.Equal(in.ServerIP) {
			e.db.Exec(`DELETE FROM dhcp_leases WHERE scope_id=? AND lower(mac)=? AND state='offered'`, s.ID, mac)
			return nil, nil // the client picked another server
		}
		want := req.IPOption(OptRequestedIP)
		renewing := false
		if want == nil && req.CIAddr != nil && !req.CIAddr.Equal(net.IPv4zero) {
			want, renewing = req.CIAddr, true
		}
		if want == nil {
			return nil, nil
		}
		ok := s.Net.Contains(want)
		if ok {
			if res := e.reservationFor(s.ID, mac); res != nil {
				ok = res.Equal(want)
			} else {
				ok = inRange(s, want) && !e.heldByOther(s.ID, want, mac) && !want.Equal(in.ServerIP) &&
					!(s.Router != nil && want.Equal(s.Router))
			}
		}
		if !ok {
			e.event("nak", mac, want, s.Name)
			n := e.reply(s, in, req, Nak)
			n.Set(OptMessage, []byte("requested address not available"))
			return n, nil
		}
		if err := e.putLease(s, want, mac, host, cid, "active", s.Lease); err != nil {
			return nil, err
		}
		// Anything else this device held in the scope is no longer needed.
		e.db.Exec(`DELETE FROM dhcp_leases WHERE scope_id=? AND lower(mac)=? AND ip<>? AND state IN ('offered','active')`, s.ID, mac, want.String())
		r := e.reply(s, in, req, Ack)
		r.YIAddr = want
		if renewing {
			r.CIAddr = req.CIAddr
		}
		e.addConfig(r, s, in, s.Lease)
		e.event("ack", mac, want, s.Name)
		e.changed()
		return r, nil

	case Decline:
		if ip := req.IPOption(OptRequestedIP); ip != nil {
			e.putLease(s, ip, "declined", "", "", "declined", declineHold)
			e.event("decline", mac, ip, s.Name)
			e.changed()
		}
		return nil, nil

	case Release:
		if ip := req.CIAddr; ip != nil && !ip.Equal(net.IPv4zero) {
			e.db.Exec(`UPDATE dhcp_leases SET state='released',expires=? WHERE scope_id=? AND ip=? AND lower(mac)=?`, e.now().Unix(), s.ID, ip.String(), mac)
			e.event("release", mac, ip, s.Name)
			e.changed()
		}
		return nil, nil

	case Inform:
		r := e.reply(s, in, req, Ack)
		r.CIAddr = req.CIAddr
		e.addConfig(r, s, in, 0)
		return r, nil
	}
	return nil, nil
}

func (e *Engine) changed() {
	e.rebuildSnapshot()
	if e.OnChange != nil {
		e.OnChange()
	}
}

// Expire removes dead rows; call it periodically.
func (e *Engine) Expire() {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now().Unix()
	res, err := e.db.Exec(`DELETE FROM dhcp_leases WHERE expires<? AND state IN ('offered','released','declined')`, now)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			e.rebuildSnapshot()
		}
	}
	// Active leases are kept after expiry for a day so a returning device gets its address back.
	e.db.Exec(`DELETE FROM dhcp_leases WHERE expires<? AND state='active'`, now-86400)
}

// LeaseRow is a lease as the console shows it.
type LeaseRow struct {
	Scope    string `json:"scope"`
	ScopeID  int64  `json:"scope_id"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Hostname string `json:"hostname"`
	Start    int64  `json:"start"`
	Expires  int64  `json:"expires"`
	State    string `json:"state"`
	Static   bool   `json:"static"`
}

// Leases lists current leases (active, not expired) and reservations that have no lease.
func (e *Engine) Leases() ([]LeaseRow, error) {
	now := e.now().Unix()
	rows, err := e.db.Query(`SELECT s.name,l.scope_id,l.ip,l.mac,l.hostname,l.start,l.expires,l.state,
		EXISTS(SELECT 1 FROM dhcp_reservations r WHERE r.scope_id=l.scope_id AND r.ip=l.ip)
		FROM dhcp_leases l JOIN dhcp_scopes s ON s.id=l.scope_id WHERE l.state='active' AND l.expires>=?`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaseRow
	for rows.Next() {
		var r LeaseRow
		var st int
		if err := rows.Scan(&r.Scope, &r.ScopeID, &r.IP, &r.MAC, &r.Hostname, &r.Start, &r.Expires, &r.State, &st); err != nil {
			return nil, err
		}
		r.Static = st == 1
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return ipToU32(ip4(out[i].IP)) < ipToU32(ip4(out[j].IP)) })
	return out, rows.Err()
}

// Revoke deletes a lease so the address returns to the pool.
func (e *Engine) Revoke(scope int64, ip string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.db.Exec(`DELETE FROM dhcp_leases WHERE scope_id=? AND ip=?`, scope, ip); err != nil {
		return err
	}
	e.changed()
	return nil
}

// Utilisation is how full each scope is.
type Utilisation struct {
	ScopeID int64  `json:"scope_id"`
	Name    string `json:"name"`
	Size    int    `json:"size"`
	Active  int    `json:"active"`
}

func (e *Engine) Utilisation() []Utilisation {
	var out []Utilisation
	now := e.now().Unix()
	for _, s := range e.Scopes() {
		u := Utilisation{ScopeID: s.ID, Name: s.Name}
		if s.Start != nil && s.End != nil && ipToU32(s.End) >= ipToU32(s.Start) {
			u.Size = int(ipToU32(s.End)-ipToU32(s.Start)) + 1
		}
		var n sql.NullInt64
		e.db.QueryRow(`SELECT COUNT(*) FROM dhcp_leases WHERE scope_id=? AND state='active' AND expires>=?`, s.ID, now).Scan(&n)
		u.Active = int(n.Int64)
		out = append(out, u)
	}
	return out
}

// Snapshot is an immutable view used by the resolver and the device list.
type Snapshot struct {
	byName map[string][]net.IP
	byIP   map[string]string
	byMAC  map[string]string // ip -> mac
}

func (e *Engine) rebuildSnapshot() {
	s := &Snapshot{byName: map[string][]net.IP{}, byIP: map[string]string{}, byMAC: map[string]string{}}
	add := func(ipS, mac, host string) {
		ip := ip4(ipS)
		if ip == nil {
			return
		}
		if mac != "" {
			s.byMAC[ip.String()] = mac
		}
		if host == "" {
			return
		}
		s.byName[host] = append(s.byName[host], ip)
		s.byIP[ip.String()] = host
	}
	if rows, err := e.db.Query(`SELECT ip,mac,hostname FROM dhcp_reservations`); err == nil {
		for rows.Next() {
			var ip, mac, h string
			rows.Scan(&ip, &mac, &h)
			add(ip, strings.ToLower(mac), cleanHostname([]byte(h)))
		}
		rows.Close()
	}
	if rows, err := e.db.Query(`SELECT ip,mac,hostname FROM dhcp_leases WHERE state='active' AND expires>=?`, e.now().Unix()); err == nil {
		for rows.Next() {
			var ip, mac, h string
			rows.Scan(&ip, &mac, &h)
			if _, exists := s.byIP[ip]; exists {
				s.byMAC[ip] = strings.ToLower(mac)
				continue // a reservation name wins over what the device announced
			}
			add(ip, strings.ToLower(mac), h)
		}
		rows.Close()
	}
	e.snap.Store(s)
}

// Lookups returns the current name snapshot.
func (e *Engine) Lookups() *Snapshot { return e.snap.Load() }

func (s *Snapshot) LookupHost(name string) []net.IP { return s.byName[strings.ToLower(name)] }

func (s *Snapshot) LookupAddr(ip net.IP) (string, bool) {
	h, ok := s.byIP[ip.String()]
	return h, ok
}

// MACFor returns the MAC of the device holding ip.
func (s *Snapshot) MACFor(ip string) string { return s.byMAC[ip] }
