# Wi-Fi

Every wireless card in the machine shows up under **Wi-Fi** in the console, unset and silent by default.
The person setting up the device chooses, per card:

- **Join a Wi-Fi network.** The card connects to another network (a hotspot, say) and Gatehouse adds it
  as an internet connection, so it can be a backup for a cable or the main link. Uses `wpa_supplicant`.
- **Broadcast a Wi-Fi network.** The card becomes an access point. Its devices get their own network
  (addresses, DNS and a firewall zone are created for it) or join an existing bridge. Uses `hostapd`.

Some cards can only join networks. Gatehouse reads what the card supports (access point mode, 2.4 and
5 GHz, 802.11ac) and says so rather than failing later. The country the device is in must be set; it is
suggested from the time zone.

Settings are written to `/etc/gatehouse/wifi` (root-only, since they hold passphrases) and each card
runs as `gatehouse-wifi-client@<card>` or `gatehouse-wifi-ap@<card>`. Changes go through the same
apply-with-rollback as other network changes. Firmware for Intel, Atheros, Realtek, Broadcom and MediaTek
cards is on the ISO.
