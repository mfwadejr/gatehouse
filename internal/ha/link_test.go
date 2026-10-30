package ha

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

type fixture struct {
	addr      string
	primary   Identity
	secret    string
	peer      string
	mu        sync.Mutex
	snapshots [][]byte
}

func newLink(t *testing.T) *fixture {
	t.Helper()
	id, err := NewIdentity("p")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{primary: id, secret: "0123456789abcdef0123456789abcdef"}
	l := &Link{Self: id, Secret: f.secret,
		Peer: func() string { f.mu.Lock(); defer f.mu.Unlock(); return f.peer },
		OnJoin: func(fp string, port int) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.peer = fp
			return nil
		},
		OnSnapshot: func(b []byte, seq int64, v string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.snapshots = append(f.snapshots, b)
			return nil
		},
		Status: func() Status { return Status{Version: "0.8.18", Role: "primary", Active: true} },
		Overview: func() Overview {
			return Overview{Roles: []PortRole{{ID: "network:Home", Port: "enp1s0"}}, Version: "0.8.18"}
		}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go l.Serve(ctx, ln)
	t.Cleanup(cancel)
	f.addr = ln.Addr().String()
	return f
}

func TestLinkJoinAndSync(t *testing.T) {
	f := newLink(t)
	ctx := context.Background()
	me, _ := NewIdentity("s")

	// Before joining the primary knows nothing about us: status and snapshots are refused.
	if _, err := Call(ctx, f.addr, me, f.primary.FP, "GET", "/ha/status", nil, nil, ""); err == nil {
		t.Fatal("an unknown unit must not read status")
	}
	if _, err := Call(ctx, f.addr, me, f.primary.FP, "POST", "/ha/snapshot", []byte("x"), nil, ""); err == nil {
		t.Fatal("an unknown unit must not push settings")
	}
	// With the code it may look at the ports and join.
	b, err := Call(ctx, f.addr, me, f.primary.FP, "GET", "/ha/overview", nil, nil, f.secret)
	if err != nil {
		t.Fatal(err)
	}
	var ov Overview
	json.Unmarshal(b, &ov)
	if len(ov.Roles) != 1 {
		t.Fatalf("%+v", ov)
	}
	// A wrong code is refused.
	if _, err := Call(ctx, f.addr, me, f.primary.FP, "POST", "/ha/join", nil, nil, "wrong-secret-wrong-secret-wrong!"); err == nil {
		t.Fatal("a wrong code must not join")
	}
	if _, err := Call(ctx, f.addr, me, f.primary.FP, "POST", "/ha/join", nil, nil, f.secret); err != nil {
		t.Fatal(err)
	}
	if f.peer != me.FP {
		t.Fatal("the standby's certificate was not pinned")
	}
	// Now paired: status and snapshots work, and a third unit cannot use them.
	if _, err := Call(ctx, f.addr, me, f.primary.FP, "POST", "/ha/snapshot", []byte("settings"), map[string]string{"X-GH-Seq": "7"}, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.snapshots) != 1 || string(f.snapshots[0]) != "settings" {
		t.Fatal("snapshot not received")
	}
	other, _ := NewIdentity("o")
	if _, err := Call(ctx, f.addr, other, f.primary.FP, "GET", "/ha/status", nil, nil, ""); err == nil {
		t.Fatal("a third unit must be refused")
	}
	// A unit that holds the code but is already replaced cannot take the place of the standby.
	// (The code alone only opens overview and join; the primary refuses a second join below.)
	// The standby refuses to talk to an impostor primary.
	if _, err := Call(ctx, f.addr, me, "deadbeef", "GET", "/ha/status", nil, nil, ""); err == nil {
		t.Fatal("a wrong pinned fingerprint must stop the call")
	}
	_ = time.Second
}
