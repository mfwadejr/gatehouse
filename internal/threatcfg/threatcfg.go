// Package threatcfg writes the files that run the threat-watching engine (Suricata) in watch-only
// mode: its settings, its service and which rule groups are on. It only builds text; the helper
// that runs as root writes the files and starts the service.
//
// Watch only means the engine reads copies of the packets on the inside ports. Nothing is held up
// or dropped, so if the engine stops, crashes or is removed, traffic is not touched.
package threatcfg

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Plan says how the engine should run.
type Plan struct {
	Enabled    bool     `json:"enabled"`
	Ifaces     []string `json:"ifaces"`
	Preset     string   `json:"preset"`      // light, balanced or strict
	CPUPercent int      `json:"cpu_percent"` // of one core, 10 to 400
	MemoryMB   int      `json:"memory_mb"`   // 256 to 16384
	OffSIDs    []int    `json:"off_sids"`    // single rules the person switched off
}

const (
	IncludeFile = "etc/gatehouse/suricata.yaml"
	UnitFile    = "etc/systemd/system/gatehouse-suricata.service"
	DisableConf = "etc/suricata/disable.conf"
	UnitName    = "gatehouse-suricata.service"
	// EveFile is where the engine writes what it notices. Gatehouse reads it and never writes to it.
	EveFile = "/var/log/suricata/eve.json"
)

var reIface = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)

// Check validates a plan and fills in the defaults.
func (p *Plan) Check() error {
	if p.Preset == "" {
		p.Preset = "balanced"
	}
	if _, ok := presets[p.Preset]; !ok {
		return errors.New("choose Light, Balanced or Strict")
	}
	if p.CPUPercent == 0 {
		p.CPUPercent = 50
	}
	if p.CPUPercent < 10 || p.CPUPercent > 400 {
		return errors.New("the processor limit must be between 10% and 400% of one core")
	}
	if p.MemoryMB == 0 {
		p.MemoryMB = 1024
	}
	if p.MemoryMB < 256 || p.MemoryMB > 16384 {
		return errors.New("the memory limit must be between 256 MB and 16 GB")
	}
	for _, s := range p.OffSIDs {
		if s <= 0 {
			return errors.New("a rule number is not valid")
		}
	}
	if p.Enabled && len(p.Ifaces) == 0 {
		return errors.New("there is no inside port to watch")
	}
	for _, i := range p.Ifaces {
		if !reIface.MatchString(i) {
			return fmt.Errorf("%q is not a usable port name", i)
		}
	}
	return nil
}

// Include is the settings file added on top of the engine's own. It keeps only alerts in the log
// (no flow, DNS or HTTP records), switches statistics off and asks for low memory use.
func Include() string {
	return `%YAML 1.1
---
# Written by Gatehouse. Watch only: the engine reads copies of packets and changes nothing.
vars:
  address-groups:
    HOME_NET: "[192.168.0.0/16,10.0.0.0/8,172.16.0.0/12]"
    EXTERNAL_NET: "!$HOME_NET"
outputs:
  - eve-log:
      enabled: yes
      filetype: regular
      filename: eve.json
      types:
        - alert
stats:
  enabled: no
detect:
  profile: low
`
}

// Unit is the service that runs the engine with a processor and memory limit.
func Unit(p Plan) string {
	var args strings.Builder
	for _, i := range p.Ifaces {
		args.WriteString(" -i " + i)
	}
	return fmt.Sprintf(`[Unit]
Description=Gatehouse threat watching (Suricata, watch only)
After=network.target

[Service]
ExecStart=/usr/bin/suricata -c /etc/suricata/suricata.yaml --include /etc/gatehouse/suricata.yaml --pidfile /run/gatehouse-suricata.pid%s
Restart=on-failure
RestartSec=30
Nice=10
CPUAccounting=yes
MemoryAccounting=yes
CPUQuota=%d%%
MemoryMax=%dM

[Install]
WantedBy=multi-user.target
`, args.String(), p.CPUPercent, p.MemoryMB)
}

