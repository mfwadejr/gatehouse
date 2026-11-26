package offsite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Stores for Dropbox, OneDrive and Google Drive. Each is limited to the one folder the person's app
// can reach (Dropbox App folder, OneDrive App folder, Google Drive "NopeWarden Backups"), so a
// stolen sign-in cannot read the rest of their files.

var (
	DropboxAPI     = "https://api.dropboxapi.com"
	DropboxContent = "https://content.dropboxapi.com"
	GraphBase      = "https://graph.microsoft.com/v1.0/me/drive/special/approot"
	GoogleAPI      = "https://www.googleapis.com"
)

func bearer(t *Token, req *http.Request) error {
	a, err := t.Access()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a)
	return nil
}

func call(t *Token, what, method, u string, hdr map[string]string, body []byte) ([]byte, int, error) {
	if _, err := checkURL(u); err != nil {
		return nil, 0, err
	}
	req, _ := http.NewRequest(method, u, bytes.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if t != nil {
		if err := bearer(t, req); err != nil {
			return nil, 0, err
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: could not reach the service: %w", what, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		resp.Body = io.NopCloser(bytes.NewReader(b))
		return b, resp.StatusCode, statusErr(what, resp)
	}
	return b, resp.StatusCode, nil
}

func jsonBody(v any) []byte { b, _ := json.Marshal(v); return b }

// ---- Dropbox ----

type Dropbox struct{ Tok *Token }

func (d *Dropbox) Put(name string, data []byte) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	if len(data) > 140<<20 {
		return errors.New("Dropbox: the backup is over 140 MB, too big for a single upload")
	}
	arg := string(jsonBody(map[string]any{"path": "/" + name, "mode": "overwrite", "mute": true}))
	_, _, err := call(d.Tok, "Dropbox", "POST", DropboxContent+"/2/files/upload", map[string]string{"Dropbox-API-Arg": arg, "Content-Type": "application/octet-stream"}, data)
	return err
}

func (d *Dropbox) List() ([]Item, error) {
	var out []Item
	u, body := DropboxAPI+"/2/files/list_folder", jsonBody(map[string]any{"path": ""})
	for i := 0; i < 50; i++ {
		b, _, err := call(d.Tok, "Dropbox", "POST", u, map[string]string{"Content-Type": "application/json"}, body)
		if err != nil {
			return nil, err
		}
		var r struct {
			Entries []struct {
				Tag      string `json:".tag"`
				Name     string
				Size     int64
				Modified string `json:"server_modified"`
			}
			Cursor  string
			HasMore bool `json:"has_more"`
		}
		if json.Unmarshal(b, &r) != nil {
			return nil, errors.New("Dropbox: the answer was not understood")
		}
		for _, e := range r.Entries {
			if e.Tag == "file" {
				t, _ := time.Parse(time.RFC3339, e.Modified)
				out = append(out, Item{Name: e.Name, Size: e.Size, Modified: t})
			}
		}
		if !r.HasMore {
			return out, nil
		}
		u, body = DropboxAPI+"/2/files/list_folder/continue", jsonBody(map[string]any{"cursor": r.Cursor})
	}
	return out, nil
}

func (d *Dropbox) Delete(name string) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	_, _, err := call(d.Tok, "Dropbox", "POST", DropboxAPI+"/2/files/delete_v2", map[string]string{"Content-Type": "application/json"}, jsonBody(map[string]any{"path": "/" + name}))
	return err
}

// ---- OneDrive ----

type OneDrive struct{ Tok *Token }

func (o *OneDrive) Put(name string, data []byte) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	if len(data) <= 3<<20 {
		_, _, err := call(o.Tok, "OneDrive", "PUT", GraphBase+":/"+name+":/content", map[string]string{"Content-Type": "application/octet-stream"}, data)
		return err
	}
	b, _, err := call(o.Tok, "OneDrive", "POST", GraphBase+":/"+name+":/createUploadSession", map[string]string{"Content-Type": "application/json"},
		jsonBody(map[string]any{"item": map[string]any{"@microsoft.graph.conflictBehavior": "replace"}}))
	if err != nil {
		return err
	}
	var s struct {
		UploadURL string `json:"uploadUrl"`
	}
	if json.Unmarshal(b, &s) != nil || s.UploadURL == "" {
		return errors.New("OneDrive: no upload address was given")
	}
	const chunk = 327680 * 10
	for off := 0; off < len(data); off += chunk {
		end := off + chunk
		if end > len(data) {
			end = len(data)
		}
		// The upload address carries its own permission; no sign-in header goes with it.
		_, _, err := call(nil, "OneDrive", "PUT", s.UploadURL, map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", off, end-1, len(data)), "Content-Type": "application/octet-stream"}, data[off:end])
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *OneDrive) List() ([]Item, error) {
	var out []Item
	u := GraphBase + "/children?$top=200"
	for i := 0; i < 50 && u != ""; i++ {
		b, _, err := call(o.Tok, "OneDrive", "GET", u, nil, nil)
		if err != nil {
			return nil, err
		}
		var r struct {
			Value []struct {
				Name     string
				Size     int64
				Modified string `json:"lastModifiedDateTime"`
				File     *struct{}
			}
			Next string `json:"@odata.nextLink"`
		}
		if json.Unmarshal(b, &r) != nil {
			return nil, errors.New("OneDrive: the answer was not understood")
		}
		for _, e := range r.Value {
			if e.File != nil {
				t, _ := time.Parse(time.RFC3339, e.Modified)
				out = append(out, Item{Name: e.Name, Size: e.Size, Modified: t})
			}
		}
		u = r.Next
	}
	return out, nil
}

func (o *OneDrive) Delete(name string) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	_, _, err := call(o.Tok, "OneDrive", "DELETE", GraphBase+":/"+name, nil, nil)
	return err
}

