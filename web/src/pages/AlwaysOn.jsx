import React, { useState } from 'react';
import { Button, Card, Pill, fmt } from '../ui.jsx';
import { useLiveHits } from './RuleLines.jsx';

// What the firewall does on its own, before any rule the person made. None of it can be changed
// here, and none of it was visible before. Each line names its counter in the ruleset (compile.go).
const BEHAVIOURS = [
  { tag: 'builtin:invalid', title: 'Broken connections are dropped', text: 'Packets that do not belong to any real connection are thrown away. This is how a firewall stays safe from malformed or forged traffic.' },
  { tag: 'builtin:replies', title: 'Replies come back in', text: 'When a device asks for something, the answer is let back in. Without this nothing you open would load.' },
  { tag: 'builtin:ping', title: 'Ping to this box is limited', text: 'Devices on your networks can ping NopeWarden, up to 20 a second, so a faulty device cannot flood it.' },
  { tag: 'builtin:dhcpdns', title: 'Devices can ask for an address and for names', text: 'Your networks may reach this box for DHCP and DNS. Without this, nothing could join or look anything up.' },
  { tag: 'builtin:health', title: 'Network health messages are let through', text: 'The small messages IPv4 and IPv6 need to find each other and to tell a sender a packet was too big.' },
  { tag: 'builtin:dot', title: 'Encrypted DNS sent to this box is refused quickly', text: 'Android and some apps try port 853 first. Refusing at once makes them fall back to normal DNS without waiting.' },
  { tag: 'default', title: 'Everything else is dropped', text: 'Anything no rule allows is dropped, and logged when logging is on. This is the default of the whole firewall.' },
];

export default function AlwaysOn() {
  const [open, setOpen] = useState(false);
  const hits = useLiveHits(false);
  return (
    <Card title="Always on: how NopeWarden behaves before your rules" desc={`${BEHAVIOURS.length} built-in behaviours, read-only.`}
      actions={<Button small onClick={() => setOpen((o) => !o)}>{open ? 'Hide' : 'Show'}</Button>}>
      {open && (
        <>
          <p className="small muted">These are things NopeWarden does before your own rules are looked at. Changing a rule never changes them.</p>
          <div className="list">
            {BEHAVIOURS.map((b) => {
              const h = hits.hit(b.tag, null);
              return (
                <div className="item" key={b.tag}>
                  <div><div className="name">{b.title}</div><div className="meta">{b.text}</div></div>
                  <div className="inline">
                    <span className="small muted">{hits.ready ? `${fmt.n(h.hits || 0)} packets` : ''}</span>
                    <Pill tone="good">Always on</Pill>
                  </div>
                </div>
              );
            })}
          </div>
        </>
      )}
    </Card>
  );
}
