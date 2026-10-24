package app

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/devices"
	"gatehouse/internal/fw"
	"gatehouse/internal/vpn"
)

var reHost = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.\-]{0,251}[A-Za-z0-9])?$`)

func validHost(h string) error {
	if !reHost.MatchString(h) {
		return errors.New("enter the address devices will use to reach this router, like home.example.com or your public IP address")
	}
	return nil
}

func validCheckTarget(t string) error {
	if t == "" {
		return nil
	}
	if !reHost.MatchString(t) {
		return errors.New("the test address must be an IP address or a name")
	}
	return nil
}

func kindOf(r *http.Request) (string, bool) {
	k := r.PathValue("kind")
	return k, k == "wireguard" || k == "openvpn"
}

func (a *App) registerVPNRoutes() {
	s := a.API
	s.Handle("GET /api/vpn", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		snap := a.vpnSnapshot()
		if snap.At == 0 || r.URL.Query().Get("fresh") == "1" {
			a.refreshVPN()
			snap = a.vpnSnapshot()
		}
		if snap.Views == nil {
			snap.Views = []VPNView{}
		}
		api.JSON(w, 200, map[string]any{"vpns": snap.Views, "at": snap.At, "endpoint": a.Config.Setting(vpnEndpointSetting, ""),
			"helper": a.Net != nil})
	})
	s.Handle("GET /api/vpn/choices", a.vpnChoices)
	s.HandleAdmin("PUT /api/vpn/settings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Endpoint string `json:"endpoint"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.Endpoint = strings.TrimSpace(in.Endpoint)
		if in.Endpoint != "" {
			if err := validHost(in.Endpoint); err != nil {
				fail(w, err)
				return
			}
		}
		a.Config.SetSetting(vpnEndpointSetting, in.Endpoint)
		a.refreshVPN()
		ok(w)
	})

	s.HandleAdmin("POST /api/vpn/wireguard/server", a.vpnNewWGServer)
	s.HandleAdmin("POST /api/vpn/wireguard/client", a.vpnNewWGClient)
	s.HandleAdmin("POST /api/vpn/wireguard/site", a.vpnNewSite)
	s.HandleAdmin("POST /api/vpn/wireguard/site/join", a.vpnJoinSite)
	s.HandleAdmin("POST /api/vpn/openvpn/server", a.vpnNewOVPNServer)
	s.HandleAdmin("POST /api/vpn/openvpn/client", a.vpnNewOVPNClient)
	s.HandleAdmin("PUT /api/vpn/{kind}/{id}", a.vpnUpdate)
	s.HandleAdmin("DELETE /api/vpn/{kind}/{id}", a.vpnDelete)
	s.HandleAdmin("POST /api/vpn/{kind}/{id}/peers", a.vpnAddPeer)
	s.HandleAdmin("DELETE /api/vpn/{kind}/{id}/peers/{pid}", a.vpnDeletePeer)
	s.HandleAdmin("PUT /api/vpn/{kind}/{id}/uses", a.vpnSetUses)
}

// vpnChoices lists what can be sent through a tunnel: networks and devices.
func (a *App) vpnChoices(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	type netChoice struct {
		Name    string   `json:"name"`
		Subnets []string `json:"subnets"`
	}
	var nets []netChoice
	zs, err := fw.Zones(a.Config)
	if err != nil {
		fail(w, err)
		return
	}
	for _, z := range zs {
		if z.Kind == fw.KindWan || z.Kind == fw.KindVpn {
			continue
		}
		if strings.HasPrefix(z.Name, "VPN") || z.Name == siteZoneName {
			continue
		}
		nc := netChoice{Name: z.Name, Subnets: []string{}}
		if rows, err := a.Config.Query(`SELECT ipv4_cidr FROM interfaces WHERE enabled=1 AND ipv4_cidr<>'' AND zone_id=?`, z.ID); err == nil {
			for rows.Next() {
				var cidr string
				rows.Scan(&cidr)
				if p, err := netip.ParsePrefix(strings.TrimSpace(cidr)); err == nil {
					nc.Subnets = append(nc.Subnets, p.Masked().String())
				}
			}
			rows.Close()
		}
		nets = append(nets, nc)
	}
	type devChoice struct {
		MAC    string `json:"mac"`
		Name   string `json:"name"`
		IP     string `json:"ip"`
		Online bool   `json:"online"`
	}
	devs := []devChoice{}
	if list, err := a.Devices.List(); err == nil {
		for _, d := range list {
			devs = append(devs, devChoice{MAC: strings.ToLower(d.MAC), Name: deviceTitle(d), IP: d.IP, Online: d.Online})
		}
	}
	if nets == nil {
		nets = []netChoice{}
	}
	api.JSON(w, 200, map[string]any{"networks": nets, "devices": devs})
}

