package app

import "testing"

func TestParseDefaultRoute(t *testing.T) {
	const hdr = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n"
	cases := map[string]string{
		hdr + "eth0\t00000000\t0102A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
			"eth1\t00000000\t0100A8C0\t0003\t0\t0\t50\t00000000\t0\t0\t0\n" +
			"eth2\t0000A8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n": "eth1",
		hdr + "eth1\t00000000\t0100A8C0\t0002\t0\t0\t10\t00000000\t0\t0\t0\n" +
			"eth0\t00000000\t0102A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n": "eth0", // lowest metric is down
		hdr + "eth2\t0000A8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n": "",
		"": "",
	}
	for in, want := range cases {
		if got := parseDefaultRoute(in); got != want {
			t.Errorf("parseDefaultRoute = %q, want %q for\n%s", got, want, in)
		}
	}
}
