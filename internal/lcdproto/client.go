// Package lcdproto talks to LCDd, the program that drives a front panel display, over its local
// text protocol (version 0.3). It draws lines of text on one screen and reports the display's size
// and the buttons that are pressed.
package lcdproto

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Conn is one connection to LCDd.
type Conn struct {
	c      net.Conn
	wmu    sync.Mutex
	Width  int
	Height int
	CellW  int
	CellH  int
	Server string

	mu     sync.Mutex
	keys   chan string
	closed bool
	acks   chan string
}

// Dial connects to LCDd and says hello. It returns the size the display reports.
func Dial(addr string, timeout time.Duration) (*Conn, error) {
	nc, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	c := &Conn{c: nc, keys: make(chan string, 64), acks: make(chan string, 64)}
	r := bufio.NewReader(nc)
	nc.SetDeadline(time.Now().Add(timeout))
	if _, err := fmt.Fprint(nc, "hello\n"); err != nil {
		nc.Close()
		return nil, err
	}
	line, err := r.ReadString('\n')
	if err != nil {
		nc.Close()
		return nil, err
	}
	if err := c.parseHello(line); err != nil {
		nc.Close()
		return nil, err
	}
	nc.SetDeadline(time.Time{})
	go c.read(r)
	return c, nil
}

// parseHello reads "connect LCDproc 0.5.9 protocol 0.3 lcd wid 20 hgt 2 cellwid 5 cellhgt 8".
func (c *Conn) parseHello(line string) error {
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "connect" {
		return errors.New("LCDd did not say hello: " + strings.TrimSpace(line))
	}
	for i := 0; i+1 < len(f); i++ {
		n, err := strconv.Atoi(f[i+1])
		switch f[i] {
		case "LCDproc":
			c.Server = f[i+1]
		case "wid":
			if err == nil {
				c.Width = n
			}
		case "hgt":
			if err == nil {
				c.Height = n
			}
		case "cellwid":
			if err == nil {
				c.CellW = n
			}
		case "cellhgt":
			if err == nil {
				c.CellH = n
			}
		}
	}
	if c.Width <= 0 || c.Height <= 0 {
		return errors.New("LCDd did not report the display's size: " + strings.TrimSpace(line))
	}
	return nil
}

// read splits what LCDd sends: "key X" lines go to Keys, everything else is an answer to a command.
func (c *Conn) read(r *bufio.Reader) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			c.mu.Lock()
			c.closed = true
			c.mu.Unlock()
			close(c.keys)
			return
		}
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "key "):
			select {
			case c.keys <- strings.TrimSpace(line[4:]):
			default:
			}
		case strings.HasPrefix(line, "listen ") || strings.HasPrefix(line, "ignore ") || strings.HasPrefix(line, "menuevent") || strings.HasPrefix(line, "bye") || line == "":
		default:
			select {
			case c.acks <- line:
			default:
			}
		}
	}
}

// Keys delivers the names of buttons as they are pressed ("Up", "Enter", "A"...). It closes when the
// connection ends.
func (c *Conn) Keys() <-chan string { return c.keys }

// Send runs one command and waits for LCDd's answer, which is "success" or an error.
func (c *Conn) Send(cmd string) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("the display program closed the connection")
	}
	c.mu.Unlock()
	for len(c.acks) > 0 { // anything left over is from an earlier command
		<-c.acks
	}
	c.c.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprint(c.c, cmd+"\n"); err != nil {
		return err
	}
	select {
	case a := <-c.acks:
		if a == "success" {
			return nil
		}
		return errors.New(strings.TrimPrefix(a, "huh? "))
	case <-time.After(3 * time.Second):
		return errors.New("LCDd did not answer")
	}
}

// Quote makes text safe inside a command: braces end a value, and a line may not break.
func Quote(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '{' || r == '}' || r == '\n' || r == '\r' || r < 32 {
			return ' '
		}
		return r
	}, s)
	return "{" + s + "}"
}

// Setup names the client, makes the one screen, gives it a line widget for each row, and asks for the
// display's buttons.
func (c *Conn) Setup(name string, keys []string) error {
	for _, cmd := range []string{
		"client_set -name " + Quote(name),
		"screen_add main",
		"screen_set main -name " + Quote(name) + " -priority foreground -heartbeat off",
	} {
		if err := c.Send(cmd); err != nil {
			return err
		}
	}
	for y := 1; y <= c.Height; y++ {
		if err := c.Send(fmt.Sprintf("widget_add main l%d string", y)); err != nil {
			return err
		}
	}
	if len(keys) > 0 {
		if err := c.Send("client_add_key -shared " + strings.Join(keys, " ")); err != nil {
			return err
		}
	}
	return nil
}

// Fit cuts or pads a line to the display's width.
func (c *Conn) Fit(s string) string {
	r := []rune(s)
	if len(r) > c.Width {
		r = r[:c.Width]
	}
	for len(r) < c.Width {
		r = append(r, ' ')
	}
	return string(r)
}

// Lines writes one line of text on each row (extra lines are ignored, missing ones are blanked).
func (c *Conn) Lines(lines []string) error {
	for y := 1; y <= c.Height; y++ {
		t := ""
		if y <= len(lines) {
			t = lines[y-1]
		}
		if err := c.Send(fmt.Sprintf("widget_set main l%d 1 %d %s", y, y, Quote(c.Fit(t)))); err != nil {
			return err
		}
	}
	return nil
}

// Backlight sets the backlight: on, off, open (the display decides), blink or flash.
func (c *Conn) Backlight(mode string) error {
	switch mode {
	case "on", "off", "open", "blink", "flash":
		return c.Send("screen_set main -backlight " + mode)
	}
	return errors.New("not a backlight setting")
}

// Icon puts one of LCDd's built-in symbols (BLOCK_FILLED, ARROW_UP, HEART...) at a place on the screen.
func (c *Conn) Icon(id string, x, y int, name string) error {
	if err := c.Send("widget_add main " + id + " icon"); err != nil {
		return err
	}
	return c.Send(fmt.Sprintf("widget_set main %s %d %d %s", id, x, y, name))
}

// Close ends the connection.
func (c *Conn) Close() error {
	c.mu.Lock()
	already := c.closed
	c.closed = true
	c.mu.Unlock()
	if already {
		return nil
	}
	return c.c.Close()
}
