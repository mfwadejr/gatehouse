import React, { useMemo, useState } from 'react';
import { del, holdAnd, put, useData } from '../api.js';
import { SAVED_LATER, SaveChoice } from '../changes.jsx';
import { Button, Card, Empty, Field, Input, Modal, Page, Paged, useAction, useUndoDelete } from '../ui.jsx';

// Services: the one place to see what NopeWarden knows by name and to make your own. A service is a
// name for one or more ports. Rules pick services by searching for the name or a port.

function ServiceForm({ initial, onClose, onSaved }) {
  const [name, setName] = useState(initial?.name || '');
  const [text, setText] = useState((initial?.ports || []).join('\n'));
  const [run, busy] = useAction();
  const ports = text.split(/[\s,]+/).filter(Boolean).map((x) => x.toLowerCase());
  return (
    <Modal title={initial?.id ? `Edit ${initial.name}` : 'New service'} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><SaveChoice busy={busy} disabled={!name.trim() || !ports.length} onSave={async (later) => { const body = { id: initial?.id || 0, name: name.trim(), ports }; if (await run(() => (later ? holdAnd(() => put('/api/fw/portlists', body)) : put('/api/fw/portlists', body)), later ? SAVED_LATER : 'Saved.')) { onSaved(); onClose(); } }} /></>}>
      <Field label="Name" hint="What you will search for when you make a rule. If you rename it, open ports and rules that use it follow the new name."><Input value={name} onChange={setName} placeholder="Plex" /></Field>
      <Field label="Ports" hint="One per line, like tcp/32400 or udp/27000-27050. A built-in name such as https also works.">
        <textarea className="in mono" rows={6} value={text} onChange={(e) => setText(e.target.value)} spellCheck={false} placeholder={'tcp/32400\nudp/1900'} />
      </Field>
    </Modal>
  );
}

export default function Services() {
  const { data, reload } = useData('/api/fw/services');
  const [q, setQ] = useState('');
  const [edit, setEdit] = useState(null);
  const [run] = useAction();
  const undoDelete = useUndoDelete();
  const needle = q.trim().toLowerCase();
  const hit = (x) => !needle || x.name.toLowerCase().includes(needle) || x.ports.some((p) => p.includes(needle));
  const mine = useMemo(() => (data?.custom || []).filter(hit), [data, needle]);
  const known = useMemo(() => (data?.builtin || []).filter(hit), [data, needle]);
  return (
    <Page title="Services" sub="Names for ports. Make your own, then pick them by name when you make a firewall rule."
      actions={<Button kind="primary" icon="plus" onClick={() => setEdit({})}>New service</Button>}>
      <Card title="Search" desc="Find a service by name to see which ports it uses.">
        <Input value={q} onChange={setQ} placeholder="Search by name or port number" aria-label="Search services" />
      </Card>
      <Card title="Your services" desc="Made by you. Used in rules by name.">
        <div className="list">
          {mine.map((s) => (
            <div className="item" key={s.id}>
              <div>
                <div className="name">{s.name}</div>
                <div className="meta mono">{s.ports.join(', ')}</div>
                <div className="meta">{s.used_by.length ? <>Used by {s.used_by.join(', ')}</> : 'Not used yet'}</div>
              </div>
              <div className="actions">
                <Button small icon="edit" onClick={() => setEdit(s)}>Edit</Button>
                <Button small kind="danger" icon="trash" onClick={() => (s.used_by.length ? run(() => del(`/api/fw/portlists/${s.id}`)).then(reload) : undoDelete(`The service "${s.name}"`, () => del(`/api/fw/portlists/${s.id}`), () => put('/api/fw/portlists', { name: s.name, ports: s.ports }), reload))}>Delete</Button>
              </div>
            </div>
          ))}
          {!mine.length && <Empty>{needle ? 'None of your services match.' : 'No services of your own yet. Choose New service to make one.'}</Empty>}
        </div>
      </Card>
      {(data?.open_ports || []).length > 0 && (
        <Card title="Open ports" desc="Apps you have opened to the internet. Change them on the Firewall page, Open ports tab.">
          <div className="list">
            {data.open_ports.map((g) => <div className="item" key={g.group}><div><div className="name">{g.group}</div><div className="meta">{g.text}</div></div></div>)}
          </div>
        </Card>
      )}
      <Card title="Built in" desc="NopeWarden already knows these by name. They cannot be changed.">
        <div className="list">
          <Paged items={known}>{(shownKnown) => shownKnown.map((s) => (
            <div className="item" key={s.name}>
              <div><div className="name">{s.name}</div><div className="meta mono">{s.ports.join(', ')}</div></div>
            </div>
          ))}</Paged>
          {!known.length && <Empty>No built-in service matches.</Empty>}
        </div>
      </Card>
      {edit && <ServiceForm initial={edit} onClose={() => setEdit(null)} onSaved={reload} />}
    </Page>
  );
}
