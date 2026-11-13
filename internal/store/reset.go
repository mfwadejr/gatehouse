package store

import (
	"fmt"
	"strings"
)

// Reset levels.
const (
	ResetNetwork  = "network"  // networks, firewall, DHCP, DNS, VPN and Wi-Fi go back to a clean state
	ResetFactory  = "factory"  // everything goes, as if freshly installed
	ResetDefaults = "defaults" // preferences go back to the install values; networks, accounts and data stay
)

// Rows are removed child tables first. Most children also cascade, but listing them keeps the
// result the same whether or not foreign keys are on.
var networkTables = []string{
	"fw_hits", "dns_exceptions", "dns_zone_targets", "fw_rules", "fw_objects", "nat_forwards", "upnp_allow",
	"routes", "vpn_routes", "vpn_ovpn_users", "vpn_ovpn", "vpn_wg_peers", "vpn_wg",
	"wan_group_routes", "wan_group_members", "wan_groups", "wan_links",
	"dhcp_leases", "dhcp_reservations", "dhcp_scopes", "interfaces", "zones",
	"dns_records", "custom_rules", "scope_rules", "block_sets", "threat_allowed", "threat_trusted", "dns_upstreams", "group_blocklists", "blocklists", "groups",
}

// A factory reset also removes the accounts, the device list and the firewall history.
var factoryExtra = []string{"recovery_codes", "sessions", "users", "device_group_members", "device_groups", "devices", "device_icons", "fw_revisions"}

// networkKeep are the settings a network reset leaves alone: how people sign in, the time zone,
// the update settings, and the way back into the console.
var networkKeep = []string{"update.", "console.port", "auth.", "session_", "system.timezone", "fw.stepup", "mgmt.rescue_iface", "mgmt.port"}

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

// preferenceKeys are the settings "Restore default settings" puts back: how backups, alerts, log
// keeping, updates and speed tests run, and how the console shows things. Networks, firewall, DNS,
// DHCP, VPNs, accounts, sign-in, the console port and outside connections (syslog, NetFlow, SNMP) stay.
var preferenceKeys = []string{
	"alerts.config", "alerts.new_since", "backup.config", "logs.long_days", "logs.max_mb", "logs.retention_days",
	"logs.admin_days", "logs.include_history", "dns.log_retention_days", "fw.revisions_keep", "fw.log_allowed", "fw.log_drops",
	"update.check_hours", "speed.on", "speedtest.config",
}

// IsPreference says whether a setting is one of the preferences "Restore default settings" resets.
func IsPreference(key string) bool {
	for _, k := range preferenceKeys {
		if key == k {
			return true
		}
	}
	if strings.HasPrefix(key, "prefs.") { // each person's table columns, hidden cards and the like
		return true
	}
	return strings.HasPrefix(key, "sched.") && !strings.HasSuffix(key, ".last") // when each job runs, not when it last ran
}

// ResetPreferences puts the preferences back to their install values by removing what was saved, so
// every part falls back to its own default. Nothing else is touched.
func ResetPreferences(db *DB) (int, error) {
	rows, err := db.Query(`SELECT key FROM settings`)
	if err != nil {
		return 0, err
	}
	var drop []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		if IsPreference(k) {
			drop = append(drop, k)
		}
	}
	rows.Close()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, k := range drop {
		if _, err := tx.Exec(`DELETE FROM settings WHERE key=?`, k); err != nil {
			return 0, err
		}
	}
	return len(drop), tx.Commit()
}

// ResetLogs empties the log database.
func ResetLogs(db *DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"dns_queries", "fw_events", "events", "metrics", "traffic", "speedtests"} {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return err
		}
	}
	return tx.Commit()
}
