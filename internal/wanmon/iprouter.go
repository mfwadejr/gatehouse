package wanmon

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// IPRouter drives the kernel through the ip command.
type IPRouter struct {
	// Netns runs every command inside a namespace; used by tests.
	Netns string
	// Metric of the main-table default route owned by the monitor.
	Metric int
}

func (r *IPRouter) ip(args ...string) ([]byte, error) {
	if r.Netns != "" {
		args = append([]string{"netns", "exec", r.Netns, "ip"}, args...)
		c := exec.Command("ip", args...)
		out, err := c.CombinedOutput()
		if err != nil {
			return out, fmt.Errorf("ip %s: %s", strings.Join(args[4:], " "), strings.TrimSpace(string(out)))
		}
		return out, nil
	}
	out, err := exec.Command("ip", args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("ip %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return out, nil
}

type routeJSON struct {
	Dst     string `json:"dst"`
	Gateway string `json:"gateway"`
	Dev     string `json:"dev"`
}

// Gateway looks for the link's default gateway: first in its own table, then in the main table.
func (r *IPRouter) Gateway(l Link) (string, error) {
	for _, args := range [][]string{
		{"-j", "-4", "route", "show", "table", strconv.Itoa(l.Table), "default", "dev", l.Dev},
		{"-j", "-4", "route", "show", "default", "dev", l.Dev},
	} {
		out, err := r.ip(args...)
		if err != nil {
			continue
		}
		var rs []routeJSON
		if json.Unmarshal(out, &rs) == nil {
			for _, rt := range rs {
				if rt.Gateway != "" {
					return rt.Gateway, nil
				}
			}
		}
	}
	return "", nil
}

func (r *IPRouter) metric() string {
	if r.Metric > 0 {
		return strconv.Itoa(r.Metric)
	}
	return "10"
}

func (r *IPRouter) SetDefault(nh []Nexthop) error {
	if len(nh) == 0 {
		return nil
	}
	args := []string{"route", "replace", "default", "metric", r.metric()}
	if len(nh) == 1 {
		args = append(args, "via", nh[0].Gateway, "dev", nh[0].Dev)
	} else {
		for _, n := range nh {
			args = append(args, "nexthop", "via", n.Gateway, "dev", n.Dev, "weight", strconv.Itoa(n.Weight))
		}
	}
	_, err := r.ip(args...)
	return err
}

func (r *IPRouter) EnsureLink(l Link, gw string) error {
	t := strconv.Itoa(l.Table)
	if _, err := r.ip("route", "replace", "default", "via", gw, "dev", l.Dev, "table", t); err != nil {
		return err
	}
	rule := []string{"fwmark", fmt.Sprintf("0x%x", l.Mark), "lookup", t, "priority", strconv.Itoa(1000 + l.Table)}
	if out, _ := r.ip(append([]string{"rule", "show"}, rule[:2]...)...); strings.Contains(string(out), "lookup "+t) {
		return nil
	}
	_, err := r.ip(append([]string{"rule", "add"}, rule...)...)
	return err
}

func (r *IPRouter) RemoveLink(l Link) error {
	t := strconv.Itoa(l.Table)
	r.ip("rule", "del", "fwmark", fmt.Sprintf("0x%x", l.Mark), "lookup", t)
	_, err := r.ip("route", "flush", "table", t)
	return err
}

// ClearDefault takes the monitor's default route away, so nothing leaves while every connection is paused.
func (r *IPRouter) ClearDefault() error {
	for i := 0; i < 8; i++ { // one delete removes one route; a few may exist for different metrics
		if _, err := r.ip("route", "del", "default", "metric", r.metric()); err != nil {
			break
		}
	}
	return nil
}

// HoldLink takes the only connection's default route away (the system owns it, so the monitor puts it back
// itself when the pause ends).
func (r *IPRouter) HoldLink(l Link, gw string) error {
	_, err := r.ip("route", "flush", "default", "dev", l.Dev)
	return err
}

// ReleaseLink gives the connection's default route back by asking the system to configure the port again,
// and falls back to adding the route by hand.
func (r *IPRouter) ReleaseLink(l Link, gw string) error {
	if r.Netns == "" {
		if err := exec.Command("networkctl", "reconfigure", l.Dev).Run(); err == nil {
			return nil
		}
	}
	if gw == "" {
		return nil
	}
	_, err := r.ip("route", "replace", "default", "via", gw, "dev", l.Dev)
	return err
}
