package dnssrv

import (
	"sync"
	"time"
)

// RateLimiter is a token bucket per client address.
type RateLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
	buckets map[string]*bucket
	lastGC  time.Time
	exempt  map[string]bool
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{rate: rate, burst: burst, now: time.Now, buckets: map[string]*bucket{}, exempt: map[string]bool{"127.0.0.1": true, "::1": true}}
}

// Configure changes the limits. A rate of 0 disables limiting.
func (r *RateLimiter) Configure(rate, burst float64) {
	r.mu.Lock()
	r.rate, r.burst = rate, burst
	r.mu.Unlock()
}

// Allow takes a token for the client, returning false when it is over the limit.
func (r *RateLimiter) Allow(client string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rate <= 0 || r.exempt[client] {
		return true
	}
	now := r.now()
	if now.Sub(r.lastGC) > time.Minute {
		r.lastGC = now
		for k, b := range r.buckets {
			if now.Sub(b.last) > 5*time.Minute {
				delete(r.buckets, k)
			}
		}
	}
	b := r.buckets[client]
	if b == nil {
		b = &bucket{tokens: r.burst, last: now}
		r.buckets[client] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * r.rate
	if b.tokens > r.burst {
		b.tokens = r.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
