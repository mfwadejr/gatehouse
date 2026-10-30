package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/crypt"
	"gatehouse/internal/fw"
	"gatehouse/internal/store"
	"gatehouse/internal/update"
)

// Backups. A backup is one file holding the settings, the key that unlocks the saved passwords in
// them, and a note of what is inside with a checksum of each part. Every backup is checked the
// moment it is made, and can be checked again by trying a restore without changing anything. A
// restore saves a fresh backup of the current settings first, so it can be undone.

const (
	bkExt      = ".ghbk"
	bkEncMagic = "GHBKENC1"
)

var reBackupName = regexp.MustCompile(`^[a-z]+(-[a-z]+)*-\d{8}-\d{6}(\.ghbk|\.db)$`)

type bkManifest struct {
	Format  int               `json:"format"`
	Created int64             `json:"created"`
	Version string            `json:"version"`
	Schema  int               `json:"schema"`
	Kind    string            `json:"kind"`
	Sums    map[string]string `json:"sums"`
}

// BackupCheck is one thing looked at when a backup is checked.
type BackupCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// BackupContents counts what a set of settings holds, so a backup can be compared with now.
type BackupContents struct {
	Networks     int `json:"networks"`
	Zones        int `json:"zones"`
	Rules        int `json:"rules"`
	Scopes       int `json:"scopes"`
	Reservations int `json:"reservations"`
	VPN          int `json:"vpn_devices"`
	Wifi         int `json:"wifi"`
	Devices      int `json:"devices"`
	Users        int `json:"users"`
}

// BackupReport is the outcome of checking a backup.
type BackupReport struct {
	At       int64          `json:"at"`
	OK       bool           `json:"ok"`
	Checks   []BackupCheck  `json:"checks"`
	Contents BackupContents `json:"contents"`
	Layout   int            `json:"layout"`
	Key      bool           `json:"has_key"`
}

type bkMeta struct {
	Report      *BackupReport `json:"report,omitempty"`
	HandChecked int64         `json:"hand_checked,omitempty"`
	HandBy      string        `json:"hand_by,omitempty"`
}

// BackupConfig says when backups are made on their own and how many are kept.
type BackupConfig struct {
	Schedule string `json:"schedule"` // off, daily or weekly
	Hour     int    `json:"hour"`
	Keep     int    `json:"keep"`
}

func (a *App) backupConfig() BackupConfig {
	c := BackupConfig{Schedule: "daily", Hour: 2, Keep: 7}
	if raw := a.setting("backup.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	if c.Schedule != "off" && c.Schedule != "daily" && c.Schedule != "weekly" {
		c.Schedule = "daily"
	}
	if c.Hour < 0 || c.Hour > 23 {
		c.Hour = 2
	}
	if c.Keep < 1 {
		c.Keep = 7
	}
	if c.Keep > 60 {
		c.Keep = 60
	}
	return c
}

func (a *App) keyPath() string { return filepath.Join(a.Cfg.DataDir, "secret.key") }

// ---- making a backup ----

// saveBackup makes a complete backup and returns its path. prefix says why: manual, auto,
// before-reset, before-restore.
func (a *App) saveBackup(prefix string) (string, error) {
	dir := a.backupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dir, ".work-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	dbf := filepath.Join(tmp, "config.db")
	if _, err := a.Config.Exec(`VACUUM INTO ?`, dbf); err != nil {
		return "", fmt.Errorf("could not save a copy of the settings: %w", err)
	}
	key, _ := os.ReadFile(a.keyPath())
	dbb, err := os.ReadFile(dbf)
	if err != nil {
		return "", err
	}
	sum := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	man := bkManifest{Format: 1, Created: time.Now().Unix(), Version: a.Cfg.Version, Schema: store.ConfigSchemaVersion(), Kind: prefix,
		Sums: map[string]string{"config.db": sum(dbb), "secret.key": sum(key)}}
	mb, _ := json.Marshal(man)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		n string
		b []byte
	}{{"manifest.json", mb}, {"config.db", dbb}, {"secret.key", key}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.n, Mode: 0o600, Size: int64(len(f.b)), ModTime: time.Now()}); err != nil {
			return "", err
		}
		if _, err := tw.Write(f.b); err != nil {
			return "", err
		}
	}
	tw.Close()
	gz.Close()
	name := filepath.Join(dir, fmt.Sprintf("%s-%s%s", prefix, time.Now().Format("20060102-150405"), bkExt))
	if err := os.WriteFile(name+".part", buf.Bytes(), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(name+".part", name); err != nil {
		return "", err
	}
	// Check it straight away, so a bad backup is found now and not on the day it is needed.
	if rep, err := a.verifyBackup(name, ""); err == nil {
		a.writeMeta(name, bkMeta{Report: rep})
	}
	a.pruneBackups(prefix)
	return name, nil
}