func (a *App) needHelper(w http.ResponseWriter) bool {
	if a.Net == nil || a.FW == nil {
		fail(w, errNoHelper)
		return false
	}
	return true
}

// WireGuard server -------------------------------------------------------------

type newServer struct {
	Label    string `json:"label"`
	Port     int    `json:"port"`
	Subnet   string `json:"subnet"`
	Endpoint string `json:"endpoint"`
	Access   string `json:"access"` // full or internet
	Proto    string `json:"proto"`
	RouteAll bool   `json:"route_all"`
	// Route is what travels through the VPN: "all" (everything the device does) or "home"
	// (only traffic for the home network). Reach says whether the device may use the devices
	// at home. Both are empty for older callers, which still send Access and RouteAll.
	Route string `json:"route"`
	Reach *bool  `json:"reach"`
}

// resolve turns the plain choices into the zone kind (full access at home or internet only)
// and whether the device sends all of its traffic through the router.
func (in newServer) resolve() (full, routeAll bool, err error) {
	if in.Route == "" {
		full = in.Access != "internet"
		return full, in.RouteAll || !full, nil
	}
	if in.Route != "all" && in.Route != "home" {
		return false, false, errors.New("choose whether everything or only the home network goes through the VPN")
	}
	routeAll = in.Route == "all"
	full = in.Reach == nil || *in.Reach
	if !routeAll && !full {
		return false, false, errors.New("with only the home network selected, devices must be allowed to reach the home network, otherwise the VPN would carry nothing")
	}
	return full, routeAll, nil
}

func (a *App) rememberEndpoint(h string) {
	if h != "" && a.Config.Setting(vpnEndpointSetting, "") == "" {
		a.Config.SetSetting(vpnEndpointSetting, h)
	}
}

