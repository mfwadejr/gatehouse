package app

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultEgress names the device the box itself sends its own traffic out through, the one behind
// the lowest-metric default route. DNS lookups are made by the box, so this is the connection
// they used. It is read from the kernel's route table (no helper needed) and kept for a few seconds.
var egressCache struct {
	mu  sync.Mutex
	at  time.Time
	dev string
}

func defaultEgress() string {
	c := &egressCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) < 3*time.Second {
		return c.dev
	}
	b, _ := os.ReadFile("/proc/net/route")
	c.dev, c.at = parseDefaultRoute(string(b)), time.Now()
	return c.dev
}

// parseDefaultRoute reads /proc/net/route text and returns the device of the usable default route
// with the lowest metric, or "" when there is none.
func parseDefaultRoute(table string) string {
	best, bestMetric := "", -1
	for i, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 8 || f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		flags, _ := strconv.ParseUint(f[3], 16, 32)
		if flags&1 == 0 { // route is not up
			continue
		}
		m, _ := strconv.Atoi(f[6])
		if bestMetric < 0 || m < bestMetric {
			best, bestMetric = f[0], m
		}
	}
	return best
}
