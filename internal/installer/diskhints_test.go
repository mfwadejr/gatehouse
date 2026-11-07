package installer

import (
	"strings"
	"testing"
)

func TestStorageHints(t *testing.T) {
	blk := "/dev/sda disk 64000000000 usb Live USB\n/dev/sdb disk 2000000000 sata Tiny DOM\n/dev/loop0 loop 1000000 \n"
	pci := `00:00.0 Host bridge [0600]: Intel Corporation Host bridge [8086:1234]
00:1f.2 SATA controller [0106]: Intel Corporation 6 Series SATA Controller [8086:1c02] (rev 05)
	Kernel driver in use: ahci
	Kernel modules: ahci
03:00.0 RAID bus controller [0104]: LSI Logic MegaRAID SAS [1000:005b]
	Kernel modules: megaraid_sas
01:00.0 Ethernet controller [0200]: Intel Corporation I350 [8086:1521]
	Kernel driver in use: igb
`
	got := strings.Join(storageHints(blk, pci), "\n")
	for _, want := range []string{
		"Disk seen: /dev/sda", "Disk seen: /dev/sdb", "too small",
		"SATA controller", "driver: ahci",
		"RAID bus controller", "driver: none loaded (available: megaraid_sas)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Ethernet") || strings.Contains(got, "Host bridge") || strings.Contains(got, "loop0") {
		t.Errorf("unrelated lines listed:\n%s", got)
	}
	if got := storageHints("", ""); !strings.Contains(strings.Join(got, "\n"), "no disks at all") {
		t.Errorf("empty case: %v", got)
	}
}
