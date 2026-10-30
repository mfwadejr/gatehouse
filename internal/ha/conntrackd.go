package ha

import (
	"errors"
	"fmt"
	"net/netip"
)

// ConntrackdPort is the UDP port the units copy their connection tables over.
const ConntrackdPort = 3780

// ConntrackdConf writes the settings that keep the standby's connection table current, so open
// connections carry on after a failover. Both units send to each other over their own management
// addresses on iface (the port that faces the other unit). Only the table is copied: nothing here
// changes what is allowed.
func ConntrackdConf(p Pairing, role Role, iface string) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if iface == "" || len(iface) > 15 {
		return "", errors.New("the port that faces the other unit is needed")
	}
	own, peer := p.Primary, p.Standby
	if role == Standby {
		own, peer = p.Standby, p.Primary
	} else if role != Primary {
		return "", errors.New("unknown role")
	}
	for _, a := range []string{own, peer} {
		if _, err := netip.ParseAddr(a); err != nil {
			return "", fmt.Errorf("%q is not an address", a)
		}
	}
	return fmt.Sprintf(`# Written by Gatehouse. Changes here are replaced.
Sync {
    Mode FTFW {
    }
    UDP {
        IPv4_address %s
        IPv4_Destination_Address %s
        Port %d
        Interface %s
        SndSocketBuffer 1249280
        RcvSocketBuffer 1249280
        Checksum on
    }
}
General {
    Nice -1
    HashSize 32768
    HashLimit 131072
    Syslog on
    LockFile /var/lock/conntrack.lock
    UNIX {
        Path /var/run/conntrackd.ctl
        Backlog 20
    }
    NetlinkBufferSize 2097152
    NetlinkBufferSizeMaxGrowth 8388608
    Filter From Userspace {
        Protocol Accept {
            TCP
            UDP
            ICMP
        }
        Address Ignore {
            IPv4_address 127.0.0.1
            IPv4_address %s
            IPv4_address %s
        }
    }
}
`, own, peer, ConntrackdPort, iface, p.Primary, p.Standby), nil
}

// NotifyScript is what keepalived runs on a change of state. Becoming active commits the copied
// connection table into the kernel and sends the update to the switch by announcing the shared
// addresses; becoming standby asks the partner for its table. It never changes the firewall.
const NotifyScript = `#!/bin/sh
# Written by Gatehouse. Called by keepalived as: ha-notify GROUP|INSTANCE name STATE priority
state="$3"
log() { logger -t gatehouse-ha "$*"; }
case "$state" in
  MASTER)
    log "this unit is now active"
    conntrackd -C /etc/conntrackd/conntrackd.conf -c 2>/dev/null
    conntrackd -C /etc/conntrackd/conntrackd.conf -f 2>/dev/null
    conntrackd -C /etc/conntrackd/conntrackd.conf -R 2>/dev/null
    conntrackd -C /etc/conntrackd/conntrackd.conf -B 2>/dev/null
    touch /run/gatehouse-ha-active
    # Only the active unit has its internet ports up, so two units never share one provider address.
    [ -r /etc/gatehouse/ha-wan ] && while read -r d; do
      [ -n "$d" ] && ip link set dev "$d" up && networkctl reconfigure "$d" 2>/dev/null
    done < /etc/gatehouse/ha-wan
    ;;
  BACKUP|FAULT|STOP)
    log "this unit is now standby ($state)"
    rm -f /run/gatehouse-ha-active
    [ -r /etc/gatehouse/ha-wan ] && while read -r d; do
      [ -n "$d" ] && ip link set dev "$d" down
    done < /etc/gatehouse/ha-wan
    conntrackd -C /etc/conntrackd/conntrackd.conf -t 2>/dev/null
    conntrackd -C /etc/conntrackd/conntrackd.conf -n 2>/dev/null
    ;;
esac
exit 0
`
