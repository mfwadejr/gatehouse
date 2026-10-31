package netd

import (
	"strings"
	"time"
)

// OVPNReason reads the recent OpenVPN messages of a client and says, in plain words, what is
// wrong. It looks from the newest line backwards, so a failure that is already past (the
// tunnel has since connected) is not reported. An empty answer means nothing is wrong that
// the messages can show.
func OVPNReason(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.ToLower(lines[i])
		switch {
		case strings.Contains(l, "initialization sequence completed"):
			return ""
		case strings.Contains(l, "auth_failed") || strings.Contains(l, "auth failed"):
			return "The provider refused the username and password"
		case strings.Contains(l, "options error"), strings.Contains(l, "unrecognized option"), strings.Contains(l, "unknown option"):
			return "OpenVPN would not accept a setting in this profile (" + trimLog(lines[i]) + ")"
		case strings.Contains(l, "verify error"), strings.Contains(l, "verify eku"), strings.Contains(l, "verify ku"),
			strings.Contains(l, "certificate verify failed"), strings.Contains(l, "does not have key usage"),
			strings.Contains(l, "certificate does not have extended key usage"), strings.Contains(l, "cert_verify"):
			return "The server's certificate was rejected"
		case strings.Contains(l, "cannot resolve host"), strings.Contains(l, "resolve: cannot"), strings.Contains(l, "name or service not known"):
			return "Gatehouse could not look up the server's name"
		case strings.Contains(l, "no shared cipher"), strings.Contains(l, "cipher negotiation failed"), strings.Contains(l, "cipher") && strings.Contains(l, "not supported"):
			return "OpenVPN and the server could not agree on encryption"
		case strings.Contains(l, "tls-crypt unwrap"), strings.Contains(l, "hmac authentication failed"), strings.Contains(l, "tls-auth") && strings.Contains(l, "fail"):
			return "The server did not accept the key in this profile"
		case strings.Contains(l, "network is unreachable"), strings.Contains(l, "no route to host"):
			return "Gatehouse could not reach the server (no way out to the internet)"
		case strings.Contains(l, "tls key negotiation failed"), strings.Contains(l, "tls handshake failed"):
			return "The server did not answer. Something between Gatehouse and the provider may be blocking it, or the profile's keys do not match"
		}
	}
	return ""
}

// OVPNConnected says whether the newest word from OpenVPN is that the tunnel finished coming up.
func OVPNConnected(lines []string) bool {
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.ToLower(lines[i])
		switch {
		case strings.Contains(l, "initialization sequence completed"):
			return true
		case strings.Contains(l, "sigusr1"), strings.Contains(l, "sigterm"), strings.Contains(l, "restart pause"),
			strings.Contains(l, "tls error"), strings.Contains(l, "auth_failed"), strings.Contains(l, "starting openvpn"), strings.Contains(l, "stopped openvpn"):
			return false
		}
	}
	return false
}

func trimLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}

// recentOVPN returns the last lines OpenVPN wrote for a client, kept for a few seconds so the
// console's regular polling does not run journalctl each time.
func (v *VPN) recentOVPN(unit string) []string {
	v.lmu.Lock()
	defer v.lmu.Unlock()
	if c, ok := v.logCache[unit]; ok && time.Since(c.at) < 15*time.Second {
		return c.lines
	}
	out, _ := wifiRun(10*time.Second, "journalctl", "-u", unit, "--no-pager", "-o", "cat", "-n", "40")
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "-- ") {
			continue
		}
		lines = append(lines, trimLog(l))
	}
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	if v.logCache == nil {
		v.logCache = map[string]logEntry{}
	}
	v.logCache[unit] = logEntry{at: time.Now(), lines: lines}
	return lines
}

type logEntry struct {
	at    time.Time
	lines []string
}
