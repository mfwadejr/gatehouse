# Getting started

This guide takes you from a blank machine to a working network in about ten minutes. Everything you set up here can be changed later.

## What you need

- A small computer with at least two wired network ports. It becomes your firewall, DNS server and DHCP server.
- The NopeWarden installer image, written to a USB stick.
- Your modem or provider's cable, and a switch or a computer for your own network.
- If you want Wi-Fi, a separate access point or Wi-Fi router plugged into your own network. NopeWarden does not include wireless support. It has no Wi-Fi drivers or firmware, and a Wi-Fi card in the machine is ignored. See Wi-Fi and wireless in the Networks help.

## Install

1. Plug the USB stick into the machine and start it from the stick.
2. Follow the installer. You always choose which port is the internet and which is your own network, and the installer says beside each connected port whether a DHCP server answers on it. For your own network it asks for NopeWarden's address and whether NopeWarden should hand out addresses (DHCP) there. If another DHCP server already answers on that network, the installer turns this off and says why, because two servers on one network give computers conflicting addresses. You can turn it on anyway, and change it later in setup. If both ports seem to be on one network, which happens in a virtual machine when both adapters share a network, the installer warns you but lets you continue. Give the LAN port its own virtual network instead.
3. When it finishes, remove the stick and restart.

## First sign-in

1. On a computer plugged into your own network, open the address shown on the machine's screen. It looks like **https://192.168.1.1**. Your browser warns that the certificate is not trusted, because the box made its own. Continue anyway; the connection is still encrypted.
2. Create the administrator account. Use at least 12 characters. A few random words is easy to remember and hard to guess.
3. Answer **How will NopeWarden be used?** Choose **NopeWarden is my router** for most homes. Choose the bridge option only if another router must stay in charge. The wizard then asks for the internet cable and your own network.

Nothing is switched on until you finish the wizard. The page then tells you the address to use if it changed.

## After setup

- Open **Overview**. A short checklist shows what is left, such as turning on two-factor sign-in and making a first backup.
- Open **Devices & access** to name your devices and give them profiles.
- Make a backup under **Settings, Backup & recovery**.

## If you cannot get in

See [Getting back in](recovery.html). You will not be locked out for good: the machine's own screen always has a recovery menu.
