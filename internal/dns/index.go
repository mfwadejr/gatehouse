package dnssrv

import (
	"strings"
	"sync/atomic"
)

// MaxLists is the number of blocklists the index can hold (one bit each).
const MaxLists = 64

// Index maps a domain to the set of lists that block it. A rule for example.com also
// blocks every subdomain, which is how list authors expect it to behave.
type Index struct {
	block map[string]uint64
	allow map[string]uint64 // exceptions that come from lists
	names [MaxLists]string
}

func newIndex() *Index {
	return &Index{block: make(map[string]uint64, 1<<16), allow: map[string]uint64{}}
}

// Match is the outcome of looking a name up.
type Match struct {
	Blocked bool
	List    string
	Rule    string
	Allowed bool // an exception or custom allow rule overrode a block
}

// Lookup checks name and each parent domain against the lists in mask.
func (ix *Index) Lookup(name string, mask uint64) (list int, rule string, ok bool) {
	for d := name; d != ""; {
		if m := ix.block[d]; m&mask != 0 {
			// An exception from any enabled list for this domain or a parent wins.
			if ix.excepted(name, mask) {
				return 0, "", false
			}
			for i := 0; i < MaxLists; i++ {
				if m&mask&(1<<uint(i)) != 0 {
					return i, d, true
				}
			}
		}
		dot := strings.IndexByte(d, '.')
		if dot < 0 {
			break
		}
		d = d[dot+1:]
	}
	return 0, "", false
}

func (ix *Index) excepted(name string, mask uint64) bool {
	if len(ix.allow) == 0 {
		return false
	}
	for d := name; d != ""; {
		if ix.allow[d]&mask != 0 {
			return true
		}
		dot := strings.IndexByte(d, '.')
		if dot < 0 {
			break
		}
		d = d[dot+1:]
	}
	return false
}

// ListName returns the display name of list bit i.
func (ix *Index) ListName(i int) string {
	if i < 0 || i >= MaxLists {
		return ""
	}
	return ix.names[i]
}

// Size returns the number of distinct blocked domains.
func (ix *Index) Size() int { return len(ix.block) }

// indexBuilder assembles an Index from lists.
type indexBuilder struct{ ix *Index }

func newIndexBuilder() *indexBuilder { return &indexBuilder{ix: newIndex()} }

func (b *indexBuilder) add(bit int, name string, r Rule) {
	b.ix.names[bit] = name
	if r.Allow {
		b.ix.allow[r.Domain] |= 1 << uint(bit)
	} else {
		b.ix.block[r.Domain] |= 1 << uint(bit)
	}
}

func (b *indexBuilder) setName(bit int, name string) { b.ix.names[bit] = name }

// CustomRules holds the administrator's own allow and block entries. Allow always wins.
type CustomRules struct {
	allow map[string]string
	block map[string]string
}

func newCustom() *CustomRules {
	return &CustomRules{allow: map[string]string{}, block: map[string]string{}}
}

func suffixes(name string) []string {
	var out []string
	for d := name; d != ""; {
		out = append(out, d)
		dot := strings.IndexByte(d, '.')
		if dot < 0 {
			break
		}
		d = d[dot+1:]
	}
	return out
}

// Check returns "allow", "block" or "" for a name.
func (c *CustomRules) Check(name string) (action, rule string) {
	for _, d := range suffixes(name) {
		if _, ok := c.allow[d]; ok {
			return "allow", d
		}
	}
	for _, d := range suffixes(name) {
		if _, ok := c.block[d]; ok {
			return "block", d
		}
	}
	return "", ""
}

// engine is the immutable snapshot the server reads; it is swapped atomically.
type engine struct {
	ix       *Index
	custom   *CustomRules
	masks    map[int64]uint64 // blocklist bits enabled per group
	nofilter map[int64]bool   // groups with filtering switched off
}

type engineHolder struct{ p atomic.Pointer[engine] }

func (h *engineHolder) get() *engine {
	e := h.p.Load()
	if e == nil {
		return &engine{ix: newIndex(), custom: newCustom(), masks: map[int64]uint64{}, nofilter: map[int64]bool{}}
	}
	return e
}
