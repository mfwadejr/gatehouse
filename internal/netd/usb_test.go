package netd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseUsb(t *testing.T) {
	js := `{"blockdevices":[
	 {"name":"sda","type":"disk","tran":"sata","children":[{"name":"sda1","type":"part","size":1000,"fstype":"ext4","uuid":"aaaa-1111","mountpoint":"/"}]},
	 {"name":"sdb","type":"disk","tran":"usb","model":"Flash Drive ","children":[
	   {"name":"sdb1","type":"part","size":8000000,"fstype":"vfat","label":"BACKUP","uuid":"ABCD-1234"},
	   {"name":"sdb2","type":"part","size":5,"fstype":"swap","uuid":"zzzz"}]}]}`
	d, err := parseUsb([]byte(js))
	if err != nil || len(d) != 1 || d[0].UUID != "ABCD-1234" || d[0].Label != "BACKUP" || d[0].Model != "Flash Drive" {
		t.Fatalf("%v %v", d, err)
	}
}

func TestCopyToDrive(t *testing.T) {
	mnt, src := t.TempDir(), filepath.Join(t.TempDir(), "offsite-out")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(filepath.Join(mnt, usbFolder), 0o755)
	os.WriteFile(filepath.Join(mnt, usbFolder, "other.txt"), []byte("mine"), 0o644)
	for _, n := range []string{"auto-20261124-010000.ghbk", "auto-20261125-010000.ghbk", "auto-20261126-010000.ghbk"} {
		p := filepath.Join(src, n)
		os.WriteFile(p, []byte("data "+n), 0o600)
		if err := copyToDrive(mnt, p, n, 2); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(mnt, usbFolder))
	if len(ents) != 3 { // two copies and the file that is not ours
		t.Fatalf("%d entries", len(ents))
	}
	if _, err := os.Stat(filepath.Join(mnt, usbFolder, "auto-20261124-010000.ghbk")); err == nil {
		t.Fatal("oldest copy should be removed")
	}
	if _, err := os.Stat(filepath.Join(mnt, usbFolder, "other.txt")); err != nil {
		t.Fatal("other files are left alone")
	}
	if err := copyToDrive(mnt, "/etc/passwd", "../x", 2); err == nil {
		t.Fatal("bad name refused")
	}
	s := &Service{}
	if err := s.UsbCopy("ABCD-1234", "/etc/shadow", "auto-20261126-010000.ghbk", 2); err == nil {
		t.Fatal("only files from offsite-out are copied")
	}
}
