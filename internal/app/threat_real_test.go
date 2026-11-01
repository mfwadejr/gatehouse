package app

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gatehouse/internal/threatcfg"
)

// pcapOne builds a capture holding one TCP packet from a home device to an outside address.
func pcapOne(payload string) []byte {
	var b []byte
	le := binary.LittleEndian
	h := make([]byte, 24)
	le.PutUint32(h[0:], 0xa1b2c3d4)
	le.PutUint16(h[4:], 2)
	le.PutUint16(h[6:], 4)
	le.PutUint32(h[16:], 65535)
	le.PutUint32(h[20:], 1)
	b = append(b, h...)
	eth := []byte{2, 0, 0, 0, 0, 2, 2, 0, 0, 0, 0, 1, 8, 0}
	ipLen := 20 + 20 + len(payload)
	ip := []byte{0x45, 0, byte(ipLen >> 8), byte(ipLen), 0, 1, 0, 0, 64, 6, 0, 0, 192, 168, 1, 20, 203, 0, 113, 9}
	// header checksum
	var sum uint32
	for i := 0; i < 20; i += 2 {
		sum += uint32(ip[i])<<8 | uint32(ip[i+1])
	}
	for sum > 0xffff {
		sum = sum&0xffff + sum>>16
	}
	cs := ^uint16(sum)
	ip[10], ip[11] = byte(cs>>8), byte(cs)
	tcp := []byte{0x30, 0x39, 0, 80, 0, 0, 0, 1, 0, 0, 0, 0, 0x50, 0x18, 0xff, 0xff, 0, 0, 0, 0}
	pkt := append(append(append(eth, ip...), tcp...), payload...)
	rec := make([]byte, 16)
	le.PutUint32(rec[0:], uint32(time.Now().Unix()))
	le.PutUint32(rec[8:], uint32(len(pkt)))
	le.PutUint32(rec[12:], uint32(len(pkt)))
	return append(append(b, rec...), pkt...)
}

// With the real engine installed, our settings file loads and an alert it writes is read by Gatehouse.
// Skipped on a machine without the engine; the CI machine has none, so this runs where it is installed.
func TestRealEngineAlertReachesGatehouse(t *testing.T) {
	bin, err := exec.LookPath("suricata")
	if err != nil {
		t.Skip("the threat-watching engine is not installed here")
	}
	dir := t.TempDir()
	inc := filepath.Join(dir, "gatehouse.yaml")
	os.WriteFile(inc, []byte(threatcfg.Include()), 0o644)
	rules := filepath.Join(dir, "t.rules")
	os.WriteFile(rules, []byte(`alert tcp $HOME_NET any -> $EXTERNAL_NET any (msg:"ET MALWARE Test Beacon"; content:"GATEHOUSE-TEST-BEACON"; classtype:trojan-activity; sid:9000001; rev:1;)`+"\n"), 0o644)
	pc := filepath.Join(dir, "t.pcap")
	os.WriteFile(pc, pcapOne("GET /GATEHOUSE-TEST-BEACON HTTP/1.1\r\n\r\n"), 0o644)
	logs := filepath.Join(dir, "logs")
	os.Mkdir(logs, 0o755)
	cmd := exec.Command(bin, "-c", "/etc/suricata/suricata.yaml", "--include", inc, "-S", rules, "-r", pc, "-l", logs, "-k", "none")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("engine: %v\n%s", err, out)
	}
	eve, err := os.ReadFile(filepath.Join(logs, "eve.json"))
	if err != nil || len(eve) == 0 {
		t.Fatalf("the engine wrote no alert: %v", err)
	}
	a := newScopeApp(t)
	a.Config.SetSetting(threatModeSetting, "watch")
	kept := false
	for _, l := range splitLines(eve) {
		kept = a.threatIngest(l, time.Now()) || kept
	}
	if !kept || countAlerts(a) != 1 {
		t.Fatalf("Gatehouse did not keep the real alert: %s", eve)
	}
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
