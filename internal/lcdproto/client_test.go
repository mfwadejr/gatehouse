package lcdproto

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLCDd answers like LCDd does: a hello line with the size, "success" for each command, and a
// key press whenever the test sends one.
type fakeLCDd struct {
	ln   net.Listener
	mu   sync.Mutex
	cmds []string
	conn net.Conn
}

func newFake(t *testing.T) *fakeLCDd {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeLCDd{ln: ln}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conn = c
		f.mu.Unlock()
		r := bufio.NewReader(c)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			f.mu.Lock()
			f.cmds = append(f.cmds, line)
			f.mu.Unlock()
			if line == "hello" {
				c.Write([]byte("connect LCDproc 0.5.9 protocol 0.3 lcd wid 20 hgt 2 cellwid 5 cellhgt 8\n"))
				continue
			}
			if strings.HasPrefix(line, "bad") {
				c.Write([]byte("huh? unknown command\n"))
				continue
			}
			c.Write([]byte("success\n"))
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeLCDd) press(k string) {
	f.mu.Lock()
	c := f.conn
	f.mu.Unlock()
	c.Write([]byte("key " + k + "\n"))
}

func (f *fakeLCDd) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.cmds...)
}

func TestConnReadsSizeDrawsAndHearsKeys(t *testing.T) {
	f := newFake(t)
	c, err := Dial(f.ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Width != 20 || c.Height != 2 || c.CellW != 5 || c.Server != "0.5.9" {
		t.Fatalf("size: %+v", c)
	}
	if err := c.Setup("NopeWarden", []string{"Up", "Down", "Enter"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Lines([]string{"NopeWarden {x}", "0.9.4.80"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Backlight("flash"); err != nil {
		t.Fatal(err)
	}
	if err := c.Backlight("evil; rm"); err == nil {
		t.Fatal("a made-up backlight mode must be refused before it is sent")
	}
	if err := c.Send("bad thing"); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("an error answer should come back: %v", err)
	}
	f.press("Up")
	select {
	case k := <-c.Keys():
		if k != "Up" {
			t.Fatalf("key %q", k)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no key arrived")
	}
	got := strings.Join(f.sent(), "\n")
	for _, want := range []string{"client_add_key -shared Up Down Enter", "widget_set main l1 1 1 {NopeWarden  x       }", "widget_set main l2 1 2 {0.9.4.80            }", "screen_set main -backlight flash"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "evil") {
		t.Error("an invalid backlight mode reached LCDd")
	}
}

func TestQuoteAndFit(t *testing.T) {
	if Quote("a}b{c\nd") != "{a b c d}" {
		t.Fatalf("%q", Quote("a}b{c\nd"))
	}
	c := &Conn{Width: 5}
	if c.Fit("abcdefgh") != "abcde" || c.Fit("ab") != "ab   " {
		t.Fatal("fit")
	}
}

func TestDialRefusesAnswerWithoutSize(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		bufio.NewReader(c).ReadString('\n')
		c.Write([]byte("connect LCDproc 0.5.9 protocol 0.3\n"))
	}()
	if _, err := Dial(ln.Addr().String(), time.Second); err == nil {
		t.Fatal("a hello with no display size must be refused")
	}
}
