package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/api"
	dnssrv "nopewarden/internal/dns"
	"nopewarden/internal/fw"
	"nopewarden/internal/tlsutil"
)

// Encrypted DNS for your devices. Phones and laptops away from home (or on a network that allows it)
// can ask this router over DNS-over-TLS or DNS-over-HTTPS, so lookups are private and still go through
// the lists and the query log. Off until switched on, offered only to the networks chosen, never to
// the internet side.

type encServerCfg struct {
	On     bool     `json:"on"`
	Ifaces []string `json:"ifaces"`
}

type encServerState struct {
	mu      sync.Mutex
	srv     *dnssrv.EncServer
	last    string
	dotErr  string
	dohErr  string
	custom  bool
	started bool
}

func (a *App) encServerConfig() encServerCfg {
	var c encServerCfg
	if raw := a.setting("dns.encserver", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	return c
}

func (a *App) encTLSDir() string { return filepath.Join(a.Cfg.DataDir, "tls") }

func (a *App) encUploaded() bool {
	_, e1 := os.Stat(filepath.Join(a.encTLSDir(), "dns-custom.crt"))
	_, e2 := os.Stat(filepath.Join(a.encTLSDir(), "dns-custom.key"))
	return e1 == nil && e2 == nil
}

// encLoadCert returns the certificate in use: the uploaded one when there is one, else our own.
func (a *App) encLoadCert() (*tls.Certificate, bool, error) {
	dir := a.encTLSDir()
	if a.encUploaded() {
		c, err := tls.LoadX509KeyPair(filepath.Join(dir, "dns-custom.crt"), filepath.Join(dir, "dns-custom.key"))
		if err == nil {
			return &c, true, nil
		}
	}
	cp, kp, err := tlsutil.EnsureNamed(dir, "dns", "nopewarden")
	if err != nil {
		return nil, false, err
	}
	c, err := tls.LoadX509KeyPair(cp, kp)
	return &c, false, err
}

func hostPort(addr, def string) string {
	if addr == "" {
		return def
	}
	return addr
}

// encServerSync starts or stops the listeners to match the setting.
func (a *App) encServerSync() error {
	c := a.encServerConfig()
	s := &a.encsrv
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil {
		s.srv = dnssrv.NewEncServer(a.DNS)
	}
	s.srv.Stop()
	s.dotErr, s.dohErr = "", ""
	if !c.On {
		return nil
	}
	cert, custom, err := a.encLoadCert()
	if err != nil {
		return errors.New("the certificate could not be made: " + err.Error())
	}
	s.custom = custom
	s.srv.SetCertificate(cert)
	dot := hostPort(a.Cfg.EncDoTAddr, fmt.Sprintf(":%d", fw.EncDNSDoTPort))
	doh := hostPort(a.Cfg.EncDoHAddr, fmt.Sprintf(":%d", fw.EncDNSDoHPort))
	if _, p, err := net.SplitHostPort(a.Cfg.WebAddr); err == nil && a.Cfg.EncDoHAddr == "" {
		if wp, _ := strconv.Atoi(p); wp == fw.EncDNSDoHPort {
			return errors.New("port 8443 is already used by the console, so encrypted DNS over HTTPS cannot use it")
		}
	}
	de, he := s.srv.Start(dot, doh)
	if de != nil {
		s.dotErr = de.Error()
	}
	if he != nil {
		s.dohErr = he.Error()
	}
	if de != nil && he != nil {
		return errors.New("neither port could be opened: " + de.Error())
	}
	return nil
}

func (a *App) encServerLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(15 * time.Second):
	}
	if err := a.encServerSync(); err != nil {
		a.encsrv.mu.Lock()
		a.encsrv.last = err.Error()
		a.encsrv.mu.Unlock()
	}
	<-ctx.Done()
	a.encsrv.mu.Lock()
	if a.encsrv.srv != nil {
		a.encsrv.srv.Stop()
	}
	a.encsrv.mu.Unlock()
}

