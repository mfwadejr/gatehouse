package app

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
	"gatehouse/internal/vpn"
)

// Site-to-site: two Gatehouse routers joined by a WireGuard link. One end creates the link and
// gets a pairing code; the other end pastes it. The code carries everything both ends need, so
// each side routes to the other's network and allows traffic both ways without any further steps.

const sitePrefix = "GATEHOUSE-SITE-1:"

const siteZoneName = "Remote sites"

type sitePairing struct {
	V      int      `json:"v"`
	Label  string   `json:"label"`
	AKey   string   `json:"a_key"`  // public key of the end that made the code
	AEp    string   `json:"a_ep"`   // where the other end connects, host:port
	ALans  []string `json:"a_lans"` // networks behind the end that made the code
	PSK    string   `json:"psk"`
	BKey   string   `json:"b_key"`  // private key for the end that uses the code
	BAddr  string   `json:"b_addr"` // its address in the link, with the link's prefix length
	BLan   string   `json:"b_lan"`  // the network behind it, as entered by the first end
	Tunnel string   `json:"tunnel"` // the first end's address in the link
}

func encodeSiteCode(p sitePairing) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return sitePrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeSiteCode(s string) (sitePairing, error) {
	var p sitePairing
	s = strings.Join(strings.Fields(s), "")
	if !strings.HasPrefix(s, sitePrefix) {
		return p, errors.New("that is not a Gatehouse site link code; copy the whole code from the other site")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, sitePrefix))
	if err != nil || json.Unmarshal(raw, &p) != nil || p.V != 1 {
		return p, errors.New("that site link code is damaged or incomplete; copy the whole code again")
	}
	return p, nil
}

// siteLans are the networks behind this router that the other site should be able to reach.
func (a *App) siteLans(q querier) []string {
	rows, err := q.Query(`SELECT i.ipv4_cidr FROM interfaces i LEFT JOIN zones z ON z.id=i.zone_id
		WHERE i.enabled=1 AND i.ipv4_cidr<>'' AND COALESCE(z.kind,'')<>'wan' ORDER BY i.id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	seen := map[string]bool{}
	for rows.Next() {
		var c string
		rows.Scan(&c)
		if p, err := netip.ParsePrefix(strings.TrimSpace(c)); err == nil && p.Addr().Is4() {
			if m := p.Masked().String(); !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

func overlapping(p netip.Prefix, with []netip.Prefix) (netip.Prefix, bool) {
	for _, u := range with {
		if u.Overlaps(p) {
			return u, true
		}
	}
	return netip.Prefix{}, false
}

// ensureSiteZone is the network that other sites join: it may reach everything at home and the
// reverse, but it does not use this router for the internet.
func (a *App) ensureSiteZone(tx *sql.Tx) (int64, error) {
	var id int64
	if err := tx.QueryRow(`SELECT id FROM zones WHERE name=?`, siteZoneName).Scan(&id); err == nil {
		return id, nil
	}
	return fw.SaveZone(tx, fw.ZoneRow{Name: siteZoneName, Kind: fw.KindTrusted, Internet: false, Management: false, Gateway: true, DNSForce: false,
		Description: "Other sites joined to this one by a site-to-site VPN, with full access in both directions"})
}

func mustPrefix(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(s))
	if err != nil || !p.Addr().Is4() {
		return p, fmt.Errorf("%q is not a network like 192.168.50.0/24", s)
	}
	return p.Masked(), nil
}

func (a *App) vpnNewSite(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		Label     string `json:"label"`
		RemoteLAN string `json:"remote_lan"`
		Endpoint  string `json:"endpoint"`
		Port      int    `json:"port"`
	}
	if !api.Decode(w, r, &in) || !a.needHelper(w) {
		return
	}
	remote, err := mustPrefix(in.RemoteLAN)
	if err != nil {
		fail(w, errors.New("enter the network at the other site, like 192.168.50.0/24"))
		return
	}
	if remote.Bits() < 8 || remote.Bits() > 29 {
		fail(w, errors.New("the network at the other site must be between /8 and /29, like 192.168.50.0/24"))
		return
	}
	host := strings.TrimSpace(in.Endpoint)
	if host == "" {
		host = a.Config.Setting(vpnEndpointSetting, "")
	}
	if host == "" {
		fail(w, errors.New("enter the address the other site will use to reach this router (your public IP address or a name like home.example.com)"))
		return
	}
	if err := validHost(host); err != nil {
		fail(w, err)
		return
	}
	var id int64
	var code string
	err = a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		used := a.usedPrefixes(tx)
		if u, clash := overlapping(remote, used); clash {
			return fmt.Errorf("the other site's network %s overlaps %s, which is already used here. Two sites that are joined need different address ranges; change one of them first", remote, u)
		}
		name, err := freeName(tx, "vpn_wg", "wg")
		if err != nil {
			return err
		}
		subnet := vpn.SuggestSubnet(append(used, remote))
		addr, err := vpn.ServerAddress(subnet)
		if err != nil {
			return err
		}
		priv, pub, err := vpn.NewKeyPair()
		if err != nil {
			return err
		}
		bPriv, bPub, err := vpn.NewKeyPair()
		if err != nil {
			return err
		}
		psk, err := vpn.NewPSK()
		if err != nil {
			return err
		}
		port := in.Port
		if port == 0 {
			port = a.freeWGPort(tx)
		}
		sp, _ := netip.ParsePrefix(addr)
		bIP, err := vpn.NextPeerAddress(addr, nil)
		if err != nil {
			return err
		}
		bAddr := strings.TrimSuffix(bIP, "/32") + "/" + strconv.Itoa(sp.Bits())
		zone, err := a.ensureSiteZone(tx)
		if err != nil {
			return err
		}
		label := cleanLabel(in.Label, "Site link")
		res, err := tx.Exec(`INSERT INTO vpn_wg(name,label,mode,listen_port,address,private_key,public_key,endpoint_host,zone_id,route_all,site,created_at) VALUES(?,?,'server',?,?,?,?,?,?,0,1,?)`,
			name, label, port, addr, a.Box.Seal(priv), pub, host, zone, time.Now().Unix())
		if err != nil {
			return friendlyDup(err, "a VPN with that name already exists")
		}
		id, _ = res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO vpn_wg_peers(wg_id,name,public_key,psk,address,allowed_ips,keepalive,enabled,created_at) VALUES(?,?,?,?,?,?,25,1,?)`,
			id, label, bPub, a.Box.Seal(psk), bIP, bIP+","+remote.String(), time.Now().Unix()); err != nil {
			return err
		}
		code, err = encodeSiteCode(sitePairing{V: 1, Label: label, AKey: pub, AEp: fmt.Sprintf("%s:%d", host, port), ALans: a.siteLans(tx),
			PSK: psk, BKey: bPriv, BAddr: bAddr, BLan: remote.String(), Tunnel: addr})
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.rememberEndpoint(host)
	a.refreshVPN()
	api.JSON(w, 200, map[string]any{"id": id, "code": code, "filename": safeFile(cleanLabel(in.Label, "Site link")) + ".gatehouse-site"})
}

