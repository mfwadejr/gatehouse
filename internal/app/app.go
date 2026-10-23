// Package app wires the services together into the running daemon.
package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/auth"
	"gatehouse/internal/crypt"
	"gatehouse/internal/devices"
	"gatehouse/internal/dhcp"
	dnssrv "gatehouse/internal/dns"
	"gatehouse/internal/events"
	"gatehouse/internal/fw"
	"gatehouse/internal/store"
)

// Config says where things live and listen. Defaults suit the installed appliance.
type Config struct {
	DataDir  string   // databases, key, blocklist cache
	WebAddr  string   // console listen address
	DNSAddrs []string // DNS listen addresses
	DHCPAddr string   // DHCP listen address; empty disables the DHCP socket
	TLSCert  string
	TLSKey   string
	Static   http.Handler // embedded console files
	// FW is the privileged firewall controller; tests inject one, production uses the helper socket.
	FW fw.Controller
	// Net is the privileged network controller; nil disables network editing.
	Net NetController
}

func DefaultConfig() Config {
	return Config{DataDir: "/var/lib/gatehouse", WebAddr: ":443", DNSAddrs: []string{":53"}, DHCPAddr: ":67"}
}

// App is the running daemon.
type App struct {
	Cfg     Config
	Config  *store.DB
	Logs    *store.DB
	Box     *crypt.Box
	Auth    *auth.Service
	Bus     *events.Bus
	API     *api.Server
	DNS     *dnssrv.Server
	Lists   *dnssrv.Manager
	QLog    *dnssrv.QueryLog
	DHCP    *dhcp.Engine
	DHCPSrv *dhcp.Server
	Devices *devices.Directory
	FW      fw.Controller // nil when no privileged helper is available
	Net     NetController
	apply   applyState
	setup   setupGate

	netApplyState struct {
		mu     sync.Mutex
		connID uint64
		print  string
	}

	liveMu  sync.Mutex
	liveBuf []dnssrv.LogEntry
}

type namesAdapter struct{ e *dhcp.Engine }

func (n namesAdapter) LookupHost(name string) []net.IP     { return n.e.Lookups().LookupHost(name) }
func (n namesAdapter) LookupAddr(ip net.IP) (string, bool) { return n.e.Lookups().LookupAddr(ip) }

// New opens storage and builds every service. Nothing listens until Run.
func New(cfg Config) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, err
	}
	a := &App{Cfg: cfg, Bus: events.NewBus()}
	var err error
	if a.Config, err = store.OpenConfig(filepath.Join(cfg.DataDir, "config.db")); err != nil {
		return nil, fmt.Errorf("config database: %w", err)
	}
	if a.Logs, err = store.OpenLogs(filepath.Join(cfg.DataDir, "logs.db")); err != nil {
		return nil, fmt.Errorf("log database: %w", err)
	}
	if a.Box, err = crypt.LoadOrCreate(filepath.Join(cfg.DataDir, "secret.key")); err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	a.Auth = auth.New(a.Config, a.Box)

	a.DHCP = dhcp.NewEngine(a.Config)
	a.Devices = devices.NewDirectory(a.Config, a.DHCP)
	a.QLog = dnssrv.NewQueryLog(a.Logs, a.pushLive)
	pool := dnssrv.NewPool([]string{"1.1.1.1:53", "9.9.9.9:53"})
	a.DNS = dnssrv.NewServer(a.dnsOptions(), pool, a.Devices, namesAdapter{a.DHCP}, a.QLog)
	a.Lists = dnssrv.NewManager(a.Config, filepath.Join(cfg.DataDir, "lists"), nil)
	a.DNS.NewManager(a.Lists)
	if err := a.Lists.SeedDefaults(); err != nil {
		return nil, err
	}
	if err := a.seedUpstreams(); err != nil {
		return nil, err
	}
	a.DHCPSrv = dhcp.NewServer(a.DHCP, cfg.DHCPAddr)

	a.DHCP.OnChange = func() { a.publishLeases() }
	a.DHCP.OnEvent = func(kind, mac string, ip net.IP, scope string) {
		if kind == "ack" {
			a.Devices.Seen(mac, ip, "")
		}
	}
	a.Lists.OnChange = func() { a.Bus.Publish("dns.lists", map[string]int64{"at": time.Now().Unix()}) }

	a.API = api.New(a.Auth, a.Bus)
	a.API.Static = cfg.Static
	a.API.Secure = cfg.TLSCert != ""
	a.API.Topic("dns.stats", "dns.live", "dns.upstreams", "dns.lists", "dhcp.leases", "dhcp.utilisation", "fw.status", "net.status")
	a.FW = cfg.FW
	a.Net = cfg.Net
	a.registerRoutes()
	a.registerSetupRoutes()
	a.prepareSetup()

	if err := a.reloadAll(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) reloadAll() error {
	if err := a.DHCP.Reload(); err != nil {
		return err
	}
	a.Devices.Rebuild()
	if err := a.Lists.Reload(); err != nil {
		return err
	}
	if err := a.applyDNS(); err != nil {
		return err
	}
	return nil
}

// settings -----------------------------------------------------------------

func (a *App) setting(key, def string) string { return a.Config.Setting(key, def) }

func (a *App) settingFloat(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(a.setting(key, ""), 64); err == nil {
		return v
	}
	return def
}

