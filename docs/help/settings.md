# Settings

## What it is for
Your account, and how this box is run. It is split into five tabs.

![The settings page, shown with no data yet](img/settings.png)

The picture is an example from a new, empty box; yours will show your own devices and numbers.

## What is on the page
- **General**: **Device mode** (Router or Bridge, with a three-step change), **Updates** (**Check now**, **How often to check**, **Off**, **Notify** or **Automatic**), **Power** (restart or shut down), **Console address** and **Appearance**.
- **Account & sign-in**: **Two-factor sign-in**, **Password**, **Where you are signed in** and **Remote login** (SSH, off unless you turn it on; if the machine has no SSH identity yet, Gatehouse makes one when you turn it on).
- **Alerts & logs**: **Alerts** (ntfy or email, with quiet hours), **Send logs elsewhere**, **Send traffic records (NetFlow / IPFIX)** and SNMP.
- **Backup & recovery**: **Backups**, **Changes** (a copy before every change, to go back to), **My network** (a printable summary with a map) and **Reset to default**.
- **Advanced**: **High availability** (two boxes as a pair), **Bridge mode** (all the detailed settings), **Traffic labels**, **Guest sign-in page** and **Lists used for blocking** (the version of the app names and topic lists Gatehouse blocks by).

Administrators see all five tabs. Other accounts see only their own sign-in settings and Appearance. The page reopens on the tab you used last.

## Common tasks
1. Change mode: **General**, **Device mode**, **Change**, then follow the three steps. Nothing is saved until the last step, and you have 60 seconds to keep it.
2. Turn on two-factor sign-in: **Account & sign-in**, **Turn on**, scan the code.
3. Make a backup: **Backup & recovery**, **Back up now**.

## Example
Use **Notify** for updates: Gatehouse tells you when one is ready and you press **Update now** when it suits you.

## If something goes wrong
A mode change that is not kept goes back on its own, and the card says so. See Getting back in for lock-outs.
