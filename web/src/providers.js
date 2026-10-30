// Internet provider presets: what a provider usually needs, so the form can be filled in with one
// choice. None of these has been tested against the real provider by the Gatehouse project, so each
// is marked unverified and the note says what to check. Providers change their setup; the letter or
// the app from your provider always wins.
export const PROVIDERS = [
  { id: 'dhcp', label: 'Cable, fibre or a modem that gives an address automatically', type: 'dhcp', vlan: 0, tested: false, note: 'Works for most cable providers and for fibre that arrives on an ordinary Ethernet socket. If you have a separate modem or ONT, plug it into the port you chose.' },
  { id: 'google-fiber', label: 'Google Fiber (USA)', type: 'dhcp', vlan: 2, tested: false, note: 'Google Fiber uses VLAN 2 when the box is replaced by your own router. Keep the network box (ONT) in place.' },
  { id: 'att-fiber', label: 'AT&T Fiber (USA)', type: 'dhcp', vlan: 0, tested: false, note: 'AT&T checks the gateway it supplied before it hands out an address. Plain Gatehouse alone may not connect: many people keep the AT&T gateway and put Gatehouse behind it in its pass-through mode.' },
  { id: 'comcast', label: 'Comcast Xfinity (USA)', type: 'dhcp', vlan: 0, tested: false, note: 'Use your own modem or the provider modem in bridge mode. If it does not connect, restart the modem so it learns the new router.' },
  { id: 'spectrum', label: 'Spectrum (USA)', type: 'dhcp', vlan: 0, tested: false, note: 'Restart the modem when you plug Gatehouse in so it hands out an address.' },
  { id: 'verizon-fios', label: 'Verizon Fios (USA)', type: 'dhcp', vlan: 0, tested: false, note: 'With the ONT connected by Ethernet this usually works as automatic. Some older installs use coax (MoCA) for TV only.' },
  { id: 'bt', label: 'BT / Openreach fibre (UK)', type: 'pppoe', vlan: 101, tested: false, note: 'Username is often bthomehub@btinternet.com with any password. Check your provider letter.' },
  { id: 'telekom', label: 'Deutsche Telekom (Germany)', type: 'pppoe', vlan: 7, tested: false, note: 'The user name is built from your access number and your Telekom number. Check your provider letter.' },
  { id: 'orange-fr', label: 'Orange (France)', type: 'dhcp', vlan: 832, tested: false, note: 'Orange fibre uses VLAN 832 with DHCP option 60 set to a value Gatehouse does not set yet, so this may not connect without the Orange box.' },
];
