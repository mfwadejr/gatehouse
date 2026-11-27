package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Renaming a network. A network is stored under its name in several places that are not tied to it by
// number: firewall rules (both ends), what each VPN tunnel carries, the DNS profile of the network,
// blocks that name it, the device-discovery pairs, and the DHCP address pool made with it. The history
// (firewall log, graphs) is keyed by the name too. Every one of them is moved to the new name in the
// same step, so nothing is left pointing at a name that no longer exists. A test lists the places and
// fails if a new one is added without being handled here.

// renameNetworkRefs moves everything stored under the old network name to the new one, inside the
// same transaction that renamed the network itself. It refuses before changing anything when another
// DHCP address pool already has the new name.
func renameNetworkRefs(tx *sql.Tx, old, name string) error {
	if old == "" || old == name {
		return nil
	}
	// The address pool made with the network carries its name; one the person named differently is left.
	var scopeID int64
	scopeFound := tx.QueryRow(`SELECT s.id FROM dhcp_scopes s JOIN interfaces i ON i.name=s.iface JOIN zones z ON z.id=i.zone_id
		WHERE s.name=? AND z.name=?`, old, name).Scan(&scopeID) == nil
	if scopeFound {
		var clash int
		tx.QueryRow(`SELECT count(*) FROM dhcp_scopes WHERE name=? AND id<>?`, name, scopeID).Scan(&clash)
		if clash > 0 {
			return errors.New("a DHCP address pool called " + name + " already exists. Choose another name, or rename that pool first")
		}
		if _, err := tx.Exec(`UPDATE dhcp_scopes SET name=? WHERE id=?`, name, scopeID); err != nil {
			return err
		}
	}
	for _, q := range []string{
		`UPDATE vpn_routes SET value=? WHERE kind='network' AND value=?`,
		`UPDATE network_profiles SET zone=? WHERE zone=?`,
	} {
		if _, err := tx.Exec(q, name, old); err != nil {
			return err
		}
	}
	// Rules the Firewall matrix wrote carry the two network names in their description. Rebuild the
	// start of each from the rule's own ends, so a name that merely contains another is not touched.
	if rows, err := tx.Query(`SELECT id,from_zone,to_zone,descr FROM fw_rules WHERE descr LIKE 'Set from the matrix: %' AND (from_zone=? OR to_zone=?)`, name, name); err == nil {
		type upd struct {
			id int64
			d  string
		}
		var todo []upd
		for rows.Next() {
			var id int64
			var from, to, d string
			if rows.Scan(&id, &from, &to, &d) != nil {
				continue
			}
			of, ot := from, to
			if from == name {
				of = old
			}
			if to == name {
				ot = old
			}
			oldHead := "Set from the matrix: " + of + " to " + ot
			if strings.HasPrefix(d, oldHead) {
				todo = append(todo, upd{id, "Set from the matrix: " + from + " to " + to + d[len(oldHead):]})
			}
		}
		rows.Close()
		for _, u := range todo {
			if _, err := tx.Exec(`UPDATE fw_rules SET descr=? WHERE id=?`, u.d, u.id); err != nil {
				return err
			}
		}
	}
	// Lists kept as JSON in a column: blocks and speed rules name networks as {scope:network,target},
	// a route names them as {kind:network,value}.
	for _, j := range []struct{ table, col, kindKey, valKey string }{
		{"block_sets", "targets", "scope", "target"},
		{"speed_rules", "targets", "scope", "target"},
		{"routes", "srcs", "kind", "value"},
	} {
		if err := renameInJSONList(tx, j.table, j.col, j.kindKey, j.valKey, old, name); err != nil {
			return err
		}
	}
	for _, q := range []string{
		`UPDATE wan_group_routes SET value=? WHERE kind='network' AND value=? COLLATE NOCASE`,
		`UPDATE scope_rules SET target=? WHERE scope='network' AND target=? COLLATE NOCASE`,
	} {
		if _, err := tx.Exec(q, name, old); err != nil {
			return err
		}
	}
	// Device discovery between networks: pairs of network names, kept in two settings.
	for _, key := range []string{"fw.mdns", "fw.mdns_ui"} {
		var raw string
		if tx.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&raw) != nil || raw == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(raw), &m) != nil {
			continue
		}
		pairs, _ := m["pairs"].([]any)
		changed := false
		for _, p := range pairs {
			if pm, ok := p.(map[string]any); ok {
				for _, k := range []string{"a", "b"} {
					if pm[k] == old {
						pm[k] = name
						changed = true
					}
				}
			}
		}
		if changed {
			b, _ := json.Marshal(m)
			if _, err := tx.Exec(`UPDATE settings SET value=? WHERE key=?`, string(b), key); err != nil {
				return err
			}
		}
	}
	return nil
}

// renameNetworkHistory moves the log and graph history to the new name, so the Logs filters, the
// network graphs and the reports keep showing the whole past under one name. It runs after the
// rename is saved; a failure here leaves the settings right and only the old history under the old name.
func (a *App) renameNetworkHistory(old, name string) {
	if old == "" || old == name {
		return
	}
	if a.Logs != nil {
		a.Logs.Exec(`UPDATE fw_events SET from_zone=? WHERE from_zone=?`, name, old)
		a.Logs.Exec(`UPDATE fw_events SET to_zone=? WHERE to_zone=?`, name, old)
		for _, suf := range []string{"down", "up"} {
			a.Logs.Exec(`UPDATE OR REPLACE metrics SET key=? WHERE key=?`, "net."+name+"."+suf, "net."+old+"."+suf)
		}
	}
	a.netTraffic.renameSeries(old, name)
	a.hist.mu.Lock()
	if z, ok := a.hist.zones[old]; ok {
		delete(a.hist.zones, old)
		a.hist.zones[name] = z
	}
	a.hist.mu.Unlock()
}

func (t *wanTraffic) renameSeries(old, name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.series[old]; ok {
		delete(t.series, old)
		s.Dev = name
		t.series[name] = s
	}
}

// renameInJSONList rewrites, in every row of a table, the entries of a JSON list of objects that name
// the old network: those whose kindKey is "network" and whose valKey is the old name.
func renameInJSONList(tx *sql.Tx, table, col, kindKey, valKey, old, name string) error {
	rows, err := tx.Query(`SELECT id,` + col + ` FROM ` + table)
	if err != nil {
		return nil // a table that does not exist on this box has nothing to move
	}
	type upd struct {
		id int64
		js string
	}
	var todo []upd
	for rows.Next() {
		var id int64
		var js string
		if rows.Scan(&id, &js) != nil || js == "" {
			continue
		}
		var list []map[string]any
		if json.Unmarshal([]byte(js), &list) != nil {
			continue
		}
		changed := false
		for _, it := range list {
			if k, _ := it[kindKey].(string); k != "network" {
				continue
			}
			if v, _ := it[valKey].(string); strings.EqualFold(v, old) {
				it[valKey] = name
				changed = true
			}
		}
		if changed {
			b, _ := json.Marshal(list)
			todo = append(todo, upd{id, string(b)})
		}
	}
	rows.Close()
	for _, u := range todo {
		if _, err := tx.Exec(`UPDATE `+table+` SET `+col+`=? WHERE id=?`, u.js, u.id); err != nil {
			return err
		}
	}
	return nil
}