// ---- Google Drive ----

const gdFolder = "NopeWarden Backups"

type GDrive struct {
	Tok    *Token
	folder string
}

func (g *GDrive) folderID() (string, error) {
	if g.folder != "" {
		return g.folder, nil
	}
	q := url.Values{"q": {"name='" + gdFolder + "' and mimeType='application/vnd.google-apps.folder' and trashed=false"}, "fields": {"files(id)"}}
	b, _, err := call(g.Tok, "Google Drive", "GET", GoogleAPI+"/drive/v3/files?"+q.Encode(), nil, nil)
	if err != nil {
		return "", err
	}
	var r struct{ Files []struct{ ID string } }
	json.Unmarshal(b, &r)
	if len(r.Files) > 0 {
		g.folder = r.Files[0].ID
		return g.folder, nil
	}
	b, _, err = call(g.Tok, "Google Drive", "POST", GoogleAPI+"/drive/v3/files?fields=id", map[string]string{"Content-Type": "application/json"},
		jsonBody(map[string]any{"name": gdFolder, "mimeType": "application/vnd.google-apps.folder"}))
	if err != nil {
		return "", err
	}
	var c struct{ ID string }
	if json.Unmarshal(b, &c) != nil || c.ID == "" {
		return "", errors.New("Google Drive: the folder could not be made")
	}
	g.folder = c.ID
	return g.folder, nil
}

type gdFile struct {
	ID       string
	Name     string
	Size     string
	Modified string `json:"modifiedTime"`
}

func (g *GDrive) files() ([]gdFile, error) {
	f, err := g.folderID()
	if err != nil {
		return nil, err
	}
	q := url.Values{"q": {"'" + f + "' in parents and trashed=false"}, "fields": {"files(id,name,size,modifiedTime)"}, "pageSize": {"1000"}}
	b, _, err := call(g.Tok, "Google Drive", "GET", GoogleAPI+"/drive/v3/files?"+q.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	var r struct{ Files []gdFile }
	if json.Unmarshal(b, &r) != nil {
		return nil, errors.New("Google Drive: the answer was not understood")
	}
	return r.Files, nil
}

func (g *GDrive) Put(name string, data []byte) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	f, err := g.folderID()
	if err != nil {
		return err
	}
	// Start a resumable upload, then send the whole file to the address it gives.
	a, err := g.Tok.Access()
	if err != nil {
		return err
	}
	if _, err := checkURL(GoogleAPI); err != nil {
		return err
	}
	req, _ := http.NewRequest("POST", GoogleAPI+"/upload/drive/v3/files?uploadType=resumable", bytes.NewReader(jsonBody(map[string]any{"name": name, "parents": []string{f}})))
	req.Header.Set("Authorization", "Bearer "+a)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Google Drive: could not reach the service: %w", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode >= 300 || loc == "" {
		return fmt.Errorf("Google Drive: the upload was refused (%d)", resp.StatusCode)
	}
	if !strings.HasPrefix(loc, "https://") && !strings.HasPrefix(loc, GoogleAPI) {
		return errors.New("Google Drive: an unexpected upload address was given")
	}
	_, _, err = call(nil, "Google Drive", "PUT", loc, map[string]string{"Content-Type": "application/octet-stream"}, data)
	return err
}

func (g *GDrive) List() ([]Item, error) {
	fs, err := g.files()
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(fs))
	for _, f := range fs {
		var n int64
		fmt.Sscan(f.Size, &n)
		t, _ := time.Parse(time.RFC3339, f.Modified)
		out = append(out, Item{Name: f.Name, Size: n, Modified: t})
	}
	return out, nil
}

func (g *GDrive) Delete(name string) error {
	fs, err := g.files()
	if err != nil {
		return err
	}
	for _, f := range fs {
		if f.Name == name {
			_, _, err := call(g.Tok, "Google Drive", "DELETE", GoogleAPI+"/drive/v3/files/"+url.PathEscape(f.ID), nil, nil)
			return err
		}
	}
	return nil
}
