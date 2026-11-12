# Protection

## What it is for
One place for keeping the network safe: a check of what to fix, and blocking apps, categories and websites. Watching for threats has its own page, Threats. Block lists for ads, trackers and adult sites stay under DNS.

## What is on the page
Tabs along the top:
- **Overview**: **Protection at a glance** gives one sentence of verdict and a line each for the security check, your blocks and threat watching. Below it, the **Security check** lists what to fix, with a button that goes to the right page.
- **Blocks**: the **Blocks** card lists one row per block: its name, what it holds, where it applies, when, how, and how many lookups it refused in the last day. **Add a block** takes an optional name and lets you choose many apps, categories and websites in one block, then where it applies (everyone, or any mix of networks, profiles and devices), how, and when (always or at set hours). **Start from** fills the form with Social media, Streaming, Gaming or AI chat, ready to change. **Pause…** stops a block for a set time or until you resume it. Blocks have the same controls as firewall rules and routes: pause, turn off, edit, remove.
- **Lists and labels**: **Lists used for blocking** shows which version of the app names and category lists blocking uses, and how fresh each download is; a list that could not be downloaded says so and the last good copy keeps working. **Traffic labels** names the app behind each connection by looking at the first packets (off by default).
- **Exceptions** is a separate card. **Add an exception** lets things through for a device, profile or network even when a block says no.
- Threat watching has its own page, **Threats**, in the left menu under Security.

## Common tasks
1. See what needs fixing: **Overview**, read the **Security check**, press the button beside a risk.
2. Block several apps for a child's devices: **Blocks**, **Add a block**, give it a name, choose the apps, choose the devices, choose the hours, **Save**.
3. Let one device keep an app: **Exceptions**, **Add an exception**, choose the app and the device, **Save**.

## What happens if something goes wrong
These features only add protection. If they fail, the protection goes and your internet carries on: your traffic is never held up. A block that cannot be worked out lets the lookup through, and the Logs say so once an hour.

## If something goes wrong
If a block does not seem to work, check that it is switched on and that the device uses NopeWarden for DNS. Blocking by name cannot catch an app that uses its own private DNS; choose both ways to cover more. Problems with threat watching are on the Threats page.

## Names inside a category, and your own names per profile
- **View names** next to a category shows the names it blocks. Search, and allow a name that should not be blocked.
- Each profile can have its own **custom blocked names**.
- **Security check**: each fix button opens the exact window that changes the setting.

## Removing things a block uses
When you remove a network, a profile or a device, it also leaves the blocks that used it. The removal question names those blocks first. A block left with no place shows **Applies nowhere** until you choose one.

## Deleting a block
Delete removes the block at once and shows an Undo button for a few seconds. Nothing asks "Are you sure?".
