package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// ServicesText describes, in plain words, whether DHCP and DNS are doing their jobs. It is for the
// recovery screen on the serial console, where the web console may be the very thing that is down,
// so it asks the system itself: which ports are open, whether a name lookup is answered, and what
// the settings say each network should be handing out.
func ServicesText(q querier, dnsAddr string) string {
	var b strings.Builder
	dhcp := portListening("udp", 67)
	dns := portListening("udp", 53) || portListening("tcp", 53)
	fmt.Fprintf(&b, "DHCP (hands out addresses)\n")
	if dhcp {
		fmt.Fprintf(&b, "  Listening: yes, devices can ask for an address.\n")
	} else {
		fmt.Fprintf(&b, "  Listening: NO. Nothing is answering address requests.\n  If the console is down, restart it from the Recovery menu or the status screen.\n")
	}
	now := time.Now().Unix()
	rows, err := q.Query(`SELECT s.name,s.iface,s.mode,s.enabled,s.range_start,s.range_end,s.router,
		(SELECT COUNT(*) FROM dhcp_leases l WHERE l.scope_id=s.id AND l.state='active' AND l.expires>=?) FROM dhcp_scopes s ORDER BY s.id`, now)
	n := 0
	var routers []string
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name, iface, mode, rs, re, router string
			var en, active int
			rows.Scan(&name, &iface, &mode, &en, &rs, &re, &router, &active)
			if router != "" {
				routers = append(routers, router)
			}
			n++
			state := "serving"
			switch {
			case en == 0:
				state = "switched off"
			case mode != "serve":
				state = mode
			}
			rng := ""
			if rs != "" && re != "" {
				rng = fmt.Sprintf(", range %s to %s", rs, re)
			}
			fmt.Fprintf(&b, "  %-14s %-9s %s%s, %d address%s in use\n", name, iface, state, rng, active, map[bool]string{true: "", false: "es"}[active == 1])
		}
	}
	if n == 0 {
		fmt.Fprintf(&b, "  No network hands out addresses yet.\n")
	}

	fmt.Fprintf(&b, "\nDNS (turns names into addresses)\n")
	if dns {
		fmt.Fprintf(&b, "  Listening: yes.\n")
	} else {
		fmt.Fprintf(&b, "  Listening: NO. Nothing is answering name lookups.\n")
	}
	cands := []string{"127.0.0.1:53"}
	if dnsAddr != "" {
		cands = []string{dnsAddr}
	}
	for _, r := range routers {
		cands = append(cands, net.JoinHostPort(r, "53"))
	}
	answered := false
	var lastWhy string
	for _, server := range cands {
		for _, name := range []string{"example.com", "debian.org"} {
			t0 := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			srv := server
			r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "udp", srv)
			}}
			addrs, err := r.LookupHost(ctx, name)
			cancel()
			if err != nil || len(addrs) == 0 {
				lastWhy = "no answer"
				if err != nil {
					lastWhy = shortErr(err)
				}
				continue
			}
			fmt.Fprintf(&b, "  Looking up %s through %s: answered in %d ms\n", name, server, time.Since(t0).Milliseconds())
			answered = true
			break
		}
		if answered {
			break
		}
	}
	if !answered {
		fmt.Fprintf(&b, "  Looking up a name FAILED (%s). Devices cannot reach websites by name.\n", lastWhy)
	}
	if up, err := q.Query(`SELECT name,address,proto FROM dns_upstreams WHERE enabled=1 ORDER BY position,id`); err == nil {
		defer up.Close()
		var names []string
		for up.Next() {
			var n, a, p string
			up.Scan(&n, &a, &p)
			names = append(names, n)
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, "  Asks these servers when it does not know: %s\n", strings.Join(names, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}

// portListening reads the kernel's socket tables: for UDP any socket bound to the port, for TCP one
// that is listening.
func portListening(proto string, port int) bool {
	want := fmt.Sprintf(":%04X", port)
	for _, f := range []string{"/proc/net/" + proto, "/proc/net/" + proto + "6"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for i, l := range strings.Split(string(b), "\n") {
			if i == 0 {
				continue
			}
			p := strings.Fields(l)
			if len(p) < 4 || !strings.HasSuffix(p[1], want) {
				continue
			}
			if proto == "tcp" && p[3] != "0A" {
				continue
			}
			return true
		}
	}
	return false
}
