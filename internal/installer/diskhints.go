package installer

import (
	"fmt"
	"strings"
)

// DiskHints says what the machine knows about its storage, for the screen that finds no disk to
// install on. A disk the BIOS shows but the system does not see is usually a controller with no
// driver loaded, a controller set to a vendor mode (RAID) in the BIOS, or a disk that is too small.
// The lines name the controllers and the driver each one uses, so the cause can be read off the screen.
func (in *Installer) DiskHints() []string {
	blk, _ := in.R.Run("", "lsblk", "-d", "-b", "-n", "-o", "PATH,TYPE,SIZE,TRAN,MODEL")
	pci, _ := in.R.Run("", "lspci", "-nnk")
	return storageHints(blk, pci)
}

func storageHints(lsblkOut, lspciOut string) []string {
	var out []string
	// Disks the system sees, and why one is left out.
	seen := 0
	for _, l := range strings.Split(lsblkOut, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || f[1] != "disk" {
			continue
		}
		var size int64
		fmt.Sscan(f[2], &size)
		seen++
		note := ""
		if size < 4<<30 {
			note = "  (under 4 GB: too small to install on)"
		}
		out = append(out, fmt.Sprintf("Disk seen: %s %s %s%s", f[0], humanSize(size), strings.Join(f[3:], " "), note))
	}
	if seen == 0 {
		out = append(out, "The system sees no disks at all.")
	}
	// Storage controllers and the driver bound to each.
	var block []string
	flush := func() {
		if len(block) == 0 {
			return
		}
		head := strings.ToLower(block[0])
		if !strings.Contains(head, "sata") && !strings.Contains(head, "raid") && !strings.Contains(head, "scsi") &&
			!strings.Contains(head, "sas") && !strings.Contains(head, "ide") && !strings.Contains(head, "non-volatile") &&
			!strings.Contains(head, "mass storage") && !strings.Contains(head, "storage controller") {
			block = nil
			return
		}
		drv, mods := "none loaded", ""
		for _, l := range block[1:] {
			l = strings.TrimSpace(l)
			if v, ok := strings.CutPrefix(l, "Kernel driver in use:"); ok {
				drv = strings.TrimSpace(v)
			}
			if v, ok := strings.CutPrefix(l, "Kernel modules:"); ok {
				mods = strings.TrimSpace(v)
			}
		}
		line := "Controller: " + strings.TrimSpace(block[0]) + "  driver: " + drv
		if drv == "none loaded" && mods != "" {
			line += " (available: " + mods + ")"
		}
		out = append(out, line)
		block = nil
	}
	for _, l := range strings.Split(lspciOut, "\n") {
		if l == "" {
			flush()
			continue
		}
		if l[0] != ' ' && l[0] != '\t' {
			flush()
		}
		block = append(block, l)
	}
	flush()
	return out
}
