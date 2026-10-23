// Package vpn holds the pure logic of the VPN features: keys, addresses and the text a
// client needs. Nothing here touches the system, so all of it is tested directly.
package vpn

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// NewKeyPair makes a WireGuard (Curve25519) key pair, both base64 as WireGuard writes them.
func NewKeyPair() (private, public string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// PublicKey derives the public key from a private one.
func PublicKey(private string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(private))
	if err != nil || len(b) != 32 {
		return "", errors.New("that is not a valid WireGuard key")
	}
	k, err := ecdh.X25519().NewPrivateKey(b)
	if err != nil {
		return "", errors.New("that is not a valid WireGuard key")
	}
	return base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// NewPSK makes a preshared key.
func NewPSK() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// ValidKey reports whether s is a 32 byte base64 key.
func ValidKey(s string) bool {
	b, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(b) == 32
}

// SuggestSubnet picks a /24 for a VPN that does not overlap anything already in use. It
// prefers 10.8.0.0/24, the number people expect, then walks 10.9, 10.10, ...
func SuggestSubnet(used []netip.Prefix) string {
	for third := 8; third < 250; third++ {
		cand := netip.MustParsePrefix(fmt.Sprintf("10.%d.0.0/24", third))
		clash := false
		for _, u := range used {
			if u.Overlaps(cand) {
				clash = true
				break
			}
		}
		if !clash {
			return cand.String()
		}
	}
	return "10.200.0.0/24"
}

// ServerAddress is the router's own address inside a tunnel subnet: the first host.
func ServerAddress(subnet string) (string, error) {
	p, err := netip.ParsePrefix(subnet)
	if err != nil || !p.Addr().Is4() || p.Bits() > 29 || p.Bits() < 16 {
		return "", errors.New("the VPN network must look like 10.8.0.0/24 (between /16 and /29)")
	}
	p = p.Masked()
	return fmt.Sprintf("%s/%d", p.Addr().Next(), p.Bits()), nil
}

// NextPeerAddress returns the lowest free host address in the tunnel's network.
func NextPeerAddress(serverCIDR string, taken []string) (string, error) {
	sp, err := netip.ParsePrefix(serverCIDR)
	if err != nil {
		return "", err
	}
	net := sp.Masked()
	used := map[netip.Addr]bool{sp.Addr(): true}
	for _, t := range taken {
		t = strings.TrimSpace(strings.SplitN(t, ",", 2)[0])
		if p, err := netip.ParsePrefix(t); err == nil {
			used[p.Addr()] = true
		} else if a, err := netip.ParseAddr(t); err == nil {
			used[a] = true
		}
	}
	for a := net.Addr().Next(); net.Contains(a); a = a.Next() {
		last := a.As4()[3]
		if last == 255 && net.Bits() >= 24 {
			break
		}
		if !used[a] && a != net.Addr() {
			// skip the broadcast address of the network
			if b := broadcast(net); a == b {
				break
			}
			return a.String() + "/32", nil
		}
	}
	return "", errors.New("every address in this VPN network is already in use")
}

func broadcast(p netip.Prefix) netip.Addr {
	a := p.Masked().Addr().As4()
	host := 32 - p.Bits()
	v := uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])
	v |= (1 << host) - 1
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// ClientConfig is what a phone or laptop imports.
type ClientConfig struct {
	PrivateKey string
	Address    string   // the device's tunnel address, CIDR
	DNS        string   // the router's tunnel address
	MTU        int      // 0 for default
	ServerKey  string   // the router's public key
	PSK        string   // optional
	Endpoint   string   // host:port
	AllowedIPs []string // everything for "send all traffic", else the home networks
	Keepalive  int
}

// Text renders a wg-quick style file.
func (c ClientConfig) Text() string {
	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\nAddress = %s\n", c.PrivateKey, c.Address)
	if c.DNS != "" {
		fmt.Fprintf(&b, "DNS = %s\n", c.DNS)
	}
	if c.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", c.MTU)
	}
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", c.ServerKey)
	if c.PSK != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", c.PSK)
	}
	fmt.Fprintf(&b, "Endpoint = %s\nAllowedIPs = %s\n", c.Endpoint, strings.Join(c.AllowedIPs, ", "))
	if c.Keepalive > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", c.Keepalive)
	}
	return b.String()
}

// QR renders text as a PNG for scanning with a phone.
func QR(text string) ([]byte, error) { return qrcode.Encode(text, qrcode.Medium, 320) }

// Imported is the useful part of a provider's wg-quick file.
type Imported struct {
	PrivateKey string
	Address    string
	DNS        string
	MTU        int
	PeerKey    string
	PSK        string
	Endpoint   string
	AllowedIPs []string
	Keepalive  int
}

// ParseConf reads a wg-quick file, as providers and other routers hand out.
func ParseConf(text string) (*Imported, error) {
	im := &Imported{}
	section := ""
	peers := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.Trim(line, "[] "))
			if section == "peer" {
				peers++
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch section {
		case "interface":
			switch k {
			case "privatekey":
				im.PrivateKey = v
			case "address":
				// IPv4 first; an IPv6 address on the same line is ignored for now.
				for _, a := range strings.Split(v, ",") {
					a = strings.TrimSpace(a)
					if p, err := netip.ParsePrefix(a); err == nil && p.Addr().Is4() {
						im.Address = a
						break
					}
				}
			case "dns":
				im.DNS = strings.TrimSpace(strings.Split(v, ",")[0])
			case "mtu":
				im.MTU, _ = strconv.Atoi(v)
			}
		case "peer":
			if peers > 1 {
				continue // one server per tunnel
			}
			switch k {
			case "publickey":
				im.PeerKey = v
			case "presharedkey":
				im.PSK = v
			case "endpoint":
				im.Endpoint = v
			case "allowedips":
				for _, a := range strings.Split(v, ",") {
					if a = strings.TrimSpace(a); a != "" {
						im.AllowedIPs = append(im.AllowedIPs, a)
					}
				}
			case "persistentkeepalive":
				im.Keepalive, _ = strconv.Atoi(v)
			}
		}
	}
	switch {
	case !ValidKey(im.PrivateKey):
		return nil, errors.New("that file has no valid PrivateKey; paste the whole configuration file")
	case im.Address == "":
		return nil, errors.New("that file has no IPv4 Address line")
	case !ValidKey(im.PeerKey):
		return nil, errors.New("that file has no valid server PublicKey")
	case im.Endpoint == "":
		return nil, errors.New("that file has no Endpoint (the server address)")
	case len(im.AllowedIPs) == 0:
		return nil, errors.New("that file has no AllowedIPs")
	}
	return im, nil
}

// SplitFull separates "send everything" ranges from specific ones. Used for client mode,
// where sending everything through a provider needs more than a plain route.
func SplitFull(ips []string) (specific []string, full bool) {
	for _, a := range ips {
		if p, err := netip.ParsePrefix(a); err == nil && p.Bits() == 0 {
			full = true
			continue
		}
		specific = append(specific, a)
	}
	sort.Strings(specific)
	return
}
