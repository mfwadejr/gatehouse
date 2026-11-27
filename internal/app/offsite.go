package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/netd"
	"nopewarden/internal/offsite"
)

// Copies off this machine. After every scheduled or hand-made backup, an encrypted copy is sent to each
// place the person chose. The copy is encrypted with a password of their own (the backup holds the key
// that protects every saved password, so it must never leave this machine unscrambled). A copy that
// failed is tried again every hour. Restoring from one: download the file from the place it is kept and
// use Upload on the Backups page with the same password.

const (
	offMaxDests   = 5
	offMaxMailRaw = 15 << 20 // bigger than this does not go by email
)

var reOffName = regexp.MustCompile(`^(auto|manual)-\d{8}-\d{6}\.ghbk$`)

type offS3 struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"access_key"`
	Secret    string `json:"secret,omitempty"` // sealed when stored; never sent to the console
	SecretSet bool   `json:"secret_set"`
	PathStyle bool   `json:"path_style"`
}

type offDAV struct {
	URL     string `json:"url"`
	Dir     string `json:"dir"`
	User    string `json:"user"`
	Pass    string `json:"pass,omitempty"` // sealed when stored
	PassSet bool   `json:"pass_set"`
}

type offMail struct {
	To string `json:"to"`
}

// offCloud is a Dropbox, OneDrive or Google Drive place: the person's own app ID, and the lasting
// sign-in the provider gave back (sealed, never sent to the console).
type offCloud struct {
	ClientID  string `json:"client_id"`
	Secret    string `json:"secret,omitempty"` // Google only; sealed when stored
	SecretSet bool   `json:"secret_set"`
	Refresh   string `json:"refresh,omitempty"` // sealed; never sent to the console
	SignedIn  bool   `json:"signed_in"`
}

type offSFTP struct {
	Host string `json:"host"`
	Port string `json:"port"`
	User string `json:"user"`
	Dir  string `json:"dir"`
}

type offSMB struct {
	Host    string `json:"host"`
	Share   string `json:"share"`
	Dir     string `json:"dir"`
	User    string `json:"user"`
	Domain  string `json:"domain"`
	Pass    string `json:"pass,omitempty"` // sealed when stored
	PassSet bool   `json:"pass_set"`
}

type offUSB struct {
	UUID  string `json:"uuid"`
	Label string `json:"label"`
}

type offDest struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"` // s3, webdav, email
	Label   string    `json:"label"`
	Enabled bool      `json:"enabled"`
	S3      *offS3    `json:"s3,omitempty"`
	DAV     *offDAV   `json:"dav,omitempty"`
	Mail    *offMail  `json:"mail,omitempty"`
	Cloud   *offCloud `json:"cloud,omitempty"`
	SFTP    *offSFTP  `json:"sftp,omitempty"`
	SMB     *offSMB   `json:"smb,omitempty"`
	USB     *offUSB   `json:"usb,omitempty"`
}

type offCfg struct {
	Password    string    `json:"password,omitempty"` // sealed when stored
	PasswordSet bool      `json:"password_set"`
	Keep        int       `json:"keep"`
	Dests       []offDest `json:"dests"`
	RemindOn    bool      `json:"remind_on"`
	RemindDays  int       `json:"remind_days"`
}

