package app

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// "My network": one printable page for a handover or your records. It never includes passwords,
// keys or codes. Wi-Fi passwords belong on a join card (Wi-Fi page), which you print on purpose.

// table runs a query and returns every row as text. It reads all rows and closes them before
// returning, because the settings database allows one connection at a time.
func (a *App) table(query string, args ...any) [][]string {
	rows, err := a.Config.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out [][]string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if rows.Scan(ptrs...) != nil {
			continue
		}
		r := make([]string, len(cols))
		for i, v := range vals {
			switch x := v.(type) {
			case nil:
			case []byte:
				r[i] = string(x)
			default:
				r[i] = fmt.Sprint(x)
			}
		}
		out = append(out, r)
	}
	return out
}

type sumSection struct {
	Title string
	Head  []string
	Rows  [][]string
	Note  string
}

func (a *App) networkSummary() (string, []sumSection) {
	var s []sumSection
	add := func(title, note string, head []string, rows [][]string) {
		s = append(s, sumSection{title, head, rows, note})
	}
	ver := a.currentVersion()
	add("This machine", "", []string{"Item", "Value"}, [][]string{
		{"Gatehouse version", ver},
		{"Console", a.consoleLine()},
		{"If you are locked out", "Use the recovery menu on the screen of this machine. It does not need the network."},
		{"Backups", a.backupSummary()},
	})
	add("Internet connections", "", []string{"Name", "Port", "Type", "VLAN", "Address"},
		a.table(`SELECT name,iface,type,CASE WHEN vlan_id=0 THEN '' ELSE vlan_id END,CASE WHEN type='static' THEN ipv4_addr ELSE 'from provider' END FROM wan_links WHERE enabled=1 ORDER BY priority,id`))
	add("Networks", "", []string{"Network", "Kind", "Address", "VLAN", "Ports"},
		a.table(`SELECT z.name,z.kind,COALESCE((SELECT group_concat(i.ipv4_cidr) FROM interfaces i WHERE i.zone_id=z.id AND i.ipv4_cidr<>''),''),
			COALESCE((SELECT group_concat(i.vlan_id) FROM interfaces i WHERE i.zone_id=z.id AND i.vlan_id>0),''),
			COALESCE((SELECT group_concat(i.name) FROM interfaces i WHERE i.zone_id=z.id),'') FROM zones z WHERE z.kind NOT IN ('wan') ORDER BY z.id`))
	add("DHCP", "", []string{"Name", "Port", "Range", "Router", "Lease (hours)"},
		a.table(`SELECT name,iface,range_start||' to '||range_end,router,lease_seconds/3600 FROM dhcp_scopes WHERE enabled=1 AND mode='serve' ORDER BY id`))
	add("Reserved addresses", "", []string{"Name", "Address", "Device"},
		a.table(`SELECT hostname,ip,mac FROM dhcp_reservations ORDER BY ip`))
	add("Devices", "Name, address and the network each was last seen on.", []string{"Device", "Last address", "Hardware address"},
		a.table(`SELECT CASE WHEN name<>'' THEN name ELSE mac END,last_ip,mac FROM devices ORDER BY last_ip`))
	add("Wi-Fi names", "Passwords are not printed here. Use the Wi-Fi page to print a join card.", []string{"Name", "Card", "Hidden"},
		a.table(`SELECT ssid,iface,CASE WHEN hidden=1 THEN 'yes' ELSE 'no' END FROM wifi_radios WHERE mode='ap' AND enabled=1 AND ssid<>''`))
	add("Open ports to the internet", "", []string{"Name", "Protocol", "Port", "Goes to"},
		a.table(`SELECT name,proto,ext_port,int_addr||CASE WHEN int_port<>'' THEN ':'||int_port ELSE '' END FROM nat_forwards WHERE enabled=1 ORDER BY id`))
	upnp := []string{}
	if m, err := fw.Load(a.Config); err == nil {
		for _, z := range m.Zones {
			if z.UPnP && z.Kind != fw.KindWan {
				upnp = append(upnp, z.Name)
			}
		}
	}
	state := "Off"
	if len(upnp) > 0 {
		state = "On for " + strings.Join(upnp, ", ")
	}
	add("UPnP", "", []string{"State"}, [][]string{{state}})
	add("VPN entry points", "Keys are never printed.", []string{"Name", "Port", "Address"},
		a.table(`SELECT name,listen_port,address FROM vpn_wg WHERE enabled=1 AND mode='server' ORDER BY id`))
	return ver, s
}

// consoleLine says where to open the console: the router's address on a network allowed to manage it.
func (a *App) consoleLine() string {
	port := strings.TrimPrefix(a.consoleAddr(), ":")
	rows := a.table(`SELECT i.ipv4_cidr FROM interfaces i JOIN zones z ON z.id=i.zone_id WHERE z.mgmt=1 AND i.ipv4_cidr<>'' ORDER BY i.id`)
	var urls []string
	for _, r := range rows {
		ip, _, _ := strings.Cut(r[0], "/")
		urls = append(urls, "https://"+ip+":"+port)
	}
	if len(urls) == 0 {
		return ":" + port
	}
	return strings.Join(urls, "  or  ")
}

func (a *App) backupSummary() string {
	var newest int64
	for _, b := range a.listBackups() {
		if kindLabel(b.Name) != "Before a change" && b.Created > newest {
			newest = b.Created
		}
	}
	if newest == 0 {
		return "No backup has been made yet."
	}
	return "Last backup " + time.Unix(newest, 0).Format("2 Jan 2006, 15:04")
}

func renderSummary(ver string, secs []sumSection, now time.Time) string {
	var b strings.Builder
	e := html.EscapeString
	b.WriteString(`<!doctype html><html lang=en><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><title>My network</title>
<style>body{font:15px/1.45 system-ui,sans-serif;margin:0;padding:2rem;max-width:56rem;margin-inline:auto;color:#111}h1{margin:0}h2{margin:1.6rem 0 .4rem;font-size:1.05rem;border-bottom:1px solid #999;padding-bottom:.2rem}table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:.25rem .5rem;border-bottom:1px solid #ddd;vertical-align:top}th{color:#555;font-weight:600}.note{color:#555;font-size:.85rem;margin:.2rem 0}.none{color:#777}.tip{color:#777;font-size:.85rem}@media print{.tip{display:none}body{padding:0}}</style>`)
	fmt.Fprintf(&b, "<h1>My network</h1><p class=note>Made %s by Gatehouse %s. Contains no passwords or keys.</p><p class=tip>Print or save as PDF with Ctrl+P or Cmd+P.</p>", e(now.Format("2 January 2006, 15:04")), e(ver))
	for _, s := range secs {
		fmt.Fprintf(&b, "<h2>%s</h2>", e(s.Title))
		if s.Note != "" {
			fmt.Fprintf(&b, "<p class=note>%s</p>", e(s.Note))
		}
		if len(s.Rows) == 0 {
			b.WriteString("<p class=none>None.</p>")
			continue
		}
		b.WriteString("<table><tr>")
		for _, h := range s.Head {
			fmt.Fprintf(&b, "<th>%s</th>", e(h))
		}
		b.WriteString("</tr>")
		for _, r := range s.Rows {
			b.WriteString("<tr>")
			for _, c := range r {
				fmt.Fprintf(&b, "<td>%s</td>", e(c))
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</table>")
	}
	b.WriteString("</html>")
	return b.String()
}

func (a *App) registerSummaryRoutes() {
	a.API.HandleAdmin("GET /api/summary/print", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		ver, secs := a.networkSummary()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprint(w, renderSummary(ver, secs, time.Now()))
	})
}
