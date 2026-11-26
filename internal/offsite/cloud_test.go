package offsite

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// One fake that speaks enough of all three providers.
type fakeCloud struct {
	mu      sync.Mutex
	files   map[string][]byte
	refresh string
	polls   int
	gdID    map[string]string
	srv     *httptest.Server
}

func (f *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	body, _ := io.ReadAll(r.Body)
	js := func(v any) { json.NewEncoder(w).Encode(v) }
	auth := r.Header.Get("Authorization") == "Bearer acc1"
	switch {
	case p == "/token":
		r.Form, _ = url.ParseQuery(string(body))
		switch r.Form.Get("grant_type") {
		case "refresh_token":
			if r.Form.Get("refresh_token") != f.refresh {
				w.WriteHeader(400)
				js(map[string]string{"error": "invalid_grant", "error_description": "bad refresh"})
				return
			}
			js(map[string]any{"access_token": "acc1", "expires_in": 3600, "refresh_token": "ref2"})
		case "authorization_code":
			if r.Form.Get("code") != "PASTED" || r.Form.Get("code_verifier") == "" {
				w.WriteHeader(400)
				js(map[string]string{"error": "invalid_grant"})
				return
			}
			js(map[string]any{"access_token": "acc1", "expires_in": 3600, "refresh_token": "ref1"})
		default: // device code
			f.polls++
			if f.polls < 2 {
				w.WriteHeader(400)
				js(map[string]string{"error": "authorization_pending"})
				return
			}
			js(map[string]any{"access_token": "acc1", "expires_in": 3600, "refresh_token": "ref1"})
		}
	case p == "/device":
		js(map[string]any{"device_code": "dc", "user_code": "ABCD-EFGH", "verification_uri": "https://example.com/device", "interval": 5})
	case !auth && !strings.HasPrefix(p, "/up/"):
		w.WriteHeader(401)
	// Dropbox
	case p == "/db/2/files/upload":
		var a map[string]any
		json.Unmarshal([]byte(r.Header.Get("Dropbox-API-Arg")), &a)
		f.files[a["path"].(string)] = body
		js(map[string]any{})
	case p == "/db/2/files/list_folder":
		es := []map[string]any{}
		for k, v := range f.files {
			es = append(es, map[string]any{".tag": "file", "name": strings.TrimPrefix(k, "/"), "size": len(v), "server_modified": "2026-11-26T10:00:00Z"})
		}
		js(map[string]any{"entries": es, "has_more": false})
	case p == "/db/2/files/delete_v2":
		var a map[string]string
		json.Unmarshal(body, &a)
		delete(f.files, a["path"])
		js(map[string]any{})
	// OneDrive
	case strings.HasPrefix(p, "/od:") && strings.HasSuffix(p, ":/content"):
		f.files["/"+strings.TrimSuffix(strings.TrimPrefix(p, "/od:/"), ":/content")] = body
		w.WriteHeader(201)
	case strings.HasSuffix(p, ":/createUploadSession"):
		n := strings.TrimSuffix(strings.TrimPrefix(p, "/od:/"), ":/createUploadSession")
		js(map[string]any{"uploadUrl": f.srv.URL + "/up/od/" + n})
	case strings.HasPrefix(p, "/up/od/"):
		n := "/" + strings.TrimPrefix(p, "/up/od/")
		f.files[n] = append(f.files[n], body...)
		w.WriteHeader(202)
	case p == "/od/children":
		vs := []map[string]any{}
		for k, v := range f.files {
			vs = append(vs, map[string]any{"name": strings.TrimPrefix(k, "/"), "size": len(v), "lastModifiedDateTime": "2026-11-26T10:00:00Z", "file": map[string]any{}})
		}
		js(map[string]any{"value": vs})
	case r.Method == "DELETE" && strings.HasPrefix(p, "/od:/"):
		delete(f.files, "/"+strings.TrimPrefix(p, "/od:/"))
		w.WriteHeader(204)
	// Google Drive
	case p == "/drive/v3/files" && r.Method == "GET":
		q := r.URL.Query().Get("q")
		if strings.Contains(q, "google-apps.folder") {
			js(map[string]any{"files": []map[string]any{}})
			return
		}
		fs := []map[string]any{}
		for k, v := range f.files {
			fs = append(fs, map[string]any{"id": "id-" + strings.TrimPrefix(k, "/"), "name": strings.TrimPrefix(k, "/"), "size": itoa(len(v)), "modifiedTime": "2026-11-26T10:00:00Z"})
		}
		js(map[string]any{"files": fs})
	case p == "/drive/v3/files" && r.Method == "POST":
		js(map[string]any{"id": "folder1"})
	case p == "/upload/drive/v3/files":
		var m map[string]any
		json.Unmarshal(body, &m)
		w.Header().Set("Location", f.srv.URL+"/up/gd/"+m["name"].(string))
		w.WriteHeader(200)
	case strings.HasPrefix(p, "/up/gd/"):
		f.files["/"+strings.TrimPrefix(p, "/up/gd/")] = body
		js(map[string]any{})
	case r.Method == "DELETE" && strings.HasPrefix(p, "/drive/v3/files/id-"):
		delete(f.files, "/"+strings.TrimPrefix(p, "/drive/v3/files/id-"))
		w.WriteHeader(204)
	default:
		w.WriteHeader(404)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func setup(t *testing.T) (*fakeCloud, *Token) {
	f := &fakeCloud{files: map[string][]byte{}, refresh: "ref1"}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	DropboxAPI, DropboxContent = f.srv.URL+"/db", f.srv.URL+"/db"
	GraphBase = f.srv.URL + "/od"
	GoogleAPI = f.srv.URL
	DropboxTokenURL, MSTokenURL, GoogleTokenURL = f.srv.URL+"/token", f.srv.URL+"/token", f.srv.URL+"/token"
	MSDeviceURL, GoogleDeviceURL = f.srv.URL+"/device", f.srv.URL+"/device"
	return f, &Token{TokenURL: f.srv.URL + "/token", ClientID: "cid", Refresh: "ref1"}
}

func roundTrip(t *testing.T, s Store, f *fakeCloud, big bool) {
	data := []byte("scrambled")
	if big {
		data = []byte(strings.Repeat("x", 3<<20+10))
	}
	if err := s.Put("auto-20261126-100000.ghbk", data); err != nil {
		t.Fatal(err)
	}
	items, err := s.List()
	if err != nil || len(items) != 1 || items[0].Name != "auto-20261126-100000.ghbk" {
		t.Fatalf("%v %v", items, err)
	}
	if len(f.files["/auto-20261126-100000.ghbk"]) != len(data) {
		t.Fatalf("stored %d bytes, sent %d", len(f.files["/auto-20261126-100000.ghbk"]), len(data))
	}
	if err := s.Delete("auto-20261126-100000.ghbk"); err != nil {
		t.Fatal(err)
	}
	if len(f.files) != 0 {
		t.Fatal("not deleted")
	}
}

func TestDropbox(t *testing.T) {
	f, tok := setup(t)
	roundTrip(t, &Dropbox{Tok: tok}, f, false)
}

func TestOneDrive(t *testing.T) {
	f, tok := setup(t)
	roundTrip(t, &OneDrive{Tok: tok}, f, false)
	f.files = map[string][]byte{}
	roundTrip(t, &OneDrive{Tok: tok}, f, true) // over 3 MB goes in pieces
}

func TestGDrive(t *testing.T) {
	f, tok := setup(t)
	roundTrip(t, &GDrive{Tok: tok}, f, false)
}

func TestTokenRotation(t *testing.T) {
	_, tok := setup(t)
	saved := ""
	tok.Saved = func(r string) { saved = r }
	if _, err := tok.Access(); err != nil || saved != "ref2" {
		t.Fatalf("%v %q", err, saved)
	}
	bad := &Token{TokenURL: tok.TokenURL, ClientID: "cid", Refresh: "nope"}
	if _, err := bad.Access(); err == nil || !strings.Contains(err.Error(), "bad refresh") {
		t.Fatalf("%v", err)
	}
}

func TestSignInFlows(t *testing.T) {
	fc, _ := setup(t)
	fl := DropboxStart("key")
	if !strings.Contains(fl.URL, "code_challenge_method=S256") || !strings.Contains(fl.URL, "token_access_type=offline") {
		t.Fatal(fl.URL)
	}
	if _, err := DropboxFinish(fl, "wrong"); err == nil {
		t.Fatal("a wrong code is refused")
	}
	if r, err := DropboxFinish(fl, " PASTED "); err != nil || r != "ref1" {
		t.Fatalf("%v %v", r, err)
	}
	for _, kind := range []string{"onedrive", "google"} {
		fc.polls = 0
		d, err := DeviceStart(kind, "cid", "sec")
		if err != nil || d.UserCode != "ABCD-EFGH" {
			t.Fatalf("%v %v", d, err)
		}
		r, done, err := DevicePoll(d)
		if err != nil || done || r != "" {
			t.Fatalf("still pending: %v %v %v", r, done, err)
		}
		if r, done, err = DevicePoll(d); err != nil || !done || r != "ref1" {
			t.Fatalf("%v %v %v", r, done, err)
		}
		d2 := &Flow{}
		_ = d2
	}
}
