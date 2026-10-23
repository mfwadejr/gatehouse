package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/dhcp"
)

func contextTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

type scopeIn struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Iface      string `json:"iface"`
	Mode       string `json:"mode"`
	Subnet     string `json:"subnet"`
	RangeStart string `json:"range_start"`
	RangeEnd   string `json:"range_end"`
	Router     string `json:"router"`
	DNS        string `json:"dns"`
	Domain     string `json:"domain"`
	NTP        string `json:"ntp"`
	Lease      int    `json:"lease_seconds"`
	Relay      string `json:"relay_addr"`
	Enabled    bool   `json:"enabled"`
}

func validIPList(s string) bool {
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if net.ParseIP(f) == nil {
			return false
		}
	}
	return true
}

func (in *scopeIn) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("a name is required")
	}
	switch in.Mode {
	case "serve", "relay", "off":
	default:
		return errors.New("mode must be serve, relay or off")
	}
	_, n, err := net.ParseCIDR(in.Subnet)
	if err != nil || n.IP.To4() == nil {
		return errors.New("the network must look like 192.168.1.0/24")
	}
	if in.Mode == "serve" || in.Mode == "relay" {
		s, e := net.ParseIP(in.RangeStart).To4(), net.ParseIP(in.RangeEnd).To4()
		if s == nil || e == nil || !n.Contains(s) || !n.Contains(e) {
			return errors.New("the address range must be inside the network")
		}
		if ipLess(e, s) {
			return errors.New("the range ends before it starts")
		}
	}
	for _, f := range []struct{ v, what string }{{in.Router, "gateway"}, {in.Relay, "relay address"}} {
		if f.v != "" && net.ParseIP(f.v).To4() == nil {
			return errors.New("the " + f.what + " is not a valid address")
		}
	}
	if !validIPList(in.DNS) || !validIPList(in.NTP) {
		return errors.New("DNS and time servers must be addresses")
	}
	if in.Lease != 0 && (in.Lease < 120 || in.Lease > 30*86400) {
		return errors.New("lease time must be between 2 minutes and 30 days")
	}
	if in.Lease == 0 {
		in.Lease = 86400
	}
	return nil
}