func (a *App) vpnNewWGServer(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in newServer
	if !api.Decode(w, r, &in) || !a.needHelper(w) {
		return
	}
	full, routeAll, err := in.resolve()
	if err != nil {
		fail(w, err)
		return
	}
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	if in.Endpoint != "" {
		if err := validHost(in.Endpoint); err != nil {
			fail(w, err)
			return
		}
	}
	var id int64
	err = a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		name, err := freeName(tx, "vpn_wg", "wg")
		if err != nil {
			return err
		}
		subnet := in.Subnet
		if subnet == "" {
			subnet = vpn.SuggestSubnet(a.usedPrefixes(tx))
		}
		addr, err := vpn.ServerAddress(subnet)
		if err != nil {
			return err
		}
		priv, pub, err := vpn.NewKeyPair()
		if err != nil {
			return err
		}
		port := in.Port
		if port == 0 {
			port = a.freeWGPort(tx)
		}
		zone, err := a.ensureVPNZone(tx, full)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO vpn_wg(name,label,mode,listen_port,address,private_key,public_key,endpoint_host,zone_id,route_all,created_at) VALUES(?,?,'server',?,?,?,?,?,?,?,?)`,
			name, cleanLabel(in.Label, "Remote access"), port, addr, a.Box.Seal(priv), pub, in.Endpoint, zone, b2i(routeAll), time.Now().Unix())
		if err != nil {
			return friendlyDup(err, "a VPN with that name already exists")
		}
		id, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.rememberEndpoint(in.Endpoint)
	a.refreshVPN()
	api.JSON(w, 200, map[string]int64{"id": id})
}

func (a *App) vpnNewWGClient(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		Label      string `json:"label"`
		Conf       string `json:"conf"`
		Check      string `json:"check"`
		KillSwitch *bool  `json:"killswitch"`
	}
	if !api.Decode(w, r, &in) || !a.needHelper(w) {
		return
	}
	im, err := vpn.ParseConf(in.Conf)
	if err != nil {
		fail(w, err)
		return
	}
	pub, err := vpn.PublicKey(im.PrivateKey)
	if err != nil {
		fail(w, err)
		return
	}
	check := strings.TrimSpace(in.Check)
	if check == "" {
		check = "1.1.1.1"
	}
	if err := validCheckTarget(check); err != nil {
		fail(w, err)
		return
	}
	ks := in.KillSwitch == nil || *in.KillSwitch
	keep := im.Keepalive
	if keep == 0 {
		keep = 25
	}
	var id int64
	err = a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		name, err := freeName(tx, "vpn_wg", "wg")
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO vpn_wg(name,label,mode,listen_port,address,private_key,public_key,dns,mtu,killswitch,check_target,created_at) VALUES(?,?,'client',0,?,?,?,?,?,?,?,?)`,
			name, cleanLabel(in.Label, "Provider VPN"), im.Address, a.Box.Seal(im.PrivateKey), pub, im.DNS, im.MTU, b2i(ks), check, time.Now().Unix())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		psk := ""
		if im.PSK != "" {
			psk = a.Box.Seal(im.PSK)
		}
		_, err = tx.Exec(`INSERT INTO vpn_wg_peers(wg_id,name,public_key,psk,address,allowed_ips,endpoint,keepalive,enabled,created_at) VALUES(?,?,?,?,?,?,?,?,1,?)`,
			id, "provider", im.PeerKey, psk, "", strings.Join(im.AllowedIPs, ","), im.Endpoint, keep, time.Now().Unix())
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.refreshVPN()
	api.JSON(w, 200, map[string]int64{"id": id})
}

// OpenVPN ------------------------------------------------------------------------