// groups are the Emerging Threats Open rule groups. A preset is a list of groups switched off.
var allGroups = []string{
	"activex", "adware_pup", "attack_response", "chat", "coinminer", "current_events", "deleted", "dns", "dos",
	"exploit", "exploit_kit", "ftp", "games", "hunting", "icmp", "icmp_info", "imap", "inappropriate", "info", "ja3",
	"malware", "misc", "mobile_malware", "netbios", "p2p", "phishing", "policy", "pop3", "rpc", "scada", "scan",
	"shellcode", "smtp", "snmp", "sql", "telnet", "tftp", "user_agents", "voip", "web_client", "web_server",
	"web_specific_apps", "worm",
}

var strictOff = []string{"deleted", "scada", "games", "chat", "voip", "activex"}
var balancedOff = append(append([]string{}, strictOff...),
	"policy", "p2p", "info", "icmp_info", "hunting", "user_agents", "inappropriate", "scan", "ja3", "snmp",
	"ftp", "imap", "pop3", "smtp", "web_server", "web_specific_apps", "sql")
var lightKeep = map[string]bool{"malware": true, "exploit_kit": true, "coinminer": true, "phishing": true,
	"mobile_malware": true, "current_events": true, "attack_response": true, "worm": true}

type presetInfo struct {
	Title string
	Text  string
	Off   []string
}

var presets = map[string]presetInfo{
	"light":    {"Light", "Only the clearest signs of infection: known malware, bad servers, scams and miners. The fewest alerts.", nil},
	"balanced": {"Balanced", "Malware, break-in attempts and tricks, without the rules meant for servers or noisy policy checks. Recommended for a home.", balancedOff},
	"strict":   {"Strict", "Nearly everything except rules that rarely matter at home. More alerts, more to review.", strictOff},
}

func init() {
	var off []string
	for _, g := range allGroups {
		if !lightKeep[g] {
			off = append(off, g)
		}
	}
	p := presets["light"]
	p.Off = off
	presets["light"] = p
}

// Preset describes a preset in plain words.
type Preset struct {
	Key   string   `json:"key"`
	Title string   `json:"title"`
	Text  string   `json:"text"`
	Off   []string `json:"-"`
}

// Presets lists them in the order the console shows.
func Presets() []Preset {
	var out []Preset
	for _, k := range []string{"light", "balanced", "strict"} {
		out = append(out, Preset{k, presets[k].Title, presets[k].Text, presets[k].Off})
	}
	return out
}

// Groups lists the rule groups with a plain description each, for the Advanced tab.
func Groups() []Group {
	desc := map[string]string{
		"malware": "Known malware calling home", "current_events": "Fresh threats being spread right now", "exploit_kit": "Sites that attack browsers",
		"phishing": "Sites that trick people", "coinminer": "Hidden coin miners", "mobile_malware": "Malware on phones", "worm": "Self-spreading malware",
		"attack_response": "Signs a break-in worked", "exploit": "Attacks on software", "shellcode": "Code used to take over a device", "dos": "Floods that overwhelm a device",
		"scan": "Someone probing for open doors", "policy": "Traffic some places ban, usually harmless at home", "p2p": "File sharing", "games": "Online games", "chat": "Chat programs",
		"info": "Interesting but usually harmless", "web_server": "Attacks on web servers you run", "web_specific_apps": "Attacks on particular web programs", "sql": "Attacks on databases",
		"dns": "Odd DNS use", "user_agents": "Programs that name themselves unusually", "inappropriate": "Adult content", "voip": "Phone calls over the internet", "scada": "Factory equipment",
	}
	var out []Group
	for _, g := range allGroups {
		d := desc[g]
		if d == "" {
			d = "Other checks"
		}
		out = append(out, Group{Name: g, Text: d})
	}
	return out
}

// Group is one rule group.
type Group struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// DisableList is the disable.conf the rule updater reads: the preset's groups and any single rules
// the person switched off. Anything else stays on.
func DisableList(p Plan) string {
	var b strings.Builder
	b.WriteString("# Written by Gatehouse\n")
	off := append([]string{}, presets[p.Preset].Off...)
	sort.Strings(off)
	for _, g := range off {
		b.WriteString("group:emerging-" + g + ".rules\n")
	}
	sids := append([]int{}, p.OffSIDs...)
	sort.Ints(sids)
	for _, s := range sids {
		fmt.Fprintf(&b, "%d\n", s)
	}
	return b.String()
}
