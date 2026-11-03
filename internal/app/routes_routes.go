package app

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// The Routes screen: send chosen devices or networks through a VPN or one internet connection,
// for everything or only for chosen destinations.

type routeTarget struct {
	Kind    string `json:"kind"` // tunnel or wan
	Target  string `json:"target"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	Note    string `json:"note,omitempty"`
}

type routeView struct {
	fw.RouteRow
	TargetLabel string   `json:"target_label"`
	State       string   `json:"state"` // working, off, nobody, target-off, one-connection
	Plain       string   `json:"plain"`
	Warnings    []string `json:"warnings"`
}

type legacyUse struct {
	Tunnel string `json:"tunnel"`
	Label  string `json:"label"`
	Uses   string `json:"uses"`
}

// routeTargets lists what a route can send traffic to.
func (a *App) routeTargets(q querier) []routeTarget {
	var out []routeTarget
	if wgs, err := a.listWG(q); err == nil {
		for _, w := range wgs {
			if w.Mode == "client" {
				out = append(out, routeTarget{"tunnel", w.Name, labelOr(w.Label, w.Name), w.Enabled, ""})
			}
		}
	}
	if ovs, err := a.listOVPN(q); err == nil {
		for _, o := range ovs {
			if o.Mode == "client" {
				out = append(out, routeTarget{"tunnel", o.Name, labelOr(o.Label, o.Name), o.Enabled, ""})
			}
		}
	}
	if ws, err := a.listWans(q); err == nil {
		for _, w := range ws {
			out = append(out, routeTarget{"wan", w.Name, w.Name, w.Enabled, ""})
		}
	}
	if gs, err := a.listGroups(q); err == nil {
		for _, g := range gs {
			var names []string
			for _, m := range g.Members {
				names = append(names, m.Name)
			}
			note := "uses " + strings.Join(names, ", ") + ", in that order"
			if g.Mode == "balance" {
				note = "shares traffic across " + strings.Join(names, ", ")
			}
			out = append(out, routeTarget{"group", g.Name, g.Name + " (connection group)", len(g.Members) > 0, note})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind > out[j].Kind }) // VPNs first
	return out
}

func labelOr(l, d string) string {
	if strings.TrimSpace(l) != "" {
		return l
	}
	return d
}

func (a *App) routesView() map[string]any {
	rows, _ := fw.Routes(a.Config)
	targets := a.routeTargets(a.Config)
	byKey := map[string]routeTarget{}
	nWan, nOn := 0, 0
	for _, t := range targets {
		byKey[t.Kind+"|"+t.Target] = t
		if t.Kind == "wan" {
			nWan++
			if t.Enabled {
				nOn++
			}
		}
	}
	nb := a.nameBook()
	specs, _ := fw.RouteSpecs(a.Config)
	working := map[string]bool{}
	for _, s := range specs {
		working[s.Name] = true
	}
	views := []routeView{}
	for _, r := range rows {
		v := routeView{RouteRow: r, Warnings: []string{}}
		t, have := byKey[r.TargetKind+"|"+r.Target]
		v.TargetLabel = labelOr(t.Label, r.Target)
		if r.TargetKind == "links" {
			names := strings.Split(r.Target, ",")
			on := 0
			for _, n := range names {
				if byKey["wan|"+n].Enabled {
					on++
				}
			}
			have, t.Enabled = on > 0, on > 0
			if r.Mode == "balance" && len(names) > 1 {
				v.TargetLabel = "shared across " + strings.Join(names, ", ")
			} else {
				v.TargetLabel = strings.Join(names, ", then ")
			}
		}
		var who []string
		for _, u := range r.Uses {
			switch u.Kind {
			case "device":
				who = append(who, nb.deviceLabel(strings.ToLower(u.Value)))
			default:
				who = append(who, u.Value)
			}
		}
		whoText := "nobody yet"
		if len(who) > 0 {
			whoText = strings.Join(who, ", ")
		}
		where := "everything"
		if len(r.Dests) > 0 {
			var d []string
			for _, x := range r.Dests {
				if x.Kind == "region" {
					d = append(d, x.Value+" (country)")
				} else {
					d = append(d, x.Value)
				}
			}
			where = strings.Join(d, ", ")
		}
		end := ""
		if r.TargetKind == "tunnel" && r.Kill {
			end = " (stops if the VPN is down)"
		} else if r.TargetKind == "tunnel" {
			end = " (uses the normal connection if the VPN is down)"
		}
		if strings.TrimSpace(r.Service) != "" {
			var sv []string
			for _, x := range strings.Split(r.Service, ",") {
				sv = append(sv, strings.TrimPrefix(x, "list:"))
			}
			if where == "everything" {
				where = "all addresses"
			}
			where += " (" + strings.Join(sv, ", ") + ")"
		}
		v.Plain = fmt.Sprintf("%s: %s only through %s%s", whoText, where, v.TargetLabel, end)
		if where == "everything" {
			v.Plain = fmt.Sprintf("%s: everything through %s%s", whoText, v.TargetLabel, end)
		}
		if r.Sched != nil {
			v.Plain += ", " + strings.ToLower(r.Sched.Describe())
		}
		switch {
		case !r.Enabled:
			v.State = "off"
		case r.PausedUntil > time.Now().Unix():
			v.State = "paused"
		case !have:
			v.State = "target-off"
			v.Warnings = append(v.Warnings, "The VPN or connection this route uses no longer exists.")
		case !t.Enabled:
			v.State = "target-off"
			v.Warnings = append(v.Warnings, v.TargetLabel+" is switched off, so this route is not working.")
		case len(r.Uses) == 0:
			v.State = "nobody"
		case (r.TargetKind == "wan" || r.TargetKind == "links") && nOn < 2:
			v.State = "one-connection"
			v.Warnings = append(v.Warnings, "Routing to one connection needs at least two internet connections turned on. Until then traffic uses the one you have.")
		case !working[r.Name]:
			v.State = "nobody"
			v.Warnings = append(v.Warnings, "None of the chosen devices has an address yet, so there is nothing to route.")
		default:
			v.State = "working"
		}
		for _, d := range r.Dests {
			if d.Kind == "domain" {
				v.Warnings = append(v.Warnings, "Websites are found from the lookups your devices make through Gatehouse. A device using its own DNS, or one that already knew the address, may go the normal way at first.")
				break
			}
		}
		views = append(views, v)
	}
	// What the VPN page set up the older way: shown here so nothing is hidden.
	var legacy []legacyUse
	if lr, err := a.Config.Query(`SELECT tunnel,kind,value,label FROM vpn_routes ORDER BY id`); err == nil {
		per := map[string][]string{}
		var order []string
		for lr.Next() {
			var t, k, v, l string
			lr.Scan(&t, &k, &v, &l)
			if k == "device" {
				v = nb.deviceLabel(strings.ToLower(v))
			}
			if _, seen := per[t]; !seen {
				order = append(order, t)
			}
			per[t] = append(per[t], v)
		}
		lr.Close()
		for _, t := range order {
			legacy = append(legacy, legacyUse{Tunnel: t, Label: labelOr(byKey["tunnel|"+t].Label, t), Uses: strings.Join(per[t], ", ")})
		}
	}
	return map[string]any{"routes": views, "targets": targets, "legacy": legacy, "max": fw.MaxRoutes}
}

func (a *App) registerRouteRoutes() {
	s := a.API
	s.Handle("GET /api/routes", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.routesView())
	})
	s.HandleAdmin("PUT /api/routes", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in fw.RouteRow
		if !api.Decode(w, r, &in) || !a.needHelper(w) {
			return
		}
		err := a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
			if in.TargetKind == "tunnel" {
				var n int
				tx.QueryRow(`SELECT (SELECT count(*) FROM vpn_wg WHERE name=? AND mode='client')+(SELECT count(*) FROM vpn_ovpn WHERE name=? AND mode='client')`, in.Target, in.Target).Scan(&n)
				if n == 0 {
					return fmt.Errorf("there is no VPN called %q to send traffic through", in.Target)
				}
			}
			if in.TargetKind == "wan" {
				var n int
				tx.QueryRow(`SELECT count(*) FROM wan_links WHERE name=?`, in.Target).Scan(&n)
				if n == 0 {
					return fmt.Errorf("there is no internet connection called %q", in.Target)
				}
			}
			if in.TargetKind == "links" {
				for _, n := range strings.Split(in.Target, ",") {
					n = strings.TrimSpace(n)
					if n == "" {
						continue
					}
					var cnt int
					tx.QueryRow(`SELECT count(*) FROM wan_links WHERE name=?`, n).Scan(&cnt)
					if cnt == 0 {
						return fmt.Errorf("there is no internet connection called %q", n)
					}
				}
			}
			if in.TargetKind == "group" {
				var n int
				tx.QueryRow(`SELECT count(*) FROM wan_groups WHERE name=?`, in.Target).Scan(&n)
				if n == 0 {
					return fmt.Errorf("there is no connection group called %q", in.Target)
				}
			}
			if _, err := fw.ParseServices(in.Service, portListsOf(tx)); err != nil {
				return err
			}
			_, err := fw.SaveRoute(tx, in)
			return err
		})
		if err != nil {
			fail(w, err)
			return
		}
		a.refreshVPN()
		api.JSON(w, 200, a.routesView())
	})
	// Pause a route for a while: minutes 0 resumes it. The firewall holds the clock itself, so the
	// route starts again on time with nothing to apply.
	s.HandleAdmin("POST /api/routes/{id}/pause", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		var in struct{ Minutes int }
		if !api.Decode(w, r, &in) || !a.needHelper(w) {
			return
		}
		until, err := pausePick(in.Minutes)
		if err != nil {
			fail(w, err)
			return
		}
		err = a.mutateVPN(c.User.Username, func(tx *sql.Tx) error {
			res, err := tx.Exec(`UPDATE routes SET paused_until=? WHERE id=?`, until, id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("that route no longer exists")
			}
			return nil
		})
		if err != nil {
			fail(w, err)
			return
		}
		a.refreshVPN()
		api.JSON(w, 200, a.routesView())
	})
	s.HandleAdmin("DELETE /api/routes/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if !a.needHelper(w) {
			return
		}
		if err := a.mutateVPN(c.User.Username, func(tx *sql.Tx) error { return fw.DeleteRoute(tx, id) }); err != nil {
			fail(w, err)
			return
		}
		a.refreshVPN()
		ok(w)
	})
	s.HandleAdmin("POST /api/routes/order", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Order []int64 `json:"order"`
		}
		if !api.Decode(w, r, &in) || !a.needHelper(w) {
			return
		}
		if err := a.mutateVPN(c.User.Username, func(tx *sql.Tx) error { return fw.MoveRoutes(tx, in.Order) }); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
}

// portListsOf gives the saved lists of ports by name, as the firewall reads them. It reads through
// the open transaction, so it can be used while one is held.
func portListsOf(q fw.Queryer) map[string][]string {
	out := map[string][]string{}
	rows, _ := fw.PortLists(q)
	for _, p := range rows {
		out[p.Name] = p.Ports
	}
	return out
}

// pausePick checks a pause choice from a policy screen and turns it into the time it ends: 0
// resumes now, -1 pauses until the person resumes, and a number is minutes, up to 30 days.
func pausePick(minutes int) (int64, error) {
	if minutes < -1 || minutes > 60*24*30 {
		return 0, fmt.Errorf("choose a pause of up to 30 days")
	}
	return pauseUntil(minutes, time.Now()), nil
}
