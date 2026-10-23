// Package upnp lets a device on the inside ask the gateway to open a port, the way games and
// media apps expect. It is off unless a network and an internet connection both allow it, and
// every request is checked against a policy before anything is opened.
package upnp

import (
	"net/netip"
	"sort"
	"sync"
	"time"
)

// Errors carry the UPnP error code a client sees.
type Error struct {
	Code int
	Text string
}

func (e *Error) Error() string { return e.Text }

var (
	ErrNotAuthorized = &Error{606, "Action not authorized"}
	ErrNoSuchEntry   = &Error{714, "NoSuchEntryInArray"}
	ErrConflict      = &Error{718, "ConflictInMappingEntry"}
	ErrNoneLeft      = &Error{728, "NoPortMapsAvailable"}
	ErrBadArgs       = &Error{402, "Invalid Args"}
)

// Policy is what a request must satisfy.
type Policy struct {
	MaxLease  time.Duration // longest a mapping lasts; the default when a device asks for "forever"
	PerDevice int
	Total     int
	// Reserved reports ports that must never be opened (the console, DNS, SSH and so on).
	Reserved func(proto string, port int) bool
	// Allowed reports whether a device may use UPnP at all.
	Allowed func(ip netip.Addr) bool
}

func DefaultPolicy() Policy {
	return Policy{MaxLease: 24 * time.Hour, PerDevice: 32, Total: 256}
}

// Mapping is one open port.
type Mapping struct {
	ID      int64     `json:"id"`
	Proto   string    `json:"proto"`
	ExtPort int       `json:"ext_port"`
	IntPort int       `json:"int_port"`
	IntAddr string    `json:"int_addr"`
	Desc    string    `json:"desc"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
}

// Table holds the open mappings.
type Table struct {
	mu       sync.Mutex
	pol      Policy
	maps     []*Mapping
	next     int64
	now      func() time.Time
	OnChange func([]Mapping) // called, without the lock held, whenever the set changes
	OnEvent  func(kind, msg string)
}

func NewTable(p Policy) *Table { return &Table{pol: p, now: time.Now} }

func (t *Table) SetPolicy(p Policy) { t.mu.Lock(); t.pol = p; t.mu.Unlock() }

func (t *Table) event(kind, msg string) {
	if t.OnEvent != nil {
		t.OnEvent(kind, msg)
	}
}

func (t *Table) changed() {
	if t.OnChange != nil {
		t.OnChange(t.List())
	}
}

// Add opens (or refreshes) a mapping on behalf of requester. The target must be the requester
// itself: a device can only open ports to itself.
func (t *Table) Add(requester, target netip.Addr, proto string, ext, internal int, desc string, lease time.Duration) error {
	err := t.add(requester, target, proto, ext, internal, desc, lease)
	if err != nil {
		t.event("deny", requester.String()+" asked for "+proto+" port "+itoa(ext)+": "+err.Error())
		return err
	}
	t.event("open", requester.String()+" opened "+proto+" port "+itoa(ext)+" → "+itoa(internal)+" ("+desc+")")
	t.changed()
	return nil
}

func (t *Table) add(requester, target netip.Addr, proto string, ext, internal int, desc string, lease time.Duration) error {
	if proto != "tcp" && proto != "udp" {
		return ErrBadArgs
	}
	if ext < 1 || ext > 65535 || internal < 1 || internal > 65535 {
		return ErrBadArgs
	}
	if !requester.IsValid() || !target.Is4() || target != requester {
		return ErrNotAuthorized
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pol.Allowed != nil && !t.pol.Allowed(requester) {
		return ErrNotAuthorized
	}
	if ext < 1024 || (t.pol.Reserved != nil && t.pol.Reserved(proto, ext)) {
		return ErrNotAuthorized
	}
	if len(desc) > 80 {
		desc = desc[:80]
	}
	now := t.now()
	t.expireLocked(now)
	if lease <= 0 || lease > t.pol.MaxLease {
		lease = t.pol.MaxLease
	}
	for _, m := range t.maps {
		if m.Proto == proto && m.ExtPort == ext {
			if m.IntAddr != target.String() {
				return ErrConflict
			}
			m.IntPort, m.Desc, m.Expires = internal, desc, now.Add(lease)
			return nil
		}
	}
	own := 0
	for _, m := range t.maps {
		if m.IntAddr == target.String() {
			own++
		}
	}
	if own >= t.pol.PerDevice || len(t.maps) >= t.pol.Total {
		return ErrNoneLeft
	}
	t.next++
	t.maps = append(t.maps, &Mapping{ID: t.next, Proto: proto, ExtPort: ext, IntPort: internal, IntAddr: target.String(), Desc: desc, Created: now, Expires: now.Add(lease)})
	return nil
}

// Delete closes a mapping. Only the device that owns it may do so.
func (t *Table) Delete(requester netip.Addr, proto string, ext int) error {
	t.mu.Lock()
	for i, m := range t.maps {
		if m.Proto == proto && m.ExtPort == ext {
			if m.IntAddr != requester.String() {
				t.mu.Unlock()
				return ErrNotAuthorized
			}
			t.maps = append(t.maps[:i], t.maps[i+1:]...)
			t.mu.Unlock()
			t.event("close", requester.String()+" closed "+proto+" port "+itoa(ext))
			t.changed()
			return nil
		}
	}
	t.mu.Unlock()
	return ErrNoSuchEntry
}

// Revoke closes a mapping by id; the console uses it.
func (t *Table) Revoke(id int64) bool {
	t.mu.Lock()
	for i, m := range t.maps {
		if m.ID == id {
			t.maps = append(t.maps[:i], t.maps[i+1:]...)
			t.mu.Unlock()
			t.event("close", "revoked "+m.Proto+" port "+itoa(m.ExtPort)+" for "+m.IntAddr)
			t.changed()
			return true
		}
	}
	t.mu.Unlock()
	return false
}

// Clear closes everything, for example when the internet connection changes.
func (t *Table) Clear(why string) {
	t.mu.Lock()
	n := len(t.maps)
	t.maps = nil
	t.mu.Unlock()
	if n > 0 {
		t.event("close", "closed all ports: "+why)
	}
	t.changed()
}

func (t *Table) Get(proto string, ext int) (Mapping, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireLocked(t.now())
	for _, m := range t.maps {
		if m.Proto == proto && m.ExtPort == ext {
			return *m, true
		}
	}
	return Mapping{}, false
}

// Index returns the n-th mapping in a stable order, for GetGenericPortMappingEntry.
func (t *Table) Index(n int) (Mapping, bool) {
	l := t.List()
	if n < 0 || n >= len(l) {
		return Mapping{}, false
	}
	return l[n], true
}

func (t *Table) List() []Mapping {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireLocked(t.now())
	out := make([]Mapping, 0, len(t.maps))
	for _, m := range t.maps {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Expire drops mappings whose lease ran out; it reports whether anything changed.
func (t *Table) Expire() bool {
	t.mu.Lock()
	n := len(t.maps)
	t.expireLocked(t.now())
	changed := len(t.maps) != n
	t.mu.Unlock()
	if changed {
		t.changed()
	}
	return changed
}

func (t *Table) expireLocked(now time.Time) {
	kept := t.maps[:0]
	for _, m := range t.maps {
		if now.Before(m.Expires) {
			kept = append(kept, m)
		}
	}
	t.maps = kept
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
