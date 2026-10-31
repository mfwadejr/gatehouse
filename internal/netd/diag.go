package netd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/miekg/dns"
)

var reDiagTarget = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,252}$`)

// DiagTargetOK says whether a name or address is safe to hand to ping or traceroute. A leading
// dash, spaces and anything unusual are refused so nothing can pass as an option.
func DiagTargetOK(t string) bool { return reDiagTarget.MatchString(t) }

// Diag runs one network check from this machine and returns its output as text. Only ping and
// traceroute exist, with fixed options.
func (s *Service) Diag(kind, target string) (string, error) {
	target = strings.TrimSpace(target)
	if !DiagTargetOK(target) {
		return "", errors.New("enter a website name or an address, such as example.com or 8.8.8.8")
	}
	if strings.HasPrefix(kind, "lookup:") {
		return diagLookup(strings.TrimPrefix(kind, "lookup:"), target), nil
	}
	var cmd *exec.Cmd
	limit := 45 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	switch kind {
	case "ping":
		cmd = exec.CommandContext(ctx, "ping", "-n", "-c", "4", "-W", "2", "--", target)
	case "trace":
		cmd = exec.CommandContext(ctx, "traceroute", "-n", "-w", "2", "-q", "1", "-m", "20", "--", target)
	default:
		return "", errors.New("unknown check")
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if out == "" && err != nil {
		return "", errors.New("the check could not run: " + err.Error())
	}
	if ctx.Err() != nil {
		out += "\n(stopped after 45 seconds)"
	}
	return out, nil
}

var reDiagDev = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,14}$`)

// lookupVia asks a public resolver for a name with the question leaving through one device.
// It is a variable so tests do not need a network.
var lookupVia = func(dev, name string) (time.Duration, string, error) {
	var last error
	for _, srv := range []string{"1.1.1.1:53", "9.9.9.9:53"} {
		c := &dns.Client{Net: "udp", Timeout: 3 * time.Second, Dialer: &net.Dialer{Control: bindToDevice(dev), Timeout: 3 * time.Second}}
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(name), dns.TypeA)
		m.RecursionDesired = true
		r, rtt, err := c.Exchange(m, srv)
		if err != nil {
			last = err
			continue
		}
		if r.Rcode != dns.RcodeSuccess || len(r.Answer) == 0 {
			last = fmt.Errorf("%s", dns.RcodeToString[r.Rcode])
			continue
		}
		return rtt, strings.TrimSuffix(srv, ":53"), nil
	}
	if last == nil {
		last = errors.New("no answer")
	}
	return 0, "", last
}

// diagLookup answers "ok|<ms>|<server>" or "fail|<reason>": whether names can be looked up
// through one internet connection's own device, without touching the machine's normal DNS.
func diagLookup(dev, name string) string {
	if !reDiagDev.MatchString(dev) {
		return "fail|not a usable connection"
	}
	rtt, srv, err := lookupVia(dev, name)
	if err != nil {
		return "fail|" + err.Error()
	}
	return fmt.Sprintf("ok|%d|%s", ms(rtt), srv)
}

func ms(d time.Duration) int {
	v := int(d / time.Millisecond)
	if v < 1 && d > 0 {
		v = 1
	}
	return v
}
