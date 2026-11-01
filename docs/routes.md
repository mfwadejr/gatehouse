# Routes

## What it is for
Send some traffic through a VPN by rule: everything from certain devices, or only some destinations.

![The routes page, shown with no data yet](img/routes.png)

The picture is an example from a new, empty box; yours will show your own devices and numbers.

## What is on the page
- **Your routes**: the first route that matches is used, so put the more specific ones at the top with **Move up** and **Move down**.
- **Add a route** takes a **Name**, **Send the traffic through** a VPN, **Kill switch**, **Who uses this route?** and **Where does it apply?** (**Everything** or **Only some destinations**, by **Website**, **Country** or **Address or range**).
- VPNs that send whole devices through them are set up on the VPN page and shown below your routes.

## Common tasks
1. Send one website through a VPN: **Add**, choose **Website**, type it, choose the VPN, **Save**.
2. Send a country's traffic: choose **Country**.
3. Pause a route without deleting it: **Pause**. **Remove** deletes it and can be undone for a few seconds.

## Example
A streaming app only works from another country: add a route for its website through a VPN in that country, for your TV only.

## If something goes wrong
**Not working** means the VPN behind the route is down. **Needs two connections** means a route needs more than one internet connection. With **Kill switch** on, the traffic stops rather than leaking.
