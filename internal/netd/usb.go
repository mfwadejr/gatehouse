package netd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Backup copies to a USB drive. The drive is only mounted for the moment a copy is written, with
// programs and device files switched off on it, and is unmounted again before this returns, so it
// can be pulled out at any other time.

// UsbDisk is a partition on a removable drive.
type UsbDisk struct {
	UUID   string `json:"uuid"`
	Label  string `json:"label"`
	FSType string `json:"fstype"`
	Size   int64  `json:"size"`
	Model  string `json:"model"`
}

var (
	reUsbUUID = regexp.MustCompile(`^[A-Za-z0-9-]{4,64}$`)
	reUsbName = regexp.MustCompile(`^(auto|manual)-\d{8}-\d{6}\.ghbk$`)
)

const usbFolder = "NopeWarden"

type lsblkNode struct {
	Name       string      `json:"name"`
	Type       string      `json:"type"`
	Size       int64       `json:"size"`
	FSType     string      `json:"fstype"`
	Label      string      `json:"label"`
	UUID       string      `json:"uuid"`
	Tran       string      `json:"tran"`
	Model      string      `json:"model"`
	RM         bool        `json:"rm"`
	Hotplug    bool        `json:"hotplug"`
	Mountpoint string      `json:"mountpoint"`
	Children   []lsblkNode `json:"children"`
}

// parseUsb finds the partitions with a file system on drives that plug in (USB), whatever the
// partition itself reports. A drive that is the system disk is never listed.
func parseUsb(js []byte) ([]UsbDisk, error) {
	var top struct {
		Blockdevices []lsblkNode `json:"blockdevices"`
	}
	if err := json.Unmarshal(js, &top); err != nil {
		return nil, errors.New("the drive list was not understood")
	}
	out := []UsbDisk{}
	var walk func(n lsblkNode, usb bool, model string)
	walk = func(n lsblkNode, usb bool, model string) {
		if n.Type == "disk" {
			usb = n.Tran == "usb"
			model = strings.TrimSpace(n.Model)
		}
		if usb && n.UUID != "" && n.FSType != "" && n.FSType != "swap" && !strings.HasPrefix(n.FSType, "LVM") && !strings.HasPrefix(n.FSType, "crypto") &&
			n.Mountpoint != "/" && !strings.HasPrefix(n.Mountpoint, "/boot") && reUsbUUID.MatchString(n.UUID) {
			out = append(out, UsbDisk{UUID: n.UUID, Label: n.Label, FSType: n.FSType, Size: n.Size, Model: model})
		}
		for _, c := range n.Children {
			walk(c, usb, model)
		}
	}
	for _, n := range top.Blockdevices {
		walk(n, false, "")
	}
	return out, nil
}

// UsbList lists the USB drives that are plugged in.
func (s *Service) UsbList() ([]UsbDisk, error) {
	if _, err := lookPath("lsblk"); err != nil {
		return nil, errors.New("this system cannot list drives")
	}
	out, err := sysRun(15*time.Second, "lsblk", "-J", "-b", "-o", "NAME,TYPE,SIZE,FSTYPE,LABEL,UUID,TRAN,MODEL,MOUNTPOINT")
	if err != nil {
		return nil, fmt.Errorf("the drives could not be listed: %s", smartLastLine(out))
	}
	return parseUsb([]byte(out))
}

// copyToDrive writes src as name into the NopeWarden folder of a mounted drive, then removes the
// oldest copies beyond keep. Nothing else on the drive is touched.
func copyToDrive(mnt, src, name string, keep int) error {
	if !reUsbName.MatchString(name) {
		return errors.New("that is not a backup file name")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	dir := filepath.Join(mnt, usbFolder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not make the folder on the drive: %v", err)
	}
	tmp := filepath.Join(dir, name+".part")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("could not write to the drive (is it read-only?): %v", err)
	}
	_, err = io.Copy(f, in)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing to the drive failed (is it full?): %v", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	ents, _ := os.ReadDir(dir)
	var mine []string
	for _, e := range ents {
		if reUsbName.MatchString(e.Name()) {
			mine = append(mine, e.Name())
		}
	}
	sort.Strings(mine)
	for len(mine) > keep && keep > 0 {
		os.Remove(filepath.Join(dir, mine[0]))
		mine = mine[1:]
	}
	return nil
}

// UsbCopy mounts the drive with this UUID, copies one backup onto it and unmounts it again. src must
// be a file in a folder named offsite-out, so this cannot be used to copy other files off the machine.
func (s *Service) UsbCopy(uuid, src, name string, keep int) error {
	if !reUsbUUID.MatchString(uuid) {
		return errors.New("that is not a drive NopeWarden can use")
	}
	if filepath.Base(filepath.Dir(src)) != "offsite-out" || filepath.Base(src) != name {
		return errors.New("that is not a file NopeWarden can copy")
	}
	if st, err := os.Lstat(src); err != nil || !st.Mode().IsRegular() {
		return errors.New("the backup file is not there")
	}
	disks, err := s.UsbList()
	if err != nil {
		return err
	}
	found := false
	for _, d := range disks {
		found = found || d.UUID == uuid
	}
	if !found {
		return errors.New("the USB drive is not plugged in")
	}
	mnt, err := os.MkdirTemp("", "nopewarden-usb-")
	if err != nil {
		return err
	}
	defer os.Remove(mnt)
	if out, err := sysRun(60*time.Second, "mount", "-o", "nosuid,nodev,noexec", "UUID="+uuid, mnt); err != nil {
		return fmt.Errorf("the drive could not be opened: %s", smartLastLine(out))
	}
	cerr := copyToDrive(mnt, src, name, keep)
	sysRun(30*time.Second, "sync")
	var uerr error
	for i := 0; i < 5; i++ {
		if _, uerr = sysRun(30*time.Second, "umount", mnt); uerr == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if cerr != nil {
		return cerr
	}
	if uerr != nil {
		return errors.New("the copy was written but the drive could not be closed cleanly; wait a minute before unplugging it")
	}
	return nil
}
