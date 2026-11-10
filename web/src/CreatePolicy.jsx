import React from 'react';
import { Button, Modal } from './ui.jsx';

// One place to start any policy. Each kind keeps its own page and list; this only asks what the
// person wants to do and opens the right form there.
const KINDS = [
  { to: 'firewall/rules', name: 'Allow or block traffic', desc: 'A firewall rule between networks, devices, websites or ports, always or on a schedule.' },
  { to: 'routes', name: 'Send traffic through a VPN or connection', desc: 'A route: choose who, where to and what for, and which VPN, internet connection or group carries it.' },
  { to: 'firewall/ports', name: 'Open a port to the internet', desc: 'A port forward for an app on one of your devices.' },
  { to: 'protection/blocks', name: 'Block an app, category or website', desc: 'Keep apps, content categories or names away from everyone, a network, a profile or one device.' },
];

export default function CreatePolicy({ onClose }) {
  const pick = (to) => { onClose(); window.location.hash = `#/${to}?new=1`; };
  return (
    <Modal title="Create policy" onClose={onClose} footer={<Button onClick={onClose}>Cancel</Button>}>
      <div className="stack">
        <p className="small muted">What do you want to do? Every kind works the same way: who it covers, where to, what for, when, and it can be turned off or paused.</p>
        {KINDS.map((k) => (
          <button type="button" key={k.to} className="btn choice" onClick={() => pick(k.to)}>
            <span className="name">{k.name}</span><span className="small muted">{k.desc}</span>
          </button>
        ))}
      </div>
    </Modal>
  );
}