type encNet struct {
	Iface   string `json:"iface"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Allowed bool   `json:"allowed"`
	Why     string `json:"why,omitempty"`
}

func (a *App) encChoices() []encNet {
	m, err := fw.Load(a.Config)
	if err != nil {
		return nil
	}
	rows, err := a.Config.Query(`SELECT i.name,COALESCE(z.name,i.name),i.ipv4_cidr FROM interfaces i JOIN zones z ON z.id=i.zone_id WHERE i.ipv4_cidr<>'' AND i.enabled=1 ORDER BY i.name`)
	if err != nil {
		return nil
	}
	var out []encNet
	for rows.Next() {
		var n encNet
		var cidr string
		if rows.Scan(&n.Iface, &n.Name, &cidr) == nil {
			n.IP = strings.SplitN(cidr, "/", 2)[0]
			out = append(out, n)
		}
	}
	rows.Close()
	probe := *m
	for i := range out {
		probe.EncDNSIfaces = []string{out[i].Iface}
		out[i].Allowed = len(probe.EncDNSNets()) == 1
		if !out[i].Allowed {
			out[i].Why = "This port faces the internet, so encrypted DNS is never offered on it."
		}
	}
	return out
}

type encCertInfo struct {
	Kind    string   `json:"kind"` // "self-signed" or "uploaded"
	Names   []string `json:"names"`
	Issuer  string   `json:"issuer"`
	Expires string   `json:"expires"`
	Days    int      `json:"days"`
}

func (a *App) encCertInfo() encCertInfo {
	c, custom, err := a.encLoadCert()
	info := encCertInfo{Kind: "self-signed"}
	if custom {
		info.Kind = "uploaded"
	}
	if err != nil || len(c.Certificate) == 0 {
		return info
	}
	if leaf, err := x509.ParseCertificate(c.Certificate[0]); err == nil {
		info.Names = leaf.DNSNames
		for _, ip := range leaf.IPAddresses {
			if !ip.IsLoopback() {
				info.Names = append(info.Names, ip.String())
			}
		}
		info.Issuer = leaf.Issuer.CommonName
		info.Expires = leaf.NotAfter.Format("2 January 2006")
		info.Days = int(time.Until(leaf.NotAfter).Hours() / 24)
	}
	return info
}

func (a *App) encServerApply(user string, old string) error {
	revert := func() {
		a.Config.SetSetting("dns.encserver", old)
		a.encServerSync()
	}
	if err := a.encServerSync(); err != nil {
		revert()
		return errors.New("encrypted DNS could not start: " + err.Error())
	}
	a.encsrv.mu.Lock()
	a.encsrv.last = ""
	a.encsrv.mu.Unlock()
	if err := a.refreshFirewallQuiet(user); err != nil {
		revert()
		a.refreshFirewallQuiet(user)
		return errors.New("the firewall could not be updated: " + err.Error())
	}
	a.fwSync()
	a.publishFW()
	return nil
}

func (a *App) registerEncServerRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/dns/encrypted-server", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.encsrv.mu.Lock()
		var dot, doh bool
		if a.encsrv.srv != nil {
			dot, doh = a.encsrv.srv.Listening()
		}
		last, de, he := a.encsrv.last, a.encsrv.dotErr, a.encsrv.dohErr
		a.encsrv.mu.Unlock()
		api.JSON(w, 200, map[string]any{
			"config": a.encServerConfig(), "networks": a.encChoices(), "cert": a.encCertInfo(),
			"dot_port": fw.EncDNSDoTPort, "doh_port": fw.EncDNSDoHPort, "doh_path": "/dns-query",
			"dot_listening": dot, "doh_listening": doh, "error": last, "dot_error": de, "doh_error": he,
		})
	})
	s.HandleAdmin("PUT /api/dns/encrypted-server", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in encServerCfg
		if !api.Decode(w, r, &in) {
			return
		}
		okNets := map[string]bool{}
		for _, n := range a.encChoices() {
			if n.Allowed {
				okNets[n.Iface] = true
			}
		}
		var keep []string
		for _, d := range in.Ifaces {
			if !okNets[d] {
				fail(w, errors.New(d+" cannot be used for encrypted DNS (it faces the internet, or does not exist)"))
				return
			}
			keep = append(keep, d)
		}
		in.Ifaces = keep
		if in.On && len(keep) == 0 {
			fail(w, errors.New("choose at least one network that may use encrypted DNS"))
			return
		}
		old := a.setting("dns.encserver", "")
		b, _ := json.Marshal(in)
		a.Config.SetSetting("dns.encserver", string(b))
		if err := a.encServerApply(c.User.Username, old); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Encrypted DNS for devices was changed", "")
		ok(w)
	})
	s.HandleAdmin("POST /api/dns/encrypted-server/cert", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Cert string `json:"cert"`
			Key  string `json:"key"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		pair, err := tls.X509KeyPair([]byte(in.Cert), []byte(in.Key))
		if err != nil {
			fail(w, errors.New("that certificate and key do not belong together, or are not in PEM form: "+err.Error()))
			return
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil || time.Now().After(leaf.NotAfter) {
			fail(w, errors.New("that certificate has already expired"))
			return
		}
		dir := a.encTLSDir()
		if err := os.MkdirAll(dir, 0o750); err != nil {
			fail(w, err)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "dns-custom.key"), []byte(in.Key), 0o600); err != nil {
			fail(w, err)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "dns-custom.crt"), []byte(in.Cert), 0o644); err != nil {
			fail(w, err)
			return
		}
		if err := a.encServerSync(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "A certificate was uploaded for encrypted DNS", "")
		ok(w)
	})
	s.HandleAdmin("DELETE /api/dns/encrypted-server/cert", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		dir := a.encTLSDir()
		os.Remove(filepath.Join(dir, "dns-custom.crt"))
		os.Remove(filepath.Join(dir, "dns-custom.key"))
		if err := a.encServerSync(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "The uploaded encrypted DNS certificate was removed", "")
		ok(w)
	})
}