func (a *App) vpnNewOVPNServer(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in newServer
	if !api.Decode(w, r, &in) || !a.needHelper(w) {
		return
	}
	full, routeAll, err := in.resolve()
	if err != nil {
		fail(w, err)
		return
	}
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	if in.Endpoint != "" {
		if err := validHost(in.Endpoint); err != nil {
			fail(w, err)
			return
		}
	}
	if in.Proto != "tcp" {
		in.Proto = "udp"
	}
	now := time.Now()
	ca, err := vpn.NewCA(now)
	if err != nil {
		fail(w, err)
		return
	}
	srv, err := ca.ServerCert(now)
	if err != nil {
		fail(w, err)
		return
	}
	tc, err := vpn.StaticKey()
	if err != nil {
		fail(w, err)
		return
	}
	crl, err := ca.CRL(1, nil, now)
	if err != nil {
		fail(w, err)
		return
	}
	var id int64
	err = a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		name, err := freeName(tx, "vpn_ovpn", "tun")
		if err != nil {
			return err
		}
		subnet := in.Subnet
		if subnet == "" {
			subnet = vpn.SuggestSubnet(a.usedPrefixes(tx))
		}
		if _, err := vpn.ServerAddr(subnet); err != nil {
			return err
		}
		port := in.Port
		if port == 0 {
			for port = 1194; port < 1294; port++ {
				var n int
				tx.QueryRow(`SELECT count(*) FROM vpn_ovpn WHERE port=? AND mode='server'`, port).Scan(&n)
				if n == 0 {
					break
				}
			}
		}
		zone, err := a.ensureVPNZone(tx, full)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO vpn_ovpn(name,label,mode,proto,port,subnet,endpoint_host,route_all,zone_id,ca_cert,ca_key,srv_cert,srv_key,tls_crypt,crl,crl_number,created_at)
			VALUES(?,?,'server',?,?,?,?,?,?,?,?,?,?,?,?,1,?)`,
			name, cleanLabel(in.Label, "Remote access (OpenVPN)"), in.Proto, port, subnet, in.Endpoint, b2i(routeAll), zone,
			ca.CertPEM, a.Box.Seal(ca.KeyPEM), srv.CertPEM, a.Box.Seal(srv.KeyPEM), a.Box.Seal(tc), crl, now.Unix())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.rememberEndpoint(in.Endpoint)
	a.refreshVPN()
	api.JSON(w, 200, map[string]int64{"id": id})
}

func (a *App) vpnNewOVPNClient(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		Label      string `json:"label"`
		Profile    string `json:"profile"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		Check      string `json:"check"`
		KillSwitch *bool  `json:"killswitch"`
	}
	if !api.Decode(w, r, &in) || !a.needHelper(w) {
		return
	}
	imp, err := vpn.ImportProfile(in.Profile)
	if err != nil {
		fail(w, err)
		return
	}
	if imp.NeedsAuth && (in.Username == "" || in.Password == "") {
		fail(w, errors.New("this provider asks for a username and password. Enter them below"))
		return
	}
	check := strings.TrimSpace(in.Check)
	if check == "" {
		check = "1.1.1.1"
	}
	if err := validCheckTarget(check); err != nil {
		fail(w, err)
		return
	}
	ks := in.KillSwitch == nil || *in.KillSwitch
	pass := ""
	if in.Password != "" {
		pass = a.Box.Seal(in.Password)
	}
	if strings.ContainsAny(in.Username+in.Password, "\n\r") {
		fail(w, errors.New("the username and password cannot contain line breaks"))
		return
	}
	var id int64
	err = a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		name, err := freeName(tx, "vpn_ovpn", "tun")
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO vpn_ovpn(name,label,mode,profile,auth_user,auth_pass,killswitch,check_target,created_at) VALUES(?,?,'client',?,?,?,?,?,?)`,
			name, cleanLabel(in.Label, "Provider VPN"), a.Box.Seal(in.Profile), in.Username, pass, b2i(ks), check, time.Now().Unix())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.refreshVPN()
	api.JSON(w, 200, map[string]int64{"id": id})
}

// Update, delete ---------------------------------------------------------------------

func (a *App) vpnDev(tx *sql.Tx, kind string, id int64) (string, string, error) {
	table := "vpn_wg"
	if kind == "openvpn" {
		table = "vpn_ovpn"
	}
	var dev, mode string
	if err := tx.QueryRow(`SELECT name,mode FROM `+table+` WHERE id=?`, id).Scan(&dev, &mode); err != nil {
		return "", "", errors.New("that VPN no longer exists")
	}
	return dev, mode, nil
}

func (a *App) vpnUpdate(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	kind, good := kindOf(r)
	id, idOK := pathID(r)
	if !good || !idOK {
		fail(w, errBadID)
		return
	}
	var in struct {
		Label      *string `json:"label"`
		Enabled    *bool   `json:"enabled"`
		KillSwitch *bool   `json:"killswitch"`
		Check      *string `json:"check"`
		Endpoint   *string `json:"endpoint"`
		RouteAll   *bool   `json:"route_all"`
	}
	if !api.Decode(w, r, &in) || !a.needHelper(w) {
		return
	}
	if in.Check != nil {
		*in.Check = strings.TrimSpace(*in.Check)
		if err := validCheckTarget(*in.Check); err != nil {
			fail(w, err)
			return
		}
	}
	if in.Endpoint != nil {
		*in.Endpoint = strings.TrimSpace(*in.Endpoint)
		if *in.Endpoint != "" {
			if err := validHost(*in.Endpoint); err != nil {
				fail(w, err)
				return
			}
		}
	}
	table, endpointCol := "vpn_wg", "endpoint_host"
	if kind == "openvpn" {
		table = "vpn_ovpn"
	}
	err := a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		if _, _, err := a.vpnDev(tx, kind, id); err != nil {
			return err
		}
		set := func(col string, v any) error {
			_, err := tx.Exec(`UPDATE `+table+` SET `+col+`=? WHERE id=?`, v, id)
			return err
		}
		if in.Label != nil {
			if err := set("label", cleanLabel(*in.Label, "VPN")); err != nil {
				return err
			}
		}
		if in.Enabled != nil {
			if err := set("enabled", b2i(*in.Enabled)); err != nil {
				return err
			}
		}
		if in.KillSwitch != nil {
			if err := set("killswitch", b2i(*in.KillSwitch)); err != nil {
				return err
			}
		}
		if in.Check != nil {
			if err := set("check_target", *in.Check); err != nil {
				return err
			}
		}
		if in.Endpoint != nil {
			if err := set(endpointCol, *in.Endpoint); err != nil {
				return err
			}
		}
		if in.RouteAll != nil {
			if err := set("route_all", b2i(*in.RouteAll)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if in.Endpoint != nil {
		a.rememberEndpoint(*in.Endpoint)
	}
	a.refreshVPN()
	ok(w)
}

func (a *App) vpnDelete(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	kind, good := kindOf(r)
	id, idOK := pathID(r)
	if !good || !idOK {
		fail(w, errBadID)
		return
	}
	if !a.needHelper(w) {
		return
	}
	table := "vpn_wg"
	if kind == "openvpn" {
		table = "vpn_ovpn"
	}
	err := a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		dev, _, err := a.vpnDev(tx, kind, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM vpn_routes WHERE tunnel=?`, dev); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM `+table+` WHERE id=?`, id)
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.refreshVPN()
	ok(w)
}

// Devices that connect in ---------------------------------------------------------------

func (a *App) vpnAddPeer(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	kind, good := kindOf(r)
	id, idOK := pathID(r)
	if !good || !idOK {
		fail(w, errBadID)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !api.Decode(w, r, &in) || !a.needHelper(w) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !vpn.ValidName(in.Name) {
		fail(w, errors.New("give the device a name with letters, numbers, spaces, dots or dashes, like Marvin's iPhone"))
		return
	}
	var out map[string]any
	var err error
	if kind == "wireguard" {
		out, err = a.addWGPeer(c.User.Username, id, in.Name)
	} else {
		out, err = a.addOVPNUser(c.User.Username, id, in.Name)
	}
	if err != nil {
		fail(w, err)
		return
	}
	a.refreshVPN()
	api.JSON(w, 200, out)
}

func (a *App) allowedForClient(q querier, full, routeAll bool, subnet string) []string {
	if routeAll {
		return []string{"0.0.0.0/0"}
	}
	out := []string{}
	if p, err := netip.ParsePrefix(subnet); err == nil {
		out = append(out, p.Masked().String())
	}
	if full {
		out = append(out, a.lanRoutes(q)...)
	}
	return out
}

func (a *App) addWGPeer(author string, id int64, name string) (map[string]any, error) {
	wgs, err := a.listWG(a.Config)
	if err != nil {
		return nil, err
	}
	var srv *wgRow
	for i := range wgs {
		if wgs[i].ID == id && wgs[i].Mode == "server" {
			srv = &wgs[i]
		}
	}
	if srv == nil {
		return nil, errors.New("that VPN no longer exists")
	}
	if srv.Site {
		return nil, errors.New("this is a link to another site; devices cannot be added to it")
	}
	host := srv.Endpoint
	if host == "" {
		host = a.Config.Setting(vpnEndpointSetting, "")
	}
	if host == "" {
		return nil, errors.New("first enter the address your devices will use to reach this router (your public IP address or a name like home.example.com)")
	}
	priv, pub, err := vpn.NewKeyPair()
	if err != nil {
		return nil, err
	}
	psk, err := vpn.NewPSK()
	if err != nil {
		return nil, err
	}
	var addr string
	err = a.mutateVPN(author, func(tx *sql.Tx) error {
		peers, err := a.listWGPeers(tx, id)
		if err != nil {
			return err
		}
		var taken []string
		for _, p := range peers {
			taken = append(taken, p.Address)
			if strings.EqualFold(p.Name, name) {
				return errors.New("a device with that name already exists on this VPN")
			}
		}
		if addr, err = vpn.NextPeerAddress(srv.Address, taken); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO vpn_wg_peers(wg_id,name,public_key,psk,address,allowed_ips,keepalive,enabled,created_at) VALUES(?,?,?,?,?,?,25,1,?)`,
			id, name, pub, a.Box.Seal(psk), addr, addr, time.Now().Unix())
		return err
	})
	if err != nil {
		return nil, err
	}
	full := a.zoneIsFull(a.Config, srv.ZoneID)
	cfg := vpn.ClientConfig{PrivateKey: priv, Address: addr, DNS: routerAddr(srv.Address), MTU: srv.MTU, ServerKey: srv.PublicKey, PSK: psk,
		Endpoint: fmt.Sprintf("%s:%d", host, srv.Port), AllowedIPs: a.allowedForClient(a.Config, full, srv.RouteAll, srv.Address), Keepalive: 25}
	text := cfg.Text()
	out := map[string]any{"name": name, "config": text, "filename": safeFile(name) + ".conf", "kind": "wireguard"}
	if png, err := vpn.QR(text); err == nil {
		out["qr"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	}
	return out, nil
}

