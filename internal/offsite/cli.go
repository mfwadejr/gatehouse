package offsite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// SFTP and SMB use the system's own sftp and smbclient programs: they do the secure parts, are kept
// up to date by the system, and are not copied into NopeWarden. SFTP signs in with a key NopeWarden
// made for this machine (the person adds its public half to the server); SMB signs in with a user
// name and password given to smbclient in a private temporary file, never on its command line.

var runCLI = func(name string, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

var rePathOK = regexp.MustCompile(`^[A-Za-z0-9 _./-]*$`)
var reHostOK = regexp.MustCompile(`^[A-Za-z0-9.:_-]+$`)

func tempFile(data []byte) (string, error) {
	f, err := os.CreateTemp("", "nw-off-")
	if err != nil {
		return "", err
	}
	defer f.Close()
	os.Chmod(f.Name(), 0o600)
	_, err = f.Write(data)
	return f.Name(), err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return firstLine(strings.TrimSpace(lines[len(lines)-1]))
}

// ---- SFTP ----

type SFTP struct {
	Host, User, Dir, KeyFile, KnownHosts string
	Port                                 string
}

func (s *SFTP) args() ([]string, error) {
	if !reHostOK.MatchString(s.Host) || !rePathOK.MatchString(s.Dir) || strings.Contains(s.Dir, "..") || s.User == "" || strings.ContainsAny(s.User, " \"'\n@") {
		return nil, errors.New("SFTP: check the server, user and folder; they contain a character that is not allowed")
	}
	port := s.Port
	if port == "" {
		port = "22"
	}
	return []string{"-b", "-", "-q", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "-o", "UserKnownHostsFile=" + s.KnownHosts,
		"-o", "IdentitiesOnly=yes", "-o", "ConnectTimeout=20", "-i", s.KeyFile, "-P", port, s.User + "@" + s.Host}, nil
}

// Check says whether the settings are usable, without connecting.
func (s *SFTP) Check() error { _, err := s.args(); return err }

func sftpErr(out string, err error) error {
	switch {
	case strings.Contains(out, "REMOTE HOST IDENTIFICATION HAS CHANGED"), strings.Contains(out, "Host key verification failed"):
		return errors.New("SFTP: the server's identity changed since the first connection. If you expected that, press Trust a new key; if not, do not continue")
	case strings.Contains(out, "Permission denied"):
		return errors.New("SFTP: the server refused the key. Add the public key shown here to the user's authorized keys on the server")
	case strings.Contains(out, "Connection refused"), strings.Contains(out, "timed out"), strings.Contains(out, "No route"), strings.Contains(out, "Could not resolve"):
		return errors.New("SFTP: could not reach the server: " + lastLine(out))
	}
	var ee *exec.Error
	if errors.As(err, &ee) {
		return errors.New("SFTP: the sftp program is not installed on this machine (apt install openssh-client)")
	}
	return fmt.Errorf("SFTP: %s", lastLine(out))
}

func (s *SFTP) run(cmds string) (string, error) {
	a, err := s.args()
	if err != nil {
		return "", err
	}
	out, err := runCLI("sftp", cmds, a...)
	if err != nil {
		return out, sftpErr(out, err)
	}
	return out, nil
}

func (s *SFTP) path(name string) string {
	d := strings.Trim(s.Dir, "/")
	if d == "" {
		return name
	}
	return d + "/" + name
}

func (s *SFTP) Put(name string, data []byte) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	tmp, err := tempFile(data)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	var b strings.Builder
	acc := ""
	for _, p := range strings.Split(strings.Trim(s.Dir, "/"), "/") {
		if p == "" {
			continue
		}
		acc += p
		fmt.Fprintf(&b, "-mkdir \"%s\"\n", acc)
		acc += "/"
	}
	fmt.Fprintf(&b, "put \"%s\" \"%s.part\"\n-rm \"%s\"\nrename \"%s.part\" \"%s\"\n", tmp, s.path(name), s.path(name), s.path(name), s.path(name))
	_, err = s.run(b.String())
	return err
}

func (s *SFTP) List() ([]Item, error) {
	dir := strings.Trim(s.Dir, "/")
	out, err := s.run(fmt.Sprintf("-mkdir \"%s\"\nls -1 \"%s\"\n", dir, dir))
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, l := range strings.Split(out, "\n") {
		n := strings.TrimSpace(l)
		if i := strings.LastIndex(n, "/"); i >= 0 {
			n = n[i+1:]
		}
		if ValidName(n) {
			items = append(items, Item{Name: n})
		}
	}
	return items, nil
}

func (s *SFTP) Delete(name string) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	_, err := s.run(fmt.Sprintf("rm \"%s\"\n", s.path(name)))
	return err
}

// ---- SMB ----

type SMB struct {
	Host, Share, Dir, User, Pass, Domain string
}