type offState struct {
	At    int64  `json:"at"`
	OK    bool   `json:"ok"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Error string `json:"error,omitempty"`
}

var offMu sync.Mutex

// offCfgMu guards changes to the saved places, so a new sign-in token cannot be lost to a save.
var offCfgMu sync.Mutex

// saveRefresh keeps a new sign-in token the provider handed out during a copy.
func (a *App) saveRefresh(id, refresh string) {
	offCfgMu.Lock()
	defer offCfgMu.Unlock()
	c := a.offsiteConfig()
	for i := range c.Dests {
		if c.Dests[i].ID == id && c.Dests[i].Cloud != nil {
			c.Dests[i].Cloud.Refresh = a.Box.Seal(refresh)
		}
	}
	b, _ := json.Marshal(c)
	a.Config.SetSetting("backup.offsite", string(b))
}

var (
	offFlowMu sync.Mutex
	offFlows  = map[string]*offsite.Flow{}
)

func (a *App) offsiteConfig() offCfg {
	c := offCfg{Keep: 14, RemindOn: true, RemindDays: 30, Dests: []offDest{}}
	if raw := a.setting("backup.offsite", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	if c.Keep < 1 {
		c.Keep = 14
	}
	if c.RemindDays < 1 {
		c.RemindDays = 30
	}
	if c.Dests == nil {
		c.Dests = []offDest{}
	}
	return c
}

func (a *App) offsiteStates() map[string]offState {
	m := map[string]offState{}
	if raw := a.setting("backup.offsite.status", ""); raw != "" {
		json.Unmarshal([]byte(raw), &m)
	}
	return m
}

func (a *App) saveOffsiteStates(m map[string]offState) {
	b, _ := json.Marshal(m)
	a.Config.SetSetting("backup.offsite.status", string(b))
}

// offsiteView is the configuration as the console sees it: no secrets, only whether each is set.
func (a *App) offsiteView(c offCfg) offCfg {
	c.PasswordSet = c.Password != ""
	c.Password = ""
	out := make([]offDest, len(c.Dests))
	for i, d := range c.Dests {
		if d.S3 != nil {
			s := *d.S3
			s.SecretSet, s.Secret = s.Secret != "", ""
			d.S3 = &s
		}
		if d.DAV != nil {
			v := *d.DAV
			v.PassSet, v.Pass = v.Pass != "", ""
			d.DAV = &v
		}
		if d.SMB != nil {
			v := *d.SMB
			v.PassSet, v.Pass = v.Pass != "", ""
			d.SMB = &v
		}
		if d.Cloud != nil {
			v := *d.Cloud
			v.SecretSet, v.SignedIn, v.Secret, v.Refresh = v.Secret != "", v.Refresh != "", "", ""
			d.Cloud = &v
		}
		out[i] = d
	}
	c.Dests = out
	return c
}

func (a *App) openSecret(s string) string {
	if s == "" {
		return ""
	}
	p, _ := a.Box.Open(s)
	return p
}

// storeFor builds the connection to a destination (not for email).
func (a *App) storeFor(d offDest) (offsite.Store, error) {
	switch d.Kind {
	case "s3":
		if d.S3 == nil {
			return nil, errors.New("this place is not filled in")
		}
		return &offsite.S3{Endpoint: d.S3.Endpoint, Region: d.S3.Region, Bucket: d.S3.Bucket, Prefix: d.S3.Prefix,
			AccessKey: d.S3.AccessKey, SecretKey: a.openSecret(d.S3.Secret), PathStyle: d.S3.PathStyle}, nil
	case "webdav":
		if d.DAV == nil {
			return nil, errors.New("this place is not filled in")
		}
		return &offsite.WebDAV{URL: d.DAV.URL, Dir: d.DAV.Dir, User: d.DAV.User, Pass: a.openSecret(d.DAV.Pass)}, nil
	case "sftp":
		if d.SFTP == nil {
			return nil, errors.New("this place is not filled in")
		}
		dir := a.offsiteDir()
		return &offsite.SFTP{Host: d.SFTP.Host, Port: d.SFTP.Port, User: d.SFTP.User, Dir: d.SFTP.Dir,
			KeyFile: filepath.Join(dir, "sftp_ed25519"), KnownHosts: filepath.Join(dir, "known_hosts")}, nil
	case "smb":
		if d.SMB == nil {
			return nil, errors.New("this place is not filled in")
		}
		return &offsite.SMB{Host: d.SMB.Host, Share: d.SMB.Share, Dir: d.SMB.Dir, User: d.SMB.User, Domain: d.SMB.Domain, Pass: a.openSecret(d.SMB.Pass)}, nil
	case "dropbox", "onedrive", "google":
		if d.Cloud == nil || d.Cloud.Refresh == "" {
			return nil, errors.New("sign in to this place first")
		}
		t := &offsite.Token{ClientID: d.Cloud.ClientID, Refresh: a.openSecret(d.Cloud.Refresh),
			Saved: func(r string) { a.saveRefresh(d.ID, r) }}
		switch d.Kind {
		case "dropbox":
			t.TokenURL = offsite.DropboxTokenURL
			return &offsite.Dropbox{Tok: t}, nil
		case "onedrive":
			t.TokenURL = offsite.MSTokenURL
			return &offsite.OneDrive{Tok: t}, nil
		}
		t.TokenURL, t.ClientSecret = offsite.GoogleTokenURL, a.openSecret(d.Cloud.Secret)
		return &offsite.GDrive{Tok: t}, nil
	}
	return nil, errors.New("this kind of place cannot store files")
}

// offsiteSend sends one backup file to every enabled place that does not have it yet.
func (a *App) offsiteSend(path string) map[string]offState {
	offMu.Lock()
	defer offMu.Unlock()
	cfg := a.offsiteConfig()
	states := a.offsiteStates()
	name := filepath.Base(path)
	todo := []offDest{}
	for _, d := range cfg.Dests {
		if d.Enabled && !(states[d.ID].OK && states[d.ID].Name == name) {
			todo = append(todo, d)
		}
	}
	if len(todo) == 0 {
		return states
	}
	raw, err := os.ReadFile(path)
	if err == nil && cfg.Password == "" {
		err = errors.New("choose a password for the copies first")
	}
	var sealed []byte
	if err == nil {
		if bytes.HasPrefix(raw, []byte(bkEncMagic)) {
			sealed = raw
		} else {
			sealed, err = encrypt(raw, a.openSecret(cfg.Password))
		}
	}
	for _, d := range todo {
		st := offState{At: time.Now().Unix(), Name: name, Size: int64(len(sealed))}
		e := err
		if e == nil {
			e = a.offsitePut(cfg, d, name, sealed)
		}
		if e != nil {
			st.Error = e.Error()
			a.backupLog("system", "copy-failed", backupKindOf(path), path, d.Label+": "+e.Error())
			a.event("system", "warning", "A backup copy could not be sent to "+d.Label, e.Error())
		} else {
			st.OK = true
			a.backupLog("system", "copied", backupKindOf(path), path, d.Label)
		}
		states[d.ID] = st
	}
	a.saveOffsiteStates(states)
	return states
}

func (a *App) offsiteDir() string {
	d := filepath.Join(a.Cfg.DataDir, "offsite")
	os.MkdirAll(d, 0o700)
	return d
}

// offsiteUSB writes the copy to a file for the privileged helper, which puts it on the drive.
func (a *App) offsiteUSB(cfg offCfg, d offDest, name string, data []byte) error {
	if d.USB == nil || d.USB.UUID == "" {
		return errors.New("choose a drive first")
	}
	dir := filepath.Join(a.Cfg.DataDir, "offsite-out")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return err
	}
	defer os.Remove(p)
	return a.Net.UsbCopy(d.USB.UUID, p, name, cfg.Keep)
}

func (a *App) offsitePut(cfg offCfg, d offDest, name string, data []byte) error {
	if d.Kind == "email" {
		return a.offsiteEmail(d, name, data)
	}
	if d.Kind == "usb" {
		return a.offsiteUSB(cfg, d, name, data)
	}
	st, err := a.storeFor(d)
	if err != nil {
		return err
	}
	if err := st.Put(name, data); err != nil {
		return err
	}
	// Keep only the newest few there, so the place does not fill up.
	if items, err := st.List(); err == nil {
		var mine []string
		for _, it := range items {
			if reOffName.MatchString(it.Name) {
				mine = append(mine, it.Name)
			}
		}
		sort.Strings(mine)
		for len(mine) > cfg.Keep {
			st.Delete(mine[0])
			mine = mine[1:]
		}
	}
	return nil
}

func (a *App) offsiteEmail(d offDest, name string, data []byte) error {
	e := a.alertConfig().Email
	if e.Host == "" {
		return errors.New("set up the mail server under Alerts first")
	}
	if len(data) > offMaxMailRaw {
		return fmt.Errorf("the backup is %d MB, too big to send by email", len(data)>>20)
	}
	to := ""
	if d.Mail != nil {
		to = d.Mail.To
	}
	return a.sendMail(e, to, func(from, rcpt *mail.Address) []byte {
		return attachmentMessage(from.String(), rcpt.String(), "backup copy "+name,
			"The attached file is an encrypted NopeWarden backup. It can only be opened with the copy password you chose.\r\nTo restore it, use Upload on the Backups page.\r\n",
			name, data)
	})
}

// attachmentMessage is an email with one file attached.
func attachmentMessage(from, to, title, body, fileName string, data []byte) []byte {
	clean := func(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
	var rnd [8]byte
	rand.Read(rnd[:])
	bnd := "nw" + hex.EncodeToString(rnd[:])
	var b bytes.Buffer
	b.WriteString("From: " + clean(from) + "\r\nTo: " + clean(to) + "\r\nSubject: " + clean("NopeWarden: "+title) +
		"\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=" + bnd + "\r\n\r\n")
	b.WriteString("--" + bnd + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n") + "\r\n")
	b.WriteString("--" + bnd + "\r\nContent-Type: application/octet-stream\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=\"" + clean(fileName) + "\"\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n--" + bnd + "--\r\n")
	return b.Bytes()
}

// offsiteAfterBackup is called when a scheduled or hand-made backup has been saved.
func (a *App) offsiteAfterBackup(prefix, path string) {
	if prefix != "auto" && prefix != "manual" {
		return
	}
	c := a.offsiteConfig()
	for _, d := range c.Dests {
		if d.Enabled {
			go a.offsiteSend(path)
			return
		}
	}
}

// offsiteRetry sends the newest backup to any place that missed it. It runs every hour.
func (a *App) offsiteRetry() {
	c := a.offsiteConfig()
	any := false
	for _, d := range c.Dests {
		any = any || d.Enabled
	}
	if !any {
		return
	}
	var files []string
	for _, p := range []string{"auto", "manual"} {
		m, _ := filepath.Glob(filepath.Join(a.backupDir(), p+"-*"+bkExt))
		files = append(files, m...)
	}
	sort.Slice(files, func(i, j int) bool {
		si, _ := os.Stat(files[i])
		sj, _ := os.Stat(files[j])
		return si != nil && sj != nil && si.ModTime().After(sj.ModTime())
	})
	if len(files) > 0 {
		a.offsiteSend(files[0])
	}
}

// offsiteSummary is what the Backups page and the attention list need.
func (a *App) offsiteSummary() map[string]any {
	c := a.offsiteConfig()
	st := a.offsiteStates()
	n := 0
	var lastOK int64
	failing := []string{}
	for _, d := range c.Dests {
		if !d.Enabled {
			continue
		}
		n++
		s := st[d.ID]
		if s.OK && s.At > lastOK {
			lastOK = s.At
		}
		if s.At > 0 && !s.OK {
			failing = append(failing, d.Label)
		}
	}
	return map[string]any{"enabled": n, "last_ok": lastOK, "failing": failing, "last_download": a.lastBackupDownload(),
		"remind_on": c.RemindOn, "remind_days": c.RemindDays, "password_set": c.Password != ""}
}

func offCleanID(id string) string {
	var b [4]byte
	rand.Read(b[:])
	if id == "" || len(id) > 20 {
		return hex.EncodeToString(b[:])
	}
	return id
}

func (a *App) registerOffsiteRoutes() {
	a.registerOffsiteDeviceRoutes()
	s := a.API
	s.HandleAdmin("GET /api/backups/offsite", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"config": a.offsiteView(a.offsiteConfig()), "status": a.offsiteStates(), "summary": a.offsiteSummary()})
	})
	s.HandleAdmin("PUT /api/backups/offsite", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in offCfg
		var pw struct {
			Password string `json:"password"`
		}
		body := new(bytes.Buffer)
		body.ReadFrom(http.MaxBytesReader(w, r.Body, 1<<20))
		if json.Unmarshal(body.Bytes(), &in) != nil || json.Unmarshal(body.Bytes(), &pw) != nil {
			fail(w, errors.New("that was not understood"))
			return
		}
		old := a.offsiteConfig()
		if in.Keep < 1 || in.Keep > 60 {
			fail(w, errors.New("keep between 1 and 60 copies in each place"))
			return
		}
		if in.RemindDays < 1 || in.RemindDays > 365 {
			fail(w, errors.New("choose a reminder between 1 and 365 days"))
			return
		}
		if len(in.Dests) > offMaxDests {
			fail(w, errors.New("up to 5 places"))
			return
		}
		out := old
		out.Keep, out.RemindOn, out.RemindDays = in.Keep, in.RemindOn, in.RemindDays
		if pw.Password != "" {
			if err := a.Auth.Policy().Check(pw.Password, ""); err != nil {
				fail(w, err)
				return
			}
			out.Password = a.Box.Seal(pw.Password)
		}
		prev := map[string]offDest{}
		for _, d := range old.Dests {
			prev[d.ID] = d
		}
		out.Dests = []offDest{}
		anyOn := false
		for _, d := range in.Dests {
			d.ID = offCleanID(d.ID)
			d.Label = strings.TrimSpace(d.Label)
			if d.Label == "" {
				fail(w, errors.New("give each place a name"))
				return
			}
			p := prev[d.ID]
			switch d.Kind {
			case "s3":
				if d.S3 == nil || strings.TrimSpace(d.S3.Bucket) == "" || strings.TrimSpace(d.S3.AccessKey) == "" {
					fail(w, errors.New(d.Label+": fill in the bucket and the access key"))
					return
				}
				v := *d.S3
				v.SecretSet = false
				if v.Secret == "" && p.S3 != nil {
					v.Secret = p.S3.Secret
				} else if v.Secret != "" {
					v.Secret = a.Box.Seal(v.Secret)
				}
				if v.Secret == "" {
					fail(w, errors.New(d.Label+": fill in the secret key"))
					return
				}
				if v.Region == "" {
					v.Region = "us-east-1"
				}
				if v.Endpoint != "" {
					if u, err := url.Parse(v.Endpoint); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
						fail(w, errors.New(d.Label+": the address must start with https://"))
						return
					}
				}
				d.S3, d.DAV, d.Mail, d.Cloud = &v, nil, nil, nil
			case "webdav":
				if d.DAV == nil || strings.TrimSpace(d.DAV.URL) == "" {
					fail(w, errors.New(d.Label+": fill in the address"))
					return
				}
				v := *d.DAV
				v.PassSet = false
				if v.Pass == "" && p.DAV != nil {
					v.Pass = p.DAV.Pass
				} else if v.Pass != "" {
					v.Pass = a.Box.Seal(v.Pass)
				}
				if u, err := url.Parse(strings.TrimSpace(v.URL)); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
					fail(w, errors.New(d.Label+": the address must start with https://"))
					return
				}
				d.DAV, d.S3, d.Mail, d.Cloud = &v, nil, nil, nil
			case "dropbox", "onedrive", "google":
				if d.Cloud == nil || strings.TrimSpace(d.Cloud.ClientID) == "" {
					fail(w, errors.New(d.Label+": fill in the app ID from the provider"))
					return
				}
				v := *d.Cloud
				v.ClientID = strings.TrimSpace(v.ClientID)
				v.SecretSet, v.SignedIn, v.Refresh = false, false, ""
				if v.Secret != "" {
					v.Secret = a.Box.Seal(v.Secret)
				} else if p.Cloud != nil {
					v.Secret = p.Cloud.Secret
				}
				if d.Kind == "google" && v.Secret == "" {
					fail(w, errors.New(d.Label+": fill in the client secret"))
					return
				}
				// Keep the sign-in unless the app ID changed, which needs a new one.
				if p.Cloud != nil && p.Cloud.ClientID == v.ClientID {
					v.Refresh = p.Cloud.Refresh
				}
				if d.Enabled && v.Refresh == "" {
					fail(w, errors.New(d.Label+": sign in first, then turn it on"))
					return
				}
				d.Cloud, d.S3, d.DAV, d.Mail = &v, nil, nil, nil
			case "sftp":
				if d.SFTP == nil || strings.TrimSpace(d.SFTP.Host) == "" || strings.TrimSpace(d.SFTP.User) == "" {
					fail(w, errors.New(d.Label+": fill in the server and the user name"))
					return
				}
				v := *d.SFTP
				v.Host, v.User, v.Dir = strings.TrimSpace(v.Host), strings.TrimSpace(v.User), strings.Trim(strings.TrimSpace(v.Dir), "/")
				if v.Dir == "" {
					v.Dir = "NopeWarden"
				}
				if err := (&offsite.SFTP{Host: v.Host, User: v.User, Dir: v.Dir}).Check(); err != nil {
					fail(w, errors.New(d.Label+": the server, user or folder has a character that is not allowed"))
					return
				}
				d.SFTP, d.S3, d.DAV, d.Mail, d.Cloud, d.SMB, d.USB = &v, nil, nil, nil, nil, nil, nil
			case "smb":
				if d.SMB == nil || strings.TrimSpace(d.SMB.Host) == "" || strings.TrimSpace(d.SMB.Share) == "" {
					fail(w, errors.New(d.Label+": fill in the server and the share name"))
					return
				}
				v := *d.SMB
				v.PassSet = false
				if v.Pass == "" && p.SMB != nil {
					v.Pass = p.SMB.Pass
				} else if v.Pass != "" {
					v.Pass = a.Box.Seal(v.Pass)
				}
				v.Host, v.Share, v.Dir = strings.TrimSpace(v.Host), strings.TrimSpace(v.Share), strings.Trim(strings.TrimSpace(v.Dir), "/")
				if v.Dir == "" {
					v.Dir = "NopeWarden"
				}
				d.SMB, d.S3, d.DAV, d.Mail, d.Cloud, d.SFTP, d.USB = &v, nil, nil, nil, nil, nil, nil
			case "usb":
				if d.USB == nil || d.USB.UUID == "" {
					fail(w, errors.New(d.Label+": choose a drive"))
					return
				}
				v := *d.USB
				d.USB, d.S3, d.DAV, d.Mail, d.Cloud, d.SFTP, d.SMB = &v, nil, nil, nil, nil, nil, nil
			case "email":
				if d.Mail == nil {
					d.Mail = &offMail{}
				}
				if t := strings.TrimSpace(d.Mail.To); t != "" {
					if _, err := mail.ParseAddress(t); err != nil {
						fail(w, errors.New(d.Label+": that is not an email address"))
						return
					}
				}
				d.S3, d.DAV, d.Cloud = nil, nil, nil
			default:
				fail(w, errors.New("choose what kind of place "+d.Label+" is"))
				return
			}
			anyOn = anyOn || d.Enabled
			out.Dests = append(out.Dests, d)
		}
		if anyOn && out.Password == "" {
			fail(w, errors.New("choose a password for the copies first; it is the only thing that opens them"))
			return
		}
		offCfgMu.Lock()
		b, _ := json.Marshal(out)
		a.Config.SetSetting("backup.offsite", string(b))
		offCfgMu.Unlock()
		a.event(c.User.Username, "info", "Changed where backup copies are sent", fmt.Sprintf("%d place(s) in use", len(out.Dests)))
		api.JSON(w, 200, map[string]any{"config": a.offsiteView(out)})
	})
	s.HandleAdmin("POST /api/backups/offsite/{id}/test", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var d *offDest
		for _, x := range a.offsiteConfig().Dests {
			if x.ID == r.PathValue("id") {
				x := x
				d = &x
			}
		}
		if d == nil {
			fail(w, errors.New("save this place first, then test it"))
			return
		}
		if d.Kind == "usb" {
			disks, err := a.Net.UsbList()
			if err != nil {
				fail(w, err)
				return
			}
			for _, x := range disks {
				if d.USB != nil && x.UUID == d.USB.UUID {
					api.JSON(w, 200, map[string]any{"status": "ok", "message": "The drive is plugged in. Nothing was written."})
					return
				}
			}
			fail(w, errors.New("the USB drive is not plugged in"))
			return
		}
		if d.Kind == "email" {
			e := a.alertConfig().Email
			to := ""
			if d.Mail != nil {
				to = d.Mail.To
			}
			if e.Host == "" {
				fail(w, errors.New("set up the mail server under Alerts first"))
				return
			}
			err := a.sendMail(e, to, func(from, rcpt *mail.Address) []byte {
				return emailMessage(from.String(), rcpt.String(), "backup copy test", "This is a test. Backups will arrive at this address as an attached, encrypted file.")
			})
			if err != nil {
				fail(w, err)
				return
			}
			api.JSON(w, 200, map[string]any{"status": "ok", "message": "A test email was sent. Check that it arrived."})
			return
		}
		st, err := a.storeFor(*d)
		if err == nil {
			err = st.Put("nopewarden-test.txt", []byte("NopeWarden connection test "+time.Now().Format(time.RFC3339)))
		}
		if err == nil {
			err = st.Delete("nopewarden-test.txt")
		}
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"status": "ok", "message": "Connected. A small test file was saved there and removed again."})
	})
	// Make a backup now and send it to every place.
	s.HandleAdmin("POST /api/backups/offsite/send", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, err := a.saveBackupBy("manual", c.User.Username)
		if err != nil {
			fail(w, err)
			return
		}
		st := a.offsiteSend(p)
		api.JSON(w, 200, map[string]any{"name": filepath.Base(p), "status": st})
	})
	// Sign in to Dropbox, OneDrive or Google Drive. Save the place first, so the app ID is stored.
	s.HandleAdmin("POST /api/backups/offsite/{id}/signin/start", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		d := a.offsiteDest(r.PathValue("id"))
		if d == nil || d.Cloud == nil {
			fail(w, errors.New("save this place first, then sign in"))
			return
		}
		var f *offsite.Flow
		var err error
		switch d.Kind {
		case "dropbox":
			f = offsite.DropboxStart(d.Cloud.ClientID)
		case "onedrive":
			f, err = offsite.DeviceStart("onedrive", d.Cloud.ClientID, "")
		default:
			f, err = offsite.DeviceStart("google", d.Cloud.ClientID, a.openSecret(d.Cloud.Secret))
		}
		if err != nil {
			fail(w, err)
			return
		}
		offFlowMu.Lock()
		offFlows[d.ID] = f
		offFlowMu.Unlock()
		api.JSON(w, 200, f)
	})
	s.HandleAdmin("POST /api/backups/offsite/{id}/signin/finish", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Code string `json:"code"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		id := r.PathValue("id")
		offFlowMu.Lock()
		f := offFlows[id]
		offFlowMu.Unlock()
		if f == nil {
			fail(w, errors.New("start the sign-in first"))
			return
		}
		var refresh string
		var err error
		if f.Kind == "dropbox" {
			refresh, err = offsite.DropboxFinish(f, in.Code)
		} else {
			var done bool
			refresh, done, err = offsite.DevicePoll(f)
			if err == nil && !done {
				api.JSON(w, 200, map[string]any{"status": "pending"})
				return
			}
		}
		if err != nil {
			fail(w, err)
			return
		}
		offFlowMu.Lock()
		delete(offFlows, id)
		offFlowMu.Unlock()
		a.saveRefresh(id, refresh)
		a.event(c.User.Username, "info", "Signed in to a backup copy place", "")
		api.JSON(w, 200, map[string]any{"status": "ok"})
	})
}

