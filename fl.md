# Fix list (after 0.9.4.11) - started 9 Nov 2026

Nothing here is built. Add to the list as more turn up. These are the things the 0.9.4.11 build leaves for real hardware or for you.

## To confirm on real hardware (yours)
Z. In-app update: on the box installed from 0.9.4.10 use Update now (no new ISO), then choose Resolve directly in DNS. Expected: no root.key error, Using Unbound, Test it passes. (ACCEPTANCE 56 and 57.) The fix is in the program, so it should arrive by update; the image-only parts (the boot countdown, the key placed at build time) need a fresh ISO install.
T. Legacy BIOS boot: no "Automatic boot in N seconds" text (ACCEPTANCE 58). If it still shows, remove the message.
U. Installer text readable on the monitor and a serial terminal (59).
V. DHCP check on real hardware: both ports into a home network; the notes name the router or say the port already has an address; DHCP defaults to off on the LAN port with the reason (60).
S. Real-hardware checks of the other installer features (a real VM with both ports on one bridge, a real router on the LAN port, the serial header and size query, centring on real screens).
Resolve directly on real hardware: Unbound recursion and DNSSEC, the fallback and the port 53 test. Note: the sandbox cannot reach the root servers, so only the settings check and starting Unbound were proven here; lookups themselves are untested.

## Left from before
Pictures: the website's app pictures still show the old left menu and Overview box (the installer pictures were retaken for 0.9.4.11). Retake all app pictures in one pass once the box is up and has some queries and dummy data (item D, yours to start); this includes Resolve directly, traffic panels, Your VPNs card, map popout and Both.
A (limitation). Rename-lost-on-discard: a non-staged change made between two waiting changes is put back if an earlier waiting change is discarded. Undo on a staged delete now discards the waiting line the same way, so it can put such a change back too; the toast window is short.
B. ISO build from the tag and install on hardware (yours). C. ACCEPTANCE rows 35 to 66 (yours). E. Logo files. F. Public .ovpn files. G. Repository rename.
Closed: N (no licence, ever).
