package fw

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// With real packets: the kernel counts what each device sends to and receives from the internet.
func TestDeviceTrafficIsCountedByTheKernel(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	for _, b := range []string{"ip", "python3"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("needs %s", b)
		}
	}
	sh := func(cmd string) string {
		out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", cmd, err, out)
		}
		return string(out)
	}
	if out, err := exec.Command("ip", "netns", "add", "ghcl").CombinedOutput(); err != nil {
		t.Skipf("no network namespaces: %s", out)
	}
	sh("ip netns add ghgw; ip netns add ghsv")
	defer exec.Command("sh", "-c", "ip netns del ghcl; ip netns del ghgw; ip netns del ghsv").Run()
	// The peers are created inside the gateway's namespace, because the machine running the test may
	// already have a port called eth0 or eth1.
	sh(`set -e
ip link add ghc0 type veth peer name eth1 netns ghgw
ip link add ghs0 type veth peer name eth0 netns ghgw
ip link set ghc0 netns ghcl; ip link set ghs0 netns ghsv
ip -n ghcl addr add 10.1.0.2/24 dev ghc0; ip -n ghcl link set ghc0 up; ip -n ghcl link set lo up; ip -n ghcl route add default via 10.1.0.1
ip -n ghsv addr add 10.2.0.2/24 dev ghs0; ip -n ghsv link set ghs0 up; ip -n ghsv route add default via 10.2.0.1
ip -n ghgw addr add 10.1.0.1/24 dev eth1; ip -n ghgw addr add 10.2.0.1/24 dev eth0; ip -n ghgw link set eth1 up; ip -n ghgw link set eth0 up
ip netns exec ghgw sysctl -qw net.ipv4.ip_forward=1`)

	m := DefaultModel([]string{"eth0"}, []string{"eth1"})
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "add @acct_up4 { ip saddr }") || !strings.Contains(script, "add @acct_down4 { ip daddr }") {
		t.Fatalf("no counting rules:\n%s", script)
	}
	ap := NewApplier(t.TempDir())
	ap.Netns = "ghgw"
	if err := ap.run(script, false); err != nil {
		t.Fatalf("%v\n%s", err, script)
	}

	dir := t.TempDir()
	os.WriteFile(dir+"/gh_srv.py", []byte(`import socket
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.bind(("10.2.0.2",9000)); s.settimeout(4)
try:
    while True:
        d,a=s.recvfrom(2000); s.sendto(b"y"*1000,a)
except Exception: pass
`), 0o644)
	os.WriteFile(dir+"/gh_cli.py", []byte(`import socket
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.settimeout(1)
for i in range(20):
    s.sendto(b"x"*500,("10.2.0.2",9000))
    try: s.recvfrom(2000)
    except Exception: pass
`), 0o644)
	srv := exec.Command("ip", "netns", "exec", "ghsv", "python3", dir+"/gh_srv.py")
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Process.Kill()
	exec.Command("sleep", "0.5").Run()
	sh("ip netns exec ghcl python3 " + dir + "/gh_cli.py")

	cs, err := ap.Counters()
	if err != nil {
		t.Fatal(err)
	}
	up, down := cs[AcctPrefix+"up:10.1.0.2"], cs[AcctPrefix+"down:10.1.0.2"]
	// 20 datagrams of 500 bytes out and 1000 back, plus 28 bytes of IP and UDP header each.
	if up.Packets != 20 || up.Bytes != 20*528 || down.Packets != 20 || down.Bytes != 20*1028 {
		t.Fatalf("counted up %+v down %+v; all: %v", up, down, cs)
	}
}
