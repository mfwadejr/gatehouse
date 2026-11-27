package app

import "testing"

func TestVlanConfigParse(t *testing.T) {
	got := parseVlanConfig("VLAN Dev name\t | VLAN ID\nName-Type: VLAN_NAME_TYPE_RAW_PLUS_VID_NO_PAD\nenp2s0.3       | 3  | enp2s0\nenp2s0.4 | 4 | enp2s0\n")
	if len(got["enp2s0"]) != 2 || got["enp2s0"][0] != "enp2s0.3" {
		t.Fatalf("got %v", got)
	}
}

// A port's own counter leaves out the tagged VLAN traffic riding on it, so Home does not count Streaming.
func TestPortCounterLeavesOutItsVlans(t *testing.T) {
	oldV := vlanChildren
	vlanChildren = func() map[string][]string { return map[string][]string{"enp2s0": {"enp2s0.3"}} }
	t.Cleanup(func() { vlanChildren = oldV })
	// readCounterReal reads /sys; check the arithmetic through a stand-in of the same shape.
	oldC := readCounterRaw
	readCounterRaw = func(dev, name string) (uint64, bool) {
		return map[string]uint64{"enp2s0": 1000, "enp2s0.3": 900}[dev], true
	}
	t.Cleanup(func() { readCounterRaw = oldC })
	if n, _ := readCounterOwn("enp2s0", "rx_bytes"); n != 100 {
		t.Fatalf("home got %d, want 100", n)
	}
	if n, _ := readCounterOwn("enp2s0.3", "rx_bytes"); n != 900 {
		t.Fatalf("streaming got %d, want 900", n)
	}
	readCounterRaw = func(dev, name string) (uint64, bool) {
		return map[string]uint64{"enp2s0": 5, "enp2s0.3": 9}[dev], true
	}
	if n, _ := readCounterOwn("enp2s0", "rx_bytes"); n != 0 {
		t.Fatalf("skew should clamp to 0, got %d", n)
	}
}
