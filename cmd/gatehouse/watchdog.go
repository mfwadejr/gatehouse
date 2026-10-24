package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/app"
	"gatehouse/internal/fw"
)

// sdNotify tells systemd how the program is doing. It does nothing outside systemd.
func sdNotify(msg string) {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return
	}
	if strings.HasPrefix(addr, "@") {
		addr = "\x00" + addr[1:]
	}
	c, err := net.Dial("unixgram", addr)
	if err != nil {
		return
	}
	defer c.Close()
	c.Write([]byte(msg))
}

// healthy is true when the console answers and its DNS server answers.
func healthy(webPort int, tlsOn bool, dnsAddr string) bool {
	scheme := "http"
	if tlsOn {
		scheme = "https"
	}
	hc := &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // our own console, over the loopback
	resp, err := hc.Get(fmt.Sprintf("%s://127.0.0.1:%d/api/health", scheme, webPort))
	if err != nil {
		return false
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return false
	}
	// The DNS server is judged only by whether anything is listening: a connected UDP socket gets
	// "connection refused" when nothing is. A slow answer (an upstream that is offline) is not a
	// failure, otherwise an unplugged internet cable would make the console restart in a loop.
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "udp", dnsAddr)
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = r.LookupHost(ctx, "health.gatehouse.invalid")
	return err == nil || !strings.Contains(err.Error(), "refused")
}

// watchdog runs inside systemd's watch. It reports "ready" once the console answers, then keeps
// saying "alive" while the console and DNS really work. If they stop working it stops saying so,
// and systemd restarts the program. Every automatic restart is written to the Gatehouse log.
func watchdog(ctx context.Context, a *app.App, cfg app.Config) {
	if os.Getenv("NOTIFY_SOCKET") == "" {
		return
	}
	dnsAddr := "127.0.0.1:53"
	if len(cfg.DNSAddrs) > 0 {
		if h, p, err := net.SplitHostPort(cfg.DNSAddrs[0]); err == nil {
			if h == "" || h == "0.0.0.0" {
				h = "127.0.0.1"
			}
			dnsAddr = net.JoinHostPort(h, p)
		}
	}
	port := func() int { return fw.SavedConsolePort() }
	tlsOn := cfg.TLSCert != ""
	// ready: the console answers
	for i := 0; i < 60; i++ { // ready is sent after a minute even if the console is not up, so systemd does not kill a slow start; the watchdog takes over
		if healthy(port(), tlsOn, dnsAddr) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	sdNotify("READY=1")
	noteRestarts(a, cfg.DataDir)
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			sdNotify("STOPPING=1")
			return
		case <-t.C:
			if healthy(port(), tlsOn, dnsAddr) {
				sdNotify("WATCHDOG=1")
			}
		}
	}
}

// noteRestarts writes an entry to the log when systemd has restarted the console on its own.
func noteRestarts(a *app.App, data string) {
	out, err := exec.Command("systemctl", "show", "gatehouse", "-p", "NRestarts", "--value").Output()
	if err != nil {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return
	}
	file := filepath.Join(data, "restarts")
	last := 0
	if b, err := os.ReadFile(file); err == nil {
		last, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	if n < last { // the count starts again after a reboot
		last = 0
	}
	if n > last {
		a.LogEvent("system", "warning", "The console restarted by itself",
			fmt.Sprintf("It was restarted automatically %d time(s) since start-up. The firewall kept protecting the network meanwhile.", n-last))
	}
	os.WriteFile(file, []byte(strconv.Itoa(n)+"\n"), 0o644)
}
