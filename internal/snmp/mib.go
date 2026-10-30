package snmp

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Iface is what the kernel knows about one network port.
type Iface struct {
	Index     int
	Name      string
	Type      int // IANAifType: 6 ethernet, 24 loopback, 1 other
	MTU       int
	SpeedMbps int
	MAC       []byte
	Up        bool
	Oper      int // 1 up, 2 down
	InOct     uint64
	OutOct    uint64
	InPkts    uint64
	OutPkts   uint64
	InErr     uint64
	OutErr    uint64
	InDrop    uint64
	OutDrop   uint64
}

func readStr(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

func readNum(p string) uint64 {
	n, _ := strconv.ParseUint(readStr(p), 10, 64)
	return n
}

// ReadIfaces lists the ports under root/sys/class/net, ordered by index.
func ReadIfaces(root string) []Iface {
	base := filepath.Join(root, "sys/class/net")
	ents, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []Iface
	for _, e := range ents {
		d := filepath.Join(base, e.Name())
		i := Iface{Name: e.Name()}
		i.Index = int(readNum(filepath.Join(d, "ifindex")))
		if i.Index == 0 {
			continue
		}
		switch readNum(filepath.Join(d, "type")) {
		case 1:
			i.Type = 6
		case 772:
			i.Type = 24
		default:
			i.Type = 1
		}
		i.MTU = int(readNum(filepath.Join(d, "mtu")))
		if sp, err := strconv.Atoi(readStr(filepath.Join(d, "speed"))); err == nil && sp > 0 {
			i.SpeedMbps = sp
		}
		for _, h := range strings.Split(readStr(filepath.Join(d, "address")), ":") {
			if n, err := strconv.ParseUint(h, 16, 8); err == nil {
				i.MAC = append(i.MAC, byte(n))
			}
		}
		st := readStr(filepath.Join(d, "operstate"))
		i.Oper = 2
		if st == "up" || st == "unknown" {
			i.Oper = 1
		}
		i.Up = readNum(filepath.Join(d, "flags"))&1 == 1 // IFF_UP is bit 0 of the flags (hex in sysfs)
		if f, err := strconv.ParseUint(strings.TrimPrefix(readStr(filepath.Join(d, "flags")), "0x"), 16, 32); err == nil {
			i.Up = f&1 == 1
		}
		s := filepath.Join(d, "statistics")
		i.InOct, i.OutOct = readNum(filepath.Join(s, "rx_bytes")), readNum(filepath.Join(s, "tx_bytes"))
		i.InPkts, i.OutPkts = readNum(filepath.Join(s, "rx_packets")), readNum(filepath.Join(s, "tx_packets"))
		i.InErr, i.OutErr = readNum(filepath.Join(s, "rx_errors")), readNum(filepath.Join(s, "tx_errors"))
		i.InDrop, i.OutDrop = readNum(filepath.Join(s, "rx_dropped")), readNum(filepath.Join(s, "tx_dropped"))
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Index < out[b].Index })
	return out
}

// System describes the device for the system group.
type System struct {
	Descr    string
	Name     string
	Contact  string
	Location string
	Uptime   uint64 // hundredths of a second
}

func oid(s string, idx ...uint32) OID {
	o := MustOID(s)
	return append(append(OID(nil), o...), idx...)
}

// MIBVars builds the standard variables: system group, ifTable and ifXTable (read only).
func MIBVars(sys System, ifs []Iface) []Var {
	vs := []Var{
		{oid("1.3.6.1.2.1.1.1", 0), Str(sys.Descr)},
		{oid("1.3.6.1.2.1.1.2", 0), ObjectID(OID{0, 0})},
		{oid("1.3.6.1.2.1.1.3", 0), Ticks(sys.Uptime)},
		{oid("1.3.6.1.2.1.1.4", 0), Str(sys.Contact)},
		{oid("1.3.6.1.2.1.1.5", 0), Str(sys.Name)},
		{oid("1.3.6.1.2.1.1.6", 0), Str(sys.Location)},
		{oid("1.3.6.1.2.1.1.7", 0), Int(70)},
		{oid("1.3.6.1.2.1.2.1", 0), Int(int64(len(ifs)))},
	}
	for _, f := range ifs {
		ix := uint32(f.Index)
		admin := int64(2)
		if f.Up {
			admin = 1
		}
		speed := uint64(f.SpeedMbps) * 1000000
		vs = append(vs,
			Var{oid("1.3.6.1.2.1.2.2.1.1", ix), Int(int64(f.Index))},
			Var{oid("1.3.6.1.2.1.2.2.1.2", ix), Str(f.Name)},
			Var{oid("1.3.6.1.2.1.2.2.1.3", ix), Int(int64(f.Type))},
			Var{oid("1.3.6.1.2.1.2.2.1.4", ix), Int(int64(f.MTU))},
			Var{oid("1.3.6.1.2.1.2.2.1.5", ix), Gauge32(speed)},
			Var{oid("1.3.6.1.2.1.2.2.1.6", ix), Raw(f.MAC)},
			Var{oid("1.3.6.1.2.1.2.2.1.7", ix), Int(admin)},
			Var{oid("1.3.6.1.2.1.2.2.1.8", ix), Int(int64(f.Oper))},
			Var{oid("1.3.6.1.2.1.2.2.1.10", ix), Counter32(f.InOct)},
			Var{oid("1.3.6.1.2.1.2.2.1.11", ix), Counter32(f.InPkts)},
			Var{oid("1.3.6.1.2.1.2.2.1.13", ix), Counter32(f.InDrop)},
			Var{oid("1.3.6.1.2.1.2.2.1.14", ix), Counter32(f.InErr)},
			Var{oid("1.3.6.1.2.1.2.2.1.16", ix), Counter32(f.OutOct)},
			Var{oid("1.3.6.1.2.1.2.2.1.17", ix), Counter32(f.OutPkts)},
			Var{oid("1.3.6.1.2.1.2.2.1.19", ix), Counter32(f.OutDrop)},
			Var{oid("1.3.6.1.2.1.2.2.1.20", ix), Counter32(f.OutErr)},
			Var{oid("1.3.6.1.2.1.31.1.1.1.1", ix), Str(f.Name)},
			Var{oid("1.3.6.1.2.1.31.1.1.1.6", ix), Counter64(f.InOct)},
			Var{oid("1.3.6.1.2.1.31.1.1.1.10", ix), Counter64(f.OutOct)},
			Var{oid("1.3.6.1.2.1.31.1.1.1.15", ix), Gauge32(uint64(f.SpeedMbps))},
			Var{oid("1.3.6.1.2.1.31.1.1.1.18", ix), Str("")},
		)
	}
	return vs
}