func ipLess(a, b net.IP) bool {
	for i := 0; i < 4; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func (a *App) registerDHCPRoutes() {
	s := a.API
	s.Handle("GET /api/dhcp/scopes", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rows, err := a.Config.Query(`SELECT id,name,iface,mode,subnet,range_start,range_end,router,dns,domain,ntp,lease_seconds,relay_addr,enabled FROM dhcp_scopes ORDER BY id`)
		if err != nil {
			fail(w, err)
			return
		}
		defer rows.Close()
		out := []scopeIn{}
		for rows.Next() {
			var x scopeIn
			var en int
			rows.Scan(&x.ID, &x.Name, &x.Iface, &x.Mode, &x.Subnet, &x.RangeStart, &x.RangeEnd, &x.Router, &x.DNS, &x.Domain, &x.NTP, &x.Lease, &x.Relay, &en)
			x.Enabled = en == 1
			out = append(out, x)
		}
		api.JSON(w, 200, out)
	})
	s.HandleAdmin("PUT /api/dhcp/scopes", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in scopeIn
		if !api.Decode(w, r, &in) {
			return
		}
		if err := in.validate(); err != nil {
			fail(w, err)
			return
		}
		en := 0
		if in.Enabled {
			en = 1
		}
		var err error
		if in.ID == 0 {
			_, err = a.Config.Exec(`INSERT INTO dhcp_scopes(name,iface,mode,subnet,range_start,range_end,router,dns,domain,ntp,lease_seconds,relay_addr,enabled) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				in.Name, in.Iface, in.Mode, in.Subnet, in.RangeStart, in.RangeEnd, in.Router, in.DNS, in.Domain, in.NTP, in.Lease, in.Relay, en)
		} else {
			_, err = a.Config.Exec(`UPDATE dhcp_scopes SET name=?,iface=?,mode=?,subnet=?,range_start=?,range_end=?,router=?,dns=?,domain=?,ntp=?,lease_seconds=?,relay_addr=?,enabled=? WHERE id=?`,
				in.Name, in.Iface, in.Mode, in.Subnet, in.RangeStart, in.RangeEnd, in.Router, in.DNS, in.Domain, in.NTP, in.Lease, in.Relay, en, in.ID)
		}
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				fail(w, errors.New("a scope with that name already exists"))
				return
			}
			fail(w, err)
			return
		}
		a.DHCP.Reload()
		a.publishLeases()
		ok(w)
	})
	s.HandleAdmin("DELETE /api/dhcp/scopes/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		a.Config.Exec(`DELETE FROM dhcp_scopes WHERE id=?`, id)
		a.DHCP.Reload()
		a.publishLeases()
		ok(w)
	})
	s.Handle("GET /api/dhcp/leases", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rows, err := a.DHCP.Leases()
		if err != nil {
			fail(w, err)
			return
		}
		if rows == nil {
			rows = []dhcp.LeaseRow{}
		}
		api.JSON(w, 200, rows)
	})
	s.Handle("GET /api/dhcp/utilisation", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.DHCP.Utilisation())
	})
	s.HandleAdmin("DELETE /api/dhcp/leases/{scope}/{ip}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var scope int64
		for _, ch := range r.PathValue("scope") {
			if ch < '0' || ch > '9' {
				fail(w, errBadID)
				return
			}
			scope = scope*10 + int64(ch-'0')
		}
		ip := net.ParseIP(r.PathValue("ip"))
		if ip == nil {
			fail(w, errBadID)
			return
		}
		if err := a.DHCP.Revoke(scope, ip.String()); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.Handle("GET /api/dhcp/reservations", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rows, err := a.Config.Query(`SELECT id,scope_id,mac,ip,hostname FROM dhcp_reservations ORDER BY ip`)
		if err != nil {
			fail(w, err)
			return
		}
		defer rows.Close()
		type res struct {
			ID       int64  `json:"id"`
			ScopeID  int64  `json:"scope_id"`
			MAC      string `json:"mac"`
			IP       string `json:"ip"`
			Hostname string `json:"hostname"`
		}
		out := []res{}
		for rows.Next() {
			var x res
			rows.Scan(&x.ID, &x.ScopeID, &x.MAC, &x.IP, &x.Hostname)
			out = append(out, x)
		}
		api.JSON(w, 200, out)
	})
	s.HandleAdmin("POST /api/dhcp/reservations", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			ScopeID  int64  `json:"scope_id"`
			MAC      string `json:"mac"`
			IP       string `json:"ip"`
			Hostname string `json:"hostname"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		hw, err := net.ParseMAC(in.MAC)
		if err != nil || len(hw) != 6 {
			fail(w, errors.New("that is not a valid device address (MAC)"))
			return
		}
		var subnet string
		if err := a.Config.QueryRow(`SELECT subnet FROM dhcp_scopes WHERE id=?`, in.ScopeID).Scan(&subnet); err != nil {
			fail(w, errors.New("pick a network for the reservation"))
			return
		}
		_, n, _ := net.ParseCIDR(subnet)
		ip := net.ParseIP(in.IP).To4()
		if ip == nil || !n.Contains(ip) {
			fail(w, errors.New("the address must be inside the network"))
			return
		}
		if _, err := a.Config.Exec(`INSERT INTO dhcp_reservations(scope_id,mac,ip,hostname) VALUES(?,?,?,?)`, in.ScopeID, strings.ToLower(hw.String()), ip.String(), strings.TrimSpace(in.Hostname)); err != nil {
			fail(w, errors.New("that device or address already has a reservation"))
			return
		}
		a.DHCP.Reload()
		ok(w)
	})
	s.HandleAdmin("DELETE /api/dhcp/reservations/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		a.Config.Exec(`DELETE FROM dhcp_reservations WHERE id=?`, id)
		a.DHCP.Reload()
		ok(w)
	})
}
