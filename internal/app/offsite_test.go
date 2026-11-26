package app

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
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
