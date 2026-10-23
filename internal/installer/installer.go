// Package installer writes Gatehouse to a disk from the live image. It runs only on the
// live system, behind a one-time code shown on the machine's own screen.
package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Runner executes commands. Tests replace it.
type Runner interface {
	Run(stdin string, name string, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(stdin, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	return out.String(), err
}

// Disk is something the system could be installed on.
type Disk struct {
	Path      string `json:"path"`
	Model     string `json:"model"`
	Size      int64  `json:"size"`
	Transport string `json:"transport"`
	Removable bool   `json:"removable"`
	InUse     bool   `json:"in_use"` // holds the live medium
	Parts     int    `json:"partitions"`
}

// Request is what the person chose: only the disk. Everything else (the administrator account,
// the networks, the device name and the time zone) is set in the browser on first start.
type Request struct {
	Disk    string `json:"disk"`
	Confirm string `json:"confirm"` // must repeat the disk path
}

// Step is one stage of the installation.
type Step struct {
	Name  string `json:"name"`
	State string `json:"state"` // waiting, running, done, error
}

// Status is polled by the console.
type Status struct {
	State string `json:"state"` // idle, running, done, error
	Steps []Step `json:"steps"`
	Error string `json:"error,omitempty"`
	Log   string `json:"log,omitempty"`
}

// Installer holds the state of one installation.
type Installer struct {
	R        Runner
	Target   string // mount point of the new system
	Squash   string // path of the root filesystem image on the live medium
	EFI      func() bool
	RebootFn func() // replaces the real restart in tests
	Binary   string // path to the gatehouse binary, for creating the account inside the target
	mu       sync.Mutex
	st       Status
	logBuf   strings.Builder
	started  bool
}

func New() *Installer {
	return &Installer{R: execRunner{}, Target: "/mnt/target", Squash: "/run/live/medium/live/filesystem.squashfs",
		EFI: func() bool { _, err := os.Stat("/sys/firmware/efi"); return err == nil }, Binary: "/usr/local/bin/gatehouse"}
}

func (in *Installer) Status() Status {
	in.mu.Lock()
	defer in.mu.Unlock()
	s := in.st
	s.Steps = append([]Step(nil), in.st.Steps...)
	if s.State == "" {
		s.State = "idle"
	}
	l := in.logBuf.String()
	if len(l) > 6000 {
		l = l[len(l)-6000:]
	}
	s.Log = l
	return s
}

const (
	defaultHost = "gatehouse"
	defaultZone = "UTC"
)

var (
	reHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	reUser = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,31}$`)
	reTZ   = regexp.MustCompile(`^[A-Za-z0-9_+\-]+(/[A-Za-z0-9_+\-]+){0,2}$`)
)

// Validate checks a request against the disks that are actually present.
func Validate(r *Request, disks []Disk) error {
	var found *Disk
	for i := range disks {
		if disks[i].Path == r.Disk {
			found = &disks[i]
		}
	}
	switch {
	case found == nil:
		return errors.New("choose one of the listed disks")
	case found.InUse:
		return errors.New("that disk holds the installer itself; choose another")
	case r.Confirm != r.Disk:
		return errors.New("type the disk name to confirm that everything on it will be erased")
	}
	return nil
}

// partition returns the path of partition n on a disk (nvme0n1p2, sda2).
func partition(disk string, n int) string {
	last := disk[len(disk)-1]
	if last >= '0' && last <= '9' {
		return disk + "p" + strconv.Itoa(n)
	}
	return disk + strconv.Itoa(n)
}

type lsblk struct {
	Blockdevices []struct {
		Path     string `json:"path"`
		Type     string `json:"type"`
		Size     int64  `json:"size"`
		Model    string `json:"model"`
		Tran     string `json:"tran"`
		RM       bool   `json:"rm"`
		Children []struct {
			Path        string   `json:"path"`
			Mountpoints []string `json:"mountpoints"`
		} `json:"children"`
	} `json:"blockdevices"`
}

// Disks lists candidate disks and marks the one the live system booted from.
func (in *Installer) Disks() ([]Disk, error) {
	out, err := in.R.Run("", "lsblk", "-J", "-b", "-o", "PATH,TYPE,SIZE,MODEL,TRAN,RM,MOUNTPOINTS")
	if err != nil {
		return nil, fmt.Errorf("could not list disks: %w", err)
	}
	var l lsblk
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		return nil, err
	}
	disks := []Disk{}
	for _, d := range l.Blockdevices {
		if d.Type != "disk" || d.Size < 4<<30 {
			continue
		}
		x := Disk{Path: d.Path, Model: strings.TrimSpace(d.Model), Size: d.Size, Transport: d.Tran, Removable: d.RM, Parts: len(d.Children)}
		for _, c := range d.Children {
			for _, m := range c.Mountpoints {
				if m == "/run/live/medium" || m == "/lib/live/mount/medium" || m == "/cdrom" {
					x.InUse = true
				}
			}
		}
		disks = append(disks, x)
	}
	return disks, nil
}

func (in *Installer) logf(f string, a ...any) {
	in.mu.Lock()
	fmt.Fprintf(&in.logBuf, f+"\n", a...)
	in.mu.Unlock()
}

func (in *Installer) setStep(i int, state string) {
	in.mu.Lock()
	in.st.Steps[i].State = state
	in.mu.Unlock()
}

// Start validates the request and runs the installation in the background.
func (in *Installer) Start(r Request) error {
	disks, err := in.Disks()
	if err != nil {
		return err
	}
	if err := Validate(&r, disks); err != nil {
		return err
	}
	in.mu.Lock()
	if in.started {
		in.mu.Unlock()
		return errors.New("an installation has already started")
	}
	in.started = true
	in.mu.Unlock()
	steps := in.plan(r)
	in.mu.Lock()
	in.st = Status{State: "running"}
	for _, s := range steps {
		in.st.Steps = append(in.st.Steps, Step{Name: s.name, State: "waiting"})
	}
	in.mu.Unlock()
	go func() {
		for i, s := range steps {
			in.setStep(i, "running")
			if err := s.fn(); err != nil {
				in.setStep(i, "error")
				in.mu.Lock()
				in.st.State, in.st.Error = "error", fmt.Sprintf("%s: %v", s.name, err)
				in.started = false
				in.mu.Unlock()
				in.cleanup()
				return
			}
			in.setStep(i, "done")
		}
		in.mu.Lock()
		in.st.State = "done"
		in.mu.Unlock()
	}()
	return nil
}

type step struct {
	name string
	fn   func() error
}

func (in *Installer) sh(stdin, name string, args ...string) error {
	in.logf("$ %s %s", name, strings.Join(args, " "))
	out, err := in.R.Run(stdin, name, args...)
	if s := strings.TrimSpace(out); s != "" {
		in.logf("%s", s)
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func (in *Installer) chroot(stdin string, args ...string) error {
	return in.sh(stdin, "chroot", append([]string{in.Target}, args...)...)
}

func (in *Installer) uuid(dev string) (string, error) {
	out, err := in.R.Run("", "blkid", "-s", "UUID", "-o", "value", dev)
	if err != nil {
		return "", fmt.Errorf("blkid %s: %w", dev, err)
	}
	return strings.TrimSpace(out), nil
}

func (in *Installer) plan(r Request) []step {
	efi := in.EFI()
	d := r.Disk
	bios, esp, root := partition(d, 1), partition(d, 2), partition(d, 3)
	t := in.Target
	bind := []string{"/dev", "/dev/pts", "/proc", "/sys", "/run"}
	return []step{
		{"Erasing the disk", func() error {
			in.sh("", "swapoff", "-a")
			if err := in.sh("", "wipefs", "-a", d); err != nil {
				return err
			}
			return in.sh("", "sgdisk", "--zap-all", d)
		}},
		{"Creating partitions", func() error {
			if err := in.sh("", "sgdisk", "-n1:0:+1M", "-t1:ef02", "-c1:bios", "-n2:0:+512M", "-t2:ef00", "-c2:efi", "-n3:0:0", "-t3:8300", "-c3:gatehouse", d); err != nil {
				return err
			}
			in.sh("", "partprobe", d)
			in.sh("", "udevadm", "settle")
			return nil
		}},
		{"Formatting", func() error {
			_ = bios
			if err := in.sh("", "mkfs.vfat", "-F", "32", "-n", "GATEHOUSE", esp); err != nil {
				return err
			}
			return in.sh("", "mkfs.ext4", "-F", "-L", "gatehouse-root", root)
		}},
		{"Copying the system", func() error {
			if err := os.MkdirAll(t, 0o755); err != nil {
				return err
			}
			if err := in.sh("", "mount", root, t); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(t, "boot/efi"), 0o755); err != nil {
				return err
			}
			if err := in.sh("", "mount", esp, filepath.Join(t, "boot/efi")); err != nil {
				return err
			}
			return in.sh("", "unsquashfs", "-f", "-d", t, in.Squash)
		}},
		{"Setting up boot files", func() error {
			ru, err := in.uuid(root)
			if err != nil {
				return err
			}
			eu, err := in.uuid(esp)
			if err != nil {
				return err
			}
			fstab := fmt.Sprintf("UUID=%s / ext4 errors=remount-ro,noatime 0 1\nUUID=%s /boot/efi vfat umask=0077 0 2\ntmpfs /tmp tmpfs defaults,nosuid,nodev 0 0\n", ru, eu)
			if err := os.WriteFile(filepath.Join(t, "etc/fstab"), []byte(fstab), 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(t, "etc/hostname"), []byte(defaultHost+"\n"), 0o644); err != nil {
				return err
			}
			hosts := fmt.Sprintf("127.0.0.1 localhost\n127.0.1.1 %s %s.lan\n::1 localhost ip6-localhost ip6-loopback\n", defaultHost, defaultHost)
			if err := os.WriteFile(filepath.Join(t, "etc/hosts"), []byte(hosts), 0o644); err != nil {
				return err
			}
			os.Remove(filepath.Join(t, "etc/localtime"))
			if err := os.Symlink("/usr/share/zoneinfo/"+defaultZone, filepath.Join(t, "etc/localtime")); err != nil {
				return err
			}
			os.WriteFile(filepath.Join(t, "etc/timezone"), []byte(defaultZone+"\n"), 0o644)
			for _, b := range bind {
				os.MkdirAll(filepath.Join(t, b), 0o755)
				if err := in.sh("", "mount", "--bind", b, filepath.Join(t, b)); err != nil {
					return err
				}
			}
			if efi {
				in.sh("", "mount", "-t", "efivarfs", "efivarfs", filepath.Join(t, "sys/firmware/efi/efivars"))
			}
			return nil
		}},
		{"Installing the boot loader", func() error {
			if efi {
				if err := in.chroot("", "grub-install", "--target=x86_64-efi", "--efi-directory=/boot/efi", "--removable", "--no-nvram"); err != nil {
					return err
				}
				in.chroot("", "grub-install", "--target=x86_64-efi", "--efi-directory=/boot/efi", "--bootloader-id=gatehouse") // best effort: a firmware entry too
			} else if err := in.chroot("", "grub-install", "--target=i386-pc", d); err != nil {
				return err
			}
			if err := in.brand(t); err != nil {
				return err
			}
			if err := in.chroot("", "update-initramfs", "-u", "-k", "all"); err != nil {
				return err
			}
			if err := in.chroot("", "update-grub"); err != nil {
				return err
			}
			// Menu entries would otherwise read "Gatehouse GNU/Linux".
			in.chroot("", "sed", "-i", "s# GNU/Linux##g", "/boot/grub/grub.cfg")
			return nil
		}},
		{"Preparing first start", func() error {
			if err := in.chroot("", "install", "-d", "-o", "gatehouse", "-g", "gatehouse", "-m", "750", "/var/lib/gatehouse"); err != nil {
				return err
			}
			return in.chroot("", "chown", "-R", "gatehouse:gatehouse", "/var/lib/gatehouse")
		}},
		{"Finishing", func() error {
			in.sh("", "sync")
			in.cleanup()
			return nil
		}},
	}
}

// cleanup unmounts everything under the target, deepest first.
func (in *Installer) cleanup() {
	for _, b := range []string{"sys/firmware/efi/efivars", "run", "sys", "proc", "dev/pts", "dev", "boot/efi", ""} {
		in.R.Run("", "umount", "-l", filepath.Join(in.Target, b))
	}
}

// Reboot restarts the machine after a finished installation.
func (in *Installer) Reboot() error {
	if in.Status().State != "done" {
		return errors.New("the installation has not finished")
	}
	go func() {
		time.Sleep(time.Second)
		in.sh("", "sync")
		in.reboot()
	}()
	return nil
}

// reboot restarts the machine straight from the kernel. The live system runs from the USB
// stick, and people pull the stick out as they restart; starting another program (systemctl,
// the shutdown scripts) would then fail to load and the machine would hang on a frozen screen.
// By now everything is written and unmounted, so nothing is lost by skipping them.
func (in *Installer) reboot() {
	if in.RebootFn != nil {
		in.RebootFn()
		return
	}
	unix.Sync()
	if err := unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART); err != nil {
		in.R.Run("", "systemctl", "reboot", "--force", "--force")
	}
}

// PinMemory keeps this program in memory, so that it still runs after the USB stick it
// was loaded from is removed.
func PinMemory() { unix.Mlockall(unix.MCL_CURRENT | unix.MCL_FUTURE) }

// grubDefaults makes the installed machine start the way the installer did: no menu unless
// a key is held, no kernel text, the Gatehouse splash, and nothing that names another product.
const grubDefaults = `# Written by the Gatehouse installer.
GRUB_DISTRIBUTOR="Gatehouse"
GRUB_TIMEOUT=0
GRUB_TIMEOUT_STYLE=hidden
GRUB_CMDLINE_LINUX_DEFAULT="quiet splash usbcore.autosuspend=-1 loglevel=0 systemd.show_status=0 vt.global_cursor_default=0"
GRUB_TERMINAL_OUTPUT=gfxterm
GRUB_GFXMODE=auto
GRUB_GFXPAYLOAD_LINUX=keep
GRUB_THEME=/boot/grub/themes/gatehouse/theme.txt
GRUB_DISABLE_OS_PROBER=true
GRUB_DISABLE_RECOVERY=true
GRUB_DISABLE_SUBMENU=y
`

// brand puts the Gatehouse boot menu and splash into the installed system.
func (in *Installer) brand(t string) error {
	if err := os.MkdirAll(filepath.Join(t, "etc/default/grub.d"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(t, "etc/default/grub.d/gatehouse.cfg"), []byte(grubDefaults), 0o644); err != nil {
		return err
	}
	// The theme ships inside the image; copy it to where the boot loader reads it.
	if err := in.sh("", "mkdir", "-p", filepath.Join(t, "boot/grub/themes")); err != nil {
		return err
	}
	in.sh("", "rm", "-rf", filepath.Join(t, "boot/grub/themes/gatehouse"))
	if err := in.sh("", "cp", "-a", filepath.Join(t, "usr/share/gatehouse/grub-theme"), filepath.Join(t, "boot/grub/themes/gatehouse")); err != nil {
		return err
	}
	return in.chroot("", "plymouth-set-default-theme", "gatehouse")
}
