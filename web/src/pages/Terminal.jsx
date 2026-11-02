import React, { useEffect, useState } from 'react';
import { del, post, put, useData } from '../api.js';
import { Button, Card, Modal, Note, Pill, useAction, useStepUp, stepReady, StepUp } from '../ui.jsx';

// A command line on the firewall, in the browser. Off until the owner switches it on, asks for the
// password or code when switched on and again when opened unless confirmed in the last 15 minutes, ends by itself when idle, and each open and close is
// recorded in the Logs (what is typed is not). It cannot touch traffic by itself. The terminal
// emulator runs in its own page (terminal.html) shown here in a frame, because it needs a looser
// style rule than the rest of the console.

function Session({ id, onEnd }) {
  useEffect(() => {
    const on = (e) => { if (e.origin === window.location.origin && e.data?.gatehouseTerminal === 'ended') onEnd(true); };
    window.addEventListener('message', on);
    return () => window.removeEventListener('message', on);
  }, [onEnd]);
  return <iframe className="termhost" title="Terminal" src={`/terminal.html?id=${encodeURIComponent(id)}`} />;
}

export default function TerminalTab() {
  const { data, reload } = useData('/api/term');
  const [run, busy] = useAction();
  const su = useStepUp();
  const [ask, setAsk] = useState(null); // 'on' | 'open'
  const [code, setCode] = useState('');
  const [pw, setPw] = useState('');
  const [id, setId] = useState(null);
  const [ended, setEnded] = useState(false);
  if (!data) return null;
  const ready = stepReady(su, code, pw);
  const reset = () => { setAsk(null); setCode(''); setPw(''); };
  const openNow = async (c = '', p = '') => {
    const r = await run(() => post('/api/term/open', { rows: 24, cols: 80, code: c, password: p }));
    if (r?.id) { reset(); setEnded(false); setId(r.id); }
  };
  const go = async () => {
    if (ask === 'on') {
      if (await run(() => put('/api/term', { enabled: true, code, password: pw }), 'The terminal is on.')) { reset(); reload(); }
    } else {
      await openNow(code, pw);
    }
  };
  const close = async () => { const cur = id; setId(null); if (cur && !ended) await run(() => del(`/api/term/${cur}`)); setEnded(false); };
  return (
    <Card title="Terminal" desc="A command line on the firewall, for when something needs looking at by hand."
      actions={<Pill tone={data.enabled ? 'good' : ''}>{data.enabled ? 'On' : 'Off'}</Pill>}>
      <div className="stack">
        <Note tone="warn" title="Read this first">{data.warning}</Note>
        {!data.enabled && <div className="actions"><Button kind="primary" disabled={busy} onClick={() => setAsk('on')}>Switch the terminal on</Button></div>}
        {data.enabled && !id && (
          <div className="actions">
            <Button kind="primary" disabled={busy} onClick={() => (data.confirmed ? openNow() : setAsk('open'))}>Open a terminal</Button>
            <Button disabled={busy} onClick={() => run(() => put('/api/term', { enabled: false }), 'The terminal is off.').then(reload)}>Switch the terminal off</Button>
          </div>
        )}
        {id && (
          <>
            <Session id={id} onEnd={setEnded} />
            <div className="actions"><Button disabled={busy} onClick={close}>{ended ? 'Done' : 'Close the terminal'}</Button></div>
          </>
        )}
      </div>
      {ask && (
        <Modal title={ask === 'on' ? 'Switch the terminal on?' : 'Open a terminal?'} onClose={reset}
          footer={<><Button onClick={reset}>Cancel</Button><Button kind="primary" disabled={busy || !ready} onClick={go}>{ask === 'on' ? 'Switch on' : 'Open'}</Button></>}>
          <p>{ask === 'on' ? 'Administrators will be able to open a command line on the firewall with full rights.' : 'This opens a command line on the firewall with full rights. It is recorded in the Logs that you opened it.'}</p>
          <StepUp su={su} code={code} setCode={setCode} password={pw} setPassword={setPw} autoFocus />
        </Modal>
      )}
    </Card>
  );
}
