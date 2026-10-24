package fw

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"gatehouse/internal/store"
)

// Mutate runs fn in a transaction and only commits if the resulting configuration is valid.
// This is what makes it impossible to save a configuration that would lock you out.
func Mutate(db *store.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	m, err := Load(tx)
	if err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	return tx.Commit()
}

// ZoneRow is a network as the console edits it.
type ZoneRow struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Ifaces      []string `json:"ifaces"`
	Internet    bool     `json:"internet"`
	Management  bool     `json:"management"`
	UPnP        bool     `json:"upnp"`
	IntraAllow  bool     `json:"intra_allow"`
	Gateway     bool     `json:"gateway"`
	SSH         bool     `json:"ssh"`
	DNSForce    bool     `json:"dns_force"`
	Description string   `json:"description"`
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Zones lists the stored (non-WAN) networks.
func Zones(db Queryer) ([]ZoneRow, error) {
	rows, err := db.Query(`SELECT id,name,kind,gateway_mode,intra_allow,management,upnp,internet,ssh,dns_force,description FROM zones ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []ZoneRow
	for rows.Next() {
		var z ZoneRow
		var gw string
		var intra, mgmt, upnp, inet, ssh, dnsf int
		if err := rows.Scan(&z.ID, &z.Name, &z.Kind, &gw, &intra, &mgmt, &upnp, &inet, &ssh, &dnsf, &z.Description); err != nil {
			rows.Close()
			return nil, err
		}
		z.Gateway, z.IntraAllow, z.Management, z.UPnP, z.Internet, z.SSH = gw == "gateway", intra == 1, mgmt == 1, upnp == 1, inet == 1, ssh == 1
		z.DNSForce = dnsf == 1
		z.Ifaces = []string{}
		out = append(out, z)
	}
	rows.Close()
	for i := range out {
		ir, err := db.Query(`SELECT name FROM interfaces WHERE zone_id=? ORDER BY id`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for ir.Next() {
			var n string
			ir.Scan(&n)
			out[i].Ifaces = append(out[i].Ifaces, n)
		}
		ir.Close()
	}
	return out, nil
}

// SaveZone creates (ID 0) or updates a network and its port assignment.
func SaveZone(tx *sql.Tx, z ZoneRow) (int64, error) {
	z.Name = strings.TrimSpace(z.Name)
	if z.Kind == KindWan {
		return 0, errors.New("the internet connection is managed on the Internet page")
	}
	gw := "host"
	if z.Gateway {
		gw = "gateway"
	}
	if z.ID == 0 {
		res, err := tx.Exec(`INSERT INTO zones(name,kind,gateway_mode,intra_allow,management,upnp,internet,ssh,dns_force,description) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			z.Name, z.Kind, gw, b2i(z.IntraAllow), b2i(z.Management), b2i(z.UPnP), b2i(z.Internet), b2i(z.SSH), b2i(z.DNSForce), z.Description)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return 0, errors.New("a network with that name already exists")
			}
			return 0, err
		}
		z.ID, _ = res.LastInsertId()
	} else {
		var old string
		tx.QueryRow(`SELECT name FROM zones WHERE id=?`, z.ID).Scan(&old)
		if _, err := tx.Exec(`UPDATE zones SET name=?,kind=?,gateway_mode=?,intra_allow=?,management=?,upnp=?,internet=?,ssh=?,dns_force=?,description=? WHERE id=?`,
			z.Name, z.Kind, gw, b2i(z.IntraAllow), b2i(z.Management), b2i(z.UPnP), b2i(z.Internet), b2i(z.SSH), b2i(z.DNSForce), z.Description, z.ID); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return 0, errors.New("a network with that name already exists")
			}
			return 0, err
		}
		if old != "" && old != z.Name { // keep rules pointing at the renamed network
			tx.Exec(`UPDATE fw_rules SET from_zone=? WHERE from_zone=?`, z.Name, old)
			tx.Exec(`UPDATE fw_rules SET to_zone=? WHERE to_zone=?`, z.Name, old)
		}
	}
	if _, err := tx.Exec(`UPDATE interfaces SET zone_id=NULL WHERE zone_id=?`, z.ID); err != nil {
		return 0, err
	}
	for _, n := range z.Ifaces {
		if !reIface.MatchString(n) {
			return 0, errors.New("interface name " + n + " is not valid")
		}
		if _, err := tx.Exec(`INSERT INTO interfaces(name,zone_id) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET zone_id=excluded.zone_id`, n, z.ID); err != nil {
			return 0, err
		}
	}
	return z.ID, nil
}

// DeleteZone removes a network and every rule that mentions it.
func DeleteZone(tx *sql.Tx, id int64) error {
	var name string
	if err := tx.QueryRow(`SELECT name FROM zones WHERE id=?`, id).Scan(&name); err != nil {
		return errors.New("no such network")
	}
	tx.Exec(`DELETE FROM fw_rules WHERE from_zone=? OR to_zone=?`, name, name)
	tx.Exec(`UPDATE interfaces SET zone_id=NULL WHERE zone_id=?`, id)
	_, err := tx.Exec(`DELETE FROM zones WHERE id=?`, id)
	return err
}

