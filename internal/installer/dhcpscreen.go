package installer

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// errNoProbe is the answer when the installer has nothing to probe with (tests, or a build without it).
var errNoProbe = errors.New("not checked")

// probeWait is how long the installer listens on each port. The check runs in the background while the person
// reads the port list, and sends its discover again every two seconds, because a router can take a few seconds
// to answer (it checks that an address is free first).
const probeWait = 8 * time.Second

// DefaultProbe listens on a port for other DHCP servers.
func DefaultProbe(p Port) ProbeResult { return ProbeDHCP(p.Name, probeWait) }

// probeBook remembers what listening found on each port. A port is probed once per cable: when it gains a
// link it is probed in the background, so the person never waits at the port list, and if the cable moves
// to another port that port is probed too.
type probeBook struct {
	mu sync.Mutex
	m  map[string]*probeEntry
}

type probeEntry struct {
	key  string // the port as it was probed: name, hardware address and link
	done chan struct{}
	res  ProbeResult
}

func newProbeBook() *probeBook { return &probeBook{m: map[string]*probeEntry{}} }

// kick starts a probe for each connected port that has none for its current state.
func (b *probeBook) kick(ports []Port, run func(Port) ProbeResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range ports {
		if !p.Link {
			delete(b.m, p.Name)
			continue
		}
		key := p.Name + "|" + p.MAC
		if e := b.m[p.Name]; e != nil && e.key == key {
			continue
		}
		e := &probeEntry{key: key, done: make(chan struct{})}
		b.m[p.Name] = e
		go func(p Port, e *probeEntry) {
			defer close(e.done)
			if run == nil {
				e.res = ProbeResult{Port: p.Name, Err: errNoProbe}
				return
			}
			e.res = run(p)
		}(p, e)
	}
}

// get returns what was found on a port, and whether the probe has finished. ok is false when none was started.
func (b *probeBook) get(port string) (res ProbeResult, finished, ok bool) {
	b.mu.Lock()
	e := b.m[port]
	b.mu.Unlock()
	if e == nil {
		return ProbeResult{Port: port, Err: errNoProbe}, true, false
	}
	select {
	case <-e.done:
		return e.res, true, true
	default:
		return ProbeResult{Port: port}, false, true
	}
}

// wait blocks until a port's probe has finished, or the time is up.
func (b *probeBook) wait(port string, d time.Duration) ProbeResult {
	b.mu.Lock()
	e := b.m[port]
	b.mu.Unlock()
	if e == nil {
		return ProbeResult{Port: port, Err: errNoProbe}
	}
	select {
	case <-e.done:
		return e.res
	case <-time.After(d):
		return ProbeResult{Port: port, Err: errors.New("timed out")}
	}
}

// signature changes when a probe finishes, so the port list is drawn again with what was found.
func (b *probeBook) signature() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var s strings.Builder
	for name, e := range b.m {
		select {
		case <-e.done:
			fmt.Fprintf(&s, "%s=%d;", name, len(e.res.Offers))
		default:
			fmt.Fprintf(&s, "%s=?;", name)
		}
	}
	return s.String()
}

func addrList(as []netip.Addr) string {
	var s []string
	for _, a := range as {
		s = append(s, a.String())
	}
	return strings.Join(s, ", ")
}

// portNote is the line under a port in the list about what was heard on it. wan is the port already chosen
// as the internet one ("" while choosing it).
func portNote(b *probeBook, p Port, forWAN bool, wan string) string {
	if !p.Link {
		return ""
	}
	res, finished, ok := b.get(p.Name)
	switch {
	case !ok:
		return ""
	case !finished:
		return dim + "      checking for a DHCP server…" + reset
	case res.Err != nil:
		return ""
	case res.Unsure() && forWAN:
		return dim + "      has an address (" + res.HasAddress.String() + ") from a server that did not answer the check" + reset
	case res.Unsure():
		return amber + "      has an address (" + res.HasAddress.String() + "), so a DHCP server is probably here; could not tell" + reset
	case res.Heard() && forWAN:
		return dim + "      a DHCP server answers here (" + addrList(res.Servers()) + ")" + reset
	case res.Heard():
		note := amber + "      another DHCP server answers here (" + addrList(res.Servers()) + ")" + reset
		if wan != "" {
			if wres, fin, _ := b.get(wan); fin {
				if s, shared := SharesServer(wres, res); shared {
					note += "\n" + amber + "      " + s.String() + " also answers on " + wan + ": one network?" + reset
				}
			}
		}
		return note
	default:
		return dim + "      no DHCP server answers here" + reset
	}
}

// wrap breaks text into lines of at most w characters, at spaces.
func wrap(text string, w int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		if cur != "" && len(cur)+1+len(word) > w {
			lines = append(lines, cur)
			cur = word
			continue
		}
		if cur != "" {
			cur += " "
		}
		cur += word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// textWidth is how wide the prompts under the heading are allowed to be.
const textWidth = 62

// networkWarning is shown after the management port is chosen when both ports seem to be on one network.
func networkWarning(wan, lan ProbeResult, wanPort, lanPort string) []string {
	s, shared := SharesServer(wan, lan)
	if !shared {
		return nil
	}
	var out []string
	out = append(out, wrap(fmt.Sprintf("%s and %s look like one network: the same DHCP server (%s) answers on both.", wanPort, lanPort, s), textWidth)...)
	out = append(out, wrap("NopeWarden only protects a network that traffic must pass through it to leave. In a virtual machine, put the LAN port on its own virtual network. You can still continue.", textWidth)...)
	return out
}
