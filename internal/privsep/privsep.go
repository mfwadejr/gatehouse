// Package privsep carries requests from the unprivileged daemon to the root helper ("netd").
// The helper accepts a short list of operations, validates every argument, and checks the
// caller's identity from the kernel on each connection.
package privsep

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"gatehouse/internal/fw"
	"gatehouse/internal/netcfg"
	"gatehouse/internal/netd"
	"gatehouse/internal/wanmon"
)

// NetController is the network side of the helper; *netd.Service implements it.
type NetController interface {
	Apply(p netd.Plan, window time.Duration) (string, time.Time, error)
	Confirm(token string) error
	Rollback() error
	Pending() (string, time.Time, bool)
	Status() map[string]wanmon.State
	ApplyVPN(p netd.VPNPlan) error
	VPNStatus() netd.VPNStatus
	FWEvents(since uint64, max int) ([]netd.FWEvent, uint64)
	SetSystem(hostname, zone string) error
	WifiScan(iface string) ([]netd.WifiNetwork, error)
	UPnPApply(ms []netd.UPnPMapping) error
	UpdateStart(path string) error
	OSSecurity(enabled bool) error
}

const maxRequest = 4 << 20

type request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

type applyParams struct {
	Script  string `json:"script"`
	Seconds int    `json:"seconds"`
	Author  string `json:"author"`
}

type tokenParams struct {
	Token string `json:"token"`
}

type applyResult struct {
	Token    string `json:"token"`
	Deadline int64  `json:"deadline"`
}

type pendingResult struct {
	OK       bool   `json:"ok"`
	Token    string `json:"token"`
	Deadline int64  `json:"deadline"`
}

// Server is the root side.
type Server struct {
	FW  fw.Controller
	Net NetController
	// AllowUID lists the user ids that may call; root is always allowed.
	AllowUID []uint32
	// MinWindow and MaxWindow bound the confirmation window a caller may ask for.
	MinWindow, MaxWindow time.Duration
}

func (s *Server) allowed(uid uint32) bool {
	if uid == 0 {
		return true
	}
	for _, u := range s.AllowUID {
		if u == uid {
			return true
		}
	}
	return false
}

// Listen creates the socket with tight permissions.
func Listen(path string, gid int) (net.Listener, error) {
	os.Remove(path)
	old := syscall.Umask(0o117)
	l, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if gid >= 0 {
		os.Chown(path, 0, gid)
	}
	os.Chmod(path, 0o660)
	return l, nil
}

func peerUID(c net.Conn) (uint32, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if cerr != nil {
		return 0, cerr
	}
	return cred.Uid, nil
}

// Serve handles connections until the listener closes.
func (s *Server) Serve(l net.Listener) error {
	if s.MinWindow == 0 {
		s.MinWindow = 15 * time.Second
	}
	if s.MaxWindow == 0 {
		s.MaxWindow = 10 * time.Minute
	}
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handle(c)
	}
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	uid, err := peerUID(c)
	if err != nil || !s.allowed(uid) {
		json.NewEncoder(c).Encode(response{Error: "not permitted"})
		return
	}
	c.SetDeadline(time.Now().Add(2 * time.Minute))
	var req request
	if err := json.NewDecoder(io.LimitReader(bufio.NewReader(c), maxRequest)).Decode(&req); err != nil {
		json.NewEncoder(c).Encode(response{Error: "bad request"})
		return
	}
	res, err := s.dispatch(req)
	out := response{}
	if err != nil {
		out.Error = err.Error()
	} else if res != nil {
		out.Result, _ = json.Marshal(res)
	}
	json.NewEncoder(c).Encode(out)
}

