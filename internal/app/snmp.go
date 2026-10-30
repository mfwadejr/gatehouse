package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/snmp"
)

// SNMP: lets a monitoring tool on a management network read this device's status (name, uptime, and
// each port's speed and traffic counts). Read only, version 2c, off by default. The firewall only
// lets the management networks reach it, and the password-like "community" is made up here.

type snmpCfg struct {
	On        bool   `json:"on"`
	Community string `json:"community"`
	Contact   string `json:"contact"`
	Location  string `json:"location"`
}

type snmpState struct {
	mu   sync.Mutex
	pc   net.PacketConn
	last string
	boot time.Time
}

func (a *App) snmpConfig() snmpCfg {
	var c snmpCfg
	if raw := a.setting("snmp.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	return c
}

func newCommunity() string {
	const letters = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 20)
	rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

func (a *App) snmpAgent(c snmpCfg) *snmp.Agent {
	return &snmp.Agent{Community: c.Community, Vars: func() []snmp.Var {
		host, _ := a.hostnameCached()
		a.snmp.mu.Lock()
		boot := a.snmp.boot
		a.snmp.mu.Unlock()
		return snmp.MIBVars(snmp.System{
			Descr: "Gatehouse " + a.Cfg.Version, Name: host, Contact: c.Contact, Location: c.Location,
			Uptime: uint64(time.Since(boot) / (10 * time.Millisecond)),
		}, snmp.ReadIfaces("/"))
	}}
}

// snmpSync starts or stops the listener to match the setting.
func (a *App) snmpSync() error {
	c := a.snmpConfig()
	a.snmp.mu.Lock()
	defer a.snmp.mu.Unlock()
	if a.snmp.boot.IsZero() {
		a.snmp.boot = time.Now()
	}
	if a.snmp.pc != nil {
		a.snmp.pc.Close()
		a.snmp.pc = nil
	}
	if !c.On || c.Community == "" {
		return nil
	}
	addr := a.Cfg.SNMPAddr
	if addr == "" {
		addr = ":161"
	}
	pc, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return err
	}
	a.snmp.pc = pc
	go a.snmpAgent(c).Serve(pc)
	return nil
}

func (a *App) snmpLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(15 * time.Second):
	}
	if err := a.snmpSync(); err != nil {
		a.snmp.mu.Lock()
		a.snmp.last = err.Error()
		a.snmp.mu.Unlock()
	}
	<-ctx.Done()
	a.snmp.mu.Lock()
	if a.snmp.pc != nil {
		a.snmp.pc.Close()
	}
	a.snmp.mu.Unlock()
}

func (a *App) registerSNMPRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/snmp", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.snmp.mu.Lock()
		listening := a.snmp.pc != nil
		last := a.snmp.last
		a.snmp.mu.Unlock()
		api.JSON(w, 200, map[string]any{"config": a.snmpConfig(), "listening": listening, "error": last})
	})
	s.HandleAdmin("PUT /api/snmp", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			On       bool   `json:"on"`
			Contact  string `json:"contact"`
			Location string `json:"location"`
			NewKey   bool   `json:"new_community"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if len(in.Contact) > 100 || len(in.Location) > 100 || strings.ContainsAny(in.Contact+in.Location, "\r\n") {
			fail(w, errors.New("contact and location must be one short line"))
			return
		}
		old := a.snmpConfig()
		n := snmpCfg{On: in.On, Community: old.Community, Contact: in.Contact, Location: in.Location}
		if n.Community == "" || in.NewKey {
			n.Community = newCommunity()
		}
		b, _ := json.Marshal(n)
		a.Config.SetSetting("snmp.config", string(b))
		a.Config.SetSetting("snmp.enabled", map[bool]string{true: "1", false: "0"}[n.On])
		err := a.snmpSync()
		a.snmp.mu.Lock()
		a.snmp.last = ""
		if err != nil {
			a.snmp.last = err.Error()
		}
		a.snmp.mu.Unlock()
		if err != nil {
			bo, _ := json.Marshal(old)
			a.Config.SetSetting("snmp.config", string(bo))
			a.Config.SetSetting("snmp.enabled", map[bool]string{true: "1", false: "0"}[old.On])
			a.snmpSync()
			fail(w, errors.New("SNMP could not start: "+err.Error()))
			return
		}
		if err := a.refreshFirewallQuiet(c.User.Username); err != nil {
			fail(w, errors.New("the firewall could not be updated: "+err.Error()))
			return
		}
		if old.On != n.On {
			a.event(c.User.Username, "warning", map[bool]string{true: "SNMP (read only) was turned on", false: "SNMP was turned off"}[n.On], "")
		}
		a.publishFW()
		ok(w)
	})
}
