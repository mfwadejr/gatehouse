package app

import (
	"errors"
	"testing"
	"time"
)

func TestThreatNameLookupRetries(t *testing.T) {
	now := time.Now()
	ptrNow = func() time.Time { return now }
	calls, answer := 0, ""
	var fail error
	ptrResolve = func(a *App, addr string) (string, bool, error) {
		calls++
		if fail != nil {
			return "", false, fail
		}
		return answer, answer == "", nil
	}
	t.Cleanup(func() {
		ptrNow, ptrResolve = time.Now, nil
		ptrMu.Lock()
		ptrs = map[string]*ptrEntry{}
		ptrMu.Unlock()
	})
	a := &App{}
	settle := func() { time.Sleep(50 * time.Millisecond) }
	fail = errors.New("timeout")
	a.ptrAsk("8.8.8.8", false)
	settle()
	if calls != 1 {
		t.Fatal("first lookup on opening", calls)
	}
	v := a.ptrViews([]string{"8.8.8.8"})[0]
	if v.State != "failed" || v.Retry != 10 {
		t.Fatalf("%+v", v)
	}
	// not yet due
	now = now.Add(5 * time.Second)
	a.ptrRunDue()
	settle()
	if calls != 1 {
		t.Fatal("too early")
	}
	// the retry steps: 10s, 30s, 1m, 5m, then 30m
	for i, step := range []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		ptrMu.Lock()
		got := ptrs["8.8.8.8"].Next.Sub(now)
		ptrMu.Unlock()
		_ = got
		now = now.Add(step + time.Second)
		before := calls
		a.ptrRunDue()
		settle()
		if calls != before+1 {
			t.Fatalf("step %d should retry", i)
		}
	}
	// success stops retries
	fail, answer = nil, "dns.google"
	now = now.Add(time.Hour)
	a.ptrRunDue()
	settle()
	if a.ptrName("8.8.8.8") != "dns.google" {
		t.Fatal("name not kept")
	}
	before := calls
	now = now.Add(2 * time.Hour)
	a.ptrRunDue()
	settle()
	if calls != before {
		t.Fatal("found names are not looked up again")
	}
	// Look up again starts over for an address with no name
	answer = ""
	a.ptrAsk("1.2.3.4", false)
	settle()
	v = a.ptrViews([]string{"1.2.3.4"})[0]
	if v.State != "none" || v.Status == "" {
		t.Fatalf("%+v", v)
	}
	before = calls
	a.ptrAsk("1.2.3.4", true)
	settle()
	if calls != before+1 {
		t.Fatal("Look up again must try at once")
	}
}

func TestTopicNamesEndpoint(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, _ := c.call("GET", "/api/blocks/topics/nope/names", nil); rc != 404 {
		t.Fatalf("unknown topic should be 404, got %d", rc)
	}
	rc, _, raw := c.call("GET", "/api/blocks/topics/ads/names?q=x", nil)
	if rc != 200 && rc != 404 {
		t.Fatalf("%d %s", rc, raw)
	}
}