func (s *SMB) base() ([]string, string, error) {
	if !reHostOK.MatchString(s.Host) || !regexp.MustCompile(`^[A-Za-z0-9 _.$-]+$`).MatchString(s.Share) || !rePathOK.MatchString(s.Dir) || strings.Contains(s.Dir, "..") ||
		strings.ContainsAny(s.User+s.Domain, "\n\r") || strings.ContainsAny(s.Pass, "\n\r") {
		return nil, "", errors.New("SMB: check the server, share and folder; they contain a character that is not allowed")
	}
	af, err := tempFile([]byte(fmt.Sprintf("username = %s\npassword = %s\ndomain = %s\n", s.User, s.Pass, s.Domain)))
	if err != nil {
		return nil, "", err
	}
	return []string{"//" + s.Host + "/" + s.Share, "-A", af, "-m", "SMB3", "-g"}, af, nil
}

func smbErr(out string, err error) error {
	switch {
	case strings.Contains(out, "NT_STATUS_LOGON_FAILURE"), strings.Contains(out, "NT_STATUS_ACCESS_DENIED"):
		return errors.New("SMB: the server refused the user name or password, or this user may not write there")
	case strings.Contains(out, "NT_STATUS_BAD_NETWORK_NAME"):
		return errors.New("SMB: the server has no share with that name")
	case strings.Contains(out, "NT_STATUS_HOST_UNREACHABLE"), strings.Contains(out, "NT_STATUS_IO_TIMEOUT"), strings.Contains(out, "NT_STATUS_CONNECTION_REFUSED"), strings.Contains(out, "Connection to"):
		return errors.New("SMB: could not reach the server")
	case strings.Contains(out, "NT_STATUS_DISK_FULL"), strings.Contains(out, "NT_STATUS_QUOTA_EXCEEDED"):
		return errors.New("SMB: the share is full")
	}
	var ee *exec.Error
	if errors.As(err, &ee) {
		return errors.New("SMB: the smbclient program is not installed on this machine (apt install smbclient)")
	}
	return fmt.Errorf("SMB: %s", lastLine(out))
}

// do runs commands on the share. ignore lists status words that are not errors for this call.
func (s *SMB) do(cmds string, ignore ...string) (string, error) {
	a, af, err := s.base()
	if err != nil {
		return "", err
	}
	defer os.Remove(af)
	out, err := runCLI("smbclient", "", append(a, "-c", cmds)...)
	bad := false
	for _, l := range strings.Split(out, "\n") {
		if i := strings.Index(l, "NT_STATUS_"); i >= 0 {
			ok := false
			for _, ig := range ignore {
				ok = ok || strings.Contains(l, ig)
			}
			bad = bad || !ok
		}
	}
	if (err != nil && len(ignore) == 0) || bad {
		return out, smbErr(out, err)
	}
	if err != nil && strings.Contains(out, "smbclient") && strings.Contains(out, "not found") {
		return out, smbErr(out, err)
	}
	return out, nil
}

func (s *SMB) Put(name string, data []byte) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	tmp, err := tempFile(data)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	acc := ""
	for _, p := range strings.Split(strings.Trim(s.Dir, "/"), "/") {
		if p == "" {
			continue
		}
		acc += p
		if _, err := s.do(fmt.Sprintf("mkdir \"%s\"", acc), "NT_STATUS_OBJECT_NAME_COLLISION", "NT_STATUS_ACCESS_DENIED", "NT_STATUS_OBJECT_NAME_NOT_FOUND"); err != nil && !strings.Contains(err.Error(), "refused") {
			return err
		}
		acc += "/"
	}
	cd := ""
	if d := strings.Trim(s.Dir, "/"); d != "" {
		cd = fmt.Sprintf("cd \"%s\"; ", d)
	}
	if _, err := s.do(fmt.Sprintf("%sput \"%s\" \"%s.part\"", cd, tmp, name)); err != nil {
		return err
	}
	_, err = s.do(fmt.Sprintf("%sdel \"%s\"; rename \"%s.part\" \"%s\"", cd, name, name, name), "NT_STATUS_NO_SUCH_FILE", "NT_STATUS_OBJECT_NAME_NOT_FOUND")
	return err
}

var reSMBLs = regexp.MustCompile(`^\s+(\S+)\s+[A-Za-z]+\s+(\d+)\s+\S+`)

func (s *SMB) List() ([]Item, error) {
	cd := ""
	if d := strings.Trim(s.Dir, "/"); d != "" {
		cd = fmt.Sprintf("cd \"%s\"; ", d)
	}
	out, err := s.do(cd + "ls")
	if err != nil {
		if strings.Contains(out, "NT_STATUS_OBJECT_NAME_NOT_FOUND") {
			return nil, nil
		}
		return nil, err
	}
	var items []Item
	for _, l := range strings.Split(out, "\n") {
		if m := reSMBLs.FindStringSubmatch(l); m != nil && ValidName(m[1]) {
			var n int64
			fmt.Sscan(m[2], &n)
			items = append(items, Item{Name: m[1], Size: n})
		}
	}
	return items, nil
}

func (s *SMB) Delete(name string) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	cd := ""
	if d := strings.Trim(s.Dir, "/"); d != "" {
		cd = fmt.Sprintf("cd \"%s\"; ", d)
	}
	_, err := s.do(fmt.Sprintf("%sdel \"%s\"", cd, name))
	return err
}
