package store

import (
	"fmt"
	"strings"
)

// Reset levels.
const (
	ResetNetwork = "network" // networks, firewall, DHCP, DNS, VPN and Wi-Fi go back to a clean state
	ResetFactory = "factory" // everything goes, as if freshly installed
)

// Rows are removed child tables first. Most children also cascade, but listing them keeps the
// result the same whether or not foreign keys are on.
var networkTables = []string{
	"fw_hits", "fw_rules", "fw_objects", "nat_forwards", "upnp_allow",
	"vpn_routes", "vpn_ovpn_users", "vpn_ovpn", "vpn_wg_peers", "vpn_wg",
	"wan_group_routes", "wan_group_members", "wan_groups", "wan_links", "wifi_radios",
	"dhcp_leases", "dhcp_reservations", "dhcp_scopes", "interfaces", "zones",
	"dns_records", "custom_rules", "dns_upstreams", "group_blocklists", "blocklists", "groups",
}

// A factory reset also removes the accounts, the device list and the firewall history.
var factoryExtra = []string{"recovery_codes", "sessions", "users", "device_group_members", "device_groups", "devices", "device_icons", "fw_revisions"}

// networkKeep are the settings a network reset leaves alone: how people sign in, the time zone,
// the update settings, and the way back into the console.
var networkKeep = []string{"update.", "console.port", "auth.", "session_", "system.timezone", "fw.stepup", "mgmt.rescue_iface"}

// factoryKeep is all a factory reset leaves: where releases come from. The installed version and
// the key updates are checked against are part of the program, not the settings.
var factoryKeep = []string{"update.repo"}

func keeps(key string, keep []string) bool {
	for _, k := range keep {
		if key == k || strings.HasSuffix(k, ".") && strings.HasPrefix(key, k) || strings.HasSuffix(k, "_") && strings.HasPrefix(key, k) {
			return true
		}
	}
	return false
}

// ResetConfig clears the settings database to the given level in one transaction. It does not
// apply anything; the caller applies the empty configuration afterwards.
func ResetConfig(db *DB, level string) error {
	var tables []string
	var keep []string
	switch level {
	case ResetNetwork:
		tables, keep = networkTables, networkKeep
	case ResetFactory:
		tables, keep = append(append([]string{}, networkTables...), factoryExtra...), factoryKeep
	default:
		return fmt.Errorf("unknown reset level %q", level)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tables {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return fmt.Errorf("clearing %s: %w", t, err)
		}
	}
	rows, err := tx.Query(`SELECT key FROM settings`)
	if err != nil {
		return err
	}
	var drop []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		if !keeps(k, keep) {
			drop = append(drop, k)
		}
	}
	rows.Close()
	for _, k := range drop {
		if _, err := tx.Exec(`DELETE FROM settings WHERE key=?`, k); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ResetLogs empties the log database.
func ResetLogs(db *DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"dns_queries", "dns_daily", "fw_events", "events", "metrics", "traffic", "speedtests"} {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return err
		}
	}
	return tx.Commit()
}
