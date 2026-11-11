package fw

import "testing"

// A route that points at a list which no longer exists must send nothing, never everything.
func TestServiceTermsMissingListIsEmpty(t *testing.T) {
	c := &compiler{m: &Model{PortLists: map[string][]string{}}}
	if got := c.serviceTerms("list:Gone"); len(got) != 0 {
		t.Fatalf("missing list gave terms %v, want none", got)
	}
	if got := c.serviceTerms(""); len(got) != 1 {
		t.Fatalf("empty service should still mean everything, got %v", got)
	}
}
