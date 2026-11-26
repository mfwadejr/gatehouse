package offsite

import (
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The key derivation matches the worked example in Amazon's Signature Version 4 documentation.
func TestSigningKeyVector(t *testing.T) {
	k := signingKey("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20150830", "us-east-1", "iam")
	if got := hex.EncodeToString(k); got != "c4afb1cc5771d871763a393e44b703571b55cc28424d1a5e86da6ed3c154a4b9" {
		t.Fatalf("signing key %s", got)
	}
	if uriEncode("a b/c+d~e", true) != "a%20b/c%2Bd~e" || uriEncode("a/b", false) != "a%2Fb" {
		t.Fatal("uri encoding")
	}
}

type fakeS3 struct {
	mu    sync.Mutex
	files map[string][]byte
	auth  []string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKID/") || r.Header.Get("x-amz-content-sha256") == "" {
		w.WriteHeader(403)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/bucket/")
	switch r.Method {
	case "PUT":
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("x-amz-content-sha256") != sha(b) {
			w.WriteHeader(400)
			return
		}
		f.files[path] = b
	case "DELETE":
		delete(f.files, path)
		w.WriteHeader(204)
	case "GET":
		prefix := r.URL.Query().Get("prefix")
		var keys []string
		for k := range f.files {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		fmt.Fprint(w, `<?xml version="1.0"?><ListBucketResult><IsTruncated>false</IsTruncated>`)
		for _, k := range keys {
			fmt.Fprintf(w, "<Contents><Key>%s</Key><LastModified>2026-11-26T10:00:00.000Z</LastModified><Size>%d</Size></Contents>", k, len(f.files[k]))
		}
		fmt.Fprint(w, `</ListBucketResult>`)
	}
}

func TestS3RoundTrip(t *testing.T) {
	f := &fakeS3{files: map[string][]byte{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	s := &S3{Endpoint: srv.URL, Region: "us-east-1", Bucket: "bucket", Prefix: "nw", AccessKey: "AKID", SecretKey: "secret", PathStyle: true}
	if err := s.Put("auto-1.ghbk", []byte("scrambled")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("auto-2.ghbk", []byte("more")); err != nil {
		t.Fatal(err)
	}
	f.files["other/elsewhere.ghbk"] = []byte("not mine") // another folder is never listed
	items, err := s.List()
	if err != nil || len(items) != 2 || items[0].Name != "auto-1.ghbk" || items[0].Size != 9 {
		t.Fatalf("%v %+v", err, items)
	}
	if err := s.Delete("auto-1.ghbk"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.files["nw/auto-1.ghbk"]; ok {
		t.Fatal("not removed")
	}
	if s.Put("../x", nil) == nil || s.Put("a/b", nil) == nil {
		t.Fatal("odd names are refused")
	}
	bad := &S3{Endpoint: srv.URL, Region: "us-east-1", Bucket: "bucket", AccessKey: "WRONG", SecretKey: "x", PathStyle: true}
	if err := bad.Put("a.ghbk", []byte("x")); err == nil || !strings.Contains(err.Error(), "refused the login") {
		t.Fatalf("a refused login is explained: %v", err)
	}
	if _, err := (&S3{Endpoint: "http://example.com", Region: "r", Bucket: "b", AccessKey: "a", SecretKey: "s"}).List(); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("plain http over the internet is refused: %v", err)
	}
}

type fakeDAV struct {
	mu    sync.Mutex
	dirs  map[string]bool
	files map[string][]byte
}

func (f *fakeDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, p, ok := r.BasicAuth(); !ok || u != "me" || p != "pw" {
		w.WriteHeader(401)
		return
	}
	switch r.Method {
	case "MKCOL":
		if f.dirs[r.URL.Path] {
			w.WriteHeader(405)
			return
		}
		f.dirs[r.URL.Path] = true
		w.WriteHeader(201)
	case "PUT":
		dir := r.URL.Path[:strings.LastIndex(r.URL.Path, "/")+1]
		if !f.dirs[dir] {
			w.WriteHeader(409)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.files[r.URL.Path] = b
		w.WriteHeader(201)
	case "DELETE":
		delete(f.files, r.URL.Path)
		w.WriteHeader(204)
	case "PROPFIND":
		if !f.dirs[r.URL.Path] {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(207)
		fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
		fmt.Fprintf(w, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>`, r.URL.Path)
		var keys []string
		for k := range f.files {
			if strings.HasPrefix(k, r.URL.Path) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:getlastmodified>Thu, 26 Nov 2026 10:00:00 GMT</d:getlastmodified><d:getcontentlength>%d</d:getcontentlength><d:resourcetype/></d:prop></d:propstat></d:response>`, k, len(f.files[k]))
		}
		fmt.Fprint(w, `</d:multistatus>`)
	}
}

func TestWebDAVRoundTrip(t *testing.T) {
	f := &fakeDAV{dirs: map[string]bool{"/dav/": true}, files: map[string][]byte{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	w := &WebDAV{URL: srv.URL + "/dav", Dir: "Backups/NopeWarden", User: "me", Pass: "pw"}
	if err := w.Put("auto-1.ghbk", []byte("scrambled")); err != nil {
		t.Fatal(err)
	}
	if !f.dirs["/dav/Backups/"] || !f.dirs["/dav/Backups/NopeWarden/"] {
		t.Fatalf("folders made one level at a time: %v", f.dirs)
	}
	if err := w.Put("auto-2.ghbk", []byte("more")); err != nil { // the folder is already there
		t.Fatal(err)
	}
	items, err := w.List()
	if err != nil || len(items) != 2 || items[0].Name != "auto-1.ghbk" || items[0].Size != 9 || items[0].Modified.Year() != 2026 {
		t.Fatalf("%v %+v", err, items)
	}
	if err := w.Delete("auto-1.ghbk"); err != nil {
		t.Fatal(err)
	}
	if items, _ = w.List(); len(items) != 1 {
		t.Fatalf("%+v", items)
	}
	bad := &WebDAV{URL: srv.URL + "/dav", Dir: "x", User: "me", Pass: "nope"}
	if err := bad.Put("a.ghbk", []byte("x")); err == nil || !strings.Contains(err.Error(), "refused the login") {
		t.Fatalf("%v", err)
	}
	if items, err := (&WebDAV{URL: srv.URL + "/dav", Dir: "empty", User: "me", Pass: "pw"}).List(); err != nil || len(items) != 0 {
		t.Fatalf("a folder not made yet lists as empty: %v %v", items, err)
	}
	_ = time.Now
	_ = xml.Name{}
}
