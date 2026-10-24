package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Release is the newest published version.
type Release struct {
	Version   string `json:"version"`
	Notes     string `json:"notes"`
	Published string `json:"published"`
	URL       string `json:"-"` // the package
	SigURL    string `json:"-"`
	Size      int64  `json:"size"`
}

// Source is where releases come from.
type Source struct {
	API       string // https://api.github.com/repos/OWNER/REPO/releases/latest
	AllowHTTP bool   // tests only
	Client    *http.Client
}

func (s Source) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s Source) ok(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(s.AllowHTTP && u.Scheme == "http")) {
		return errors.New("the update address is not a secure web address")
	}
	return nil
}

func (s Source) get(ctx context.Context, raw string, max int64) ([]byte, error) {
	if err := s.ok(raw); err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
	req.Header.Set("User-Agent", "gatehouse-updater")
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the update server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("no release has been published yet")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("the update server answered %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("the download was interrupted: %v", err)
	}
	if int64(len(b)) > max {
		return nil, errors.New("the download is larger than expected")
	}
	return b, nil
}

// Latest asks for the newest published release that has an update package.
func (s Source) Latest(ctx context.Context) (*Release, error) {
	b, err := s.get(ctx, s.API, 4<<20)
	if err != nil {
		return nil, err
	}
	var in struct {
		Tag       string `json:"tag_name"`
		Body      string `json:"body"`
		Published string `json:"published_at"`
		Assets    []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if json.Unmarshal(b, &in) != nil || in.Tag == "" {
		return nil, errors.New("the update server's answer was not understood")
	}
	r := &Release{Version: strings.TrimPrefix(in.Tag, "v"), Notes: in.Body, Published: in.Published}
	if len(r.Notes) > 4000 {
		r.Notes = r.Notes[:4000]
	}
	for _, a := range in.Assets {
		switch {
		case strings.HasPrefix(a.Name, "gatehouse-update-") && strings.HasSuffix(a.Name, ".tar.gz"):
			r.URL, r.Size = a.URL, a.Size
		case strings.HasPrefix(a.Name, "gatehouse-update-") && strings.HasSuffix(a.Name, ".tar.gz.sig"):
			r.SigURL = a.URL
		}
	}
	if r.URL == "" || r.SigURL == "" {
		return nil, errors.New("the newest release has no update package, only an installer image")
	}
	return r, nil
}

// Fetch downloads a release's package and signature.
func (s Source) Fetch(ctx context.Context, r *Release) (pkg []byte, sig string, err error) {
	if pkg, err = s.get(ctx, r.URL, maxPackage); err != nil {
		return nil, "", err
	}
	sb, err := s.get(ctx, r.SigURL, 4096)
	if err != nil {
		return nil, "", err
	}
	return pkg, string(sb), nil
}
