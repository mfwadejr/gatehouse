package app

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// Spotting an internet connection that cannot be reached from outside: the address Gatehouse
// gets from the provider is either in the shared carrier range (CGNAT) or an ordinary private
// address, which means another router (a provider gateway, a UniFi) sits in front.

var cgnatNet = netip.MustParsePrefix("100.64.0.0/10")

// natClass names what kind of address a connection has.
func natClass(a netip.Addr) string {
	switch {
	case !a.IsValid():
		return "unknown"
	case cgnatNet.Contains(a):
		return "cgnat"
	case a.IsPrivate() || a.IsLinkLocalUnicast():
		return "private"
	default:
		return "public"
	}
}

type natInput struct {
	Name, Address, Gateway string
	Up                     bool
}

type natForward struct {
	Name   string `json:"name"`
	Proto  string `json:"proto"`
	Port   string `json:"port"`
	Target string `json:"target"` // what to point the upstream router at
	Text   string `json:"text"`
}

type natLink struct {
	Name     string       `json:"name"`
	Address  string       `json:"address"`
	Gateway  string       `json:"gateway"`
	Class    string       `json:"class"` // public, cgnat, private, unknown
	Blocked  bool         `json:"blocked"`
	Title    string       `json:"title"`
	Text     string       `json:"text"`
	Options  []string     `json:"options,omitempty"`
	Forwards []natForward `json:"forwards,omitempty"`
}

// buildNat turns what Gatehouse knows about each connection into plain-language findings. The
// ports listed for a double NAT are the ones already set up here, so they can be copied to the
// upstream router as they are.
func buildNat(links []natInput, fwds []fw.ForwardRow) []natLink {
	out := []natLink{}
	for _, l := range links {
		addr, _ := netip.ParseAddr(l.Address)
		n := natLink{Name: l.Name, Address: l.Address, Gateway: l.Gateway, Class: natClass(addr)}
		switch n.Class {
		case "cgnat":
			n.Blocked = true
			n.Title = "Your provider shares one public address between customers"
			n.Text = fmt.Sprintf("%s has the address %s, which is in the range providers use to share one public address between many homes (called CGNAT). Nothing from the internet can reach this network, so port forwards, UPnP, and reaching your home from outside do not work on this connection. Browsing, streaming and most games work normally.", l.Name, l.Address)
			n.Options = []string{
				"Ask your provider for a public address. Many give one free or for a small fee. Say you need \"a public IPv4 address\" or to be taken off CGNAT.",
				"Reach home through a tunnel or remote-access service instead: your own VPN server on a cheap cloud machine, or a service such as Tailscale or Cloudflare Tunnel.",
				"If your provider gives IPv6, devices can be reached over IPv6 with a firewall rule, which does not need a shared IPv4 address.",
			}
		case "private":
			n.Blocked = true
			n.Title = "Another router sits in front of Gatehouse"
			n.Text = fmt.Sprintf("%s has the private address %s, so it is behind another router (%s), such as a provider gateway or a UniFi. Devices on your networks can use the internet, but connections from outside stop at that router before they reach Gatehouse. Port forwards and UPnP do not work from outside until that router passes the traffic on.", l.Name, l.Address, orWord(l.Gateway, "its address is not known"))
			n.Options = []string{
				"Best: put the other router in bridge mode (or IP passthrough) so Gatehouse gets the public address directly. Your provider's help page will name the setting.",
				fmt.Sprintf("Or: on the other router, send the ports you need to Gatehouse's address on that network, %s, using the list below.", l.Address),
				"Or: in the other router, place Gatehouse as its \"DMZ\" or \"exposed host\" device.",
			}
			for _, f := range fwds {
				if !f.Enabled || (f.WAN != "" && f.WAN != l.Name) {
					continue
				}
				port := f.ExtPort
				n.Forwards = append(n.Forwards, natForward{Name: f.Name, Proto: f.Proto, Port: port, Target: l.Address,
					Text: fmt.Sprintf("%s: on the other router, open %s %s and send it to %s (the same port).", f.Name, strings.ToUpper(f.Proto), port, l.Address)})
			}
		case "public":
			n.Title = "Reachable from the internet"
			n.Text = fmt.Sprintf("%s has the public address %s, so port forwards and remote access can work.", l.Name, l.Address)
		default:
			n.Title = "Not checked yet"
			n.Text = fmt.Sprintf("%s has no address yet.", l.Name)
		}
		out = append(out, n)
	}
	return out
}

func (a *App) natReport() []natLink {
	in := []natInput{}
	if a.Net != nil {
		st, _ := a.Net.WanStatus()
		rows, err := a.Config.Query(`SELECT name,iface,type,vlan_id FROM wan_links WHERE enabled=1 ORDER BY priority,id`)
		if err == nil {
			for rows.Next() {
				var name, iface, typ string
				var vlan int
				rows.Scan(&name, &iface, &typ, &vlan)
				l := natInput{Name: name}
				if s, ok := st[name]; ok {
					l.Up, l.Gateway = s.Up, s.Gateway
				}
				if ip := firstV4(fw.WanDevice(iface, typ, vlan, name)); ip != nil {
					l.Address = ip.Addr.String()
				}
				in = append(in, l)
			}
			rows.Close()
		}
	}
	fwds, _ := fw.Forwards(a.Config)
	return buildNat(in, fwds)
}

func (a *App) registerNatRoutes() {
	a.API.Handle("GET /api/internet/nat", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"links": a.natReport()})
	})
}
