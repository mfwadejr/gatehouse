package offsite

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Sign-in for Dropbox, OneDrive and Google Drive. The person registers their own app with the provider
// and pastes its ID, so nothing secret is shipped inside NopeWarden. Each provider uses the sign-in
// that suits a box with no web browser of its own: Dropbox shows a code to paste back, OneDrive and
// Google show a short code to type on a phone or computer. What is kept afterwards is a refresh
// token, which can only reach the one folder the app is allowed to use.

// Endpoints are variables so tests can point them at a fake server.
var (
	DropboxAuthURL  = "https://www.dropbox.com/oauth2/authorize"
	DropboxTokenURL = "https://api.dropboxapi.com/oauth2/token"
	MSDeviceURL     = "https://login.microsoftonline.com/common/oauth2/v2.0/devicecode"
	MSTokenURL      = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	GoogleDeviceURL = "https://oauth2.googleapis.com/device/code"
	GoogleTokenURL  = "https://oauth2.googleapis.com/token"
)

const (
	MSScope     = "Files.ReadWrite.AppFolder offline_access"
	GoogleScope = "https://www.googleapis.com/auth/drive.file"
)

// Token is the sign-in a store uses: it trades the saved refresh token for short-lived access tokens.
type Token struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Refresh      string
	Saved        func(refresh string) // called when the provider hands out a new refresh token

	mu     sync.Mutex
	access string
	exp    time.Time
}

func (t *Token) Access() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.access != "" && time.Until(t.exp) > time.Minute {
		return t.access, nil
	}
	if t.Refresh == "" {
		return "", errors.New("this place is not signed in yet")
	}
	v := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {t.Refresh}, "client_id": {t.ClientID}}
	if t.ClientSecret != "" {
		v.Set("client_secret", t.ClientSecret)
	}
	r, err := tokenPost(t.TokenURL, v)
	if err != nil {
		return "", err
	}
	t.access, t.exp = r.AccessToken, time.Now().Add(time.Duration(r.ExpiresIn)*time.Second)
	if r.RefreshToken != "" && r.RefreshToken != t.Refresh {
		t.Refresh = r.RefreshToken
		if t.Saved != nil {
			t.Saved(r.RefreshToken)
		}
	}
	return t.access, nil
}

type tokenReply struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func tokenPost(u string, v url.Values) (*tokenReply, error) {
	if _, err := checkURL(u); err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("POST", u, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the sign-in service: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r tokenReply
	json.Unmarshal(b, &r)
	if r.Error != "" {
		d := r.ErrorDesc
		if d == "" {
			d = r.Error
		}
		return &r, errors.New(firstLine(d))
	}
	if resp.StatusCode != 200 || r.AccessToken == "" {
		return &r, fmt.Errorf("the sign-in service answered %d", resp.StatusCode)
	}
	return &r, nil
}

// ---- Dropbox: open a link, paste back a code (with PKCE, so no secret is needed) ----

type Flow struct {
	Kind         string `json:"kind"` // dropbox, onedrive, google
	URL          string `json:"url,omitempty"`
	UserCode     string `json:"user_code,omitempty"`
	Interval     int    `json:"interval,omitempty"`
	ClientID     string `json:"-"`
	ClientSecret string `json:"-"`
	verifier     string
	device       string
	started      time.Time
}

func DropboxStart(clientID string) *Flow {
	var b [32]byte
	rand.Read(b[:])
	ver := base64.RawURLEncoding.EncodeToString(b[:])
	h := sha256.Sum256([]byte(ver))
	q := url.Values{"client_id": {clientID}, "response_type": {"code"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(h[:])},
		"code_challenge_method": {"S256"}, "token_access_type": {"offline"}}
	return &Flow{Kind: "dropbox", URL: DropboxAuthURL + "?" + q.Encode(), ClientID: clientID, verifier: ver, started: time.Now()}
}

// DropboxFinish trades the pasted code for a refresh token.
func DropboxFinish(f *Flow, code string) (string, error) {
	r, err := tokenPost(DropboxTokenURL, url.Values{"grant_type": {"authorization_code"}, "code": {strings.TrimSpace(code)},
		"client_id": {f.ClientID}, "code_verifier": {f.verifier}})
	if err != nil {
		return "", err
	}
	if r.RefreshToken == "" {
		return "", errors.New("Dropbox did not give a lasting sign-in; check that the app is set to full access to its own folder")
	}
	return r.RefreshToken, nil
}

// ---- OneDrive and Google: show a short code to type elsewhere ----

type deviceReply struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	VerificationURL string `json:"verification_url"`
	Interval        int    `json:"interval"`
	Error           string `json:"error"`
	ErrorDesc       string `json:"error_description"`
}

func DeviceStart(kind, clientID, clientSecret string) (*Flow, error) {
	u, scope := MSDeviceURL, MSScope
	if kind == "google" {
		u, scope = GoogleDeviceURL, GoogleScope
	}
	if _, err := checkURL(u); err != nil {
		return nil, err
	}
	resp, err := httpClient.PostForm(u, url.Values{"client_id": {clientID}, "scope": {scope}})
	if err != nil {
		return nil, fmt.Errorf("could not reach the sign-in service: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r deviceReply
	json.Unmarshal(b, &r)
	if r.Error != "" {
		d := r.ErrorDesc
		if d == "" {
			d = r.Error
		}
		return nil, errors.New(firstLine(d))
	}
	if r.DeviceCode == "" {
		return nil, fmt.Errorf("the sign-in service answered %d", resp.StatusCode)
	}
	link := r.VerificationURI
	if link == "" {
		link = r.VerificationURL
	}
	if r.Interval < 2 {
		r.Interval = 5
	}
	return &Flow{Kind: kind, URL: link, UserCode: r.UserCode, Interval: r.Interval, ClientID: clientID, ClientSecret: clientSecret, device: r.DeviceCode, started: time.Now()}, nil
}

// DevicePoll asks once whether the person has finished. done is false while still waiting.
func DevicePoll(f *Flow) (refresh string, done bool, err error) {
	u := MSTokenURL
	v := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {f.ClientID}, "device_code": {f.device}}
	if f.Kind == "google" {
		u = GoogleTokenURL
		v.Set("client_secret", f.ClientSecret)
	}
	if time.Since(f.started) > 15*time.Minute {
		return "", false, errors.New("the code ran out; start again")
	}
	r, err := tokenPost(u, v)
	if err != nil {
		if r != nil && (r.Error == "authorization_pending" || r.Error == "slow_down") {
			return "", false, nil
		}
		if r != nil && (r.Error == "expired_token" || r.Error == "access_denied" || r.Error == "authorization_declined") {
			return "", false, errors.New("the sign-in was not completed; start again")
		}
		return "", false, err
	}
	if r.RefreshToken == "" {
		return "", false, errors.New("no lasting sign-in was given")
	}
	return r.RefreshToken, true, nil
}
