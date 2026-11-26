package offsite

import (
	"strings"
	"testing"
)

func TestSFTPCommands(t *testing.T) {
	old := runCLI
	defer func() { runCLI = old }()
	var gotName, gotIn string
	var gotArgs []string
	runCLI = func(name, in string, args ...string) (string, error) {
		gotName, gotIn, gotArgs = name, in, args
		if strings.Contains(in, "ls -1") {
			return "sftp> ls -1 \"bk/nw\"\nbk/nw/auto-20261126-010000.ghbk\nbk/nw/readme.txt\n", nil
		}
		return "", nil
	}
	s := &SFTP{Host: "nas.lan", User: "backup", Dir: "bk/nw", KeyFile: "/k", KnownHosts: "/kh"}
	if err := s.Put("auto-20261126-010000.ghbk", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if gotName != "sftp" || !strings.Contains(gotIn, `-mkdir "bk"`) || !strings.Contains(gotIn, `-mkdir "bk/nw"`) || !strings.Contains(gotIn, `rename "bk/nw/auto-20261126-010000.ghbk.part" "bk/nw/auto-20261126-010000.ghbk"`) {
		t.Fatalf("%s\n%s", gotName, gotIn)
	}
	if strings.Join(gotArgs, " ") == "" || !strings.Contains(strings.Join(gotArgs, " "), "backup@nas.lan") || !strings.Contains(strings.Join(gotArgs, " "), "BatchMode=yes") {
		t.Fatal(gotArgs)
	}
	items, err := s.List()
	if err != nil || len(items) != 2 || items[0].Name != "auto-20261126-010000.ghbk" {
		t.Fatalf("%v %v", items, err)
	}
	if (&SFTP{Host: "a;b", User: "u", Dir: "x"}).Put("auto-1.ghbk", nil) == nil {
		t.Fatal("odd host refused")
	}
	if (&SFTP{Host: "a", User: "u", Dir: "../x"}).Delete("auto-1.ghbk") == nil {
		t.Fatal("dir escape refused")
	}
}

func TestSFTPErrors(t *testing.T) {
	old := runCLI
	defer func() { runCLI = old }()
	runCLI = func(string, string, ...string) (string, error) {
		return "user@h: Permission denied (publickey).\n", errTest
	}
	err := (&SFTP{Host: "h", User: "u", Dir: "d", KeyFile: "/k"}).Delete("auto-1.ghbk")
	if err == nil || !strings.Contains(err.Error(), "public key") {
		t.Fatalf("%v", err)
	}
}

var errTest = &testErr{}

type testErr struct{}

func (*testErr) Error() string { return "exit status 1" }

func TestSMBCommands(t *testing.T) {
	old := runCLI
	defer func() { runCLI = old }()
	var calls []string
	runCLI = func(name, in string, args ...string) (string, error) {
		c := args[len(args)-1]
		calls = append(calls, c)
		switch {
		case strings.HasPrefix(c, "mkdir"):
			return "NT_STATUS_OBJECT_NAME_COLLISION making remote directory\n", errTest
		case strings.HasSuffix(c, "ls"):
			return "  .                                   D        0  Thu Nov 26 10:00:00 2026\n  auto-20261126-010000.ghbk           N     1234  Thu Nov 26 10:00:00 2026\n", nil
		case strings.Contains(c, "del \"auto"):
			return "NT_STATUS_NO_SUCH_FILE deleting remote file\n", nil
		}
		return "putting file as x (10.0 kb/s)\n", nil
	}
	s := &SMB{Host: "nas", Share: "backups", Dir: "nw", User: "u", Pass: "p"}
	if err := s.Put("auto-20261126-010000.ghbk", []byte("x")); err != nil {
		t.Fatalf("%v %v", err, calls)
	}
	items, err := s.List()
	if err != nil || len(items) != 1 || items[0].Size != 1234 {
		t.Fatalf("%v %v", items, err)
	}
	runCLI = func(string, string, ...string) (string, error) {
		return "session setup failed: NT_STATUS_LOGON_FAILURE\n", errTest
	}
	if err := s.Delete("auto-1.ghbk"); err == nil || !strings.Contains(err.Error(), "user name or password") {
		t.Fatalf("%v", err)
	}
}
