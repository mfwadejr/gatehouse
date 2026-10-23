module gatehouse

go 1.24

require (
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/miekg/dns v1.1.62
)

require (
	golang.org/x/net v0.27.0 // indirect
	golang.org/x/sys v0.22.0 // indirect
)

// golang.org is not reachable from the build environment, so the x/ modules
// the DNS library needs are taken from their GitHub mirrors at the exact
// versions it requires. The module paths inside those repos are unchanged.
replace (
	golang.org/x/mod => github.com/golang/mod v0.18.0
	golang.org/x/net => github.com/golang/net v0.27.0
	golang.org/x/sync => github.com/golang/sync v0.7.0
	golang.org/x/sys => github.com/golang/sys v0.22.0
	golang.org/x/tools => github.com/golang/tools v0.22.0
)
