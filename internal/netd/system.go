package netd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

var (
	reHostname = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	reZone     = regexp.MustCompile(`^[A-Za-z0-9_+\-]+(/[A-Za-z0-9_+\-]+){0,2}$`)
)

// SetSystem changes the device name and time zone. Either may be empty to leave it alone.
func (s *Service) SetSystem(hostname, zone string) error {
	root := s.Root // empty on a real system; a directory in tests
	if hostname != "" {
		if !reHostname.MatchString(hostname) {
			return errors.New("the device name can use lowercase letters, digits and dashes")
		}
		if err := os.WriteFile(filepath.Join(root, "etc/hostname"), []byte(hostname+"\n"), 0o644); err != nil {
			return err
		}
		hosts := fmt.Sprintf("127.0.0.1 localhost\n127.0.1.1 %s %s.lan\n::1 localhost ip6-localhost ip6-loopback\n", hostname, hostname)
		if err := os.WriteFile(filepath.Join(root, "etc/hosts"), []byte(hosts), 0o644); err != nil {
			return err
		}
		if root == "" {
			unix.Sethostname([]byte(hostname))
		}
	}
	if zone != "" {
		if !reZone.MatchString(zone) || strings.Contains(zone, "..") {
			return errors.New("unknown time zone")
		}
		src := filepath.Join(root, "usr/share/zoneinfo", zone)
		if st, err := os.Stat(src); err != nil || st.IsDir() {
			return errors.New("unknown time zone")
		}
		link := filepath.Join(root, "etc/localtime")
		os.Remove(link)
		if err := os.Symlink("/usr/share/zoneinfo/"+zone, link); err != nil {
			return err
		}
		os.WriteFile(filepath.Join(root, "etc/timezone"), []byte(zone+"\n"), 0o644)
	}
	return nil
}
