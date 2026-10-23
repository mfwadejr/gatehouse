package netd

import (
	"errors"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// WifiNetwork is one network a card can see.
type WifiNetwork struct {
	SSID     string `json:"ssid"`
	Signal   int    `json:"signal"`   // dBm
	Freq     int    `json:"freq"`     // MHz
	Security string `json:"security"` // open, wpa2, wpa3
}

var reWifiDev = regexp.MustCompile(`^(wl|wlan)[A-Za-z0-9]{0,12}$`)

// WifiScan lists the networks a card can see. A card that is busy broadcasting its own network
// cannot scan, and says so.
func (s *Service) WifiScan(iface string) ([]WifiNetwork, error) {
	if !reWifiDev.MatchString(iface) {
		return nil, errors.New("that is not a wireless card")
	}
	exec.Command("rfkill", "unblock", "wifi").Run()
	exec.Command("ip", "link", "set", iface, "up").Run()
	out, err := exec.Command("iw", "dev", iface, "scan").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "busy") {
			return nil, errors.New("this card is busy; turn its network off for a moment to look around")
		}
		return nil, errors.New("the card could not scan: " + strings.TrimSpace(string(out)))
	}
	return ParseScan(string(out)), nil
}

// ParseScan reads `iw dev X scan` output. Hidden networks (no name) are left out and each name is
// listed once, at its strongest signal.
func ParseScan(text string) []WifiNetwork {
	var all []WifiNetwork
	var cur *WifiNetwork
	flush := func() {
		if cur != nil && cur.SSID != "" && !strings.Contains(cur.SSID, `\x00`) {
			all = append(all, *cur)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "BSS "):
			flush()
			cur = &WifiNetwork{Security: "open"}
		case cur == nil:
		case strings.HasPrefix(l, "freq:"):
			f, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(l, "freq:")), 64)
			cur.Freq = int(f)
		case strings.HasPrefix(l, "signal:"):
			f, _ := strconv.ParseFloat(strings.Fields(strings.TrimPrefix(l, "signal:"))[0], 64)
			cur.Signal = int(f)
		case strings.HasPrefix(l, "SSID:"):
			cur.SSID = unescape(strings.TrimSpace(strings.TrimPrefix(l, "SSID:")))
		case strings.HasPrefix(l, "RSN:"):
			cur.Security = "wpa2"
		case strings.HasPrefix(l, "WPA:") && cur.Security == "open":
			cur.Security = "wpa2"
		case strings.Contains(l, "Authentication suites:") && strings.Contains(l, "SAE") && !strings.Contains(l, "PSK"):
			cur.Security = "wpa3"
		}
	}
	flush()
	best := map[string]WifiNetwork{}
	for _, n := range all {
		if b, ok := best[n.SSID]; !ok || n.Signal > b.Signal {
			best[n.SSID] = n
		}
	}
	out := make([]WifiNetwork, 0, len(best))
	for _, n := range best {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Signal > out[j].Signal })
	return out
}

// unescape turns iw's \xNN sequences back into bytes.
func unescape(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s)+0 && s[i+1] == 'x' {
			if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b = append(b, byte(v))
				i += 3
				continue
			}
		}
		b = append(b, s[i])
	}
	return string(b)
}
