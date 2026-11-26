package app

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"nopewarden/internal/netd"
	"nopewarden/internal/offsite"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type tinyDAV struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (f *tinyDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case "MKCOL":
		w.WriteHeader(201)
	case "PUT":
		b, _ := io.ReadAll(r.Body)
		f.files[r.URL.Path] = b
		w.WriteHeader(201)
	case "DELETE":
		delete(f.files, r.URL.Path)
		w.WriteHeader(204)
	case "PROPFIND":
		w.WriteHeader(207)
		w.Write([]byte(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>` + r.URL.Path + `</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>`))
		for k, v := range f.files {
			w.Write([]byte(`<d:response><d:href>` + k + `</d:href><d:propstat><d:prop><d:getlastmodified>Thu, 26 Nov 2026 10:00:00 GMT</d:getlastmodified><d:getcontentlength>` + strconv.Itoa(len(v)) + `</d:getcontentlength><d:resourcetype/></d:prop></d:propstat></d:response>`))
		}
		w.Write([]byte(`</d:multistatus>`))
	}
}

func TestOffsiteCopies(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	dav := &tinyDAV{files: map[string][]byte{}}
	srv := httptest.NewServer(dav)
	defer srv.Close()
	cfg := func(pw string, keep int, enabled bool) map[string]any {
		return map[string]any{"password": pw, "keep": keep, "remind_on": true, "remind_days": 30,
			"dests": []map[string]any{{"id": "d1", "kind": "webdav", "label": "My NAS", "enabled": enabled,
				"dav": map[string]any{"url": srv.URL + "/dav", "dir": "bk", "user": "me", "pass": "pw"}}}}
	}
	if code, _, _ := c.call("PUT", "/api/backups/offsite", cfg("", 2, true)); code != 400 {
		t.Fatalf("a place cannot be switched on without a copy password: %d", code)
	}
	if code, _, raw := c.call("PUT", "/api/backups/offsite", cfg("a long password", 1, true)); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	_, _, raw := c.call("GET", "/api/backups/offsite", nil)
	if bytes.Contains(raw, []byte("a long password")) || bytes.Contains(raw, []byte(`"pass":"`)) && !bytes.Contains(raw, []byte(`"pass":""`)) {
		t.Fatalf("secrets must not reach the console: %s", raw)
	}
	if !strings.Contains(string(raw), `"pass_set":true`) || !strings.Contains(string(raw), `"password_set":true`) {
		t.Fatalf("the console should be told secrets are set: %s", raw)
	}
	if code, _, raw := c.call("POST", "/api/backups/offsite/d1/test", nil); code != 200 {
		t.Fatalf("test failed: %d %s", code, raw)
	}
	// Saving again with the secrets left blank keeps them.
	c2 := cfg("", 1, true)
	c2["dests"].([]map[string]any)[0]["dav"].(map[string]any)["pass"] = ""
	if code, _, raw := c.call("PUT", "/api/backups/offsite", c2); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	for i := 0; i < 2; i++ {
		code, m, raw := c.call("POST", "/api/backups/offsite/send", nil)
		if code != 200 {
			t.Fatalf("%d %s", code, raw)
		}
		st := m["status"].(map[string]any)["d1"].(map[string]any)
		if st["ok"] != true {
			t.Fatalf("not sent: %v", st)
		}
		// Backup names carry the second they were made in; keep the two apart.
		timeSleepSecond()
	}
	dav.mu.Lock()
	defer dav.mu.Unlock()
	n := 0
	for k, v := range dav.files {
		n++
		if !bytes.HasPrefix(v, []byte(bkEncMagic)) {
			t.Fatalf("%s was not encrypted", k)
		}
		if _, err := decrypt(v, "a long password"); err != nil {
			t.Fatalf("copy does not open with the password: %v", err)
		}
	}
	if n != 1 {
		t.Fatalf("only the newest copy should stay (keep 1), have %d", n)
	}
}

func TestAttachmentMessage(t *testing.T) {
	b := attachmentMessage("a@b.c", "d@e.f", "backup copy x", "hello", "auto-20261126-100000.ghbk", []byte(strings.Repeat("xyz", 100)))
	m, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, _ := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if mt != "multipart/mixed" {
		t.Fatal(mt)
	}
	mr := multipart.NewReader(m.Body, params["boundary"])
	parts := 0
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		parts++
		if p.FileName() != "" && p.FileName() != "auto-20261126-100000.ghbk" {
			t.Fatal(p.FileName())
		}
	}
	if parts != 2 {
		t.Fatalf("parts %d", parts)
	}
}

