package offsite

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WebDAV is a WebDAV folder: Nextcloud, ownCloud, a Synology or QNAP share, or any WebDAV server.
type WebDAV struct {
	URL  string // the server's WebDAV address
	Dir  string // a folder inside it, made if missing
	User string
	Pass string
}

func (w *WebDAV) folder() (*url.URL, error) {
	u, err := checkURL(w.URL)
	if err != nil {
		return nil, err
	}
	p := strings.TrimRight(u.Path, "/")
	for _, seg := range strings.Split(strings.Trim(w.Dir, "/"), "/") {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		p += "/" + seg
	}
	u.Path = p + "/"
	u.RawPath = ""
	return u, nil
}

func (w *WebDAV) do(method string, u *url.URL, body []byte, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if w.User != "" {
		req.SetBasicAuth(w.User, w.Pass)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the server: %v", err)
	}
	return resp, nil
}

// ensure makes the folder (and each folder above it, below the server address) when it is missing.
func (w *WebDAV) ensure() error {
	root, err := checkURL(w.URL)
	if err != nil {
		return err
	}
	base := strings.TrimRight(root.Path, "/")
	cur := *root
	for _, seg := range strings.Split(strings.Trim(w.Dir, "/"), "/") {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		base += "/" + seg
		cur.Path, cur.RawPath = base+"/", ""
		resp, err := w.do("MKCOL", &cur, nil, nil)
		if err != nil {
			return err
		}
		resp.Body.Close()
		// 201 made it; 405 means it is already there; 301 is a server tidying the address.
		if resp.StatusCode != 201 && resp.StatusCode != 405 && resp.StatusCode != 301 && resp.StatusCode != 200 && resp.StatusCode != 204 {
			if resp.StatusCode == 401 || resp.StatusCode == 403 {
				return errors.New("the server refused the login or the permission to make the folder. Check the user name and password")
			}
			return fmt.Errorf("could not make the folder %q: the server answered %d", seg, resp.StatusCode)
		}
	}
	return nil
}

func (w *WebDAV) Put(name string, data []byte) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	if err := w.ensure(); err != nil {
		return err
	}
	f, err := w.folder()
	if err != nil {
		return err
	}
	f.Path += name
	resp, err := w.do("PUT", f, data, map[string]string{"Content-Type": "application/octet-stream"})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 204 {
		return statusErr("sending the backup", resp)
	}
	return nil
}

func (w *WebDAV) Delete(name string) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	f, err := w.folder()
	if err != nil {
		return err
	}
	f.Path += name
	resp, err := w.do("DELETE", f, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != 404 {
		return statusErr("removing an old backup", resp)
	}
	return nil
}

type davMulti struct {
	Responses []struct {
		Href  string `xml:"href"`
		Props []struct {
			Modified string    `xml:"prop>getlastmodified"`
			Size     int64     `xml:"prop>getcontentlength"`
			IsDir    *struct{} `xml:"prop>resourcetype>collection"`
		} `xml:"propstat"`
	} `xml:"response"`
}

func (w *WebDAV) List() ([]Item, error) {
	f, err := w.folder()
	if err != nil {
		return nil, err
	}
	resp, err := w.do("PROPFIND", f, []byte(`<?xml version="1.0"?><propfind xmlns="DAV:"><prop><getlastmodified/><getcontentlength/><resourcetype/></prop></propfind>`), map[string]string{"Depth": "1", "Content-Type": "application/xml"})
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil // nothing made yet
	}
	if resp.StatusCode != 207 && resp.StatusCode != 200 {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil, statusErr("looking in the folder", resp)
	}
	var m davMulti
	if err := xml.Unmarshal(body, &m); err != nil {
		return nil, errors.New("the server answered in a way NopeWarden does not understand")
	}
	var out []Item
	for _, r := range m.Responses {
		h, err := url.PathUnescape(r.Href)
		if err != nil || strings.HasSuffix(h, "/") {
			continue
		}
		n := h[strings.LastIndex(h, "/")+1:]
		if !ValidName(n) {
			continue
		}
		it := Item{Name: n}
		for _, p := range r.Props {
			if p.Size > 0 {
				it.Size = p.Size
			}
			if p.Modified != "" {
				if t, err := time.Parse(time.RFC1123, p.Modified); err == nil {
					it.Modified = t
				} else if t, err := time.Parse(time.RFC1123Z, p.Modified); err == nil {
					it.Modified = t
				}
			}
		}
		out = append(out, it)
	}
	return out, nil
}
