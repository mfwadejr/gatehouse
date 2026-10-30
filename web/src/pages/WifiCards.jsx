import React, { useState } from 'react';
import { del, put, useData } from '../api.js';
import { Button, Card, Field, Input, Modal, Note, Pill, ToggleRow, confirmBox, useAction } from '../ui.jsx';

// Wi-Fi join cards: a QR code that joins the network when scanned, and a card to print. Works for
// the Wi-Fi Gatehouse broadcasts and for any other access point, because it only needs the name and
// password. Passwords for other access points are kept sealed, like the other saved passwords.
export default function WifiCards() {
  const { data, reload } = useData('/api/wificards');
  const [edit, setEdit] = useState(null);
  const [show, setShow] = useState(null);
  const [run, busy] = useAction();
  if (!data) return null;
  const save = async () => {
    const r = await run(() => put('/api/wificards', { id: edit.id || 0, title: edit.title || '', ssid: edit.ssid || '', psk: edit.psk || '', hidden: !!edit.hidden }), 'Saved.');
    if (r) { setEdit(null); reload(); }
  };
  const remove = async (c) => {
    if (!(await confirmBox(`Forget ${c.ssid}? Only the saved name and password for this card are deleted.`))) return;
    run(async () => { await del(`/api/wificards/${c.id}`); reload(); }, 'Deleted.');
  };
  return (
    <Card title="Join cards and QR codes" desc="Give guests a code to scan, or a card to print, with the Wi-Fi name and password. For Wi-Fi from another access point, type its name and password once."
      actions={<Button small onClick={() => setEdit({ hidden: false })}>Add another Wi-Fi</Button>}>
      {data.length === 0 && <p className="muted small">No Wi-Fi to make a card for yet. Broadcast a network from a card above, or add another access point's Wi-Fi.</p>}
      <div className="list">
        {data.map((c) => (
          <div className="item" key={c.key}>
            <div><div className="name">{c.ssid} {c.own ? <Pill>Broadcast by Gatehouse</Pill> : <Pill>Other access point</Pill>} {!c.has_psk && <Pill tone="warn">Open, no password</Pill>}</div></div>
            <div className="actions">
              <Button small onClick={() => setShow(c)}>Show QR code</Button>
              <a className="btn small" href={`/api/wificards/print?src=${encodeURIComponent(c.key)}`} target="_blank" rel="noreferrer">Print card</a>
              {!c.own && <><Button small onClick={() => setEdit({ id: c.id, title: c.title, ssid: c.ssid, hidden: c.hidden, psk: '' })}>Edit</Button><Button small onClick={() => remove(c)}>Delete</Button></>}
            </div>
          </div>
        ))}
      </div>
      {show && (
        <Modal title={show.ssid} onClose={() => setShow(null)} footer={<Button onClick={() => setShow(null)}>Close</Button>}>
          <div style={{ textAlign: 'center' }}><img alt={`QR code to join ${show.ssid}`} src={`/api/wificards/qr?src=${encodeURIComponent(show.key)}`} width="280" height="280" /></div>
          <p className="small muted" style={{ textAlign: 'center' }}>Point a phone camera at it to join.</p>
        </Modal>
      )}
      {edit && (
        <Modal title={edit.id ? 'Edit Wi-Fi card' : 'Add another Wi-Fi'} onClose={() => setEdit(null)}
          footer={<><Button onClick={() => setEdit(null)}>Cancel</Button><Button kind="primary" disabled={busy || !edit.ssid} onClick={save}>Save</Button></>}>
          <Field label="Wi-Fi name"><Input value={edit.ssid || ''} onChange={(v) => setEdit({ ...edit, ssid: v })} /></Field>
          <Field label="Password" hint={edit.id ? 'Leave empty to keep the saved password.' : 'Leave empty for an open network. Kept sealed, never shown in lists.'}><Input type="password" value={edit.psk || ''} onChange={(v) => setEdit({ ...edit, psk: v })} autoComplete="new-password" /></Field>
          <ToggleRow title="Hidden network" desc="The name is not announced; the code tells the phone to look for it." checked={!!edit.hidden} onChange={(v) => setEdit({ ...edit, hidden: v })} />
          <Note>Gatehouse does not set this Wi-Fi up. It only makes the card from what you type, so the name and password must match what the access point uses.</Note>
        </Modal>
      )}
    </Card>
  );
}