func timeSleepSecond() { time.Sleep(1100 * time.Millisecond) }

func TestOffsiteDropboxSignIn(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	files := map[string][]byte{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/token":
			if strings.Contains(string(b), "authorization_code") && !strings.Contains(string(b), "PASTED") {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			w.Write([]byte(`{"access_token":"a","expires_in":3600,"refresh_token":"r1"}`))
		case "/up/2/files/upload":
			files[r.Header.Get("Dropbox-API-Arg")] = b
		case "/api/2/files/delete_v2":
			files = map[string][]byte{}
		case "/api/2/files/list_folder":
			w.Write([]byte(`{"entries":[],"has_more":false}`))
		}
	}))
	defer srv.Close()
	oldT, oldA, oldC := offsite.DropboxTokenURL, offsite.DropboxAPI, offsite.DropboxContent
	offsite.DropboxTokenURL, offsite.DropboxAPI, offsite.DropboxContent = srv.URL+"/token", srv.URL+"/api", srv.URL+"/up"
	defer func() { offsite.DropboxTokenURL, offsite.DropboxAPI, offsite.DropboxContent = oldT, oldA, oldC }()
	cfg := func(enabled bool) map[string]any {
		return map[string]any{"password": "a long password", "keep": 3, "remind_on": true, "remind_days": 30,
			"dests": []map[string]any{{"id": "dbx", "kind": "dropbox", "label": "My Dropbox", "enabled": enabled, "cloud": map[string]any{"client_id": "appkey"}}}}
	}
	if code, _, _ := c.call("PUT", "/api/backups/offsite", cfg(true)); code != 400 {
		t.Fatalf("cannot be turned on before signing in: %d", code)
	}
	if code, _, raw := c.call("PUT", "/api/backups/offsite", cfg(false)); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	code, m, raw := c.call("POST", "/api/backups/offsite/dbx/signin/start", nil)
	if code != 200 || !strings.Contains(m["url"].(string), "client_id=appkey") {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _, _ := c.call("POST", "/api/backups/offsite/dbx/signin/finish", map[string]string{"code": "wrong"}); code == 200 {
		t.Fatal("a wrong code is refused")
	}
	if code, _, raw := c.call("POST", "/api/backups/offsite/dbx/signin/finish", map[string]string{"code": "PASTED"}); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	_, _, raw = c.call("GET", "/api/backups/offsite", nil)
	if !strings.Contains(string(raw), `"signed_in":true`) || strings.Contains(string(raw), `"refresh":"`) {
		t.Fatalf("signed in, and the token stays hidden: %s", raw)
	}
	if code, _, raw := c.call("PUT", "/api/backups/offsite", cfg(true)); code != 200 {
		t.Fatalf("sign-in survives a save: %d %s", code, raw)
	}
	if code, _, raw := c.call("POST", "/api/backups/offsite/dbx/test", nil); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestOffsiteUSB(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	fn.mu.Lock()
	fn.usbDisks = []netd.UsbDisk{{UUID: "ABCD-1234", Label: "BACKUP", FSType: "vfat", Size: 8e9}}
	fn.mu.Unlock()
	code, m, raw := c.call("GET", "/api/backups/offsite/usb", nil)
	if code != 200 || len(m["drives"].([]any)) != 1 {
		t.Fatalf("%d %s", code, raw)
	}
	cfg := map[string]any{"password": "a long password", "keep": 3, "remind_on": true, "remind_days": 30,
		"dests": []map[string]any{{"id": "u1", "kind": "usb", "label": "Stick", "enabled": true, "usb": map[string]any{"uuid": "ABCD-1234"}}}}
	if code, _, raw := c.call("PUT", "/api/backups/offsite", cfg); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _, raw := c.call("POST", "/api/backups/offsite/u1/test", nil); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _, raw := c.call("POST", "/api/backups/offsite/send", nil); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	fn.mu.Lock()
	defer fn.mu.Unlock()
	if len(fn.usbCopies) != 1 || !strings.HasPrefix(fn.usbCopies[0], "ABCD-1234 manual-") {
		t.Fatalf("%v", fn.usbCopies)
	}
	left, _ := os.ReadDir(c.app.Cfg.DataDir + "/offsite-out")
	if len(left) != 0 {
		t.Fatalf("the staged copy is removed afterwards: %v", left)
	}
}
