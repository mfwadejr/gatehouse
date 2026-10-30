import React, { useState } from 'react';
import { Button, Card, Pill } from '../ui.jsx';

// Recipes: common goals in plain words. Each says what you will end up with, the steps in order,
// and a button that opens the page where the first step happens. Nothing here changes anything by
// itself.
const RECIPES = [
  {
    id: 'guest', title: 'Guest Wi-Fi', page: 'networks', go: 'Start on Networks',
    what: 'Visitors get internet, but cannot see your computers, cameras or this console.',
    steps: ['On Networks, add a network and choose the type Guest.', 'Give it its own address range and turn on DHCP so devices get addresses.', 'On Wi-Fi, add a network name and password and put it on that network.', 'Apply. Test by joining the guest Wi-Fi from a phone.'],
  },
  {
    id: 'cameras', title: 'Cameras on their own network', page: 'networks', go: 'Start on Networks',
    what: 'Cameras can talk to each other and to your recorder, but not to the internet, so they cannot send video away.',
    steps: ['On Networks, add a network and choose the type Isolated.', 'Plug the cameras (or their switch) into the port you chose, or use a VLAN.', 'Apply.', 'If your recorder is on another network, add one rule on the Firewall page that lets only the recorder reach the cameras.'],
  },
  {
    id: 'plex', title: 'Reach Plex from outside', page: 'firewall', go: 'Open the Firewall page',
    what: 'You can watch your library away from home, with the door open only for the countries you choose.',
    steps: ['Give the Plex machine a fixed address on the Devices page (Always use this address).', 'On Firewall, Open ports, choose Plex from the app list and pick the machine.', 'Limit it to your own country, or to the addresses that need it.', 'Apply. Check from your phone on mobile data.'],
  },
  {
    id: 'unifi', title: 'Use my UniFi access points', page: 'networks', go: 'Start on Networks',
    what: 'Your UniFi access points broadcast Wi-Fi, and Gatehouse stays the router.',
    steps: ['Put the access points on a network of their own (a Trusted network on a PoE switch port is fine).', 'Set them up with the UniFi mobile app in its stand-alone mode, or with a UniFi controller you already run.', 'Match each Wi-Fi name to a network or VLAN you made on Networks.', 'Gatehouse does not manage UniFi access points by itself yet; this is the recipe that works today.'],
  },
];

export default function Recipes({ bare }) {
  const [open, setOpen] = useState('');
  const Wrap = bare ? ({ children }) => <div>{children}</div> : (p) => <Card {...p} />;
  return (
    <Wrap title="Common setups" desc="Pick what you want to do. Each one lists the steps in order and takes you to the first.">
      <div className="list">
        {RECIPES.map((r) => (
          <div className="item" key={r.id} style={{ display: 'block' }}>
            <div className="row" style={{ justifyContent: 'space-between', gap: 8 }}>
              <div><div className="name">{r.title}</div><div className="meta">{r.what}</div></div>
              <Button small onClick={() => setOpen(open === r.id ? '' : r.id)}>{open === r.id ? 'Hide steps' : 'Show steps'}</Button>
            </div>
            {open === r.id && (
              <div className="stack" style={{ marginTop: 'var(--space-2)' }}>
                <ol style={{ margin: 0, paddingLeft: '1.2rem' }}>{r.steps.map((s, i) => <li key={i}>{s}</li>)}</ol>
                <div><Button small kind="primary" onClick={() => { window.location.hash = `#/${r.page}`; }}>{r.go}</Button> {r.id === 'unifi' && <Pill>Built-in controller: planned</Pill>}</div>
              </div>
            )}
          </div>
        ))}
      </div>
    </Wrap>
  );
}
