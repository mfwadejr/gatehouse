# Updates

## What it is for
Keeping NopeWarden, and the system underneath it, up to date. Updates have their own page so you can see at a glance whether one is waiting, and what happened to the last one. It has four tabs: Updates, Schedule, What's new and Advanced.

## What is on the page
- **Updates**: the version you are running, when NopeWarden last looked, and what is ready: a new NopeWarden version, system updates, or both. **Check now** looks straight away. **Update now** installs everything that is ready. While it installs, the page shows a ticked list of steps; the console restarts near the end and the page reconnects by itself. If a restart is needed afterwards, **Restart now** appears here.
- **Schedule**: **Updating** is **Off** (only when you press **Check now**), **Notify** (NopeWarden looks as often as you choose and tells you) or **Automatic** (it installs by itself at the hour you choose in **Start automatic updates at**). **How often to check** sets how often it looks. **Other recurring jobs** lists the same schedule for block lists, country lists and the other jobs.
- **What's new**: what changed in each version, newest first.
- **Advanced**: **Release page**, the place releases are fetched from. Only releases signed with this build's key are ever installed.

## Where you are told about an update
- A blue notice at the top of the Overview says an update is ready, whichever way **Updating** is set. **Update now** opens this page. **Remind me later** hides the notice until tomorrow morning; **Dismiss** hides it for that version only. A newer version shows it again.
- A yellow notice means the system was updated and a restart is needed. A red one means the last update did not work and was undone. These stay until the restart happens or the next update works.
- The Updates entry in the left menu shows a blue dot when an update is ready, yellow when a restart is needed and red when the last update failed. The line at the bottom of the menu says **Update available** too.

## Common tasks
1. Install an update: **Updates**, **Update now**. Leave the page open; it reconnects when the console restarts.
2. Have updates install at night: **Schedule**, **Automatic**, then pick the hour.
3. Be told but stay in charge: **Schedule**, **Notify**.

## If something goes wrong
If an update does not work, the old version is put back automatically and the page says so, with the reason. Nothing is lost. If this page cannot reach the machine during an update, look at the machine's own screen: it shows what is happening.
