package app

import (
	"fmt"
	"html"
	"strings"
)

// A printable picture of the network for the My network page: the internet connection(s), Gatehouse,
// each network and the devices on it. It is plain SVG with no scripts, no passwords and no keys.

type topoNet struct {
	Name    string
	Addr    string
	VLAN    string
	Ports   string
	Devices []string
	More    int
}

type topology struct {
	Internet []string // one line per connection, for example "Fibre (DHCP)"
	Nets     []topoNet
}

const (
	topoW    = 900
	topoCols = 3
	topoBoxW = 270
	topoGap  = 30
	topoLine = 16
	topoCap  = 10 // devices listed per network before "and N more"
)

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// topologySVG draws t. All text is escaped.
func topologySVG(t topology) string {
	e := html.EscapeString
	var b strings.Builder
	// Row heights depend on the busiest network in the row.
	rows := (len(t.Nets) + topoCols - 1) / topoCols
	heights := make([]int, rows)
	for i, n := range t.Nets {
		h := 62 + topoLine*len(n.Devices)
		if n.More > 0 {
			h += topoLine
		}
		if n.VLAN != "" {
			h += topoLine
		}
		if h > heights[i/topoCols] {
			heights[i/topoCols] = h
		}
	}
	inetH := 36 + topoLine*len(t.Internet)
	if len(t.Internet) == 0 {
		inetH = 52
	}
	top := 10
	inetY := top
	ghY := inetY + inetH + 36
	ghH := 44
	netsY := ghY + ghH + 40
	total := netsY
	for _, h := range heights {
		total += h + 24
	}
	if len(t.Nets) == 0 {
		total += 40
	}
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" role="img" aria-label="Map of the network" style="width:100%%;height:auto;font:13px system-ui,sans-serif">`, topoW, total)
	box := func(x, y, w, h int, fill string) {
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="8" fill="%s" stroke="#444" stroke-width="1.2"/>`, x, y, w, h, fill)
	}
	text := func(x, y int, s string, bold bool, grey bool) {
		w, c := "400", "#111"
		if bold {
			w = "700"
		}
		if grey {
			c = "#666"
		}
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-weight="%s" fill="%s">%s</text>`, x, y, w, c, e(s))
	}
	cx := topoW / 2
	// Internet
	box(cx-170, inetY, 340, inetH, "#eef3fa")
	text(cx-155, inetY+22, "Internet", true, false)
	if len(t.Internet) == 0 {
		text(cx-155, inetY+42, "No connection set up", false, true)
	}
	for i, l := range t.Internet {
		text(cx-155, inetY+40+i*topoLine, trunc(l, 44), false, false)
	}
	// Gatehouse
	fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#444" stroke-width="1.5"/>`, cx, inetY+inetH, cx, ghY)
	box(cx-110, ghY, 220, ghH, "#e6f4ea")
	text(cx-95, ghY+27, "Gatehouse", true, false)
	// Networks. Each row has a line just above it that the boxes hang from; the rows join up by a
	// line down the left edge, so no line ever crosses a box.
	rowY := make([]int, rows)
	y := netsY
	for r := range rowY {
		rowY[r] = y
		y += heights[r] + 24
	}
	line := func(x1, y1, x2, y2 int, w string) {
		fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#777" stroke-width="%s"/>`, x1, y1, x2, y2, w)
	}
	rowX := func(row, col int) int {
		inRow := topoCols
		if rem := len(t.Nets) - row*topoCols; rem < topoCols {
			inRow = rem
		}
		return (topoW-(inRow*topoBoxW+(inRow-1)*topoGap))/2 + col*(topoBoxW+topoGap)
	}
	for r := 0; r < rows; r++ {
		bus := rowY[r] - 12
		inRow := topoCols
		if rem := len(t.Nets) - r*topoCols; rem < topoCols {
			inRow = rem
		}
		first, last := rowX(r, 0)+topoBoxW/2, rowX(r, inRow-1)+topoBoxW/2
		if r == 0 {
			line(cx, ghY+ghH, cx, bus, "1.5")
			lo, hi := first, last
			if cx < lo {
				lo = cx
			}
			if cx > hi {
				hi = cx
			}
			if rows > 1 {
				lo = 18
			}
			line(lo, bus, hi, bus, "1.2")
		} else {
			line(18, rowY[r-1]-12, 18, bus, "1.2")
			line(18, bus, last, bus, "1.2")
		}
		for c := 0; c < inRow; c++ {
			line(rowX(r, c)+topoBoxW/2, bus, rowX(r, c)+topoBoxW/2, rowY[r], "1.2")
		}
	}
	for i, n := range t.Nets {
		col, row := i%topoCols, i/topoCols
		x, y, h := rowX(row, col), rowY[row], heights[row]
		box(x, y, topoBoxW, h, "#fff")
		text(x+12, y+22, trunc(n.Name, 30), true, false)
		ly := y + 40
		if n.Addr != "" {
			text(x+12, ly, trunc(n.Addr, 36), false, true)
			ly += topoLine
		}
		if n.VLAN != "" {
			text(x+12, ly, "VLAN "+n.VLAN, false, true)
			ly += topoLine
		}
		ly += 4
		for _, d := range n.Devices {
			text(x+22, ly, "• "+trunc(d, 32), false, false)
			ly += topoLine
		}
		if n.More > 0 {
			text(x+22, ly, fmt.Sprintf("and %d more", n.More), false, true)
		}
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// networkTopology gathers what the picture shows.
func (a *App) networkTopology() topology {
	var t topology
	for _, r := range a.table(`SELECT name,type FROM wan_links WHERE enabled=1 ORDER BY priority,id`) {
		kind := map[string]string{"dhcp": "address from provider", "static": "fixed address", "pppoe": "provider login"}[r[1]]
		if kind == "" {
			kind = r[1]
		}
		t.Internet = append(t.Internet, r[0]+" ("+kind+")")
	}
	byNet := map[string][]string{}
	if rows, err := a.deviceRows(); err == nil {
		for _, d := range rows {
			if d.Source != "device" || d.Network == "" {
				continue
			}
			nm := d.Name
			if nm == "" {
				nm = d.Hostname
			}
			if nm == "" {
				nm = d.Vendor
			}
			if nm == "" {
				nm = d.MAC
			}
			if !d.Online {
				nm += " (away)"
			}
			byNet[d.Network] = append(byNet[d.Network], nm)
		}
	}
	for _, r := range a.table(`SELECT z.name,
			COALESCE((SELECT group_concat(i.ipv4_cidr) FROM interfaces i WHERE i.zone_id=z.id AND i.ipv4_cidr<>''),''),
			COALESCE((SELECT group_concat(i.vlan_id) FROM interfaces i WHERE i.zone_id=z.id AND i.vlan_id>0),''),
			COALESCE((SELECT group_concat(i.name) FROM interfaces i WHERE i.zone_id=z.id),'') FROM zones z WHERE z.kind NOT IN ('wan') ORDER BY z.id`) {
		n := topoNet{Name: r[0], Addr: r[1], VLAN: r[2], Ports: r[3]}
		ds := byNet[r[0]]
		if len(ds) > topoCap {
			n.More = len(ds) - topoCap
			ds = ds[:topoCap]
		}
		n.Devices = ds
		t.Nets = append(t.Nets, n)
	}
	return t
}
