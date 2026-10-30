// Package devices is the single place that knows what a device is: its address from DHCP,
// its name, its group and how it is filtered. DNS and the console both read from here.
package devices

import (
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"gatehouse/internal/dhcp"
	dnssrv "gatehouse/internal/dns"
	"gatehouse/internal/store"
)

type entry struct {
	info dnssrv.ClientInfo
}

type view struct {
	byMAC   map[string]dnssrv.ClientInfo
	defGrp  int64
	defFilt bool
	defSafe bool
}

// Directory implements dnssrv.Directory from the devices, groups and lease tables.
type Directory struct {
	db    *store.DB
	dhcp  *dhcp.Engine
	v     atomic.Pointer[view]
	clock func() time.Time
}

func NewDirectory(db *store.DB, e *dhcp.Engine) *Directory {
	d := &Directory{db: db, dhcp: e, clock: time.Now}
	d.v.Store(&view{byMAC: map[string]dnssrv.ClientInfo{}, defFilt: true})
	return d
}

// Rebuild re-reads the tables. Call it when devices or groups change.
func (d *Directory) Rebuild() {
	v := &view{byMAC: map[string]dnssrv.ClientInfo{}, defFilt: true}
	var gid int64
	var filt, defSafe int
	if d.db.QueryRow(`SELECT id,filtering,safe_search FROM groups WHERE name='Default'`).Scan(&gid, &filt, &defSafe) == nil {
		v.defGrp, v.defFilt, v.defSafe = gid, filt == 1, defSafe == 1
	}
	rows, err := d.db.Query(`SELECT d.mac,d.name,COALESCE(d.group_id,0),d.policy,COALESCE(g.filtering,1),COALESCE(g.safe_search,0) FROM devices d LEFT JOIN groups g ON g.id=d.group_id`)
	if err == nil {
		for rows.Next() {
			var mac, name, policy string
			var g int64
			var f, safe int
			rows.Scan(&mac, &name, &g, &policy, &f, &safe)
			ci := dnssrv.ClientInfo{Name: name, MAC: strings.ToLower(mac), GroupID: g, Filtering: f == 1 && policy != "unfiltered", SafeSearch: safe == 1}
			if g == 0 {
				ci.GroupID, ci.Filtering, ci.SafeSearch = v.defGrp, v.defFilt && policy != "unfiltered", defSafe == 1
			}
			v.byMAC[ci.MAC] = ci
		}
		rows.Close()
	}
	d.v.Store(v)
}

// Client implements dnssrv.Directory.
func (d *Directory) Client(ip string) dnssrv.ClientInfo {
	v := d.v.Load()
	mac := d.dhcp.Lookups().MACFor(ip)
	if mac != "" {
		if ci, ok := v.byMAC[mac]; ok {
			if ci.Name == "" {
				ci.Name, _ = d.dhcp.Lookups().LookupAddr(net.ParseIP(ip))
			}
			return ci
		}
	}
	name := ""
	if h, ok := d.dhcp.Lookups().LookupAddr(net.ParseIP(ip)); ok {
		name = h
	}
	return dnssrv.ClientInfo{Name: name, MAC: mac, GroupID: v.defGrp, Filtering: v.defFilt, SafeSearch: v.defSafe}
}

// Seen records that a device was active. New devices appear in the list automatically.
func (d *Directory) Seen(mac string, ip net.IP, hostname string) {
	now := d.clock().Unix()
	mac = strings.ToLower(mac)
	res, err := d.db.Exec(`INSERT INTO devices(mac,name,first_seen,last_seen,last_ip) VALUES(?,?,?,?,?)
		ON CONFLICT(mac) DO UPDATE SET last_seen=excluded.last_seen,last_ip=excluded.last_ip`, mac, "", now, now, ip.String())
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		d.Rebuild()
	}
	_ = hostname
}

