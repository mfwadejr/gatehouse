package dnssrv

import (
	"sync"
	"time"
)

// Outcome says how a query was answered.
type Outcome string

const (
	OutForwarded   Outcome = "forwarded"
	OutCached      Outcome = "cached"
	OutBlocked     Outcome = "blocked"
	OutLocal       Outcome = "local"
	OutRateLimited Outcome = "rate_limited"
	OutFailed      Outcome = "failed"
)

var latencyEdgesMS = []float64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000}

type minute struct {
	at       int64 // unix minute
	counts   map[Outcome]uint64
	nx       uint64
	total    uint64
	latSumUS uint64
	latN     uint64
	hist     [11]uint64
}

// Stats keeps one hour of per-minute counters plus a per-second rate history.
type Stats struct {
	mu      sync.Mutex
	now     func() time.Time
	mins    [60]*minute
	secs    [60]float64
	secBuf  uint64
	lastSec int64
}

func NewStats() *Stats { return &Stats{now: time.Now} }

func (s *Stats) cur(now time.Time) *minute {
	m := now.Unix() / 60
	i := int(m % 60)
	if s.mins[i] == nil || s.mins[i].at != m {
		s.mins[i] = &minute{at: m, counts: map[Outcome]uint64{}}
	}
	return s.mins[i]
}

// Record notes one answered query.
func (s *Stats) Record(o Outcome, latency time.Duration, nxdomain bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	m := s.cur(now)
	m.counts[o]++
	m.total++
	if nxdomain {
		m.nx++
	}
	if o == OutForwarded {
		m.latSumUS += uint64(latency.Microseconds())
		m.latN++
		ms := float64(latency.Microseconds()) / 1000
		idx := len(latencyEdgesMS)
		for i, e := range latencyEdgesMS {
			if ms <= e {
				idx = i
				break
			}
		}
		m.hist[idx]++
	}
	s.secBuf++
}

// Tick must be called once per second; it moves the current second into the rate history.
func (s *Stats) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy(s.secs[:], s.secs[1:])
	s.secs[59] = float64(s.secBuf)
	s.secBuf = 0
}

// Snapshot is the data the Overview and DNS pages show.
type Snapshot struct {
	QPS         float64            `json:"qps"`
	History     []float64          `json:"history"`
	HourTotal   uint64             `json:"hour_total"`
	Outcomes    map[Outcome]uint64 `json:"outcomes"`
	BlockedPct  float64            `json:"blocked_pct"`
	CachedPct   float64            `json:"cached_pct"`
	NXDomainPct float64            `json:"nxdomain_pct"`
	AvgMS       float64            `json:"avg_ms"`
	P95MS       float64            `json:"p95_ms"`
	CacheSize   int                `json:"cache_size"`
}

// Snapshot summarises the last hour.
func (s *Stats) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Unix()/60 - 59
	out := Snapshot{Outcomes: map[Outcome]uint64{}, History: append([]float64(nil), s.secs[:]...)}
	var nx, latSum, latN uint64
	var hist [11]uint64
	for _, m := range s.mins {
		if m == nil || m.at < cutoff {
			continue
		}
		out.HourTotal += m.total
		nx += m.nx
		latSum += m.latSumUS
		latN += m.latN
		for o, n := range m.counts {
			out.Outcomes[o] += n
		}
		for i, n := range m.hist {
			hist[i] += n
		}
	}
	if n := out.HourTotal; n > 0 {
		out.BlockedPct = 100 * float64(out.Outcomes[OutBlocked]) / float64(n)
		out.CachedPct = 100 * float64(out.Outcomes[OutCached]) / float64(n)
		out.NXDomainPct = 100 * float64(nx) / float64(n)
	}
	if latN > 0 {
		out.AvgMS = float64(latSum) / float64(latN) / 1000
		target := uint64(float64(latN)*0.95 + 0.5)
		var acc uint64
		for i, n := range hist {
			acc += n
			if acc >= target {
				if i < len(latencyEdgesMS) {
					out.P95MS = latencyEdgesMS[i]
				} else {
					out.P95MS = 2000
				}
				break
			}
		}
	}
	// Rate over the last 5 seconds smooths the headline number.
	var sum float64
	for _, v := range s.secs[55:] {
		sum += v
	}
	out.QPS = sum / 5
	return out
}