// pruneBackups keeps the number the person chose of the backups made by hand or on a schedule, and
// the last few of each safety copy. The newest backup that was checked by hand is never removed.
func (a *App) pruneBackups(prefix string) {
	dir := a.backupDir()
	files, _ := filepath.Glob(filepath.Join(dir, "*"+bkExt))
	var mine, safety = map[string][]string{}, map[string][]string{}
	for _, f := range files {
		k := strings.SplitN(filepath.Base(f), "-2", 2)[0]
		if k == "auto" || k == "manual" || k == "upload" {
			mine["all"] = append(mine["all"], f)
		} else {
			safety[k] = append(safety[k], f)
		}
	}
	sort.Strings(mine["all"])
	keep := a.backupConfig().Keep
	protect := ""
	for i := len(mine["all"]) - 1; i >= 0; i-- {
		if a.readMeta(mine["all"][i]).HandChecked > 0 {
			protect = mine["all"][i]
			break
		}
	}
	for len(mine["all"]) > keep {
		f := mine["all"][0]
		mine["all"] = mine["all"][1:]
		if f == protect {
			continue
		}
		os.Remove(f)
		os.Remove(f + ".json")
	}
	for k, l := range safety {
		sort.Strings(l)
		limit := 5
		if k == restorePointKind {
			limit = restorePointKeep
		}
		for len(l) > limit {
			os.Remove(l[0])
			os.Remove(l[0] + ".json")
			l = l[1:]
		}
	}
}

func (a *App) writeMeta(path string, m bkMeta) {
	b, _ := json.Marshal(m)
	os.WriteFile(path+".json", b, 0o600)
}

func (a *App) readMeta(path string) bkMeta {
	var m bkMeta
	if b, err := os.ReadFile(path + ".json"); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

// ---- reading a backup ----

type openedBackup struct {
	dir   string
	man   *bkManifest
	key   []byte
	hasDB bool
}

func (o *openedBackup) close() { os.RemoveAll(o.dir) }

const sqliteMagic = "SQLite format 3\x00"

func encrypt(plain []byte, password string) ([]byte, error) {
	salt := make([]byte, 16)
	rand.Read(salt)
	k, err := pbkdf2.Key(sha256.New, password, salt, 200000, 32)
	if err != nil {
		return nil, err
	}
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	nonce := make([]byte, g.NonceSize())
	rand.Read(nonce)
	out := append([]byte(bkEncMagic), salt...)
	out = append(out, nonce...)
	return g.Seal(out, nonce, plain, nil), nil
}

func decrypt(raw []byte, password string) ([]byte, error) {
	h := len(bkEncMagic)
	if len(raw) < h+16+12+16 {
		return nil, errors.New("the file is too short to be a backup")
	}
	if password == "" {
		return nil, errors.New("this backup is protected by a password; enter it")
	}
	k, err := pbkdf2.Key(sha256.New, password, raw[h:h+16], 200000, 32)
	if err != nil {
		return nil, err
	}
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	n := g.NonceSize()
	pt, err := g.Open(nil, raw[h+16:h+16+n], raw[h+16+n:], nil)
	if err != nil {
		return nil, errors.New("that password does not open this backup")
	}
	return pt, nil
}

// openBackup unpacks a backup into a private folder. It understands the older plain settings
// copies (just the database) as well as the current package, with or without a password.
func openBackup(path, password string) (*openedBackup, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("the backup file cannot be read")
	}
	if bytes.HasPrefix(raw, []byte(bkEncMagic)) {
		if raw, err = decrypt(raw, password); err != nil {
			return nil, err
		}
	}
	dir, err := os.MkdirTemp(filepath.Dir(path), ".open-")
	if err != nil {
		return nil, err
	}
	o := &openedBackup{dir: dir}
	fail := func(e error) (*openedBackup, error) { o.close(); return nil, e }
	if bytes.HasPrefix(raw, []byte(sqliteMagic)) { // an older plain copy
		if err := os.WriteFile(filepath.Join(dir, "config.db"), raw, 0o600); err != nil {
			return fail(err)
		}
		o.hasDB = true
		return o, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return fail(errors.New("this is not a Gatehouse backup file"))
	}
	tr := tar.NewReader(zr)
	files := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(errors.New("the backup file is damaged (it ends too early)"))
		}
		switch h.Name {
		case "manifest.json", "config.db", "secret.key":
			b, err := io.ReadAll(io.LimitReader(tr, 512<<20))
			if err != nil {
				return fail(errors.New("the backup file is damaged"))
			}
			files[h.Name] = b
		}
	}
	mb, ok := files["manifest.json"]
	if !ok || files["config.db"] == nil {
		return fail(errors.New("the backup file is missing parts; it is incomplete"))
	}
	var man bkManifest
	if json.Unmarshal(mb, &man) != nil {
		return fail(errors.New("the backup's contents list is damaged"))
	}
	sum := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	for n, want := range man.Sums {
		if got, ok := files[n]; !ok || sum(got) != want {
			return fail(fmt.Errorf("the backup is damaged: %s does not match its checksum", n))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "config.db"), files["config.db"], 0o600); err != nil {
		return fail(err)
	}
	o.man, o.key, o.hasDB = &man, files["secret.key"], true
	return o, nil
}