// Device is one row of the device list.
type Device struct {
	ID       int64  `json:"id"`
	MAC      string `json:"mac"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	Online   bool   `json:"online"`
	GroupID  int64  `json:"group_id"`
	Group    string `json:"group"`
	Policy   string `json:"policy"`
	Internet bool   `json:"internet"`
	Static   bool   `json:"static"`
	LastSeen int64  `json:"last_seen"`
	Notes    string `json:"notes"`
	Icon     string `json:"icon"`    // a built-in icon chosen by the person; empty means guess
	IconID   int64  `json:"icon_id"` // an uploaded picture, 0 for none
}

// List returns every known device with its live state merged in.
func (d *Directory) List() ([]Device, error) {
	live := map[string]dhcp.LeaseRow{}
	if rows, err := d.dhcp.Leases(); err == nil {
		for _, l := range rows {
			live[strings.ToLower(l.MAC)] = l
		}
	}
	rows, err := d.db.Query(`SELECT d.id,d.mac,d.name,COALESCE(d.group_id,0),COALESCE(g.name,''),d.policy,d.internet,d.last_seen,d.last_ip,d.notes,d.icon,d.icon_id
		FROM devices d LEFT JOIN groups g ON g.id=d.group_id ORDER BY d.last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var x Device
		var inet int
		if err := rows.Scan(&x.ID, &x.MAC, &x.Name, &x.GroupID, &x.Group, &x.Policy, &inet, &x.LastSeen, &x.IP, &x.Notes, &x.Icon, &x.IconID); err != nil {
			return nil, err
		}
		x.Internet = inet == 1
		if l, ok := live[strings.ToLower(x.MAC)]; ok {
			x.Online, x.IP, x.Hostname, x.Static = true, l.IP, l.Hostname, l.Static
		}
		if x.Group == "" {
			x.Group = "Default"
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Update changes the parts of a device an administrator may edit.
type Update struct {
	Name     *string `json:"name"`
	GroupID  *int64  `json:"group_id"`
	Policy   *string `json:"policy"`
	Internet *bool   `json:"internet"`
	Notes    *string `json:"notes"`
	Icon     *string `json:"icon"`
	IconID   *int64  `json:"icon_id"`
}

func (d *Directory) Update(id int64, u Update) error {
	if u.Name != nil {
		if _, err := d.db.Exec(`UPDATE devices SET name=? WHERE id=?`, strings.TrimSpace(*u.Name), id); err != nil {
			return err
		}
	}
	if u.GroupID != nil {
		var v any = *u.GroupID
		if *u.GroupID == 0 {
			v = nil
		}
		if _, err := d.db.Exec(`UPDATE devices SET group_id=? WHERE id=?`, v, id); err != nil {
			return err
		}
	}
	if u.Policy != nil && (*u.Policy == "filtered" || *u.Policy == "unfiltered") {
		if _, err := d.db.Exec(`UPDATE devices SET policy=? WHERE id=?`, *u.Policy, id); err != nil {
			return err
		}
	}
	if u.Internet != nil {
		n := 0
		if *u.Internet {
			n = 1
		}
		if _, err := d.db.Exec(`UPDATE devices SET internet=? WHERE id=?`, n, id); err != nil {
			return err
		}
	}
	if u.Notes != nil {
		if _, err := d.db.Exec(`UPDATE devices SET notes=? WHERE id=?`, *u.Notes, id); err != nil {
			return err
		}
	}
	if u.Icon != nil {
		if *u.Icon != "" && !ValidIcon(*u.Icon) {
			return errors.New("that is not one of the icons")
		}
		if _, err := d.db.Exec(`UPDATE devices SET icon=? WHERE id=?`, *u.Icon, id); err != nil {
			return err
		}
	}
	if u.IconID != nil {
		if _, err := d.db.Exec(`UPDATE devices SET icon_id=? WHERE id=?`, *u.IconID, id); err != nil {
			return err
		}
	}
	d.Rebuild()
	return nil
}

// Create adds a device by hand, for one that has not been seen on the network yet.
func (d *Directory) Create(mac, name string) (int64, error) {
	mac = strings.ToLower(strings.TrimSpace(mac))
	if hw, err := net.ParseMAC(mac); err != nil || len(hw) != 6 {
		return 0, errors.New("a device address looks like aa:bb:cc:dd:ee:ff")
	}
	var id int64
	if d.db.QueryRow(`SELECT id FROM devices WHERE mac=?`, mac).Scan(&id) == nil {
		return 0, errors.New("that device is already in the list")
	}
	res, err := d.db.Exec(`INSERT INTO devices(mac,name,first_seen,last_seen) VALUES(?,?,?,0)`, mac, strings.TrimSpace(name), d.clock().Unix())
	if err != nil {
		return 0, err
	}
	d.Rebuild()
	return res.LastInsertId()
}

// Forget removes a device from the list.
func (d *Directory) Forget(id int64) error {
	_, err := d.db.Exec(`DELETE FROM devices WHERE id=?`, id)
	d.Rebuild()
	return err
}

var _ = entry{}
