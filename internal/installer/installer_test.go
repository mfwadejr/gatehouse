package installer

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/seed"
)

type fake struct {
	cmds  []string
	stdin map[string]string
	fail  string
}

func (f *fake) Run(stdin, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	f.cmds = append(f.cmds, line)
	if stdin != "" {
		if f.stdin == nil {
			f.stdin = map[string]string{}
		}
		f.stdin[line] = stdin
	}
	if f.fail != "" && strings.HasPrefix(line, f.fail) {
		return "boom", fmt.Errorf("exit 1")
	}
	switch name {
	case "lsblk":
		b, _ := json.Marshal(map[string]any{"blockdevices": []map[string]any{
			{"path": "/dev/sda", "type": "disk", "size": 64 << 30, "model": "Live USB", "tran": "usb", "rm": true,
				"children": []map[string]any{{"path": "/dev/sda1", "mountpoints": []string{"/run/live/medium"}}}},
			{"path": "/dev/nvme0n1", "type": "disk", "size": 256 << 30, "model": "Samsung", "tran": "nvme", "rm": false},
			{"path": "/dev/loop0", "type": "loop", "size": 1 << 30},
		}})
		return string(b), nil
	case "blkid":
		return "1111-AAAA\n", nil
	}
	return "", nil
}

func newTest(t *testing.T) (*Installer, *fake) {
	f := &fake{}
	old := PortExists
	PortExists = func(n string) bool { return n == "enp1s0" || n == "enp2s0" || n == "enp3s0" }
	t.Cleanup(func() { PortExists = old })
	in := &Installer{R: f, Target: t.TempDir(), Squash: "/live/fs.squashfs", EFI: func() bool { return true }}
	os.MkdirAll(filepath.Join(in.Target, "etc"), 0o755)
	return in, f
}

func good() Request {
	return Request{Disk: "/dev/nvme0n1", Confirm: "/dev/nvme0n1", Hostname: "gatehouse", Username: "marvin", Password: "a long enough password", Timezone: "UTC",
		Network: seed.Network{LAN: seed.LAN{Port: "enp2s0", Mode: "router", Address: "192.168.1.1/24"}}}
}

func TestDisksMarkTheLiveMedium(t *testing.T) {
	in, _ := newTest(t)
	d, err := in.Disks()
	if err != nil || len(d) != 2 {
		t.Fatalf("%v %v", d, err)
	}
	if !d[0].InUse || d[1].InUse {
		t.Fatalf("in-use flags wrong: %+v", d)
	}
}

func TestValidate(t *testing.T) {
	in, _ := newTest(t)
	disks, _ := in.Disks()
	bad := map[string]func(*Request){
		"unknown disk":    func(r *Request) { r.Disk = "/dev/sdz"; r.Confirm = "/dev/sdz" },
		"live medium":     func(r *Request) { r.Disk = "/dev/sda"; r.Confirm = "/dev/sda" },
		"no confirmation": func(r *Request) { r.Confirm = "" },
		"bad hostname":    func(r *Request) { r.Hostname = "Bad Name;" },
		"bad user":        func(r *Request) { r.Username = "Root; rm" },
		"short password":  func(r *Request) { r.Password = "short" },
		"newline":         func(r *Request) { r.Password = "long enough password\nx" },
		"bad zone":        func(r *Request) { r.Timezone = "../../etc/passwd" },
		"no lan port":     func(r *Request) { r.Network.LAN.Port = "" },
		"no lan mode":     func(r *Request) { r.Network.LAN.Mode = "" },
		"missing port":    func(r *Request) { r.Network.LAN.Port = "enp9s9" },
		"wan same port":   func(r *Request) { r.Network.WAN = &seed.WAN{Port: "enp2s0", Type: "dhcp"} },
		"wan missing":     func(r *Request) { r.Network.WAN = &seed.WAN{Port: "enp8s0", Type: "dhcp"} },
	}
	for n, mut := range bad {
		r := good()
		mut(&r)
		if Validate(&r, disks) == nil {
			t.Errorf("%s accepted", n)
		}
	}
	g := good()
	if err := Validate(&g, disks); err != nil {
		t.Fatal(err)
	}
	// The internet connection is optional.
	g.Network.WAN = &seed.WAN{Port: "enp1s0", Type: "dhcp"}
	g.Network.Bridge = []string{"enp3s0"}
	if err := Validate(&g, disks); err != nil {
		t.Fatalf("lan with optional wan and bridge: %v", err)
	}
}

