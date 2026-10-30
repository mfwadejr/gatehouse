package fw

// The addresses of the best-known public DNS-over-HTTPS servers. This list cannot be complete:
// anyone can run such a server on any address, so it stops the common cases (browsers that switch
// to Cloudflare or Google by themselves), not a determined person.
var DoHServers4 = []string{
	"1.1.1.1", "1.0.0.1", "1.1.1.2", "1.0.0.2", "1.1.1.3", "1.0.0.3",
	"8.8.8.8", "8.8.4.4",
	"9.9.9.9", "149.112.112.112", "9.9.9.10", "149.112.112.10", "9.9.9.11", "149.112.112.11",
	"208.67.222.222", "208.67.220.220", "208.67.222.123", "208.67.220.123",
	"94.140.14.14", "94.140.15.15", "94.140.14.15", "94.140.15.16",
	"185.228.168.9", "185.228.169.9", "185.228.168.10", "185.228.169.11",
}

var DoHServers6 = []string{
	"2606:4700:4700::1111", "2606:4700:4700::1001", "2606:4700:4700::1112", "2606:4700:4700::1002",
	"2001:4860:4860::8888", "2001:4860:4860::8844",
	"2620:fe::fe", "2620:fe::9", "2620:fe::10", "2620:fe::11",
	"2620:119:35::35", "2620:119:53::53",
	"2a10:50c0::ad1:ff", "2a10:50c0::ad2:ff",
}
