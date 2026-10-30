import React, { useEffect, useRef, useState } from 'react';
import { get, post } from './api.js';
import { Button, Field, Input, Modal, Note, Problems, useToast } from './ui.jsx';

// The "review and apply" bar shared by the firewall and the internet pages. Applying starts a
// countdown; unless it is confirmed in time, the previous settings come back by themselves.
// After applying, if this browser can still reach the console a moment later, the change is kept
// without asking. That is the very thing the countdown protects: a change that cuts you off never
// gets the second answer, so it undoes itself. "Ask me every time" turns this off for this browser.
const autoKey = 'gatehouse.autoConfirm';
const readAuto = () => { try { return localStorage.getItem(autoKey) !== 'off'; } catch { return true; } };

export default function ApplyBar({ kind, label, status, me, onChanged, summary }) {
  const toast = useToast();
  const [review, setReview] = useState(null);
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [left, setLeft] = useState(null);
  const pending = status?.pending;
  const [auto, setAuto] = useState(readAuto);
  const tried = useRef('');
  const setAutoPref = (v) => { setAuto(v); try { localStorage.setItem(autoKey, v ? 'on' : 'off'); } catch { /* ignore */ } };

  useEffect(() => {
    if (!pending || !auto || tried.current === pending.token) return undefined;
    tried.current = pending.token;
    let dead = false;
    const run = async () => {
      try {
        await new Promise((r) => setTimeout(r, 4000));
        if (dead) return;
        await get('/api/auth/me');
        await new Promise((r) => setTimeout(r, 2000));
        if (dead) return;
        await get('/api/auth/me');
        if (dead) return;
        await post(`/api/${kind}/confirm`, { token: pending.token });
        toast('Applied and kept. You can still reach Gatehouse, so the change is permanent.');
        onChanged?.();
      } catch { /* cannot reach it: leave the countdown so the change undoes itself or is confirmed by hand */ }
    };
    run();
    return () => { dead = true; };
  }, [pending, auto]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!pending) { setLeft(null); return undefined; }
    const tick = () => setLeft(Math.max(0, pending.deadline - Math.floor(Date.now() / 1000)));
    tick();
    const t = setInterval(tick, 500);
    return () => clearInterval(t);
  }, [pending]);

  const start = async () => {
    setBusy(true);
    try { setReview(await post(`/api/${kind}/preview`)); setCode(''); }
    catch (e) { toast(e.message, 'bad'); }
    finally { setBusy(false); }
  };
  const apply = async () => {
    setBusy(true);
    try { await post(`/api/${kind}/apply`, { code }); setReview(null); toast('Applied. Check that everything still works, then confirm.'); onChanged?.(); }
    catch (e) { toast(e.message, 'bad'); }
    finally { setBusy(false); }
  };
  const confirm = async () => {
    setBusy(true);
    try { await post(`/api/${kind}/confirm`, { token: pending.token }); toast('Confirmed. The change is now permanent.'); onChanged?.(); }
    catch (e) { toast(e.message, 'bad'); }
    finally { setBusy(false); }
  };
  const undo = async () => {
    setBusy(true);
    try { await post(`/api/${kind}/rollback`); toast('Undone. The previous settings are back.'); onChanged?.(); }
    catch (e) { toast(e.message, 'bad'); }
    finally { setBusy(false); }
  };

  if (pending) {
    return (
      <div className="confirmbar countdown" role="alert">
        <div>
          <div className="count">{left ?? ''}s</div>
          <div className="sec">{auto ? 'Checking that you can still reach Gatehouse. If you can, the change is kept on its own.' : 'Does everything still work? Confirm to keep this change.'} If you cannot, the previous settings come back on their own.</div>
          <button type="button" className="link small" onClick={() => setAutoPref(!auto)}>{auto ? 'Ask me to confirm every time' : 'Keep changes automatically when I can still reach Gatehouse'}</button>
        </div>
        <div className="actions">
          <Button onClick={undo} disabled={busy}>Undo now</Button>
          <Button kind="primary" onClick={confirm} disabled={busy}>Keep this change</Button>
        </div>
      </div>
    );
  }
  if (!status?.dirty && !review) return null;
  return (
    <>
      <div className="confirmbar">
        <div>
          <strong>You have changes that are not in use yet.</strong>
          <div className="sec small">{summary || `Review them, then apply. You get 60 seconds to confirm, or the previous settings come back automatically.`}</div>
        </div>
        <div className="actions"><Button kind="primary" onClick={start} disabled={busy || !status?.helper}>Review and apply</Button></div>
      </div>
      {review && (
        <Modal title="Apply changes" onClose={() => setReview(null)}
          footer={<><Button onClick={() => setReview(null)}>Cancel</Button><Button kind="primary" onClick={apply} disabled={busy || !review.ok}>Apply with 60-second safety</Button></>}>
          <Problems list={review.problems} />
          {review.ok && !review.changed && <Note>Nothing is different from what is running.</Note>}
          {review.summary && review.ok && (
            <div className="stack">
              <strong>What this does</strong>
              <ul className="stack" style={{ margin: 0, paddingLeft: '1.2rem' }}>{review.summary.map((s, i) => <li key={i}>{s}</li>)}</ul>
            </div>
          )}
          {review.warnings?.length > 0 && <Note tone="warn" title="Worth knowing"><ul>{review.warnings.map((w, i) => <li key={i}>{w}</li>)}</ul></Note>}
          {review.ok && <Note tone="good">After you apply, Gatehouse checks that you can still reach it and keeps the change. If this change cuts you off, nothing more is needed: it undoes itself within 60 seconds.</Note>}
          {me.user.totp_enabled && review.ok && (
            <Field label="Code from your authenticator app" hint="Needed to apply changes to the network.">
              <Input className="codebox" value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" />
            </Field>
          )}
        </Modal>
      )}
    </>
  );
}