func TestPrivateOnly(t *testing.T) {
	for ip, want := range map[string]bool{"192.168.1.5": true, "10.1.2.3": true, "172.20.0.9": true, "169.254.4.4": true, "127.0.0.1": true,
		"100.64.5.5": true, "8.8.8.8": false, "203.0.113.9": false, "172.32.0.1": false} {
		if got := privateAddr(net.ParseIP(ip)); got != want {
			t.Errorf("%s: got %v want %v", ip, got, want)
		}
	}
}

func TestPartitionNames(t *testing.T) {
	if partition("/dev/nvme0n1", 2) != "/dev/nvme0n1p2" || partition("/dev/sda", 3) != "/dev/sda3" || partition("/dev/mmcblk0", 1) != "/dev/mmcblk0p1" {
		t.Fatal("partition naming")
	}
}

func wait(t *testing.T, in *Installer) Status {
	for i := 0; i < 200; i++ {
		s := in.Status()
		if s.State == "done" || s.State == "error" {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout")
	return Status{}
}

func TestInstallSequence(t *testing.T) {
	in, f := newTest(t)
	if err := in.Start(good()); err != nil {
		t.Fatal(err)
	}
	s := wait(t, in)
	if s.State != "done" {
		t.Fatalf("%+v\n%s", s, s.Log)
	}
	all := strings.Join(f.cmds, "\n")
	order := []string{"wipefs -a /dev/nvme0n1", "sgdisk --zap-all", "sgdisk -n1:0:+1M", "mkfs.vfat", "mkfs.ext4 -F -L gatehouse-root /dev/nvme0n1p3", "unsquashfs -f -d", "grub-install --target=x86_64-efi", "update-grub", "gatehouse user add", "gatehouse seed"}
	pos := -1
	for _, o := range order {
		i := strings.Index(all, o)
		if i < 0 || i < pos {
			t.Fatalf("%q missing or out of order in:\n%s", o, all)
		}
		pos = i
	}
	// The network choice reaches the seed command as JSON on standard input.
	seeded := false
	for line, v := range f.stdin {
		if strings.Contains(line, "gatehouse seed") && strings.Contains(v, `"port":"enp2s0"`) && strings.Contains(v, `"mode":"router"`) {
			seeded = true
		}
	}
	if !seeded {
		t.Fatalf("the network choice was not passed to seed: %v", f.stdin)
	}
	// The password goes to stdin, never into a command line.
	if strings.Contains(all, "a long enough password") {
		t.Fatal("password leaked into a command line")
	}
	found := false
	for _, v := range f.stdin {
		if strings.HasPrefix(v, "a long enough password") {
			found = true
		}
	}
	if !found {
		t.Fatal("password not delivered on stdin")
	}
	fs, _ := os.ReadFile(filepath.Join(in.Target, "etc/fstab"))
	if !strings.Contains(string(fs), "UUID=1111-AAAA / ext4") {
		t.Fatalf("fstab: %s", fs)
	}
	// Nothing may touch any other disk.
	for _, c := range f.cmds {
		if strings.Contains(c, "/dev/sda") && !strings.HasPrefix(c, "lsblk") {
			t.Fatalf("touched the live medium: %s", c)
		}
	}
}

func TestFailureStopsAndCleansUp(t *testing.T) {
	in, f := newTest(t)
	f.fail = "mkfs.ext4"
	in.Start(good())
	s := wait(t, in)
	if s.State != "error" || !strings.Contains(s.Error, "Formatting") {
		t.Fatalf("%+v", s)
	}
	if strings.Contains(strings.Join(f.cmds, "\n"), "grub-install") {
		t.Fatal("continued after a failure")
	}
	if in.Start(good()) != nil {
		t.Fatal("cannot retry after a failure")
	}
}

func TestGrubDefaultsAreQuietAndBranded(t *testing.T) {
	for _, want := range []string{`GRUB_DISTRIBUTOR="Gatehouse"`, "GRUB_TIMEOUT_STYLE=hidden", "splash", "GRUB_DISABLE_OS_PROBER=true"} {
		if !strings.Contains(grubDefaults, want) {
			t.Errorf("grub defaults lack %q", want)
		}
	}
	if strings.Contains(strings.ToLower(grubDefaults), "debian") {
		t.Error("grub defaults name another product")
	}
}
