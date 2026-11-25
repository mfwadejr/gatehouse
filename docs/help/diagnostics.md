# Diagnostics

## What it is for
Answers one question: is NopeWarden itself working? It shows how each part is doing, lets you read the system log, test the connection, and, if you switch it on, open a command line on the firewall. It is for the day something is not working and there is no screen or keyboard on the machine.

## What is on the page
Tabs along the top:
- **Health & services**: the top card, **Is NopeWarden itself working?**, gives one sentence (**Everything is working.** or how many things need a look), with problems first and a **Look** button that opens the right place. Under it are facts: how long NopeWarden and the machine have been running (a restart you did not expect stands out), when the firewall was last applied, disk and log space, whether the clock is set, and warnings in the last hour. Then **Services**: each part of the system with a state: Running, Starting, Failing or Off. A failing one says why in plain words, such as the last thing it complained about. **Show log** opens its log and **Restart** restarts it. **Save a support bundle** saves a text file with the machine, the network, every service's state and recent log lines. Anything that looks like a password or key is hidden in it. Send it when asking for help.
- Below those, **Drive health** gives each drive one word: **Healthy**, **Worn**, **Warning** or **Not reported**. A drive is amber or red only when it reports a problem; a worn-out one is blue first. **Details** opens a blue box with what it reported, and a line for what this hardware did not report. Virtual disks say so. If the small SMART program is missing, **Install the SMART tool** installs it. **Certificates** lists the days left on each VPN authority, server and user certificate and on the link between two machines in a pair: blue about 60 days out, amber inside 30, red once one has run out. The console's own certificate is listed only if it is in its last 30 days. Both also appear in the Overview list and the Diagnostics menu dot.
- **Drive health** also watches over time. Every 15 minutes NopeWarden notes each drive's sectors moved to spare, sectors waiting, cable errors, media errors and wear, and says in plain words when one has risen, for example "3 more sectors moved to spare than a week ago". A rise, a drive that was there and has gone, a drive that starts reporting a problem, or disk errors in the system log turn the drive amber or red, show on the Overview list and the Diagnostics menu dot, and send the alert **A drive has a problem** if it is on (when it is off and a drive has a problem, the card says so). The **Self-test schedule** below the drives sets when each drive runs its own short and long self-test, by default a short one every Sunday at 03:00 and a long one on the first Sunday of the month at 03:30. **Details** has **Run a short self-test** and **Run a long self-test** to start one now, and the last result shows beside the verdict. A line under the list says whether a backup has been downloaded to somewhere other than this machine's own drive.
- **Power (UPS)** watches a battery backup, such as an APC, on a USB cable or on the network. **Set up UPS monitoring** installs the small UPS program and starts watching. The card shows the model, **Battery charge**, **Time left on battery**, **Load**, **Input voltage** and **Battery health**. Under **What happens when**, each of **Power cut**, **Battery low**, **Battery needs replacing**, **Power is back** and **UPS not responding** has its own switches: write it in the log, send an alert, show on Overview, and (for the first, second and last) shut down cleanly. A low battery shuts the machine down cleanly after the wait you set, and if the power returns first the shutdown is cancelled; **Cancel the shutdown** stops one that is waiting. **Switch the UPS off and back on with the machine** tells the UPS to cut its output and start again when the power returns, so the machine comes back by itself; the machine's firmware setting "Restore on AC power loss" must be "Power on". **Test power cut** plays a power cut, a low battery and the power coming back, with real alerts and log lines marked as a test; the machine is not shut down unless you tick the box. **Stop watching the UPS** switches it off. The alert is **The power fails (UPS)**.
- **System log**: messages from NopeWarden and the machine. Only warnings and errors show until you tick **Everything**. Choose **From** (everything, or one service), **Time**, and **Search**: every word must appear, the box looks through up to 5000 lines, and the words are highlighted. **Follow** refreshes every 5 seconds. Known messages get a plain explanation next to them.
- **Tools**: **Test my internet**, ping, traceroute, name lookups, and **Why can't it reach that?** for one device and one site. These are the only copies; Internet, Troubleshoot points here.
- **Terminal**: a command line on the firewall. See below.

**Console connections**, under Services, records the live connection your browser keeps open: one row for each visit, with when it opened, from which address, how long it lasted and why it ended. The latest 5 are shown; **Show older** lists the rest. **What is using the disk** lists the biggest places on the disk, three at first; **Show all** lists the rest. A failed write usually means the network between you and the box dropped it.

The **Log** card under Services has **Show the log of** (one service, or **Warnings from everything**), **How many lines**, **Only lines containing** and **Refresh**.

- **My network** (bottom of the **Health & services** tab): a printable summary for a handover or your records, with a map. It never contains passwords or keys.

## Common tasks
1. Find out why a service is not working: **Health & services**, read the reason, press **Show log**.
2. Restart a service that is stuck: **Restart**.
3. Get help: **Save a support bundle**, and send the file.
4. Run a command by hand: **Terminal**, **Switch the terminal on**, enter your password or code, then **Open a terminal**.

## The terminal
A terminal is a full command line with the highest rights on the machine. Anyone who can sign in as an administrator and pass the check can do anything on it, so NopeWarden treats it carefully:
- It is off until you switch it on, and only administrators can use it.
- Switching it on asks for your password, or your authenticator code if you use one. Opening it asks again only if the last check was more than 15 minutes ago.
- It closes by itself when nobody has typed for 10 minutes, and after two hours at most. Two can be open at once.
- The Logs record who opened it, when, from where, and when it closed. What is typed is not recorded.
- It cannot hold up your traffic by itself. If you break something from it, that is on the command you typed.

## If something goes wrong
If a service shows **Failing**, read its reason and its log; the reason is the last line it printed that looked like an error. If the page itself shows nothing, you may not be signed in as an administrator. If the terminal will not open, check that it is switched on and that two terminals are not already open. If the terminal stops responding, **Close the terminal** and open a new one.
