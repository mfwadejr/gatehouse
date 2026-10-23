package dnssrv

import (
	"errors"
	"strings"
)

// Group is a set of devices that share filtering.
type Group struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Filtering   bool    `json:"filtering"`
	Description string  `json:"description"`
	Lists       []int64 `json:"lists"`
	Devices     int     `json:"devices"`
}

// Groups lists groups with their assigned blocklists.
func (m *Manager) Groups() ([]Group, error) {
	rows, err := m.db.Query(`SELECT g.id,g.name,g.filtering,g.description,(SELECT count(*) FROM devices d WHERE d.group_id=g.id) FROM groups g ORDER BY g.id`)
	if err != nil {
		return nil, err
	}
	var out []Group
	for rows.Next() {
		var g Group
		var f int
		if err := rows.Scan(&g.ID, &g.Name, &f, &g.Description, &g.Devices); err != nil {
			rows.Close()
			return nil, err
		}
		g.Filtering = f == 1
		g.Lists = []int64{}
		out = append(out, g)
	}
	rows.Close()
	for i := range out {
		lr, err := m.db.Query(`SELECT blocklist_id FROM group_blocklists WHERE group_id=? ORDER BY blocklist_id`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for lr.Next() {
			var id int64
			lr.Scan(&id)
			out[i].Lists = append(out[i].Lists, id)
		}
		lr.Close()
	}
	return out, nil
}

// SaveGroup creates (id 0) or updates a group and its list assignment.
func (m *Manager) SaveGroup(g Group) (int64, error) {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		return 0, errors.New("a name is required")
	}
	f := 0
	if g.Filtering {
		f = 1
	}
	tx, err := m.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if g.ID == 0 {
		res, err := tx.Exec(`INSERT INTO groups(name,filtering,description) VALUES(?,?,?)`, g.Name, f, g.Description)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return 0, errors.New("a group with that name already exists")
			}
			return 0, err
		}
		g.ID, _ = res.LastInsertId()
	} else if _, err := tx.Exec(`UPDATE groups SET name=?,filtering=?,description=? WHERE id=?`, g.Name, f, g.Description, g.ID); err != nil {
		return 0, err
	}
	if g.Lists != nil {
		if _, err := tx.Exec(`DELETE FROM group_blocklists WHERE group_id=?`, g.ID); err != nil {
			return 0, err
		}
		for _, l := range g.Lists {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO group_blocklists(group_id,blocklist_id) VALUES(?,?)`, g.ID, l); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return g.ID, m.Reload()
}

// DeleteGroup removes a group; its devices fall back to the default group.
func (m *Manager) DeleteGroup(id int64) error {
	var name string
	if err := m.db.QueryRow(`SELECT name FROM groups WHERE id=?`, id).Scan(&name); err != nil {
		return err
	}
	if name == "Default" {
		return errors.New("the Default group cannot be deleted")
	}
	if _, err := m.db.Exec(`DELETE FROM groups WHERE id=?`, id); err != nil {
		return err
	}
	return m.Reload()
}

// SetEnabled switches a blocklist on or off.
func (m *Manager) SetEnabled(id int64, on bool) error {
	n := 0
	if on {
		n = 1
	}
	if _, err := m.db.Exec(`UPDATE blocklists SET enabled=? WHERE id=?`, n, id); err != nil {
		return err
	}
	return m.Reload()
}