// RuleRow is an exception rule as the console edits it.
type RuleRow struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Src     string    `json:"src"`
	Dst     string    `json:"dst"`
	Service string    `json:"service"`
	Action  string    `json:"action"`
	Limit   int       `json:"limit"`
	Log     bool      `json:"log"`
	Enabled bool      `json:"enabled"`
	IPVer   string    `json:"ipver"`
	CtState string    `json:"ctstate"`
	Sched   *Schedule `json:"sched"`
	Descr   string    `json:"descr"`
	AlsoMAC bool      `json:"also_mac"`
}

func Rules(db Queryer) ([]RuleRow, error) {
	rows, err := db.Query(`SELECT id,name,from_zone,to_zone,src,dst,service,action,limit_rate,log,enabled,ipver,ctstate,sched,descr,also_mac FROM fw_rules ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuleRow{}
	for rows.Next() {
		var r RuleRow
		var lg, en, am int
		var sched string
		if err := rows.Scan(&r.ID, &r.Name, &r.From, &r.To, &r.Src, &r.Dst, &r.Service, &r.Action, &r.Limit, &lg, &en, &r.IPVer, &r.CtState, &sched, &r.Descr, &am); err != nil {
			return nil, err
		}
		r.Log, r.Enabled, r.AlsoMAC = lg == 1, en == 1, am == 1
		r.Sched, _ = ParseSchedule(sched)
		out = append(out, r)
	}
	return out, rows.Err()
}

func SaveRule(tx *sql.Tx, r RuleRow) (int64, error) {
	r.Name = strings.TrimSpace(r.Name)
	r.Descr = strings.TrimSpace(r.Descr)
	if r.Src == "" {
		r.Src = "any"
	}
	if r.Dst == "" {
		r.Dst = "any"
	}
	if r.Service == "" {
		r.Service = "any"
	}
	sched := ""
	if r.Sched != nil && r.Sched.Mode != "" && r.Sched.Mode != "always" {
		b, _ := json.Marshal(r.Sched)
		sched = string(b)
	}
	if r.ID == 0 {
		var pos int
		tx.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM fw_rules`).Scan(&pos)
		res, err := tx.Exec(`INSERT INTO fw_rules(from_zone,to_zone,position,name,src,dst,service,action,limit_rate,log,enabled,ipver,ctstate,sched,descr,also_mac) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.From, r.To, pos, r.Name, r.Src, r.Dst, r.Service, r.Action, r.Limit, b2i(r.Log), b2i(r.Enabled), r.IPVer, r.CtState, sched, r.Descr, b2i(r.AlsoMAC))
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return 0, errors.New("a rule with that name already exists between those networks")
			}
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := tx.Exec(`UPDATE fw_rules SET from_zone=?,to_zone=?,name=?,src=?,dst=?,service=?,action=?,limit_rate=?,log=?,enabled=?,ipver=?,ctstate=?,sched=?,descr=?,also_mac=? WHERE id=?`,
		r.From, r.To, r.Name, r.Src, r.Dst, r.Service, r.Action, r.Limit, b2i(r.Log), b2i(r.Enabled), r.IPVer, r.CtState, sched, r.Descr, b2i(r.AlsoMAC), r.ID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return 0, errors.New("a rule with that name already exists between those networks")
	}
	return r.ID, err
}

func DeleteRule(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(`DELETE FROM fw_rules WHERE id=?`, id)
	return err
}

// MoveRule reorders by assigning positions from the given id order.
func MoveRule(tx *sql.Tx, order []int64) error {
	for i, id := range order {
		if _, err := tx.Exec(`UPDATE fw_rules SET position=? WHERE id=?`, i+1, id); err != nil {
			return err
		}
	}
	return nil
}

// ForwardRow is a port forward as the console edits it.
type ForwardRow struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	WAN     string `json:"wan"`
	Proto   string `json:"proto"`
	ExtPort string `json:"ext_port"`
	IntAddr string `json:"int_addr"`
	IntPort string `json:"int_port"`
	Enabled bool   `json:"enabled"`
}

func Forwards(db Queryer) ([]ForwardRow, error) {
	rows, err := db.Query(`SELECT id,name,wan,proto,ext_port,int_addr,int_port,enabled FROM nat_forwards ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ForwardRow{}
	for rows.Next() {
		var f ForwardRow
		var en int
		if err := rows.Scan(&f.ID, &f.Name, &f.WAN, &f.Proto, &f.ExtPort, &f.IntAddr, &f.IntPort, &en); err != nil {
			return nil, err
		}
		f.Enabled = en == 1
		out = append(out, f)
	}
	return out, rows.Err()
}

func SaveForward(tx *sql.Tx, f ForwardRow) (int64, error) {
	f.Name = strings.TrimSpace(f.Name)
	if f.ID == 0 {
		res, err := tx.Exec(`INSERT INTO nat_forwards(name,wan,proto,ext_port,int_addr,int_port,enabled) VALUES(?,?,?,?,?,?,?)`,
			f.Name, f.WAN, f.Proto, f.ExtPort, f.IntAddr, f.IntPort, b2i(f.Enabled))
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return 0, errors.New("a port forward with that name already exists")
			}
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := tx.Exec(`UPDATE nat_forwards SET name=?,wan=?,proto=?,ext_port=?,int_addr=?,int_port=?,enabled=? WHERE id=?`,
		f.Name, f.WAN, f.Proto, f.ExtPort, f.IntAddr, f.IntPort, b2i(f.Enabled), f.ID)
	return f.ID, err
}

func DeleteForward(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(`DELETE FROM nat_forwards WHERE id=?`, id)
	return err
}
