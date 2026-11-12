import React, { useEffect, useState } from 'react';
import { post } from '../api.js';
import { Button, Field, Input, Modal, useAction, useStepUp } from '../ui.jsx';

const POWER = {
  reboot: { title: 'Restart this machine', verb: 'Restart', warn: 'The network is down for about a minute while it restarts. This page reconnects by itself.', waiting: 'Restarting…' },
  poweroff: { title: 'Shut down this machine', verb: 'Shut down', warn: 'The network goes down and stays down until someone switches the machine back on.', waiting: 'Shutting down…' },
  'restart-nopewarden': { title: 'Restart NopeWarden only', verb: 'Restart NopeWarden', warn: 'The console, DNS and DHCP are down for a few seconds. The machine itself keeps running.', waiting: 'Restarting NopeWarden…' },
};

// Waits until the machine stops answering and then answers again, then reloads the page.
export function useReconnect() {
  const [waiting, setWaiting] = useState(false);
  useEffect(() => {
    if (!waiting) return undefined;
    let down = false;
    const started = Date.now();
    const t = setInterval(async () => {
      try {
        const r = await fetch('/api/health', { cache: 'no-store' });
        if (r.ok && (down || Date.now() - started > 45000)) { clearInterval(t); window.location.reload(); }
      } catch { down = true; }
    }, 3000);
    return () => clearInterval(t);
  }, [waiting]);
  return [waiting, setWaiting];
}

export function PowerModal({ action, me, onClose }) {
  const meta = POWER[action];
  const [code, setCode] = useState('');
  const [run, busy] = useAction();
  const [waiting, setWaiting] = useReconnect();
  const needCode = useStepUp().everyday;
  const go = async () => { if (await run(() => post('/api/system/power', { action, code }))) setWaiting(true); };
  if (waiting) {
    return (
      <Modal title={meta.waiting} onClose={() => {}} footer={null}>
        <p>{action === 'poweroff' ? 'The machine is shutting down. You can close this page.' : 'Waiting for the machine to come back. This page reloads by itself when it does.'}</p>
      </Modal>
    );
  }
  return (
    <Modal title={meta.title} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind="primary" disabled={busy || (needCode && code.length < 6)} onClick={go}>{meta.verb}</Button></>}>
      <p>{meta.warn}</p>
      {needCode && <Field label="Code from your authenticator app"><Input value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" autoFocus /></Field>}
    </Modal>
  );
}
