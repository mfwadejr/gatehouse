# Threats

## What it is for
Watching for infected devices, break-in attempts and probing, and telling you about them in plain words. It is an extra program that is installed only if you ask. It reads copies of your traffic and never holds it up.

## What is on the page
One row of tabs along the top: **Overview**, **Alerts**, **Settings**, **Rules** and **Advanced**.
- **Overview** opens on one line that says where things stand (not installed, off, watching, or in trouble) and where to go next. Once the program is installed it shows **Status**, **Traffic watched** (on the watched ports), **Load on this machine** (processor and memory against their limits), **Rules** (how many are loaded, when they were updated and when the next update is due) and **Alerts** (last hour, day and week). Below are charts for the last hour, day or week, **What was seen** (alerts by kind), **Devices involved**, and **What it has been doing** (rule updates, switch-offs and changes).
- **Alerts** show what was noticed in plain words, named after the device (the name you gave it, or its network name). The same thing happening many times is one row with a count. Harmless engine chatter is kept in a folded section at the bottom. Each row has **Allow for this device**, **Allow for all** and **Never check this device**.
- **Settings** holds everything you set. Until the program is installed it offers **Install threat watching**, and tells you the packages, the download size, the disk space and any worry about this machine's memory or processor before you agree. While it installs (or is removed), a ticked list shows the step it is on. Once installed it has **Threat watching** (the switch **Off** or **Watch and tell**, which moves as soon as you click and says what it is doing, with the plain explanation of what it does and does not do), **How closely to watch** (Light, Balanced, Strict or Custom), **Limits** for the processor and memory, and **The program**, where **Remove threat watching** is. **Custom** appears once you change a group under Rules; **Restore defaults** goes back to the setting's own groups.
- **Rules** has **Update rules now**, **Roll back to the previous rules**, **Groups of checks** and **Single checks**. Each group has a plain description. Switch groups on or off, then press **Apply**; nothing changes until you do, and **Discard** drops your changes.
- **Advanced** has **Allowed checks**, **Devices never checked**, **Save or load your tuning**, **Reset all threat settings**, **Program log** (the last lines the program wrote, the first place to look when it is not running) and **Where it writes**.

## Common tasks
1. Turn on threat watching: **Settings**, **Install threat watching**, read the sizes, **Install**. When it finishes, choose **Watch and tell**.
2. Quiet a harmless alert: **Alerts**, **Allow for this device**.
3. Lower its load on a small machine: **Settings**, choose **Light**, or lower the **Limits**.
4. Make your own set: **Rules**, switch groups, **Apply**. To undo, **Settings**, **Restore defaults**.
5. Go back after a bad rule update: **Rules**, **Roll back to the previous rules**.
6. Take it off the machine: **Settings**, **Remove threat watching**.

## What happens if something goes wrong
Threat watching only adds protection. If it fails, the protection goes and your internet carries on: your traffic is never held up.
- It reads copies of the traffic. If it stops, crashes or is removed, nothing changes for your devices.
- If it uses all of its processor share for ten minutes, NopeWarden switches it off by itself, says why on this page and in the Logs, and leaves it off until you switch it on again.
- Blocking threats (**Protect**) is not available. It stays off until watching has proved itself on real traffic.
- Most traffic is encrypted, so watching finds known bad servers and attack patterns, not everything.

## Example
A new camera starts talking to an address on a bad list. The **Alerts** tab shows "A device may be infected and talking to a known bad server", with the camera's name. You unplug it, or choose **Never check this device** if you know it is fine.

## If something goes wrong
If the page says **The program keeps failing**, the program starts and stops again and again; read the reason, then **Program log** under **Advanced** (or Diagnostics in the left menu). If **Install threat watching** fails, the page shows the reason; try again once the internet works and there is disk space. If the page says the program is not running, your traffic is not affected and NopeWarden keeps trying to start it. If it keeps stopping, choose **Light** under **Settings**. If a rule update breaks things, use **Roll back to the previous rules**.

## Looking up names in alerts
**Threat alerts** look up the name behind an address and try again if the first lookup finds nothing; **Look up again** retries now.
