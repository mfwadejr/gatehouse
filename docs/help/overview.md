# Overview

## What it is for
One screen that tells you whether everything is fine, and what needs you. Open it first when something feels wrong.

![The overview page, shown with example data](img/overview.png)

The picture is an example from a new, empty box; yours will show your own devices and numbers.

## What is on the page
- A status line under the title: a coloured dot, how many things need your attention ("1 thing needs your attention", "2 things need your attention"), a link to where, and when the page last updated. With one thing it reads "Open Protection →"; with several it shows the first and "and 2 more", which opens the box. The dot is red or amber when something needs you and green when nothing does. A round arrow at the right end of the line hides or shows the box, and NopeWarden remembers which you chose.
- A box with two lists: **Needs your attention** (amber and red) and **Suggestions** (blue, such as a first backup or turning on two-factor sign-in). Each row has an **Open** button. A suggestion can be **Dismissed**; it stays hidden until what it says changes, and **Show dismissed suggestions** brings it back. An amber row has **Remind me later** (in a day or in a week) and returns by itself; **Remind me now** brings it back sooner. A red row, something broken, can be hidden with the whole box but never dismissed or snoozed. The same things always show as a small dot beside the page in the left menu, whatever you hid here. Your choices are saved under your account, so they follow you to every browser.
- A large card for the internet connection: speed now, and a chart over 10 minutes, an hour, 24 hours or 30 days.
- **Devices**, **DNS**, **DHCP** and **Protection**: one card each with the numbers that matter now, such as how many devices are online and how many connections were blocked in the last day.
- **Where connections go**: a world map of where your connections went, by country, with lines from your place to the busiest countries. Choose **Outgoing**, **Incoming** or **Both**, and a period. **Both** draws green lines for what your devices connect to and amber lines for attempts from the internet, shades each country by the two added together (green where outgoing is busier, amber where incoming is), and lists each country once with its outgoing and incoming counts. Until you set your place, the lines start from the middle of your country.
- **What happened**: the latest events in plain words, newest first, with repeats folded into one row and a count.
- **Your networks**: each network and how busy it is.
- **Busiest devices** and **Data used, last 24 hours**.
- **This machine**: processor, memory and disk.

Cards update on their own. If updates stop, a "Reconnecting" label appears at the top right.

## Common tasks
1. To see why the internet is down, look at the first card. It says in words what is wrong and links to **Internet**.
2. To find the busiest device, read **Busiest devices** and click it.
3. To follow a suggestion, press **Open** beside it.
4. To start the map's lines from your home, open Settings, General, **Your place on the map** and use **Find a place** with a zip code, city or address.

## Example
The **What happened** card shows a device contacting a known-bad address nine times. Click the row to open Protection, or **Open logs** to see exactly which destinations they were.

## If something goes wrong
If a card is empty, it has nothing to show yet, or the machine has no sensor for it. If every card says it cannot refresh, check that you can still reach the machine, then press **Try again now**.

## New in 0.9.4.11
- The status line and box were reworked as described above, and the map has **Both**.
- Sign-in, setup and the browser installer show a larger badge above the name. The left menu's header is larger too, and the box at the bottom of the menu has the theme button, a **Sign out** button and a small "Version … · What's new" line.

## New in 0.9.4
- The map has **5 minute** and **15 minute** ranges as well as 1 hour, 24 hours, 7 days and 30 days. Hover a country to see a popout beside it with its numbers.
- **Your VPNs** card lists tunnels with their state and speed, capped to the first few; open the VPN page for the rest. A tunnel with no traffic shows a grey **Idle** pill instead of a warning.
- Each VPN and each network has its own traffic graph and totals on its own page (the **Traffic** button on Networks).
