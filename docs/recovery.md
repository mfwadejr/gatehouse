# Getting back in

Gatehouse is built so that a wrong setting cannot lock you out for good. This page lists the ways back, from the easiest.

## A change went wrong and you can still see the console

Every network and firewall change you apply starts a 60-second countdown. If you can still reach the console, the change is kept on its own. If you cannot, everything goes back by itself after the minute. To go back sooner, press **Undo now** on the countdown.

To go back to an earlier point later, open **Settings, Backup & recovery** and use **Changes**. It keeps a copy from before each change. Pick a point to return to it.

## You cannot reach the console from your computer

1. Check the cable and that your computer is on the network you manage Gatehouse from.
2. Look at the screen of the Gatehouse machine. It always shows the console address.
3. Open that address in your browser. If you changed the console port and forgot it, the screen shows the port too.

## You are locked out of the console

Use the recovery screen on the machine's own keyboard and screen. It does not need the network, and it only works at the machine, never over the network.

1. Press **Alt+F2** on the machine to open the recovery screen.
2. Choose what you need: set a new password for an account, clear an account's two-step sign-in, restore the safe starting firewall rules, put the console back on port 443, or remove any limit on who may manage Gatehouse.
3. Restart the console or the machine if the screen says so, then sign in again.

## You forgot your password, or lost your authenticator

Both are fixed from the recovery screen above: set a new password, or clear two-step sign-in for the account and turn it on again after you are in.

## The internet is down

Open **Overview** or **Internet**. Each connection shows what is wrong in words: **No link** means check the cable or modem, **No internet** means the modem is answering but the provider is not. Restart the modem first. If you have two connections, Gatehouse uses the other one on its own.

## Last resort

Under **Settings, Backup & recovery**, **Reset network settings** puts networks, firewall rules, port forwards, DHCP, DNS names and lists, VPNs and Wi-Fi back to a clean start, and keeps your accounts, sign-in settings, update settings and console port. **Factory reset** erases everything and opens the setup wizard again. Both save a copy of your settings first, which you can download, and ask for your authenticator code if you use one, or your password.