func (a *App) dnsOptions() dnssrv.Options {
	o := dnssrv.DefaultOptions()
	o.BlockMode = a.setting("dns.block_mode", o.BlockMode)
	o.LocalDomain = a.setting("dns.local_domain", o.LocalDomain)
	o.RateLimit = a.settingFloat("dns.rate_limit", o.RateLimit)
	o.RateBurst = a.settingFloat("dns.rate_burst", o.RateBurst)
	o.CacheSize = int(a.settingFloat("dns.cache_size", float64(o.CacheSize)))
	o.Canary = a.setting("dns.canary", "1") == "1"
	return o
}

func (a *App) localRecords() []dnssrv.LocalRecord {
	rows, err := a.Config.Query(`SELECT name,type,value,ttl FROM dns_records`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []dnssrv.LocalRecord
	for rows.Next() {
		var r dnssrv.LocalRecord
		var ttl int
		rows.Scan(&r.Name, &r.Type, &r.Value, &ttl)
		r.TTL = uint32(ttl)
		out = append(out, r)
	}
	return out
}

// applyDNS pushes settings, records and upstreams into the running resolver.
func (a *App) applyDNS() error {
	o := a.dnsOptions()
	a.DNS.Apply(o, a.localRecords())
	a.QLog.Anonymize.Store(a.setting("dns.anonymize", "0") == "1")
	return a.loadUpstreams()
}

func (a *App) seedUpstreams() error {
	var n int
	a.Config.QueryRow(`SELECT count(*) FROM dns_upstreams`).Scan(&n)
	if n > 0 {
		return nil
	}
	// Encrypted by default: Cloudflare and Quad9 over TLS.
	for _, u := range []struct{ name, addr, tls string }{
		{"Cloudflare", "1.1.1.1:853", "cloudflare-dns.com"},
		{"Quad9", "9.9.9.9:853", "dns.quad9.net"},
	} {
		if _, err := a.Config.Exec(`INSERT INTO dns_upstreams(name,address,proto,tls_name) VALUES(?,?,?,?)`, u.name, u.addr, "tls", u.tls); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) loadUpstreams() error {
	rows, err := a.Config.Query(`SELECT id,name,address,proto,tls_name FROM dns_upstreams WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var list []*dnssrv.Upstream
	for rows.Next() {
		u := &dnssrv.Upstream{}
		if err := rows.Scan(&u.ID, &u.Name, &u.Addr, &u.Proto, &u.TLSName); err != nil {
			return err
		}
		list = append(list, u)
	}
	a.DNS.Pool().Set(list)
	return rows.Err()
}

// live events ------------------------------------------------------------

func (a *App) pushLive(e dnssrv.LogEntry) {
	a.liveMu.Lock()
	a.liveBuf = append(a.liveBuf, e)
	if len(a.liveBuf) > 50 {
		a.liveBuf = a.liveBuf[len(a.liveBuf)-50:]
	}
	a.liveMu.Unlock()
}

func (a *App) flushLive() {
	a.liveMu.Lock()
	buf := a.liveBuf
	a.liveBuf = nil
	a.liveMu.Unlock()
	if len(buf) > 0 {
		a.Bus.Publish("dns.live", buf)
	}
}

func (a *App) publishLeases() {
	rows, err := a.DHCP.Leases()
	if err != nil {
		return
	}
	a.Bus.Publish("dhcp.leases", rows)
	a.Bus.Publish("dhcp.utilisation", a.DHCP.Utilisation())
}

// Run starts every listener and background job and blocks until ctx ends.
func (a *App) Run(ctx context.Context) error {
	if err := a.DNS.Start(a.Cfg.DNSAddrs); err != nil {
		return err
	}
	defer a.DNS.Stop()
	if a.Cfg.DHCPAddr != "" {
		if err := a.DHCPSrv.Start(); err != nil {
			log.Printf("dhcp: not started: %v", err)
		} else {
			defer a.DHCPSrv.Stop()
		}
	}
	var wg sync.WaitGroup
	run := func(f func()) { wg.Add(1); go func() { defer wg.Done(); f() }() }
	stopLog := make(chan struct{})
	run(func() { a.QLog.Run(stopLog) })
	run(func() { a.DNS.Run(ctx, func(s dnssrv.Snapshot) { a.Bus.Publish("dns.stats", s) }) })
	run(func() { a.Lists.Run(ctx) })
	run(func() { a.DHCPSrv.Run(ctx) })
	run(func() { a.housekeeping(ctx) })
	run(func() { a.firstBoot(ctx) })

	l, err := net.Listen("tcp", a.Cfg.WebAddr)
	if err != nil {
		close(stopLog)
		return fmt.Errorf("console listen on %s: %w", a.Cfg.WebAddr, err)
	}
	srv := &http.Server{Handler: a.API.Handler(), ConnContext: api.ConnContext, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	log.Printf("console listening on %s", l.Addr())
	err = api.Serve(ctx, srv, l, a.Cfg.TLSCert, a.Cfg.TLSKey)
	close(stopLog)
	wg.Wait()
	return err
}

func (a *App) housekeeping(ctx context.Context) {
	fast := time.NewTicker(time.Second)
	slow := time.NewTicker(5 * time.Second)
	daily := time.NewTicker(time.Hour)
	defer fast.Stop()
	defer slow.Stop()
	defer daily.Stop()
	a.publishLeases()
	for {
		select {
		case <-ctx.Done():
			return
		case <-fast.C:
			a.flushLive()
		case <-slow.C:
			a.Bus.Publish("dns.upstreams", a.DNS.Pool().Status())
		case <-daily.C:
			days := int(a.settingFloat("dns.log_retention_days", 30))
			a.QLog.Prune(days)
			a.Auth.PurgeExpired()
		}
	}
}

// Close releases the databases.
func (a *App) Close() {
	a.Config.Close()
	a.Logs.Close()
}