func countOf(q interface{ QueryRow(string, ...any) *sql.Row }, query string) int {
	var n int
	q.QueryRow(query).Scan(&n)
	return n
}

func contents(q interface{ QueryRow(string, ...any) *sql.Row }) BackupContents {
	return BackupContents{
		Networks:     countOf(q, `SELECT COUNT(*) FROM interfaces WHERE zone_id IS NOT NULL`),
		Zones:        countOf(q, `SELECT COUNT(*) FROM zones`),
		Rules:        countOf(q, `SELECT COUNT(*) FROM fw_rules`),
		Scopes:       countOf(q, `SELECT COUNT(*) FROM dhcp_scopes`),
		Reservations: countOf(q, `SELECT COUNT(*) FROM dhcp_reservations`),
		VPN:          countOf(q, `SELECT COUNT(*) FROM vpn_wg_peers`) + countOf(q, `SELECT COUNT(*) FROM vpn_ovpn_users`),
		Wifi:         countOf(q, `SELECT COUNT(*) FROM wifi_radios`),
		Devices:      countOf(q, `SELECT COUNT(*) FROM devices`),
		Users:        countOf(q, `SELECT COUNT(*) FROM users`),
	}
}

// verifyBackup looks inside a backup and reports what it found, without changing anything.
func (a *App) verifyBackup(path, password string) (*BackupReport, error) {
	rep := &BackupReport{At: time.Now().Unix(), OK: true}
	add := func(name string, ok bool, detail string) {
		rep.Checks = append(rep.Checks, BackupCheck{name, ok, detail})
		if !ok {
			rep.OK = false
		}
	}
	o, err := openBackup(path, password)
	if err != nil {
		add("The file is complete and not damaged", false, err.Error())
		return rep, nil
	}
	defer o.close()
	if o.man != nil {
		add("The file is complete and not damaged", true, "Every part matches its checksum.")
	} else {
		add("The file is complete and not damaged", true, "An older plain copy of the settings; it has no checksums to compare.")
	}
	db, from, err := store.OpenBackupCopy(filepath.Join(o.dir, "config.db"))
	if err != nil {
		add("The settings open", false, err.Error())
		return rep, nil
	}
	defer db.Close()
	rep.Layout = from
	add("The settings open", true, fmt.Sprintf("Made with settings layout %d; this version uses %d.", from, store.ConfigSchemaVersion()))
	var ic string
	db.QueryRow(`PRAGMA integrity_check`).Scan(&ic)
	fkBad := 0
	if rows, err := db.Query(`PRAGMA foreign_key_check`); err == nil {
		for rows.Next() {
			fkBad++
		}
		rows.Close()
	}
	add("The database is sound", ic == "ok" && fkBad == 0, map[bool]string{true: "No damage or broken links found.", false: fmt.Sprintf("Integrity check said %q, with %d broken links.", ic, fkBad)}[ic == "ok" && fkBad == 0])
	if m, err := fw.Load(db); err != nil {
		add("The firewall settings are valid", false, err.Error())
	} else if countOf(db, `SELECT COUNT(*) FROM interfaces`) == 0 {
		add("The firewall settings are valid", true, "No local networks are set up yet, so there is nothing for the firewall to protect.")
	} else if err := m.Validate(); err != nil {
		add("The firewall settings are valid", false, err.Error())
	} else {
		add("The firewall settings are valid", true, "They pass the same checks as applying them.")
	}
	// Saved passwords (Wi-Fi, PPPoE) can only be read with the key that sealed them.
	key := o.key
	if key == nil {
		key, _ = os.ReadFile(a.keyPath())
	}
	rep.Key = o.key != nil
	if box, err := newBox(key); err != nil {
		add("Saved passwords can be read", false, "The key stored with this backup is missing or unusable.")
	} else {
		tried, bad := 0, 0
		for _, q := range []string{`SELECT psk FROM wifi_radios WHERE psk<>''`, `SELECT pppoe_pass FROM wan_links WHERE pppoe_pass<>''`} {
			if rows, err := db.Query(q); err == nil {
				for rows.Next() {
					var s string
					rows.Scan(&s)
					tried++
					if _, err := box.Open(s); err != nil {
						bad++
					}
				}
				rows.Close()
			}
		}
		switch {
		case bad > 0:
			add("Saved passwords can be read", false, fmt.Sprintf("%d of %d saved passwords cannot be unlocked with the key in this backup.", bad, tried))
		case tried == 0:
			add("Saved passwords can be read", true, "No Wi-Fi or internet passwords are saved.")
		default:
			add("Saved passwords can be read", true, fmt.Sprintf("All %d saved passwords unlock.", tried))
		}
	}
	rep.Contents = contents(db)
	return rep, nil
}