func (s *Server) dispatch(r request) (any, error) {
	switch r.Method {
	case "fw.check":
		var p applyParams
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		return nil, s.FW.Check(p.Script)
	case "fw.apply":
		var p applyParams
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		w := time.Duration(p.Seconds) * time.Second
		if w < s.MinWindow || w > s.MaxWindow {
			return nil, fmt.Errorf("confirmation window must be between %s and %s", s.MinWindow, s.MaxWindow)
		}
		if len(p.Author) > 64 {
			p.Author = p.Author[:64]
		}
		tok, dl, err := s.FW.Apply(p.Script, w, p.Author)
		if err != nil {
			return nil, err
		}
		return applyResult{tok, dl.Unix()}, nil
	case "fw.confirm":
		var p tokenParams
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		return nil, s.FW.Confirm(p.Token)
	case "fw.rollback":
		return nil, s.FW.Rollback()
	case "fw.pending":
		t, d, ok := s.FW.Pending()
		return pendingResult{ok, t, d.Unix()}, nil
	}
	if len(r.Method) > 4 && (r.Method[:4] == "net." || r.Method[:4] == "wan." || r.Method[:4] == "vpn.") {
		return s.dispatchNet(r)
	}
	return nil, errors.New("unknown method")
}

func (s *Server) dispatchNet(r request) (any, error) {
	if s.Net == nil {
		return nil, errors.New("network management is not enabled")
	}
	switch r.Method {
	case "net.apply":
		var p netd.Plan
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		w := time.Duration(p.Seconds) * time.Second
		if w < s.MinWindow || w > s.MaxWindow {
			return nil, fmt.Errorf("confirmation window must be between %s and %s", s.MinWindow, s.MaxWindow)
		}
		tok, dl, err := s.Net.Apply(p, w)
		if err != nil {
			return nil, err
		}
		return applyResult{tok, dl.Unix()}, nil
	case "net.confirm":
		var p tokenParams
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		return nil, s.Net.Confirm(p.Token)
	case "net.rollback":
		return nil, s.Net.Rollback()
	case "net.pending":
		t, d, ok := s.Net.Pending()
		return pendingResult{ok, t, d.Unix()}, nil
	case "wan.status":
		return s.Net.Status(), nil
	case "vpn.apply":
		var p netd.VPNPlan
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		return nil, s.Net.ApplyVPN(p)
	case "vpn.status":
		return s.Net.VPNStatus(), nil
	case "net.wifiscan":
		var p struct {
			Iface string `json:"iface"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		return s.Net.WifiScan(p.Iface)
	case "net.upnp":
		var p struct {
			Maps []netd.UPnPMapping `json:"maps"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		return nil, s.Net.UPnPApply(p.Maps)
	case "update.start":
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		return nil, s.Net.UpdateStart(p.Path)
	case "os.security":
		var p struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		return nil, s.Net.OSSecurity(p.Enabled)
	case "net.system":
		var p struct {
			Hostname string `json:"hostname"`
			Zone     string `json:"zone"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		return nil, s.Net.SetSystem(p.Hostname, p.Zone)
	case "fwlog.read":
		var p struct {
			Since uint64 `json:"since"`
			Max   int    `json:"max"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, errors.New("bad parameters")
		}
		ev, last := s.Net.FWEvents(p.Since, p.Max)
		return FWBatch{Events: ev, Last: last}, nil
	}
	return nil, errors.New("unknown method")
}

// Client is the daemon side. It satisfies fw.Controller.
type Client struct{ Path string }

var _ fw.Controller = (*Client)(nil)

func (c *Client) call(method string, params any, out any) error {
	conn, err := net.DialTimeout("unix", c.Path, 3*time.Second)
	if err != nil {
		return fmt.Errorf("the network helper is not running: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Minute))
	p, _ := json.Marshal(params)
	if err := json.NewEncoder(conn).Encode(request{Method: method, Params: p}); err != nil {
		return err
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	if out != nil && resp.Result != nil {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

func (c *Client) Check(script string) error {
	return c.call("fw.check", applyParams{Script: script}, nil)
}

func (c *Client) Apply(script string, window time.Duration, author string) (string, time.Time, error) {
	var r applyResult
	err := c.call("fw.apply", applyParams{Script: script, Seconds: int(window / time.Second), Author: author}, &r)
	if err != nil {
		if err.Error() == fw.ErrPending.Error() {
			return "", time.Time{}, fw.ErrPending
		}
		return "", time.Time{}, err
	}
	return r.Token, time.Unix(r.Deadline, 0), nil
}

func (c *Client) Confirm(token string) error { return c.call("fw.confirm", tokenParams{token}, nil) }
func (c *Client) Rollback() error            { return c.call("fw.rollback", nil, nil) }

func (c *Client) Pending() (string, time.Time, bool) {
	var r pendingResult
	if err := c.call("fw.pending", nil, &r); err != nil || !r.OK {
		return "", time.Time{}, false
	}
	return r.Token, time.Unix(r.Deadline, 0), true
}

// NetClient is the daemon side of the network operations.
func (c *Client) NetApply(p netd.Plan, window time.Duration) (string, time.Time, error) {
	p.Seconds = int(window / time.Second)
	var r applyResult
	if err := c.call("net.apply", p, &r); err != nil {
		if err.Error() == netcfg.ErrPending.Error() {
			return "", time.Time{}, netcfg.ErrPending
		}
		return "", time.Time{}, err
	}
	return r.Token, time.Unix(r.Deadline, 0), nil
}

func (c *Client) NetConfirm(token string) error {
	return c.call("net.confirm", tokenParams{token}, nil)
}
func (c *Client) NetRollback() error { return c.call("net.rollback", nil, nil) }

func (c *Client) NetPending() (string, time.Time, bool) {
	var r pendingResult
	if err := c.call("net.pending", nil, &r); err != nil || !r.OK {
		return "", time.Time{}, false
	}
	return r.Token, time.Unix(r.Deadline, 0), true
}

func (c *Client) WanStatus() (map[string]wanmon.State, error) {
	var out map[string]wanmon.State
	err := c.call("wan.status", nil, &out)
	return out, err
}

func (c *Client) ApplyVPN(p netd.VPNPlan) error { return c.call("vpn.apply", p, nil) }

func (c *Client) VPNStatus() (netd.VPNStatus, error) {
	var out netd.VPNStatus
	err := c.call("vpn.status", nil, &out)
	return out, err
}

// SetSystem changes the device name and time zone.
func (c *Client) SetSystem(hostname, zone string) error {
	return c.call("net.system", map[string]string{"hostname": hostname, "zone": zone}, nil)
}

// WifiScan lists the networks a wireless card can see.
func (c *Client) WifiScan(iface string) ([]netd.WifiNetwork, error) {
	var out []netd.WifiNetwork
	err := c.call("net.wifiscan", map[string]string{"iface": iface}, &out)
	return out, err
}

// UPnPApply replaces the set of ports opened on request by devices on the inside.
func (c *Client) UPnPApply(ms []netd.UPnPMapping) error {
	return c.call("net.upnp", map[string]any{"maps": ms}, nil)
}

// UpdateStart asks the helper to check and install a downloaded update.
func (c *Client) UpdateStart(path string) error {
	return c.call("update.start", map[string]string{"path": path}, nil)
}

// OSSecurity switches automatic operating-system security updates.
func (c *Client) OSSecurity(enabled bool) error {
	return c.call("os.security", map[string]bool{"enabled": enabled}, nil)
}

// FWBatch is a run of firewall events and the sequence number to ask for next.
type FWBatch struct {
	Events []netd.FWEvent `json:"events"`
	Last   uint64         `json:"last"`
}

func (c *Client) FWEvents(since uint64, max int) ([]netd.FWEvent, uint64, error) {
	var out FWBatch
	err := c.call("fwlog.read", map[string]any{"since": since, "max": max}, &out)
	return out.Events, out.Last, err
}
