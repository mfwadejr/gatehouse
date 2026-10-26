package fw

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// Forced DNS in more detail than one switch: each network can send its lookups to this router or to
// a server of its own, and single devices or groups can be left alone, forced to this router, or
// sent to a server, whatever their network says.

const (
	DNSOff       = "off"
	DNSGatehouse = "gatehouse"
	DNSServer    = "server"
)

// CheckDNSServer says whether text is usable as the server forced DNS is sent to.
func CheckDNSServer(t string) error {
	ip := net.ParseIP(strings.TrimSpace(t))
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("the DNS server must be an IPv4 address, not %q", t)
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("%s cannot be used as the DNS server", t)
	}
	return nil
}

// DNSExceptions lists the stored device and group exceptions.
func DNSExceptions(db Queryer) []DNSException {
	rows, err := db.Query(`SELECT id,who,mode,target FROM dns_exceptions ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []DNSException
	for rows.Next() {
		var e DNSException
		rows.Scan(&e.ID, &e.Who, &e.Mode, &e.Target)
		out = append(out, e)
	}
	return out
}

// SaveDNSException adds or changes the treatment of one device or group.
func SaveDNSException(tx *sql.Tx, e DNSException) (int64, error) {
	e.Who = strings.TrimSpace(e.Who)
	switch {
	case strings.HasPrefix(e.Who, "dev:"):
		e.Who = "dev:" + strings.ToLower(strings.TrimPrefix(e.Who, "dev:"))
		if !reMAC.MatchString(strings.TrimPrefix(e.Who, "dev:")) {
			return 0, errors.New("pick a device or a device group")
		}
	case strings.HasPrefix(e.Who, "grp:") && len(e.Who) > 4:
	default:
		return 0, errors.New("pick a device or a device group")
	}
	switch e.Mode {
	case DNSOff, DNSGatehouse:
		e.Target = ""
	case DNSServer:
		e.Target = strings.TrimSpace(e.Target)
		if err := CheckDNSServer(e.Target); err != nil {
			return 0, err
		}
	default:
		return 0, errors.New("choose leave alone, this router, or another server")
	}
	if _, err := tx.Exec(`INSERT INTO dns_exceptions(who,mode,target) VALUES(?,?,?)
		ON CONFLICT(who) DO UPDATE SET mode=excluded.mode,target=excluded.target`, e.Who, e.Mode, e.Target); err != nil {
		return 0, err
	}
	var id int64
	tx.QueryRow(`SELECT id FROM dns_exceptions WHERE who=?`, e.Who).Scan(&id)
	return id, nil
}

// DeleteDNSException removes one exception.
func DeleteDNSException(tx *sql.Tx, id int64) error {
	res, err := tx.Exec(`DELETE FROM dns_exceptions WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("no such exception")
	}
	return nil
}

// SetZoneDNS sets how one network is treated: forced (to the router-wide choice, or to its own
// server when target is set) or left alone.
func SetZoneDNS(tx *sql.Tx, zoneID int64, force bool, target string) error {
	target = strings.TrimSpace(target)
	if force && target != "" {
		if err := CheckDNSServer(target); err != nil {
			return err
		}
	}
	if !force {
		target = ""
	}
	res, err := tx.Exec(`UPDATE zones SET dns_force=? WHERE id=?`, b2i(force), zoneID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("no such network")
	}
	if target == "" {
		_, err = tx.Exec(`DELETE FROM dns_zone_targets WHERE zone_id=?`, zoneID)
		return err
	}
	_, err = tx.Exec(`INSERT INTO dns_zone_targets(zone_id,target) VALUES(?,?) ON CONFLICT(zone_id) DO UPDATE SET target=excluded.target`, zoneID, target)
	return err
}

// ZoneDNSTargets maps network id to its own server, for the console.
func ZoneDNSTargets(db Queryer) map[int64]string {
	out := map[int64]string{}
	rows, err := db.Query(`SELECT zone_id,target FROM dns_zone_targets`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var t string
		rows.Scan(&id, &t)
		out[id] = t
	}
	return out
}

// macsOf resolves a device or group exception to device addresses. A device or group that no
// longer exists gives none, so the exception does nothing until it is removed.
func (m *Model) macsOf(who string) []string {
	switch {
	case strings.HasPrefix(who, "dev:"):
		mac := strings.TrimPrefix(who, "dev:")
		if m.Devices[mac] {
			return []string{mac}
		}
	case strings.HasPrefix(who, "grp:"):
		if g, ok := m.DevGroups[strings.TrimPrefix(who, "grp:")]; ok {
			out := append([]string(nil), g.MACs...)
			sort.Strings(out)
			return out
		}
	}
	return nil
}

// dnsPlan says which interfaces have their lookups sent where.
type dnsPlan struct {
	local   []string            // sent to this router
	remote  map[string][]string // server address -> interfaces
	servers []string            // the keys of remote, sorted
}

func (p dnsPlan) forced() []string {
	out := append([]string(nil), p.local...)
	for _, s := range p.servers {
		out = append(out, p.remote[s]...)
	}
	return out
}

// exceptionMACs groups the exception devices by treatment.
func (m *Model) exceptionMACs() (off, gate []string, server map[string][]string, serverKeys []string) {
	server = map[string][]string{}
	for _, e := range m.DNSExceptions {
		macs := m.macsOf(e.Who)
		if len(macs) == 0 {
			continue
		}
		switch e.Mode {
		case DNSOff:
			off = append(off, macs...)
		case DNSGatehouse:
			gate = append(gate, macs...)
		case DNSServer:
			if CheckDNSServer(e.Target) == nil {
				server[e.Target] = append(server[e.Target], macs...)
			}
		}
	}
	sort.Strings(off)
	off = uniq(off)
	sort.Strings(gate)
	gate = uniq(gate)
	for k := range server {
		sort.Strings(server[k])
		server[k] = uniq(server[k])
		serverKeys = append(serverKeys, k)
	}
	sort.Strings(serverKeys)
	return
}
