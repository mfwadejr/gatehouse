package app

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"gatehouse/internal/api"
)

// sqlq is what both the database and a transaction can do; the removal helpers use either.
type sqlq interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type removalScope struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Subnet string `json:"subnet"`
	Range  string `json:"range"`
	Iface  string `json:"iface"`
}

// removalImpact says what taking a network away also takes with it, so the person is told before
// confirming and nothing is left behind afterwards.
type removalImpact struct {
	Name       string         `json:"name"`
	Iface      string         `json:"iface,omitempty"`
	Zone       string         `json:"zone,omitempty"`
	ZoneID     int64          `json:"zone_id,omitempty"`
	RemoveZone bool           `json:"remove_zone"`
	Management bool           `json:"management"`
	Rules      int            `json:"rules"`
	Scopes     []removalScope `json:"scopes"`
	OtherPorts []string       `json:"other_ports,omitempty"`
	Kept       string         `json:"kept,omitempty"` // why the firewall network stays, in plain words
}

func scopesWhere(q sqlq, where string, args ...any) []removalScope {
	rows, err := q.Query(`SELECT id,name,iface,subnet,range_start,range_end FROM dhcp_scopes WHERE `+where, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []removalScope
	for rows.Next() {
		var s removalScope
		var a, b string
		rows.Scan(&s.ID, &s.Name, &s.Iface, &s.Subnet, &a, &b)
		if a != "" {
			s.Range = a + " to " + b
		}
		out = append(out, s)
	}
	return out
}

func zoneRules(q sqlq, name string) int {
	var n int
	q.QueryRow(`SELECT count(*) FROM fw_rules WHERE from_zone=? OR to_zone=?`, name, name).Scan(&n)
	return n
}

// ifaceImpact is what removing one port's network (its address, DHCP range and, when it was the
// last port, its firewall network) takes with it.
func ifaceImpact(q sqlq, id int64) (*removalImpact, error) {
	var name string
	var zid sql.NullInt64
	if err := q.QueryRow(`SELECT name,zone_id FROM interfaces WHERE id=?`, id).Scan(&name, &zid); err != nil {
		return nil, fmt.Errorf("that network was not found")
	}
	imp := &removalImpact{Name: name, Iface: name, Scopes: scopesWhere(q, `iface=?`, name)}
	if !zid.Valid {
		return imp, nil
	}
	var zname, kind string
	var mgmt int
	if err := q.QueryRow(`SELECT name,kind,management FROM zones WHERE id=?`, zid.Int64).Scan(&zname, &kind, &mgmt); err != nil {
		return imp, nil
	}
	imp.Zone, imp.ZoneID, imp.Management = zname, zid.Int64, mgmt == 1
	imp.Name = zname
	rows, err := q.Query(`SELECT name FROM interfaces WHERE zone_id=? AND id<>?`, zid.Int64, id)
	if err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			imp.OtherPorts = append(imp.OtherPorts, n)
		}
		rows.Close()
	}
	switch {
	case len(imp.OtherPorts) > 0:
		imp.Kept = fmt.Sprintf("%s stays, because %s is still part of it.", zname, strings.Join(imp.OtherPorts, ", "))
	case mgmt == 1:
		imp.Kept = fmt.Sprintf("%s can manage Gatehouse, so its firewall network stays until you have another one that can. Remove it from Firewall > Networks when you do.", zname)
	default:
		imp.RemoveZone = true
		imp.Rules = zoneRules(q, zname)
	}
	return imp, nil
}

// zoneImpact is what removing a firewall network takes with it. The ports stay (with their
// addresses) and show as not attached to anything, except that their DHCP ranges go.
func zoneImpact(q sqlq, id int64) (*removalImpact, error) {
	var name string
	var mgmt int
	if err := q.QueryRow(`SELECT name,management FROM zones WHERE id=?`, id).Scan(&name, &mgmt); err != nil {
		return nil, fmt.Errorf("that network was not found")
	}
	imp := &removalImpact{Name: name, Zone: name, ZoneID: id, RemoveZone: true, Management: mgmt == 1, Rules: zoneRules(q, name)}
	rows, err := q.Query(`SELECT name FROM interfaces WHERE zone_id=? ORDER BY id`, id)
	if err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			imp.OtherPorts = append(imp.OtherPorts, n)
		}
		rows.Close()
	}
	if mgmt == 1 {
		// Its DHCP range keeps handing out addresses so a laptop on it is not cut off.
		return imp, nil
	}
	for _, p := range imp.OtherPorts {
		imp.Scopes = append(imp.Scopes, scopesWhere(q, `iface=?`, p)...)
	}
	// A range left behind by an earlier removal: same name, port gone.
	imp.Scopes = append(imp.Scopes, scopesWhere(q, `name=? AND iface NOT IN (SELECT name FROM interfaces)`, name)...)
	return imp, nil
}

func (a *App) networkCleanupRoutes(s *api.Server) {
	s.Handle("GET /api/net/interfaces/{id}/removal", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		imp, err := ifaceImpact(a.Config, id)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, imp)
	})
	s.Handle("GET /api/fw/zones/{id}/removal", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		imp, err := zoneImpact(a.Config, id)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, imp)
	})
}

func deleteScopes(tx *sql.Tx, scopes []removalScope) {
	for _, s := range scopes {
		tx.Exec(`DELETE FROM dhcp_scopes WHERE id=?`, s.ID)
	}
}
