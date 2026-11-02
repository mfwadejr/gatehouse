package netd

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A shell on this machine for the console's Terminal page. It runs as root, so the console only
// opens one after the administrator's password or code, for administrators only, and only while the
// owner has switched the Terminal on. Sessions end by themselves when idle or after two hours.

var (
	termShell   = []string{"/bin/bash", "-l"}
	termIdle    = 10 * time.Minute
	termMaxLife = 2 * time.Hour
	termMax     = 2
	termKeep    = 256 << 10 // output kept for a browser that reconnects
)

type termSession struct {
	id        string
	cmd       *exec.Cmd
	pty       *os.File
	mu        sync.Mutex
	buf       []byte
	base      int // offset of buf[0] in everything the shell has written
	closed    bool
	started   time.Time
	lastInput time.Time
	in, out   int
	changed   chan struct{}
}

var terms = struct {
	sync.Mutex
	m map[string]*termSession
}{m: map[string]*termSession{}}

func openPty() (master, slave *os.File, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	if err := unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		m.Close()
		return nil, nil, err
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	if err != nil {
		m.Close()
		return nil, nil, err
	}
	s, err := os.OpenFile("/dev/pts/"+itoa(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		return nil, nil, err
	}
	return m, s, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func setWinsize(f *os.File, rows, cols int) {
	ws := struct{ Row, Col, X, Y uint16 }{uint16(rows), uint16(cols), 0, 0}
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws)))
}

func clampSize(rows, cols int) (int, int) {
	if rows < 5 || rows > 200 {
		rows = 24
	}
	if cols < 20 || cols > 400 {
		cols = 80
	}
	return rows, cols
}

// TermOpen starts a shell and returns its id.
func (s *Service) TermOpen(rows, cols int) (string, error) {
	terms.Lock()
	live := 0
	for _, t := range terms.m {
		t.mu.Lock()
		if !t.closed {
			live++
		}
		t.mu.Unlock()
	}
	terms.Unlock()
	if live >= termMax {
		return "", errors.New("two terminals are already open; close one first")
	}
	m, sl, err := openPty()
	if err != nil {
		return "", errors.New("could not open a terminal: " + err.Error())
	}
	rows, cols = clampSize(rows, cols)
	setWinsize(m, rows, cols)
	start := func(dir string, ctty bool) (*exec.Cmd, error) {
		cmd := exec.Command(termShell[0], termShell[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = sl, sl, sl
		cmd.Env = []string{"TERM=xterm-256color", "HOME=/root", "USER=root", "LANG=C.UTF-8", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: ctty, Ctty: 0}
		return cmd, cmd.Start()
	}
	// Try the home folder first, then the root folder, then without taking
	// the terminal as controlling (some locked-down hosts refuse that).
	cmd, err := start("/root", true)
	if err != nil {
		cmd, err = start("/", true)
	}
	if err != nil {
		cmd, err = start("/", false)
	}
	if err != nil {
		m.Close()
		sl.Close()
		return "", errors.New("could not start the shell: " + err.Error())
	}
	sl.Close()
	var idb [8]byte
	rand.Read(idb[:])
	t := &termSession{id: hex.EncodeToString(idb[:]), cmd: cmd, pty: m, started: time.Now(), lastInput: time.Now(), changed: make(chan struct{}, 1)}
	terms.Lock()
	terms.m[t.id] = t
	terms.Unlock()
	go t.pump()
	go t.watch()
	return t.id, nil
}

func (t *termSession) pump() {
	b := make([]byte, 4096)
	for {
		n, err := t.pty.Read(b)
		if n > 0 {
			t.mu.Lock()
			t.buf = append(t.buf, b[:n]...)
			t.out += n
			if over := len(t.buf) - termKeep; over > 0 {
				t.buf = t.buf[over:]
				t.base += over
			}
			t.mu.Unlock()
			t.poke()
		}
		if err != nil {
			break
		}
	}
	t.cmd.Wait()
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	t.poke()
}

func (t *termSession) poke() {
	select {
	case t.changed <- struct{}{}:
	default:
	}
}

// watch ends the session when nobody has typed for a while or it has run too long.
func (t *termSession) watch() {
	for {
		time.Sleep(time.Second)
		t.mu.Lock()
		done := t.closed
		idle := time.Since(t.lastInput) > termIdle
		old := time.Since(t.started) > termMaxLife
		t.mu.Unlock()
		if done {
			return
		}
		if idle || old {
			t.kill()
			return
		}
	}
}

func (t *termSession) kill() {
	if t.cmd.Process != nil {
		syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
		t.cmd.Process.Kill()
	}
	t.pty.Close()
}

func termGet(id string) (*termSession, error) {
	terms.Lock()
	defer terms.Unlock()
	t := terms.m[id]
	if t == nil {
		return nil, errors.New("that terminal is not open")
	}
	return t, nil
}

// TermRead returns what the shell has written after offset since, waiting up to wait for something new.
func (s *Service) TermRead(id string, since int, wait time.Duration) (data []byte, next int, closed bool, err error) {
	t, err := termGet(id)
	if err != nil {
		return nil, since, true, err
	}
	deadline := time.Now().Add(wait)
	for {
		t.mu.Lock()
		if since < t.base {
			since = t.base
		}
		end := t.base + len(t.buf)
		if end > since || t.closed {
			d := append([]byte(nil), t.buf[since-t.base:]...)
			c := t.closed
			t.mu.Unlock()
			return d, end, c && len(d) == 0, nil
		}
		t.mu.Unlock()
		left := time.Until(deadline)
		if left <= 0 {
			return nil, since, false, nil
		}
		select {
		case <-t.changed:
		case <-time.After(left):
		}
	}
}

// TermWrite sends keystrokes to the shell.
func (s *Service) TermWrite(id string, data []byte) error {
	t, err := termGet(id)
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return errors.New("too much at once")
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("that terminal has ended")
	}
	t.lastInput = time.Now()
	t.in += len(data)
	t.mu.Unlock()
	_, err = t.pty.Write(data)
	return err
}

// TermResize changes the size the shell believes the window has.
func (s *Service) TermResize(id string, rows, cols int) error {
	t, err := termGet(id)
	if err != nil {
		return err
	}
	rows, cols = clampSize(rows, cols)
	setWinsize(t.pty, rows, cols)
	return nil
}

// TermSummary says how long a session ran and how much went each way. It is what the Logs record.
type TermSummary struct {
	Seconds  int `json:"seconds"`
	BytesIn  int `json:"bytes_in"`
	BytesOut int `json:"bytes_out"`
}

// TermClose ends a session and forgets it.
func (s *Service) TermClose(id string) (TermSummary, error) {
	t, err := termGet(id)
	if err != nil {
		return TermSummary{}, err
	}
	t.kill()
	t.mu.Lock()
	sum := TermSummary{Seconds: int(time.Since(t.started).Seconds()), BytesIn: t.in, BytesOut: t.out}
	t.mu.Unlock()
	terms.Lock()
	delete(terms.m, id)
	terms.Unlock()
	return sum, nil
}
