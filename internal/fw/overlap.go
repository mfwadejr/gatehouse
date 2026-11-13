package fw

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Overlap says that a rule is already covered by an earlier one. It is a hint for the person who
// wrote the rules; nothing is changed or removed because of it.
type Overlap struct {
	Kind   string `json:"kind"` // duplicate, redundant or shadowed
	With   string `json:"with"` // the earlier rule's name
	WithID int64  `json:"with_id"`
	Text   string `json:"text"`
}

// FindOverlaps looks at the rules in the order the firewall reads them and, for each enabled rule,
// finds the first earlier rule that matches everything it matches. It is deliberately careful: it only
// compares what is written (the same networks, the same names and ports, the same addresses), so it
// can miss an overlap, but it should not report one that is not there. Rules the matrix makes, paused
// rules, rate-limited rules and rules that apply only at set times are left out of the earlier side.
func FindOverlaps(rules []RuleRow) map[int64]Overlap {
	out := map[int64]Overlap{}
	for i, b := range rules {
		if !checkable(b) {
			continue
		}
		for _, a := range rules[:i] {
			if !checkable(a) || a.Limit != 0 || !covers(a, b) {
				continue
			}
			o := Overlap{With: a.Name, WithID: a.ID}
			switch {
			case a.Action == b.Action && covers(b, a):
				o.Kind, o.Text = "duplicate", fmt.Sprintf("Same as %q above. One of them can go.", a.Name)
			case a.Action == b.Action:
				o.Kind, o.Text = "redundant", fmt.Sprintf("%q above already does this, so this rule adds nothing.", a.Name)
			default:
				o.Kind, o.Text = "shadowed", fmt.Sprintf("%q above decides this traffic first (it %s), so this rule never applies.", a.Name, actionWord(a.Action))
			}
			out[b.ID] = o
			break
		}
	}
	return out
}

func actionWord(a string) string {
	switch a {
	case "accept":
		return "allows it"
	case "drop":
		return "blocks it"
	case "reject":
		return "refuses it"
	}
	return "handles it"
}

func checkable(r RuleRow) bool {
	return r.Enabled && r.PausedUntil == 0 && !IsMatrixRule(r.Name)
}

// covers reports whether every connection b matches is also matched by a.
func covers(a, b RuleRow) bool {
	if a.From != b.From || a.To != b.To {
		return false
	}
	if a.IPVer != "" && a.IPVer != b.IPVer {
		return false
	}
	if a.CtState != "" && a.CtState != b.CtState {
		return false
	}
	if a.AlsoMAC && !b.AlsoMAC {
		return false
	}
	if !sameSched(a.Sched, b.Sched) && !always(a.Sched) {
		return false
	}
	return tokensCover(a.Src, b.Src) && tokensCover(a.Dst, b.Dst) && tokensCover(a.Service, b.Service)
}

func always(s *Schedule) bool { return s == nil || s.Mode == "" || s.Mode == "always" }

func sameSched(a, b *Schedule) bool {
	if always(a) && always(b) {
		return true
	}
	if always(a) != always(b) {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func tokenSet(s string) (set map[string]bool, any bool) {
	set = map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || t == "any" {
			return nil, true
		}
		set[t] = true
	}
	return set, false
}

// tokensCover is true when a's list of things includes everything in b's: a is "any", or each of b's
// entries is written in a.
func tokensCover(a, b string) bool {
	as, aAny := tokenSet(a)
	if aAny {
		return true
	}
	bs, bAny := tokenSet(b)
	if bAny {
		return false
	}
	for t := range bs {
		if !as[t] {
			return false
		}
	}
	return true
}
