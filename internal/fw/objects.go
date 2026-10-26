package fw

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Device groups and reusable port lists, the two things rules can point at besides single devices.

// GroupRow is a device group as the console edits it.
type GroupRow struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ReservedOnly bool     `json:"reserved_only"`
	Members      []string `json:"members"` // device MAC addresses
}

// DeviceGroups lists the groups with their members.
func DeviceGroups(db Queryer) ([]GroupRow, error) {
	rows, err := db.Query(`SELECT id,name,description,reserved_only FROM device_groups ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	var out []GroupRow
	for rows.Next() {
		var g GroupRow
		var ro int
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &ro); err != nil {
			rows.Close()
			return nil, err
		}
		g.ReservedOnly = ro == 1
		g.Members = []string{}
		out = append(out, g)
	}
	rows.Close()
	for i := range out {
		mr, err := db.Query(`SELECT lower(mac) FROM device_group_members WHERE group_id=? ORDER BY mac`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for mr.Next() {
			var mac string
			mr.Scan(&mac)
			out[i].Members = append(out[i].Members, mac)
		}
		mr.Close()
	}
	if out == nil {
		out = []GroupRow{}
	}
	return out, nil
}

func uniqueErr(err error, what string) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return errors.New(what + " with that name already exists")
	}
	return err
}

// SaveDeviceGroup creates (ID 0) or updates a group and replaces its members.
func SaveDeviceGroup(tx *sql.Tx, g GroupRow) (int64, error) {
	g.Name = strings.TrimSpace(g.Name)
	if !reName.MatchString(g.Name) {
		return 0, errors.New("a group name uses letters, numbers, spaces, dots and dashes (up to 32 characters)")
	}
	for _, mac := range g.Members {
		if !reMAC.MatchString(mac) {
			return 0, fmt.Errorf("%q is not a device address", mac)
		}
	}
	if g.ID == 0 {
		res, err := tx.Exec(`INSERT INTO device_groups(name,description,reserved_only) VALUES(?,?,?)`, g.Name, g.Description, b2i(g.ReservedOnly))
		if err != nil {
			return 0, uniqueErr(err, "a group")
		}
		g.ID, _ = res.LastInsertId()
	} else {
		var old string
		if tx.QueryRow(`SELECT name FROM device_groups WHERE id=?`, g.ID).Scan(&old) != nil {
			return 0, errors.New("no such group")
		}
		if _, err := tx.Exec(`UPDATE device_groups SET name=?,description=?,reserved_only=? WHERE id=?`, g.Name, g.Description, b2i(g.ReservedOnly), g.ID); err != nil {
			return 0, uniqueErr(err, "a group")
		}
		if old != g.Name { // rules follow the new name
			renameRef(tx, "grp:"+old, "grp:"+g.Name)
		}
	}
	tx.Exec(`DELETE FROM device_group_members WHERE group_id=?`, g.ID)
	for _, mac := range g.Members {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO device_group_members(group_id,mac) VALUES(?,?)`, g.ID, strings.ToLower(mac)); err != nil {
			return 0, err
		}
	}
	return g.ID, nil
}

func tokenIn(field, tok string) bool {
	for _, p := range strings.FieldsFunc(field, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if strings.EqualFold(p, tok) {
			return true
		}
	}
	return false
}

