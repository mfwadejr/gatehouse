package upnp

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

var ssdpGroup = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}

var searchTargets = []string{
	"upnp:rootdevice",
	"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
	"urn:schemas-upnp-org:device:WANDevice:1",
	"urn:schemas-upnp-org:device:WANConnectionDevice:1",
	svcType,
}

// matches returns the targets a search should be answered with.
func matches(st string) []string {
	st = strings.TrimSpace(st)
	if st == "ssdp:all" {
		return searchTargets
	}
	for _, t := range searchTargets {
		if strings.EqualFold(t, st) {
			return []string{t}
		}
	}
	return nil
}

// searchTarget pulls the ST header out of an M-SEARCH, or "" for anything else.
func searchTarget(pkt []byte) string {
	lines := strings.Split(string(pkt), "\r\n")
	if len(lines) == 0 || !strings.HasPrefix(strings.ToUpper(lines[0]), "M-SEARCH") {
		return ""
	}
	for _, l := range lines[1:] {
		if i := strings.IndexByte(l, ':'); i > 0 && strings.EqualFold(strings.TrimSpace(l[:i]), "ST") {
			return strings.TrimSpace(l[i+1:])
		}
	}
	return ""
}

func response(st, location, uuid string) []byte {
	usn := "uuid:" + uuid + "::" + st
	if st == "uuid:"+uuid {
		usn = st
	}
	return []byte(fmt.Sprintf("HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nEXT:\r\nLOCATION: %s\r\nSERVER: Gatehouse UPnP/1.0\r\nST: %s\r\nUSN: %s\r\n\r\n", location, st, usn))
}

func notify(st, location, uuid string) []byte {
	return []byte(fmt.Sprintf("NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nCACHE-CONTROL: max-age=1800\r\nLOCATION: %s\r\nNT: %s\r\nNTS: ssdp:alive\r\nSERVER: Gatehouse UPnP/1.0\r\nUSN: uuid:%s::%s\r\n\r\n", location, st, uuid, st))
}

// serveSSDP answers searches from one LAN until ctx ends. Searches from anywhere else are ignored,
// and each device is answered at most a few times a second.
func serveSSDP(ctx context.Context, ifi *net.Interface, lan netip.Prefix, location, uuid string) error {
	c, err := net.ListenMulticastUDP("udp4", ifi, ssdpGroup)
	if err != nil {
		return err
	}
	go func() { <-ctx.Done(); c.Close() }()
	send := func(b []byte, to *net.UDPAddr) { c.WriteToUDP(b, to) }
	for _, t := range searchTargets {
		send(notify(t, location, uuid), ssdpGroup)
	}
	go func() {
		tk := time.NewTicker(15 * time.Minute)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				for _, t := range searchTargets {
					send(notify(t, location, uuid), ssdpGroup)
				}
			}
		}
	}()
	last := map[netip.Addr]time.Time{}
	buf := make([]byte, 2048)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return nil
		}
		ip, ok := netip.AddrFromSlice(from.IP)
		if !ok || !lan.Contains(ip.Unmap()) {
			continue
		}
		ts := matches(searchTarget(buf[:n]))
		if ts == nil {
			continue
		}
		now := time.Now()
		if len(last) > 512 {
			last = map[netip.Addr]time.Time{}
		}
		if now.Sub(last[ip]) < 200*time.Millisecond {
			continue
		}
		last[ip] = now
		for _, t := range ts {
			send(response(t, location, uuid), from)
		}
	}
}
