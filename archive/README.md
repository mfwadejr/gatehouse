# Archive: wireless support (removed in v0.8.53)

Gatehouse once ran Wi-Fi itself: a card could broadcast a network (hostapd) or join one (wpa_supplicant),
with a Wi-Fi page, join cards with QR codes, a stall watchdog and a Wi-Fi internet option in the setup
wizard. It was removed on purpose to keep the product a wired firewall, DNS and DHCP appliance. Wi-Fi is
added with a separate access point.

What is here:

- `wireless-support-v0.8.52.tar.gz`: every file that was only about wireless, as it was at v0.8.52 (the
  Go files in `internal/app`, `internal/netcfg`, `internal/netd`, the console pages `Wifi.jsx` and
  `WifiCards.jsx`, the two systemd units, `docs/WIFI.md` and the help chapter `docs/help/wifi.md`).
- `wireless-removal.patch`: the edits to every other file (wizard, devices list, backup, schema, ISO
  package list and so on). `git apply -R archive/wireless-removal.patch` undoes them on the v0.8.53 tree.

To bring Wi-Fi back: check out tag `v0.8.52` (the whole product with Wi-Fi), or restore the files from the
archive, reverse the patch, delete the migration that drops `wifi_radios` and `wifi_cards` (never edit an
applied migration: add a new one that recreates the tables), restore the packages `hostapd`,
`wpasupplicant`, `iw`, `rfkill`, `wireless-regdb` and the Wi-Fi firmware packages in
`iso/config/package-lists/gatehouse.list.chroot`, and remove `/etc/modprobe.d/gatehouse-no-wireless.conf`
from the ISO hook.
