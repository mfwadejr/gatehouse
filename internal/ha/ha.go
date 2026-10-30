// Package ha holds the tested building blocks for running two Gatehouse machines as an
// active/standby pair: the pairing code, the keepalived (VRRP) settings, and the rule for what a
// standby unit may be asked to do. It is NOT connected to the console yet. Nothing here starts a
// service or changes a machine, so having this package changes nothing about how Gatehouse runs.
package ha

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

const codePrefix = "GATEHOUSE-PAIR-1:"

// NotifyPath is the script keepalived runs when a unit becomes active or standby.
const NotifyPath = "/usr/lib/gatehouse/ha-notify"

// VIP is a shared address held by whichever unit is active, on one network.
type VIP struct {
	Iface string `json:"iface"` // the interface on the unit, such as br0
	CIDR  string `json:"cidr"`  // the shared address with its prefix, such as 192.168.1.1/24
}

// Pairing is everything the second machine needs to join, carried in a single code.
type Pairing struct {
	V       int    `json:"v"`
	ID      string `json:"id"`      // names this pair; also seeds the VRRP router ids
	Secret  string `json:"secret"`  // shared by both units; authenticates them to each other
	Primary string `json:"primary"` // the primary's own management address
	Standby string `json:"standby"` // the standby's own management address
	VIPs    []VIP  `json:"vips"`
	// FP is the primary's link certificate fingerprint; the standby pins it.
	FP string `json:"fp,omitempty"`
	// Port is the primary's link port; zero means LinkPort.
	Port int `json:"port,omitempty"`
}

// NewSecret makes a random secret for a new pair.
func NewSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Validate checks a pairing before a code is made from it or after one is read.
func (p Pairing) Validate() error {
	if p.V != 1 {
		return errors.New("this pairing code is from a different version of Gatehouse")
	}
	if len(p.Secret) < 24 {
		return errors.New("the pairing code is damaged: its secret is too short")
	}
	for _, a := range []struct{ what, v string }{{"primary", p.Primary}, {"standby", p.Standby}} {
		ip, err := netip.ParseAddr(a.v)
		if err != nil || !ip.Is4() {
			return fmt.Errorf("the %s unit's own address %q is not an IPv4 address", a.what, a.v)
		}
	}
	if p.Primary == p.Standby {
		return errors.New("the two units need different management addresses")
	}
	if len(p.VIPs) == 0 || len(p.VIPs) > 16 {
		return errors.New("choose between 1 and 16 shared addresses")
	}
	seen := map[string]bool{}
	for _, v := range p.VIPs {
		pf, err := netip.ParsePrefix(v.CIDR)
		if err != nil || !pf.Addr().Is4() {
			return fmt.Errorf("%q is not a shared address like 192.168.1.1/24", v.CIDR)
		}
		if v.Iface == "" || strings.ContainsAny(v.Iface, " \t\n{}\"#;") || len(v.Iface) > 15 {
			return fmt.Errorf("%q is not a usable network port name", v.Iface)
		}
		if seen[v.Iface] {
			return fmt.Errorf("%s has two shared addresses; one per network is supported", v.Iface)
		}
		seen[v.Iface] = true
		if v.CIDR == p.Primary+"/"+fmt.Sprint(pf.Bits()) || pf.Addr().String() == p.Primary || pf.Addr().String() == p.Standby {
			return fmt.Errorf("the shared address %s must be different from both units' own addresses", pf.Addr())
		}
	}
	return nil
}

// Encode turns a pairing into the code the person copies to the second machine.
func Encode(p Pairing) (string, error) {
	p.V = 1
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, _ := json.Marshal(p)
	return codePrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// Decode reads a code back, refusing anything damaged.
func Decode(code string) (Pairing, error) {
	var p Pairing
	code = strings.Join(strings.Fields(code), "")
	if !strings.HasPrefix(code, codePrefix) {
		return p, errors.New("that is not a Gatehouse pairing code")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, codePrefix))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return p, errors.New("the pairing code is damaged; copy the whole code again")
	}
	return p, p.Validate()
}

// Role is what a unit is doing in the pair.
type Role string

const (
	Primary Role = "primary" // the unit that normally holds the shared addresses
	Standby Role = "standby"
)

// KeepalivedConf writes the VRRP settings for one unit. Each network gets one VRRP instance that
// holds that network's shared address. Properties chosen on purpose:
//   - nopreempt: a recovered unit stays standby until the owner hands the role back, so the pair
//     never flaps;
//   - unicast between the units' own addresses, so nothing depends on multicast;
//   - a fast advertisement (0.1 s) for a failover in about a second;
//   - the primary starts with the higher priority, the standby lower, both as BACKUP so the first
//     election decides who is active.
func KeepalivedConf(p Pairing, role Role, ownAddr string) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if role != Primary && role != Standby {
		return "", errors.New("unknown role")
	}
	peer, own := p.Standby, p.Primary
	prio := 150
	if role == Standby {
		peer, own, prio = p.Primary, p.Standby, 100
	}
	if ownAddr != "" && ownAddr != own {
		return "", fmt.Errorf("this unit's address %s does not match the pairing (%s)", ownAddr, own)
	}
	vips := append([]VIP(nil), p.VIPs...)
	sort.Slice(vips, func(i, j int) bool { return vips[i].Iface < vips[j].Iface })
	var b strings.Builder
	b.WriteString("# Written by Gatehouse. Changes here are replaced.\n")
	b.WriteString("global_defs {\n  vrrp_version 3\n  enable_script_security\n  script_user root\n}\n\n")
	for i, v := range vips {
		fmt.Fprintf(&b, "vrrp_instance GH_%d {\n", i+1)
		fmt.Fprintf(&b, "  state BACKUP\n  interface %s\n  virtual_router_id %d\n  priority %d\n  nopreempt\n  advert_int 0.1\n", v.Iface, routerID(p.ID, i), prio)
		fmt.Fprintf(&b, "  unicast_src_ip %s\n  unicast_peer {\n    %s\n  }\n", own, peer)
		fmt.Fprintf(&b, "  authentication {\n    auth_type PASS\n    auth_pass %s\n  }\n", passFor(p.Secret))
		fmt.Fprintf(&b, "  virtual_ipaddress {\n    %s dev %s\n  }\n  notify %s\n}\n\n", v.CIDR, v.Iface, NotifyPath)
	}
	return b.String(), nil
}

// routerID gives each network its own VRRP id (1..254), the same on both units.
func routerID(pairID string, i int) int {
	h := 0
	for _, c := range pairID {
		h = (h*31 + int(c)) % 200
	}
	return 1 + (h+i)%254
}

// VRRP's password field holds 8 characters, so use the first eight of the secret.
func passFor(secret string) string {
	if len(secret) > 8 {
		return secret[:8]
	}
	return secret
}

// Allowed says whether a standby unit should carry out a request. The standby shows status and
// can be restarted, shut down, updated or made active; everything else is changed on the active
// unit and copied across. method is the HTTP method and path the URL path.
func Allowed(role Role, method, path string) bool {
	if role != Standby {
		return true
	}
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		return true
	}
	for _, p := range []string{"/api/ha", "/api/auth", "/api/session", "/api/system/restart", "/api/system/shutdown", "/api/updates"} {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}
