package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"gatehouse/internal/api"
)

// Add more ports to a plain network (H42). A network on one port is changed, in place, into a
// network on several ports that share its addresses, like extra ports on a switch. Its address,
// address service (DHCP), firewall network and any VLANs on it are kept; only the name of the
// thing carrying them changes. Nothing is live until the person applies, and the usual 60-second
// rollback covers it.

var reIfName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)

// sharedName picks a name for the shared-port group that nothing else is using.
func sharedName(tx *sql.Tx) string {
	used := map[string]bool{}
	if rows, err := tx.Query(`SELECT name FROM interfaces`); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			used[n] = true
		}
		rows.Close()
	}
	for _, p := range portsLister() {
		if n, ok := p["name"].(string); ok {
			used[n] = true
		}
	}
	for i := 0; ; i++ {
		if n := fmt.Sprintf("br%d", i); !used[n] {
			return n
		}
	}
}

func (a *App) addPortsToNetwork(id int64, add []string) (string, error) {
	if len(add) == 0 {
		return "", errors.New("choose at least one more port")
	}
	have := map[string]bool{}
	for _, p := range portsLister() {
		if n, ok := p["name"].(string); ok {
			if w, _ := p["wifi"].(bool); !w {
				have[n] = true
			}
		}
	}
	for _, p := range add {
		if !reIfName.MatchString(p) || !have[p] {
			return "", fmt.Errorf("%q is not a wired port on this machine", p)
		}
	}
	var name string
	err := a.mutateNet(func(tx *sql.Tx) error {
		var kind string
		if tx.QueryRow(`SELECT name,kind FROM interfaces WHERE id=?`, id).Scan(&name, &kind) != nil {
			return errors.New("that network was not found")
		}
		if kind != "phys" {
			return errors.New("only a network on a single port can be changed this way")
		}
		members := append([]string{name}, add...)
		if err := bridgePortsFree(tx, id, members); err != nil {
			return err
		}
		br := sharedName(tx)
		if _, err := tx.Exec(`UPDATE interfaces SET name=?,kind='bridge',members=? WHERE id=?`, br, strings.Join(members, ","), id); err != nil {
			return err
		}
		// Everything that pointed at the old port now points at the shared group.
		if _, err := tx.Exec(`UPDATE dhcp_scopes SET iface=? WHERE iface=?`, br, name); err != nil {
			return err
		}
		// VLAN networks on the old port move onto the shared group, with names that follow it.
		type vl struct {
			id   int64
			name string
			vid  int
		}
		var vlans []vl
		if rows, err := tx.Query(`SELECT id,name,vlan_id FROM interfaces WHERE kind='vlan' AND parent=?`, name); err == nil {
			for rows.Next() {
				var v vl
				rows.Scan(&v.id, &v.name, &v.vid)
				vlans = append(vlans, v)
			}
			rows.Close()
		}
		for _, v := range vlans {
			nn := fmt.Sprintf("%s.%d", br, v.vid)
			if _, err := tx.Exec(`UPDATE interfaces SET name=?,parent=? WHERE id=?`, nn, br, v.id); err != nil {
				return err
			}
			tx.Exec(`UPDATE dhcp_scopes SET iface=? WHERE iface=?`, nn, v.name)
			tx.Exec(`UPDATE devices SET override_iface=? WHERE override_iface=?`, nn, v.name)
			tx.Exec(`UPDATE settings SET value=? WHERE key='approval.holding' AND value=?`, nn, v.name)
		}
		tx.Exec(`UPDATE devices SET override_home=? WHERE override_home=?`, br, name)
		name = br
		return nil
	})
	if err != nil {
		return "", err
	}
	a.DHCP.Reload()
	a.publishLeases()
	a.publishNet()
	return name, nil
}

func (a *App) registerBridgeifyRoutes() {
	a.API.HandleAdmin("POST /api/networks/add-ports", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			ID    int64    `json:"id"`
			Ports []string `json:"ports"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		name, err := a.addPortsToNetwork(in.ID, in.Ports)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]string{"name": name})
	})
}
