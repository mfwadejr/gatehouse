# Protection

## What it is for
One place for keeping the network safe: a check of what to fix, blocking apps and topics, and watching for threats. Block lists for ads, trackers and adult sites stay under DNS.

## What is on the page
Tabs along the top:
- **Overview**: **Protection at a glance** gives one sentence of verdict and a line each for the security check, your blocks and threat watching. Below it, the **Security check** lists what to fix, with a button that goes to the right page.
- **Apps & topics**: **Block apps, topics and websites**. **Add a block** chooses what to block (an app, a topic such as adult, gambling or games, or a website), for whom (everyone, a network, a profile or one device), and when (always or at set hours). A device can be given an exception, and **Pause** stops a block for a while.
- **Threats**: **Threat watching**. Until you install it, the page offers **Install threat watching** and tells you the packages, the download size, the disk space and any worry about this machine's memory or processor before you agree. Once installed: the switch **Off** or **Watch and tell**, and the tabs **Alerts**, **Settings**, **Rules** and **Advanced**.
  - **Alerts** show what was noticed in plain words, named after the device (the name you gave it, or its network name). The same thing happening many times is one row with a count. Harmless engine chatter is kept in a folded section at the bottom. Each row has **Allow for this device**, **Allow for all** and **Never check this device**.
  - **Settings** has **How closely to watch** (Light, Balanced, Strict or Custom) and **Limits** for the processor and memory. **Custom** appears once you change a group under Rules; **Restore defaults** goes back to the setting's own groups.
  - **Rules** has **Update rules now**, **Roll back to the previous rules**, **Groups of checks** and **Single checks**. Each group has a plain description. Switch groups on or off, then press **Apply**; nothing changes until you do, and **Discard** drops your changes.
  - **Advanced** has **Reset all threat settings**, **Program log** (the last lines the program wrote, the first place to look when it is not running), **Allowed checks**, **Devices never checked**, **Save or load your tuning**, and **Program**, where **Remove threat watching** is.

## Common tasks
1. See what needs fixing: **Overview**, read the **Security check**, press the button beside a risk.
2. Block an app for a child's device: **Apps & topics**, **Add a block**, choose the app, choose the device, choose the hours, **Save**.
3. Turn on threat watching: **Threats**, **Install threat watching**, read the sizes, **Install**. When it finishes, choose **Watch and tell**.
4. Quiet a harmless alert: **Alerts**, **Allow for this device**.
5. Lower its load on a small machine: **Threats**, **Settings**, choose **Light**, or lower the **Limits**.
6. Make your own set: **Threats**, **Rules**, switch groups, **Apply**. To undo, **Settings**, **Restore defaults**.
7. Go back after a bad rule update: **Threats**, **Rules**, **Roll back to the previous rules**.

## What happens if something goes wrong
These features only add protection. If they fail, the protection goes and your internet carries on: your traffic is never held up.
- A block that cannot be worked out lets the lookup through, and the Logs say so once an hour.
- Threat watching reads copies of the traffic. If it stops, crashes or is removed, nothing changes for your devices. If it uses all of its processor share for ten minutes, Gatehouse switches it off by itself, says why on this page and in the Logs, and leaves it off until you switch it on again.
- Blocking threats (**Protect**) is not available. It stays off until watching has proved itself on real traffic.
- Most traffic is encrypted, so watching finds known bad servers and attack patterns, not everything.

## Example
A new camera starts talking to an address on a bad list. The **Threats** tab shows "A device may be infected and talking to a known bad server", with the camera's name. You unplug it, or choose **Never check this device** if you know it is fine.

## If something goes wrong
If the page says **The program keeps failing**, the program starts and stops again and again; read the reason, then **Program log** under **Advanced** (or Diagnostics in the left menu). If **Install threat watching** fails, the page shows the reason; try again once the internet works and there is disk space. If the page says the program is not running, your traffic is not affected and Gatehouse keeps trying to start it. If it keeps stopping, choose **Light** under **Settings**. If a rule update breaks things, use **Roll back to the previous rules**.
