import React, { useState } from 'react';
import { put, useData } from '../api.js';
import { Button, Card, Field, Input, Modal, Note, Pill, useAction, useStepUp, stepReady, StepUp } from '../ui.jsx';

// Remote login (SSH) with the console's own administrator names and passwords. Off until switched on.

export default function RemoteLogin({ me }) {
  const { data, reload } = useData('/api/system/ssh', { topics: ['fw.status'] });
  const [run, busy] = useAction();
  const [ask, setAsk] = useState(null); // 'on' | 'off'
  const [code, setCode] = useState('');
  const su = useStepUp();
  const [pw, setPw] = useState('');
  const needStep = ask === 'on';
  if (!data) return null;
  const addrs = (data.networks || []).filter((n) => n.address);
  const usable = (data.users || []).filter((u) => u.usable);
  const example = usable[0]?.login || 'name';
  const set = async () => {
    const ok = await run(() => put('/api/system/ssh', { enabled: ask === 'on', code, password: pw }), ask === 'on' ? 'Remote login is on.' : 'Remote login is off.');
    if (ok) { setAsk(null); setCode(''); setPw(''); reload(); }
  };
  return (
    <Card title="Remote login (SSH)" desc="Sign in to the machine from another computer with a terminal, using your console name and password. Off unless you turn it on."
      actions={<Pill tone={data.enabled ? 'good' : ''}>{data.enabled ? 'On' : 'Off'}</Pill>}>
      <div className="stack">
        {data.enabled ? (
          <>
            <p>From a computer on {addrs.length ? 'one of these networks' : 'a network that may manage this device'}, open a terminal and type:</p>
            {addrs.length ? addrs.map((n) => <div key={n.network + n.address}><code className="mono">ssh {example}@{n.address}</code> <span className="small muted">on {n.network}</span></div>) : <code className="mono">ssh {example}@&lt;this machine's address&gt;</code>}
            <p className="small muted">Then type your console password. Two-factor sign-in is a console feature, so SSH asks for the password only.</p>
          </>
        ) : <p className="small muted">Turning it on opens SSH from the networks that can manage Gatehouse, and nowhere else. It is never reachable from the internet side.</p>}
        {(data.users || []).some((u) => !u.usable) && (
          <Note tone="warn" title="Some accounts cannot use SSH">
            {(data.users || []).filter((u) => !u.usable).map((u) => u.name).join(', ')}: a login name can only have lower-case letters, digits, dash and underscore. Make a new administrator with a simple name to use SSH.
          </Note>
        )}
        {data.last_error && <Note tone="bad" title="Last attempt failed">{data.last_error}</Note>}
        <ul className="small muted" style={{ margin: 0, paddingLeft: 'var(--space-4)' }}>
          <li>Only administrators can sign in. Changing a password in the console changes it here at once.</li>
          <li>Three wrong passwords end a connection, and one address can start only a few connections a minute.</li>
          <li>Every sign-in and every failed attempt shows up in the Logs, under events.</li>
          <li>You get a normal login with access to the logs. It is not a root login.</li>
        </ul>
        <div className="actions">
          {data.enabled
            ? <Button disabled={busy} onClick={() => setAsk('off')}>Turn off remote login</Button>
            : <Button kind="primary" disabled={busy} onClick={() => setAsk('on')}>Turn on remote login</Button>}
        </div>
      </div>
      {ask && (
        <Modal title={ask === 'on' ? 'Turn on remote login?' : 'Turn off remote login?'} onClose={() => setAsk(null)}
          footer={<><Button onClick={() => setAsk(null)}>Cancel</Button><Button kind="primary" disabled={busy || (needStep && !stepReady(su, code, pw))} onClick={set}>{ask === 'on' ? 'Turn on' : 'Turn off'}</Button></>}>
          {ask === 'on'
            ? <p>Administrators will be able to sign in over SSH from the networks that may manage Gatehouse. The firewall opens port 22 for them and limits how fast anyone can try.</p>
            : <p>The SSH server stops and port 22 closes. Anyone signed in is disconnected.</p>}
          {needStep && <StepUp su={su} code={code} setCode={setCode} password={pw} setPassword={setPw} />}
        </Modal>
      )}
    </Card>
  );
}
