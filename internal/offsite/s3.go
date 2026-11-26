package offsite

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3 is any storage that speaks the S3 protocol: Amazon S3, Backblaze B2, Wasabi, Cloudflare R2,
// MinIO and others. Requests are signed with AWS Signature Version 4, written out here so that no
// extra library is needed.
type S3 struct {
	Endpoint  string // empty for Amazon S3
	Region    string
	Bucket    string
	Prefix    string // a folder inside the bucket, may be empty
	AccessKey string
	SecretKey string
	PathStyle bool // bucket in the path, not the host name (needed by most non-Amazon stores)
	now       func() time.Time
}

func (s *S3) clock() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func hmacSHA(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// signingKey derives the Signature V4 key for a day, region and service.
func signingKey(secret, date, region, service string) []byte {
	k := hmacSHA([]byte("AWS4"+secret), date)
	k = hmacSHA(k, region)
	k = hmacSHA(k, service)
	return hmacSHA(k, "aws4_request")
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// uriEncode is the encoding Signature V4 wants: everything but A-Z a-z 0-9 - _ . ~ (and / in a path).
func uriEncode(s string, slash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && slash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func (s *S3) base() (*url.URL, error) {
	if s.Bucket == "" || s.Region == "" || s.AccessKey == "" || s.SecretKey == "" {
		return nil, errors.New("fill in the bucket, region, access key and secret key")
	}
	ep := strings.TrimSpace(s.Endpoint)
	if ep == "" {
		ep = "https://s3." + s.Region + ".amazonaws.com"
	}
	u, err := checkURL(ep)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *S3) key(name string) string {
	p := strings.Trim(s.Prefix, "/")
	if p == "" {
		return name
	}
	return p + "/" + name
}

func (s *S3) do(method, key string, q url.Values, body []byte) (*http.Response, error) {
	u, err := s.base()
	if err != nil {
		return nil, err
	}
	host := u.Host
	var path string
	if s.PathStyle {
		path = "/" + s.Bucket
	} else {
		host = s.Bucket + "." + u.Host
	}
	if key != "" {
		path += "/" + key
	}
	if path == "" {
		path = "/"
	}
	canonPath := uriEncode(path, true)
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var cq []string
	for _, k := range keys {
		cq = append(cq, uriEncode(k, false)+"="+uriEncode(q.Get(k), false))
	}
	query := strings.Join(cq, "&")
	t := s.clock()
	date, stamp := t.Format("20060102"), t.Format("20060102T150405Z")
	payload := sha(body)
	canonHeaders := "host:" + host + "\nx-amz-content-sha256:" + payload + "\nx-amz-date:" + stamp + "\n"
	signed := "host;x-amz-content-sha256;x-amz-date"
	canon := strings.Join([]string{method, canonPath, query, canonHeaders, signed, payload}, "\n")
	scope := date + "/" + s.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + sha([]byte(canon))
	sig := hex.EncodeToString(hmacSHA(signingKey(s.SecretKey, date, s.Region, "s3"), toSign))
	full := u.Scheme + "://" + host + canonPath
	if query != "" {
		full += "?" + query
	}
	req, err := http.NewRequest(method, full, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-amz-date", stamp)
	req.Header.Set("x-amz-content-sha256", payload)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.AccessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
	if method == "PUT" {
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the storage: %v", err)
	}
	return resp, nil
}

func (s *S3) Put(name string, data []byte) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	resp, err := s.do("PUT", s.key(name), nil, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return statusErr("sending the backup", resp)
	}
	return nil
}

func (s *S3) Delete(name string) error {
	if !ValidName(name) {
		return errors.New("that file name is not allowed")
	}
	resp, err := s.do("DELETE", s.key(name), nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != 404 {
		return statusErr("removing an old backup", resp)
	}
	return nil
}

type s3List struct {
	Truncated bool   `xml:"IsTruncated"`
	Next      string `xml:"NextContinuationToken"`
	Contents  []struct {
		Key      string    `xml:"Key"`
		Modified time.Time `xml:"LastModified"`
		Size     int64     `xml:"Size"`
	} `xml:"Contents"`
}

func (s *S3) List() ([]Item, error) {
	var out []Item
	prefix := strings.Trim(s.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	token := ""
	for page := 0; page < 20; page++ {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		resp, err := s.do("GET", "", q, nil)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			return nil, statusErr("looking in the bucket", resp)
		}
		var l s3List
		if err := xml.Unmarshal(body, &l); err != nil {
			return nil, errors.New("the storage answered in a way NopeWarden does not understand")
		}
		for _, c := range l.Contents {
			n := strings.TrimPrefix(c.Key, prefix)
			if n != "" && !strings.Contains(n, "/") {
				out = append(out, Item{Name: n, Modified: c.Modified, Size: c.Size})
			}
		}
		if !l.Truncated || l.Next == "" {
			break
		}
		token = l.Next
	}
	return out, nil
}
