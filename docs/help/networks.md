# Networks

## What it is for
Your own networks, such as Home, Guests or Cameras. Each one is separate. What each can reach is decided on the Firewall page.

![The networks page, shown with example data](img/networks.png)

The picture uses made-up devices and numbers. Yours will show your own.

## What is on the page
- **Network layout** is a map of the box. The internet connection is at the top and each of your networks sits below it, with live speeds, devices online and tags.
  - When two or more internet connections share the load, every working one shows as in use, with its share and its own live speed. Until you apply the change it says "Sharing is saved, not applied yet". "Standing by" is only for a connection that waits for another to fail.
  - Select a network to open it. Select a tag to open the page that explains it: devices online opens Devices & access, DHCP opens DHCP, Guest opens Firewall, Pool full opens the DHCP address pools.
  - If you have set a management port, a dashed box under the networks shows it. It has the port's name and address, whether it answers, its live speed, and whether the console is closed on the other networks. Nothing is routed through it. Select it to open Settings, Managing this device.
  - **What changed** has its own 1 h, 24 h and 7 d choice. It lists recent updates and warnings, or a line saying nothing changed.
- **Your networks** has one row per network with its address, its ports and whether it hands out addresses (DHCP). **Add a network** makes another.
- **Edit** a network to change its **Address of this router on the network**, the **Port** it is plugged into, a **VLAN number** if it shares a port, and **What it can reach**. **In use** switches it off without deleting it.
- **Ports on this network** and **Add more ports to this network**: tick the ports that act as a switch for this network. Devices on any ticked port share its addresses.
- **Network ports on this device** shows the live status and addresses of every port. **Rename** gives a port a name you will recognise, such as your internet gateway or a spare. If the port carries an internet connection, that connection takes the same name, and renaming the connection renames the port.

## Wi-Fi and wireless
Wireless is not part of NopeWarden. The installer has no Wi-Fi drivers or firmware, the console has no Wi-Fi page, and a Wi-Fi card in the machine is ignored. It cannot be used as an internet connection or as a network.

For Wi-Fi, plug a separate access point or Wi-Fi router into a port or VLAN on Networks, choose the type that fits (Guest for visitors), and set up the Wi-Fi name and password on that device. NopeWarden still filters, names and protects every device that connects through it, because they all arrive over a wired port.

## Common tasks
1. Add a network: **Add a network**, choose what it is for (**Trusted**, **Private**, **Guest**, **Isolated** or **Custom**), choose **The whole port** or **A VLAN on the port**, **Save**, **Review and apply**. A Guest network keeps its devices from seeing each other. Turn on **Devices here can talk to each other** only if they need to.
2. Connect a switch or access point that hands out addresses itself (this is also how Wi-Fi is added, see above): choose **A VLAN on the port** and turn on **No address, no DHCP**. NopeWarden makes the VLAN and the firewall network and gives out nothing.
3. Name a port: **Rename**, type a name, **Save**. **Use the original name** puts it back.
4. Give a network more ports: **Edit**, **Add more ports to this network**, tick them, **Save**.

## Example
Make a Cameras network on its own port with the address 192.168.30.1/24, and on the Firewall page let it reach nothing but itself.

## If something goes wrong
NopeWarden refuses a port that already belongs to another network or the internet, and says which. Nothing changes until you apply, and the 60-second way back covers it.
