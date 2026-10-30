import React, { useState } from 'react';
import { post, useData } from '../api.js';
import { Button, Card, Empty, Field, Input, Modal, Note, Pill, useAction, useStepUp, stepReady, StepUp } from '../ui.jsx';

// Undo. Gatehouse saves a restore point just before every firewall or network change goes live.
// Revert puts the settings back as they were, as changes that are not live yet: you review and apply
// them the usual way, with the usual 60 seconds to take it back.

const when = (t) => new Date(t * 1000).toLocaleString([], { weekday: 'short', day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });

export default function Changes({ me }) {
  const { data, reload } = useData('/api/changes', { every: 30000 });
  const [pick, setPick] = useState(null);
  const [code, setCode] = useState('');
  const [pw, setPw] = useState('');
  const su = useStepUp();
  const [run, busy] = useAction();
  if (!data) return null;
  const go = async () => {
    const r = await run(() => post('/api/changes/revert', { name: pick.name, code, password: pw }), 'Reverted. Review and apply to put it live.');
    if (r) { setPick(null); setCode(''); setPw(''); reload(); }
  };
  return (
    <Card title="Changes" desc={`Before every change to the firewall or network, Gatehouse keeps a copy of how things were (the last ${data.keep}). Pick a point to go back to it.`}>
      {data.points.length === 0 ? <Empty>No changes yet. The first one you apply will appear here.</Empty> : (
        <div className="list">
          {data.points.map((p, i) => (
            <div className="item" key={p.name}>
              <div><div className="name">Before the change on {when(p.created)} {i === 0 && <Pill>Latest</Pill>}</div><div className="meta">{p.ok ? 'Checked and ready to revert to.' : 'This copy did not pass its check, so it cannot be used.'}</div></div>
              <Button small disabled={!p.ok || busy} onClick={() => setPick(p)}>Revert to this</Button>
            </div>
          ))}
        </div>
      )}
      {pick && (
        <Modal title="Go back to this point?" onClose={() => setPick(null)}
          footer={<><Button onClick={() => setPick(null)}>Cancel</Button><Button kind="primary" disabled={busy || !stepReady(su, code, pw)} onClick={go}>Revert settings</Button></>}>
          <Note title="Nothing changes on the network yet">The settings go back to how they were before {when(pick.created)}. A copy of what you have now is saved first. Then review and apply as usual; if it goes wrong, the 60-second rollback brings the current settings back.</Note>
          <StepUp su={su} code={code} setCode={setCode} password={pw} setPassword={setPw} />
        </Modal>
      )}
    </Card>
  );
}
