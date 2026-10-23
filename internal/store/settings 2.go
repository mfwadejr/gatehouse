package store

import "database/sql"

// Setting returns a stored setting or the default when it is not set.
func (d *DB) Setting(key, def string) string {
	var v string
	err := d.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows || err != nil {
		return def
	}
	return v
}

// SetSetting stores a setting.
func (d *DB) SetSetting(key, value string) error {
	_, err := d.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