func (a *App) offsiteDest(id string) *offDest {
	for _, x := range a.offsiteConfig().Dests {
		if x.ID == id {
			x := x
			return &x
		}
	}
	return nil
}

// USB drives that are plugged in, and the key that SFTP places use.
func (a *App) registerOffsiteDeviceRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/backups/offsite/usb", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		d, err := a.Net.UsbList()
		if err != nil {
			fail(w, err)
			return
		}
		if d == nil {
			d = []netd.UsbDisk{}
		}
		api.JSON(w, 200, map[string]any{"drives": d})
	})
	s.HandleAdmin("GET /api/backups/offsite/sftp-key", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"public": a.sftpPublicKey()})
	})
	s.HandleAdmin("POST /api/backups/offsite/sftp-key", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if k := a.sftpPublicKey(); k != "" {
			api.JSON(w, 200, map[string]any{"public": k})
			return
		}
		key := filepath.Join(a.offsiteDir(), "sftp_ed25519")
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "nopewarden-backup", "-f", key).CombinedOutput(); err != nil {
			if _, e := exec.LookPath("ssh-keygen"); e != nil {
				fail(w, errors.New("the ssh tools are not installed on this machine (apt install openssh-client)"))
				return
			}
			fail(w, errors.New("the key could not be made: "+strings.TrimSpace(string(out))))
			return
		}
		api.JSON(w, 200, map[string]any{"public": a.sftpPublicKey()})
	})
	// Trust a new key from a server whose key changed on purpose (a new server, a reinstall).
	s.HandleAdmin("POST /api/backups/offsite/{id}/forget-key", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		d := a.offsiteDest(r.PathValue("id"))
		if d == nil || d.SFTP == nil {
			fail(w, errors.New("that place was not found"))
			return
		}
		exec.Command("ssh-keygen", "-q", "-R", d.SFTP.Host, "-f", filepath.Join(a.offsiteDir(), "known_hosts")).Run()
		a.event(c.User.Username, "info", "Trusted a new server key for a backup copy place", d.Label)
		ok(w)
	})
}

func (a *App) sftpPublicKey() string {
	b, err := os.ReadFile(filepath.Join(a.offsiteDir(), "sftp_ed25519.pub"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