func safeFile(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "vpn"
	}
	return b.String()
}

func (a *App) addOVPNUser(author string, id int64, name string) (map[string]any, error) {
	ovs, err := a.listOVPN(a.Config)
	if err != nil {
		return nil, err
	}
	var srv *ovpnRow
	for i := range ovs {
		if ovs[i].ID == id && ovs[i].Mode == "server" {
			srv = &ovs[i]
		}
	}
	if srv == nil {
		return nil, errors.New("that VPN no longer exists")
	}
	host := srv.Endpoint
	if host == "" {
		host = a.Config.Setting(vpnEndpointSetting, "")
	}
	if host == "" {
		return nil, errors.New("first enter the address your devices will use to reach this router (your public IP address or a name like home.example.com)")
	}
	ca := &vpn.CA{CertPEM: srv.CACert, KeyPEM: srv.CAKey}
	iss, err := ca.ClientCert(name, time.Now())
	if err != nil {
		return nil, err
	}
	err = a.mutateVPN(author, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO vpn_ovpn_users(ovpn_id,name,serial,cert,created_at) VALUES(?,?,?,?,?)`, id, name, iss.Serial, iss.CertPEM, time.Now().Unix())
		if err != nil {
			return friendlyDup(err, "a device with that name already exists on this VPN")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	text := vpn.Profile{Host: host, Port: srv.Port, Proto: srv.Proto, CA: srv.CACert, Cert: iss.CertPEM, Key: iss.KeyPEM, TLSCrypt: srv.TLSCrypt}.Text()
	return map[string]any{"name": name, "config": text, "filename": safeFile(name) + ".ovpn", "kind": "openvpn"}, nil
}

func (a *App) vpnDeletePeer(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	kind, good := kindOf(r)
	id, idOK := pathID(r)
	pid, err := parseInt(r.PathValue("pid"))
	if !good || !idOK || err != nil {
		fail(w, errBadID)
		return
	}
	if !a.needHelper(w) {
		return
	}
	err = a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		if kind == "wireguard" {
			res, err := tx.Exec(`DELETE FROM vpn_wg_peers WHERE id=? AND wg_id=?`, pid, id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return errors.New("that device no longer exists")
			}
			return nil
		}
		res, err := tx.Exec(`UPDATE vpn_ovpn_users SET revoked_at=? WHERE id=? AND ovpn_id=? AND revoked_at=0`, time.Now().Unix(), pid, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errors.New("that device no longer exists")
		}
		return a.rebuildCRL(tx, id)
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.refreshVPN()
	ok(w)
}

func (a *App) rebuildCRL(tx *sql.Tx, id int64) error {
	ovs, err := a.listOVPN(tx)
	if err != nil {
		return err
	}
	var srv *ovpnRow
	for i := range ovs {
		if ovs[i].ID == id {
			srv = &ovs[i]
		}
	}
	if srv == nil {
		return errors.New("that VPN no longer exists")
	}
	users, err := a.listOVPNUsers(tx, id)
	if err != nil {
		return err
	}
	var rev []vpn.Revoked
	for _, u := range users {
		if u.RevokedAt > 0 {
			rev = append(rev, vpn.Revoked{Serial: u.Serial, At: time.Unix(u.RevokedAt, 0)})
		}
	}
	ca := &vpn.CA{CertPEM: srv.CACert, KeyPEM: srv.CAKey}
	n := srv.CRLNumber + 1
	crl, err := ca.CRL(n, rev, time.Now())
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE vpn_ovpn SET crl=?,crl_number=? WHERE id=?`, crl, n, id)
	return err
}