func swapToken(field, old, repl string) string {
	parts := strings.FieldsFunc(field, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' })
	for i, p := range parts {
		if strings.EqualFold(p, old) {
			parts[i] = repl
		}
	}
	return strings.Join(parts, ",")
}

func renameRef(tx *sql.Tx, old, repl string) {
	rows, err := tx.Query(`SELECT id,src,dst FROM fw_rules`)
	if err != nil {
		return
	}
	type upd struct {
		id       int64
		src, dst string
	}
	var ups []upd
	for rows.Next() {
		var id int64
		var src, dst string
		rows.Scan(&id, &src, &dst)
		if tokenIn(src, old) || tokenIn(dst, old) {
			ups = append(ups, upd{id, swapToken(src, old, repl), swapToken(dst, old, repl)})
		}
	}
	rows.Close()
	for _, u := range ups {
		tx.Exec(`UPDATE fw_rules SET src=?,dst=? WHERE id=?`, u.src, u.dst, u.id)
	}
}

// RulesUsing names the rules that mention a reference such as "dev:aa:bb:..." or "grp:Kids".
func RulesUsing(db Queryer, ref string) []string {
	rows, err := db.Query(`SELECT name,src,dst,service FROM fw_rules ORDER BY position,id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, src, dst, svc string
		rows.Scan(&name, &src, &dst, &svc)
		if tokenIn(src, ref) || tokenIn(dst, ref) || tokenIn(svc, ref) {
			out = append(out, name)
		}
	}
	return out
}

func inUse(what string, rules []string) error {
	if len(rules) == 0 {
		return nil
	}
	return fmt.Errorf("%s is used by %s. Change or remove %s first", what, quoteList(rules), pluralRule(len(rules)))
}

func pluralRule(n int) string {
	if n == 1 {
		return "that rule"
	}
	return "those rules"
}

func quoteList(l []string) string {
	q := make([]string, len(l))
	for i, s := range l {
		q[i] = `"` + s + `"`
	}
	return strings.Join(q, ", ")
}

// DeleteDeviceGroup removes a group unless a rule still uses it.
func DeleteDeviceGroup(tx *sql.Tx, id int64) error {
	var name string
	if tx.QueryRow(`SELECT name FROM device_groups WHERE id=?`, id).Scan(&name) != nil {
		return errors.New("no such group")
	}
	if err := inUse("The group "+name, RulesUsing(tx, "grp:"+name)); err != nil {
		return err
	}
	tx.Exec(`DELETE FROM device_group_members WHERE group_id=?`, id)
	_, err := tx.Exec(`DELETE FROM device_groups WHERE id=?`, id)
	return err
}

// CheckDeviceRemovable refuses to forget a device that a rule names directly.
func CheckDeviceRemovable(db Queryer, mac string) error {
	return inUse("This device", RulesUsing(db, "dev:"+strings.ToLower(mac)))
}

// PortListRow is a named list of ports.
type PortListRow struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Ports []string `json:"ports"` // https, tcp/8080, udp/1000-2000
}

func PortLists(db Queryer) ([]PortListRow, error) {
	rows, err := db.Query(`SELECT id,name,members FROM fw_objects WHERE kind='ports' ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PortListRow{}
	for rows.Next() {
		var p PortListRow
		var raw string
		if err := rows.Scan(&p.ID, &p.Name, &raw); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(raw), &p.Ports) != nil || p.Ports == nil {
			p.Ports = []string{}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func SavePortList(tx *sql.Tx, p PortListRow) (int64, error) {
	p.Name = strings.TrimSpace(p.Name)
	if !reName.MatchString(p.Name) {
		return 0, errors.New("a list name uses letters, numbers, spaces, dots and dashes (up to 32 characters)")
	}
	if len(p.Ports) == 0 || len(p.Ports) > 100 {
		return 0, errors.New("a list needs between 1 and 100 ports")
	}
	clean := make([]string, 0, len(p.Ports))
	for _, it := range p.Ports {
		it = strings.ToLower(strings.TrimSpace(it))
		sp, err := ParseService(it)
		if err != nil || sp == nil {
			return 0, fmt.Errorf("%q is not a port. Use a name like https, or tcp/8080, or udp/1000-2000", it)
		}
		clean = append(clean, it)
	}
	raw, _ := json.Marshal(clean)
	if p.ID == 0 {
		res, err := tx.Exec(`INSERT INTO fw_objects(name,kind,members) VALUES(?,'ports',?)`, p.Name, string(raw))
		if err != nil {
			return 0, uniqueErr(err, "a list or address group")
		}
		return res.LastInsertId()
	}
	var old string
	if tx.QueryRow(`SELECT name FROM fw_objects WHERE id=? AND kind='ports'`, p.ID).Scan(&old) != nil {
		return 0, errors.New("no such list")
	}
	if _, err := tx.Exec(`UPDATE fw_objects SET name=?,members=? WHERE id=?`, p.Name, string(raw), p.ID); err != nil {
		return 0, uniqueErr(err, "a list or address group")
	}
	if old != p.Name {
		rows, _ := tx.Query(`SELECT id,service FROM fw_rules`)
		type upd struct {
			id  int64
			svc string
		}
		var ups []upd
		for rows != nil && rows.Next() {
			var id int64
			var svc string
			rows.Scan(&id, &svc)
			parts := strings.Split(svc, ",")
			changed := false
			for i, x := range parts {
				if strings.TrimSpace(x) == "list:"+old {
					parts[i], changed = "list:"+p.Name, true
				}
			}
			if changed {
				ups = append(ups, upd{id, strings.Join(parts, ",")})
			}
		}
		if rows != nil {
			rows.Close()
		}
		for _, u := range ups {
			tx.Exec(`UPDATE fw_rules SET service=? WHERE id=?`, u.svc, u.id)
		}
	}
	return p.ID, nil
}

func DeletePortList(tx *sql.Tx, id int64) error {
	var name string
	if tx.QueryRow(`SELECT name FROM fw_objects WHERE id=? AND kind='ports'`, id).Scan(&name) != nil {
		return errors.New("no such list")
	}
	var used []string
	rows, _ := tx.Query(`SELECT name,service FROM fw_rules`)
	for rows != nil && rows.Next() {
		var n, svc string
		rows.Scan(&n, &svc)
		for _, x := range strings.Split(svc, ",") {
			if strings.TrimSpace(x) == "list:"+name {
				used = append(used, n)
				break
			}
		}
	}
	if rows != nil {
		rows.Close()
	}
	if err := inUse("The list "+name, used); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM fw_objects WHERE id=?`, id)
	return err
}

// RulesUsingService names the rules whose "what for" field includes the saved list. Lists may have
// spaces in their names, so only commas separate entries here.
func RulesUsingService(db Queryer, list string) []string {
	rows, err := db.Query(`SELECT name,service FROM fw_rules ORDER BY position,id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, svc string
		rows.Scan(&name, &svc)
		for _, x := range strings.Split(svc, ",") {
			if strings.EqualFold(strings.TrimSpace(x), "list:"+list) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}
