package installer

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The management port. The installer asks which network port the person will manage Gatehouse
// from and leaves the answer on the new system. Until setup is finished, that port is the one
// that matters: it asks for an address like every wired port does, and if no router answers it
// gives itself 192.168.100.1 and hands out addresses to whatever is plugged in, so a laptop
// connected straight to the machine can open the setup page with nothing configured.

// MgmtPortPath is where the installer left the choice: the port's name and its MAC address.
var MgmtPortPath = "/var/lib/gatehouse/mgmt-port"

// FallbackFile is the temporary network file that gives the management port its own address.
// It lives in /run, so a restart removes it, and it is removed as soon as setup is finished.
var FallbackFile = "/run/systemd/network/05-gatehouse-fallback.network"

const (
	// FallbackAddr is the management address when no router gives one.
	FallbackAddr = "192.168.100.1"
	fallbackWait = 30 * time.Second
)

// MgmtPort returns the name of the management port, found by MAC address when the kernel has
// renamed it, or "" when none was chosen.
func MgmtPort() string {
	b, err := os.ReadFile(MgmtPortPath)
	if err != nil {
		return ""
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return ""
	}
	if len(f) > 1 {
		for _, p := range WiredPorts() {
			if strings.EqualFold(p.MAC, f[1]) {
				return p.Name
			}
		}
	}
	for _, p := range WiredPorts() {
		if p.Name == f[0] {
			return p.Name
		}
	}
	return f[0]
}

// FallbackConfig is the network file for the management port's own address.
func FallbackConfig(port string) string {
	return fmt.Sprintf(`# Written by Gatehouse while setup is not finished and no router answered.
[Match]
Name=%s

[Network]
Address=%s/24
DHCPServer=yes

[DHCPServer]
PoolOffset=100
PoolSize=100
EmitDNS=no
`, port, FallbackAddr)
}

// fallbackPort says which port should carry the fallback address right now, or "". It wants one
// only while setup is unfinished, the port has a cable, and no private address has turned up on it
// for a while (waited).
func fallbackPort(setup, mgmt string, ports []Port, hasAddr func(string) bool, waited time.Duration) string {
	if setup != "account" && setup != "wizard" {
		return ""
	}
	cand := ""
	switch {
	case mgmt != "":
		for _, p := range ports {
			if p.Name == mgmt && p.Link {
				cand = p.Name
			}
		}
	default:
		n := 0
		for _, p := range ports {
			if p.Link {
				cand, n = p.Name, n+1
			}
		}
		if n != 1 {
			cand = ""
		}
	}
	if cand == "" || hasAddr(cand) || waited < fallbackWait {
		return ""
	}
	return cand
}

// portHasAddress is true when the port has a private IPv4 address that is not link-local.
func portHasAddress(name string) bool {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return false
	}
	as, _ := ifi.Addrs()
	for _, a := range as {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() && privateAddr(n.IP) {
			return true
		}
	}
	return false
}

// fallback keeps the temporary management address in step with what the screen sees.
type fallback struct {
	since  time.Time // when the management port last had a cable and no address
	active string    // the port carrying the fallback address now
	now    func() time.Time
	reload func(port string)
}

func newFallback() *fallback {
	return &fallback{now: time.Now, reload: func(port string) {
		exec.Command("networkctl", "reload").Run()
		if port != "" {
			exec.Command("networkctl", "reconfigure", port).Run()
		}
	}}
}

// update is called every couple of seconds with how far setup has got.
func (f *fallback) update(setup string, ports []Port, hasAddr func(string) bool) {
	mgmt := MgmtPort()
	// Time without an address starts counting once there is a cable and nothing else on the port.
	waiting := false
	for _, p := range ports {
		if p.Link && !hasAddr(p.Name) && (mgmt == "" || p.Name == mgmt) {
			waiting = true
		}
	}
	switch {
	case f.active != "":
		waiting = false
	case !waiting:
		f.since = time.Time{}
	case f.since.IsZero():
		f.since = f.now()
	}
	var waited time.Duration
	if !f.since.IsZero() {
		waited = f.now().Sub(f.since)
	}
	want := fallbackPort(setup, mgmt, ports, hasAddr, waited)
	if f.active != "" {
		// Keep it for as long as setup is unfinished and the cable stays in.
		want = fallbackPort(setup, mgmt, ports, func(string) bool { return false }, fallbackWait)
	}
	switch {
	case want != "" && want != f.active:
		os.MkdirAll(filepath.Dir(FallbackFile), 0o755)
		if os.WriteFile(FallbackFile, []byte(FallbackConfig(want)), 0o644) == nil {
			old := f.active
			f.active = want
			f.reload(want)
			if old != "" && old != want {
				f.reload(old)
			}
		}
	case want == "" && f.active != "":
		old := f.active
		os.Remove(FallbackFile)
		f.active = ""
		f.reload(old)
	}
}
