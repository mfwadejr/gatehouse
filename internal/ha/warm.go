package ha

// Silent warm standby. Normally the standby keeps its internet ports down and brings them up when
// it takes over, which costs the time to negotiate the link and ask the provider for an address.
// With the warm option the standby keeps those ports connected but silent: no address, no
// neighbour or router chatter, and not managed by the network service, so the provider sees nothing
// from it. On takeover it:
//
//   - waits a second for the unit that was active to go silent first, so two units never speak on
//     the same provider connection;
//   - copies the other unit's hardware address on that port, so a provider that remembers devices
//     by address sees the same one (a DHCP lease can then come back with the same address; if it
//     does not, a fresh request is made);
//   - brings the port under the network service again, which asks for an address, or applies the
//     fixed address for a static connection;
//   - for a fixed address, announces it to the provider's equipment so it learns where it now is.
//
// A PPPoE connection, which is a login rather than a port, is not part of this: it still starts a
// new session on takeover, and a Wi-Fi hotspot connection is left as it was.
// Without the option the script is exactly the earlier one.

// NotifyScriptFor returns the script keepalived runs on a change of state.
func NotifyScriptFor(warm bool) string {
	if !warm {
		return NotifyScript
	}
	return NotifyScriptWarm
}

// NotifyScriptWarm is the warm-standby version of NotifyScript.
const NotifyScriptWarm = `#!/bin/sh
# Written by Gatehouse. Called by keepalived as: ha-notify GROUP|INSTANCE name STATE priority
state="$3"
log() { logger -t gatehouse-ha "$*"; }
each() { [ -r "$1" ] && while read -r a b; do [ -n "$a" ] && "$2" "$a" "$b"; done < "$1"; }
silent() {
  printf '# Written by Gatehouse (standby)\n[Match]\nName=%s\n[Link]\nUnmanaged=yes\n' "$1" > "/run/systemd/network/00-gh-warm-$1.network"
}
go_silent() {
  silent "$1"
  networkctl reload 2>/dev/null
  ip addr flush dev "$1" 2>/dev/null
  ip link set dev "$1" arp off multicast off 2>/dev/null
  sysctl -q -w "net.ipv6.conf.$1.disable_ipv6=1" 2>/dev/null
  ip link set dev "$1" up
}
go_down() { ip link set dev "$1" down; }
go_loud() {
  rm -f "/run/systemd/network/00-gh-warm-$1.network"
  sysctl -q -w "net.ipv6.conf.$1.disable_ipv6=0" 2>/dev/null
  ip link set dev "$1" arp on multicast on 2>/dev/null
}
set_mac() { [ -n "$2" ] && { ip link set dev "$1" down; ip link set dev "$1" address "$2"; }; }
bring_up() { ip link set dev "$1" up; networkctl reconfigure "$1" 2>/dev/null; }
announce() { ( sleep 3; arping -q -U -c 3 -I "$1" "$2" ) >/dev/null 2>&1 & }
case "$state" in
  MASTER)
    log "this unit is now active"
    conntrackd -C /etc/conntrackd/conntrackd.conf -c 2>/dev/null
    conntrackd -C /etc/conntrackd/conntrackd.conf -f 2>/dev/null
    conntrackd -C /etc/conntrackd/conntrackd.conf -R 2>/dev/null
    conntrackd -C /etc/conntrackd/conntrackd.conf -B 2>/dev/null
    touch /run/gatehouse-ha-active
    # Let the unit that was active go silent first.
    sleep 1
    each /etc/gatehouse/ha-wan-warm go_loud
    each /etc/gatehouse/ha-wan-macs set_mac
    each /etc/gatehouse/ha-wan-warm bring_up
    networkctl reload 2>/dev/null
    each /etc/gatehouse/ha-wan bring_up
    each /etc/gatehouse/ha-wan-static announce
    ;;
  BACKUP|FAULT|STOP)
    log "this unit is now standby ($state)"
    rm -f /run/gatehouse-ha-active
    each /etc/gatehouse/ha-wan go_down
    each /etc/gatehouse/ha-wan-warm go_silent
    conntrackd -C /etc/conntrackd/conntrackd.conf -t 2>/dev/null
    conntrackd -C /etc/conntrackd/conntrackd.conf -n 2>/dev/null
    ;;
esac
exit 0
`