// Who uses a tunnel -----------------------------------------------------------------------------

type useIn struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Label string `json:"label"`
}

func (a *App) vpnSetUses(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	kind, good := kindOf(r)
	id, idOK := pathID(r)
	if !good || !idOK {
		fail(w, errBadID)
		return
	}
	var in struct {
		Uses []useIn `json:"uses"`
	}
	if !api.Decode(w, r, &in) || !a.needHelper(w) {
		return
	}
	devs := map[string]devices.Device{}
	if list, err := a.Devices.List(); err == nil {
		for _, d := range list {
			devs[strings.ToLower(d.MAC)] = d
		}
	}
	var warnings []string
	err := a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		dev, mode, err := a.vpnDev(tx, kind, id)
		if err != nil {
			return err
		}
		if mode != "client" {
			return errors.New("only a VPN that connects out to a provider can carry your devices' traffic")
		}
		if _, err := tx.Exec(`DELETE FROM vpn_routes WHERE tunnel=?`, dev); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, u := range in.Uses {
			u.Value = strings.TrimSpace(u.Value)
			key := u.Kind + "|" + strings.ToLower(u.Value)
			if seen[key] {
				continue
			}
			seen[key] = true
			switch u.Kind {
			case "network":
				var n int
				tx.QueryRow(`SELECT count(*) FROM zones WHERE name=? AND kind NOT IN ('wan','vpn')`, u.Value).Scan(&n)
				if n == 0 {
					return fmt.Errorf("there is no network called %q", u.Value)
				}
			case "device":
				u.Value = strings.ToLower(u.Value)
				d, known := devs[u.Value]
				if !known {
					return fmt.Errorf("that device is not known to the router (%s)", u.Value)
				}
				if err := a.reserveForDevice(tx, d); err != nil {
					warnings = append(warnings, err.Error())
				}
			case "cidr":
				if p, err := netip.ParsePrefix(u.Value); err != nil || !p.Addr().Is4() {
					if ad, err2 := netip.ParseAddr(u.Value); err2 != nil || !ad.Is4() {
						return fmt.Errorf("%q is not an IPv4 address or range like 192.168.1.50 or 192.168.1.0/24", u.Value)
					}
				}
			default:
				return errors.New("choose a network, a device or an address range")
			}
			if _, err := tx.Exec(`DELETE FROM vpn_routes WHERE kind=? AND value=? AND tunnel<>?`, u.Kind, u.Value, dev); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO vpn_routes(tunnel,kind,value,label) VALUES(?,?,?,?)`, dev, u.Kind, u.Value, strings.TrimSpace(u.Label)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.DHCP.Reload()
	a.refreshVPN()
	api.JSON(w, 200, map[string]any{"status": "ok", "warnings": warnings})
}

// reserveForDevice pins a device's address, so the tunnel keeps following it when the lease
// would otherwise change.
func (a *App) reserveForDevice(tx *sql.Tx, d devices.Device) error {
	if d.IP == "" {
		return fmt.Errorf("%s has not been seen on the network yet. Connect it once so its address can be fixed", deviceTitle(d))
	}
	ip, err := netip.ParseAddr(d.IP)
	if err != nil {
		return nil
	}
	rows, err := tx.Query(`SELECT id,subnet FROM dhcp_scopes WHERE mode='serve'`)
	if err != nil {
		return err
	}
	var scope int64
	for rows.Next() {
		var id int64
		var subnet string
		rows.Scan(&id, &subnet)
		if p, err := netip.ParsePrefix(subnet); err == nil && p.Contains(ip) {
			scope = id
		}
	}
	rows.Close()
	if scope == 0 {
		return nil
	}
	var n int
	tx.QueryRow(`SELECT count(*) FROM dhcp_reservations WHERE scope_id=? AND lower(mac)=?`, scope, strings.ToLower(d.MAC)).Scan(&n)
	if n > 0 {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO dhcp_reservations(scope_id,mac,ip,hostname) VALUES(?,?,?,?)`, scope, strings.ToLower(d.MAC), d.IP, d.Hostname); err != nil {
		return fmt.Errorf("could not fix the address of %s: %v", deviceTitle(d), err)
	}
	return nil
}

func parseInt(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
