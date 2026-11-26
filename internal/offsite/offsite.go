// Package offsite sends backup files to places that are not this machine: S3-style cloud storage and
// WebDAV (Nextcloud, ownCloud, Synology and others). Only the standard library is used, so there is
// nothing extra to install. Every file sent has already been encrypted with the person's copy
// password, so a store only ever holds scrambled bytes.
package offsite

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Item is one file in a store.
type Item struct {
	Name     string    `json:"name"`
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
}

// Store is a place backups can be kept.
type Store interface {
	Put(name string, data []byte) error
	List() ([]Item, error)
	Delete(name string) error
}

var httpClient = &http.Client{Timeout: 5 * time.Minute}

// SetClient replaces the HTTP client; tests use it.
func SetClient(c *http.Client) { httpClient = c }

// ValidName is a file name this package will send or remove: letters, digits, dot, dash, underscore.
func ValidName(n string) bool {
	if n == "" || len(n) > 200 || strings.HasPrefix(n, ".") {
		return false
	}
	for _, r := range n {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// plainHTTPOK is true for an address on this network or this machine, where sending a password
// without encryption is the person's own choice; anywhere else, https is required.
func plainHTTPOK(u *url.URL) bool {
	h := u.Hostname()
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
	}
	return h == "localhost" || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") || !strings.Contains(h, ".")
}

func checkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, errors.New("that is not a web address; start it with https://")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !plainHTTPOK(u) {
			return nil, errors.New("this address would send your password across the internet without encryption; use https://")
		}
	default:
		return nil, errors.New("the address must start with https://")
	}
	return u, nil
}

func statusErr(what string, resp *http.Response) error {
	body := make([]byte, 300)
	n, _ := resp.Body.Read(body)
	msg := strings.TrimSpace(string(body[:n]))
	switch resp.StatusCode {
	case 401, 403:
		return fmt.Errorf("%s: the server refused the login or the permission (%d). Check the user name and password", what, resp.StatusCode)
	case 404:
		return fmt.Errorf("%s: the server says that place does not exist (404). Check the address and folder", what)
	case 507:
		return fmt.Errorf("%s: the store is full (507)", what)
	}
	if msg != "" {
		return fmt.Errorf("%s: the server answered %d: %s", what, resp.StatusCode, firstLine(msg))
	}
	return fmt.Errorf("%s: the server answered %d", what, resp.StatusCode)
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