func (a *App) vpnJoinSite(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		Code  string `json:"code"`
		Label string `json:"label"`
	}
	if !api.Decode(w, r, &in) || !a.needHelper(w) {
		return
	}
	p, err := decodeSiteCode(in.Code)
	if err != nil {
		fail(w, err)
		return
	}
	bPub, err := vpn.PublicKey(p.BKey)
	if err != nil || p.AKey == "" || p.PSK == "" || len(p.ALans) == 0 {
		fail(w, errors.New("that site link code is damaged or incomplete; copy the whole code again"))
		return
	}
	aAddr, err := netip.ParsePrefix(p.Tunnel)
	if err != nil {
		fail(w, errors.New("that site link code is damaged or incomplete; copy the whole code again"))
		return
	}
	bAddr, err := netip.ParsePrefix(p.BAddr)
	if err != nil {
		fail(w, errors.New("that site link code is damaged or incomplete; copy the whole code again"))
		return
	}
	var aLans []netip.Prefix
	for _, l := range p.ALans {
		lp, err := mustPrefix(l)
		if err != nil {
			fail(w, errors.New("that site link code is damaged or incomplete; copy the whole code again"))
			return
		}
		aLans = append(aLans, lp)
	}
	var warning string
	var id int64
	label := cleanLabel(in.Label, cleanLabel(p.Label, "Site link"))
	err = a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
		var n int
		tx.QueryRow(`SELECT count(*) FROM vpn_wg_peers WHERE public_key=?`, p.AKey).Scan(&n)
		if n > 0 {
			return errors.New("this site link is already set up here")
		}
		used := a.usedPrefixes(tx)
		for _, lp := range aLans {
			if u, clash := overlapping(lp, used); clash {
				return fmt.Errorf("the other site's network %s overlaps %s, which is already used here. Two sites that are joined need different address ranges; change one of them first", lp, u)
			}
		}
		if u, clash := overlapping(aAddr.Masked(), used); clash {
			return fmt.Errorf("the link's own address range %s overlaps %s used here; create the link again from the other site", aAddr.Masked(), u)
		}
		if want, err := mustPrefix(p.BLan); err == nil {
			found := false
			for _, l := range a.siteLans(tx) {
				if lp, err := netip.ParsePrefix(l); err == nil && lp.Overlaps(want) {
					found = true
				}
			}
			if !found {
				warning = fmt.Sprintf("The other site expects this site's network to be %s, but none of the networks here match. Traffic to this site will not arrive until they agree.", want)
			}
		}
		name, err := freeName(tx, "vpn_wg", "wg")
		if err != nil {
			return err
		}
		zone, err := a.ensureSiteZone(tx)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO vpn_wg(name,label,mode,listen_port,address,private_key,public_key,endpoint_host,zone_id,route_all,site,created_at) VALUES(?,?,'server',?,?,?,?,'',?,0,1,?)`,
			name, label, a.freeWGPort(tx), bAddr.String(), a.Box.Seal(p.BKey), bPub, zone, time.Now().Unix())
		if err != nil {
			return friendlyDup(err, "a VPN with that name already exists")
		}
		id, _ = res.LastInsertId()
		allowed := []string{aAddr.Addr().String() + "/32"}
		for _, lp := range aLans {
			allowed = append(allowed, lp.String())
		}
		_, err = tx.Exec(`INSERT INTO vpn_wg_peers(wg_id,name,public_key,psk,address,allowed_ips,endpoint,keepalive,enabled,created_at) VALUES(?,?,?,?,?,?,?,25,1,?)`,
			id, label, p.AKey, a.Box.Seal(p.PSK), aAddr.Addr().String()+"/32", strings.Join(allowed, ","), p.AEp, time.Now().Unix())
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.refreshVPN()
	api.JSON(w, 200, map[string]any{"id": id, "warning": warning})
}