// ---- restoring ----

// settings the restore leaves as they are on this machine.
var restoreKeepAlways = []string{"update.", "console.port", "mgmt.rescue_iface", "backup.", "ha."}

// settings that belong with the accounts, and are kept while the accounts are kept.
var restoreKeepWithAccounts = []string{"auth.", "session_", "fw.stepup", "system.timezone"}

func hasPrefixAny(k string, l []string) bool {
	for _, p := range l {
		if k == p || strings.HasSuffix(p, ".") && strings.HasPrefix(k, p) || strings.HasSuffix(p, "_") && strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// restoreTables lists the tables a restore replaces, children first. The accounts and sessions
// are added or left out by the caller; sign-in sessions are never touched.
var restoreTables = []string{
	"fw_hits", "dns_exceptions", "dns_zone_targets", "fw_rules", "fw_objects", "nat_forwards", "upnp_allow",
	"routes", "vpn_routes", "vpn_ovpn_users", "vpn_ovpn", "vpn_wg_peers", "vpn_wg",
	"wan_group_routes", "wan_group_members", "wan_groups", "wan_links", "wifi_radios",
	"dhcp_leases", "dhcp_reservations", "dhcp_scopes", "interfaces", "zones",
	"dns_records", "custom_rules", "dns_upstreams", "group_blocklists", "blocklists", "network_profiles", "groups", "wifi_cards",
	"device_group_members", "device_groups", "devices", "device_icons", "fw_revisions",
}

var restoreAccountTables = []string{"recovery_codes", "users"}

func newBox(key []byte) (*crypt.Box, error) { return crypt.New(key) }

type restoreResult struct {
	Safety       string `json:"safety"`
	KeyChanged   bool   `json:"key_changed"`
	AccountsFrom string `json:"accounts_from"`
}

// restoreBackup puts the settings from a backup in place of the current ones. It saves a backup of
// the current settings first. Nothing is applied to the network: the person reviews and applies it
// the usual way, with the usual way back if it goes wrong.
func (a *App) restoreBackup(path, password, actor string) (*restoreResult, error) {
	rep, err := a.verifyBackup(path, password)
	if err != nil {
		return nil, err
	}
	if !rep.OK {
		return nil, errors.New("this backup did not pass its checks, so it was not restored. Check it to see why")
	}
	o, err := openBackup(path, password)
	if err != nil {
		return nil, err
	}
	defer o.close()
	cur, _ := os.ReadFile(a.keyPath())
	differentKey := o.key != nil && !bytes.Equal(o.key, cur)
	safety, err := a.saveBackup("before-restore")
	if err != nil {
		return nil, err
	}
	// Bring the copy up to this version's layout, then close it so the live database can read it.
	cp, _, err := store.OpenBackupCopy(filepath.Join(o.dir, "config.db"))
	if err != nil {
		return nil, err
	}
	cp.Close()
	takeAccounts := differentKey // the saved sign-in secrets are sealed with the key, so they travel with it
	tables := append([]string{}, restoreTables...)
	if takeAccounts {
		tables = append(tables, restoreAccountTables...)
	}
	keep := append([]string{}, restoreKeepAlways...)
	if !takeAccounts {
		keep = append(keep, restoreKeepWithAccounts...)
	}
	ctx := context.Background()
	conn, err := a.Config.Conn(ctx)
	if err != nil {
		return nil, err
	}
	released := false
	release := func() {
		if !released {
			released = true
			conn.ExecContext(ctx, `DETACH DATABASE bk`)
			conn.Close()
		}
	}
	defer release()
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS bk`, filepath.Join(o.dir, "config.db")); err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, err
	}
	rollback := func(e error) (*restoreResult, error) {
		conn.ExecContext(ctx, `ROLLBACK`)
		return nil, fmt.Errorf("the restore was stopped and nothing was changed: %w", e)
	}
	conn.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`)
	for _, t := range tables { // delete children first
		if _, err := conn.ExecContext(ctx, `DELETE FROM main.`+t); err != nil {
			return rollback(fmt.Errorf("clearing %s: %w", t, err))
		}
	}
	for i := len(tables) - 1; i >= 0; i-- { // parents first
		t := tables[i]
		if _, err := conn.ExecContext(ctx, `INSERT INTO main.`+t+` SELECT * FROM bk.`+t); err != nil {
			return rollback(fmt.Errorf("restoring %s: %w", t, err))
		}
	}
	rows, err := conn.QueryContext(ctx, `SELECT key FROM main.settings`)
	if err != nil {
		return rollback(err)
	}
	var drop []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		if !hasPrefixAny(k, keep) {
			drop = append(drop, k)
		}
	}
	rows.Close()
	for _, k := range drop {
		if _, err := conn.ExecContext(ctx, `DELETE FROM main.settings WHERE key=?`, k); err != nil {
			return rollback(err)
		}
	}
	rows, err = conn.QueryContext(ctx, `SELECT key,value FROM bk.settings`)
	if err != nil {
		return rollback(err)
	}
	type kv struct{ k, v string }
	var add []kv
	for rows.Next() {
		var x kv
		rows.Scan(&x.k, &x.v)
		if !hasPrefixAny(x.k, keep) {
			add = append(add, x)
		}
	}
	rows.Close()
	for _, x := range add {
		if _, err := conn.ExecContext(ctx, `INSERT INTO main.settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, x.k, x.v); err != nil {
			return rollback(err)
		}
	}
	var bad int
	if r, err := conn.QueryContext(ctx, `PRAGMA foreign_key_check`); err == nil {
		for r.Next() {
			bad++
		}
		r.Close()
	}
	if bad > 0 {
		return rollback(errors.New("the restored settings contain broken links"))
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return rollback(err)
	}
	release() // the single database connection must be free before anything else uses it
	res := &restoreResult{Safety: filepath.Base(safety), AccountsFrom: map[bool]string{true: "the backup", false: "this machine"}[takeAccounts]}
	if differentKey {
		tmp := a.keyPath() + ".new"
		if err := os.WriteFile(tmp, o.key, 0o600); err == nil {
			err = os.Rename(tmp, a.keyPath())
		}
		a.Box.SetKey(o.key)
		res.KeyChanged = true
	}
	a.afterRestore(actor)
	return res, nil
}

func (a *App) afterRestore(actor string) {
	a.Lists.Reload()
	if a.DHCP != nil {
		a.DHCP.Reload()
	}
	a.Devices.Rebuild()
	a.applyDNS()
	a.writeSetupState()
	a.event(actor, "warning", "Settings were restored from a backup", "A backup of the settings from just before is kept in the backups list. Review and apply the changes to put them live.")
	a.publishNet()
	a.publishFW()
	a.publishLeases()
}

// ---- schedule ----

// backupTick makes a scheduled backup when one is due. It is called every hour.
func (a *App) backupTick(now time.Time) {
	c := a.backupConfig()
	if c.Schedule == "off" || now.Hour() < c.Hour {
		return
	}
	files, _ := filepath.Glob(filepath.Join(a.backupDir(), "auto-*"+bkExt))
	sort.Strings(files)
	gap := 20 * time.Hour
	if c.Schedule == "weekly" {
		gap = 6*24*time.Hour + 20*time.Hour
	}
	if len(files) > 0 {
		if st, err := os.Stat(files[len(files)-1]); err == nil && now.Sub(st.ModTime()) < gap {
			return
		}
	}
	if st := update.ReadState(a.updateState()); st != "idle" {
		return
	}
	if _, err := a.saveBackup("auto"); err != nil {
		a.event("system", "warning", "A scheduled backup could not be made", err.Error())
	}
}

// ---- routes ----

type backupItem struct {
	Name        string        `json:"name"`
	Kind        string        `json:"kind"`
	Created     int64         `json:"created"`
	Size        int64         `json:"size"`
	Protected   bool          `json:"protected"` // protected by a password
	Report      *BackupReport `json:"report,omitempty"`
	HandChecked int64         `json:"hand_checked,omitempty"`
	HandBy      string        `json:"hand_by,omitempty"`
}

func kindLabel(name string) string {
	switch k := strings.SplitN(name, "-2", 2)[0]; k {
	case "auto":
		return "Scheduled"
	case "manual":
		return "Made by hand"
	case "upload":
		return "Uploaded"
	case "before-reset":
		return "Before a reset"
	case "before-restore":
		return "Before a restore"
	case restorePointKind:
		return "Before a change"
	case "before-update":
		return "Before an update"
	default:
		if strings.HasPrefix(name, "download") {
			return "Download"
		}
		return "Other"
	}
}

func (a *App) backupPath(name string) (string, error) {
	if !reBackupName.MatchString(name) {
		return "", errors.New("that backup was not found")
	}
	p := filepath.Join(a.backupDir(), name)
	if _, err := os.Stat(p); err != nil {
		return "", errors.New("that backup was not found")
	}
	return p, nil
}

func (a *App) listBackups() []backupItem {
	out := []backupItem{}
	files, _ := filepath.Glob(filepath.Join(a.backupDir(), "*"))
	for _, f := range files {
		n := filepath.Base(f)
		if !reBackupName.MatchString(n) {
			continue
		}
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		it := backupItem{Name: n, Kind: kindLabel(n), Created: st.ModTime().Unix(), Size: st.Size()}
		if b, err := os.ReadFile(f); err == nil && bytes.HasPrefix(b, []byte(bkEncMagic)) {
			it.Protected = true
		}
		m := a.readMeta(f)
		it.Report, it.HandChecked, it.HandBy = m.Report, m.HandChecked, m.HandBy
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out
}

func (a *App) registerBackupRoutes() {
	s := a.API
	s.Handle("GET /api/backups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"backups": a.listBackups(), "config": a.backupConfig(), "now": contents(a.Config)})
	})
	s.HandleAdmin("PUT /api/backups/config", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in BackupConfig
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Schedule != "off" && in.Schedule != "daily" && in.Schedule != "weekly" {
			fail(w, errors.New("choose off, daily or weekly"))
			return
		}
		if in.Hour < 0 || in.Hour > 23 {
			fail(w, errors.New("choose an hour from 0 to 23"))
			return
		}
		if in.Keep < 1 || in.Keep > 60 {
			fail(w, errors.New("keep between 1 and 60 backups"))
			return
		}
		b, _ := json.Marshal(in)
		a.Config.SetSetting("backup.config", string(b))
		a.pruneBackups("manual")
		ok(w)
	})
	s.HandleAdmin("POST /api/backups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, err := a.saveBackup("manual")
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"name": filepath.Base(p), "report": a.readMeta(p).Report})
	})
	// Check a backup again, and show what a restore would bring back next to what is here now.
	s.HandleAdmin("POST /api/backups/{name}/check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, err := a.backupPath(r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		var in struct {
			Password string `json:"password"`
		}
		api.Decode(w, r, &in)
		rep, err := a.verifyBackup(p, in.Password)
		if err != nil {
			fail(w, err)
			return
		}
		m := a.readMeta(p)
		m.Report = rep
		a.writeMeta(p, m)
		api.JSON(w, 200, map[string]any{"report": rep, "now": contents(a.Config)})
	})
	// "I looked at what is inside and it is what I expect."
	s.HandleAdmin("POST /api/backups/{name}/hand-check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, err := a.backupPath(r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		m := a.readMeta(p)
		if m.Report == nil || !m.Report.OK {
			fail(w, errors.New("run the check first; only a backup that passes can be marked as checked"))
			return
		}
		m.HandChecked, m.HandBy = time.Now().Unix(), c.User.Username
		a.writeMeta(p, m)
		ok(w)
	})
	s.HandleAdmin("DELETE /api/backups/{name}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, err := a.backupPath(r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		os.Remove(p)
		os.Remove(p + ".json")
		ok(w)
	})
	// The file as it is. A backup that is not protected by a password holds the secrets of this
	// machine, so downloading it is a serious action; one already protected by a password is not.
	s.HandleAdmin("POST /api/backups/{name}/plain", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, err := a.backupPath(r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		var in struct {
			Code     string `json:"code"`
			StepPass string `json:"step_password"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			fail(w, err)
			return
		}
		if !bytes.HasPrefix(raw, []byte(bkEncMagic)) {
			if err := a.serious(c, in.Code, in.StepPass); err != nil {
				api.Error(w, http.StatusForbidden, err.Error())
				return
			}
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(p)+`"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(raw)
	})
	// The same file protected by a password of the person's choice.
	s.HandleAdmin("POST /api/backups/{name}/download", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, err := a.backupPath(r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		var in struct {
			Password string `json:"password"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if len(in.Password) < 8 {
			fail(w, errors.New("use a password of at least 8 characters"))
			return
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			fail(w, err)
			return
		}
		if bytes.HasPrefix(raw, []byte(bkEncMagic)) {
			fail(w, errors.New("this backup is already protected by a password"))
			return
		}
		enc, err := encrypt(raw, in.Password)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+strings.TrimSuffix(filepath.Base(p), bkExt)+`-protected`+bkExt+`"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(enc)
	})
	// A backup from somewhere else, kept in the list so it can be checked and restored.
	s.HandleAdmin("POST /api/backups/upload", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		r.Body = http.MaxBytesReader(w, r.Body, 128<<20)
		raw, err := io.ReadAll(r.Body)
		if err != nil || len(raw) == 0 {
			fail(w, errors.New("the file could not be read, or is too large"))
			return
		}
		pw := r.Header.Get("X-Backup-Password")
		if bytes.HasPrefix(raw, []byte(bkEncMagic)) {
			if raw, err = decrypt(raw, pw); err != nil {
				fail(w, err)
				return
			}
		}
		os.MkdirAll(a.backupDir(), 0o700)
		p := filepath.Join(a.backupDir(), "upload-"+time.Now().Format("20060102-150405")+bkExt)
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			fail(w, err)
			return
		}
		rep, _ := a.verifyBackup(p, "")
		a.writeMeta(p, bkMeta{Report: rep})
		a.pruneBackups("upload")
		api.JSON(w, 200, map[string]any{"name": filepath.Base(p), "report": rep})
	})
	s.HandleAdmin("POST /api/backups/{name}/restore", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, err := a.backupPath(r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		var in struct {
			Code     string `json:"code"`
			StepPass string `json:"step_password"`
			Confirm  string `json:"confirm"`
			Password string `json:"password"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Confirm != "restore" {
			fail(w, errors.New("type the word restore to confirm"))
			return
		}
		if err := a.serious(c, in.Code, in.StepPass); err != nil {
			api.Error(w, http.StatusForbidden, err.Error())
			return
		}
		if st := update.ReadState(a.updateState()); st != "idle" {
			api.Error(w, http.StatusConflict, "an update is running ("+st+"); wait for it to finish")
			return
		}
		res, err := a.restoreBackup(p, in.Password, c.User.Username)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, res)
	})
}
