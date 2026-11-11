import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { get, post, useData } from './api.js';
import { Button, useToast } from './ui.jsx';

// The one place that applies changes. Saving a change to the firewall, the network or a policy
// applies it straight away (after a quiet moment, so several quick saves go live together) unless
// the person pressed "Save for later", or NopeWarden found something worth a second look; either
// way the change waits on the Review page. Every apply keeps the 60-second safety: if this browser
// can still reach NopeWarden a few seconds after applying, the change is kept; if not, it undoes
// itself. "Ask me to confirm every time" (per browser) turns the automatic keeping off.
const autoKey = 'nopewarden.autoConfirm';
const readAuto = () => { try { return localStorage.getItem(autoKey) !== 'off'; } catch { return true; } };

const Ctx = createContext(null);
export const useChanges = () => useContext(Ctx);
const AREAS = [['net', 'network settings'], ['dns', 'DNS settings'], ['fw', 'the firewall']];
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const review = () => { window.location.hash = '#/review'; };

export function ChangeProvider({ me, children }) {
  const toast = useToast();
  const admin = me?.user?.role === 'admin';
  const { data: st, reload: reloadSt } = useData('/api/applystate', { topics: ['fw.status', 'net.status'], every: 15000 });
  const { data: staged, reload: reloadStaged } = useData('/api/staged', { topics: ['fw.status', 'net.status'], every: 15000 });
  const [auto, setAuto] = useState(readAuto);
  const [busy, setBusy] = useState(false);
  const [acting, setActing] = useState(false); // Keep or Undo in progress; separate so they work while a commit is still waiting
  const [left, setLeft] = useState(null);
  const busyRef = useRef(false);
  const queued = useRef(false);
  const timer = useRef(null);
  const tried = useRef('');
  const stRef = useRef(st);
  stRef.current = st;
  const reload = useCallback(() => { reloadSt(); reloadStaged(); }, [reloadSt, reloadStaged]);
  const setAutoPref = (v) => { setAuto(v); try { localStorage.setItem(autoKey, v ? 'on' : 'off'); } catch { /* ignore */ } };

  const pend = st?.fw?.pending || st?.net?.pending;
  const pendKind = st?.net?.pending ? 'net' : st?.fw?.pending ? 'fw' : '';
  const dirtyKinds = AREAS.filter(([k]) => st?.[k]?.dirty).map(([k]) => k);
  const fresh = async () => { const s = await get('/api/applystate'); stRef.current = s; return s; };

  // Keep a change by itself once this browser has proved it can still reach NopeWarden.
  useEffect(() => {
    if (!pend || !auto || tried.current === pend.token) return undefined;
    tried.current = pend.token;
    const kind = pendKind;
    let dead = false;
    (async () => {
      try {
        await sleep(4000); if (dead) return;
        await get('/api/auth/me');
        await sleep(2000); if (dead) return;
        await get('/api/auth/me'); if (dead) return;
        await post(`/api/${kind}/confirm`, { token: pend.token });
        toast('Applied and kept. You can still reach NopeWarden, so the change is permanent.');
        reload();
      } catch (e) {
        // Cannot keep it by itself: the countdown undoes the change unless it is kept by hand. Say why.
        toast(`The change could not be kept automatically: ${e?.message || 'NopeWarden did not answer'}. Press Keep this change to keep it, or it is undone when the time runs out.`, 'warn');
      }
    })();
    return () => { dead = true; };
  }, [pend, pendKind, auto]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!pend) { setLeft(null); return undefined; }
    const tick = () => setLeft(Math.max(0, pend.deadline - Math.floor(Date.now() / 1000)));
    tick();
    const t = setInterval(tick, 500);
    // When the time is up the change has been kept or put back by now: ask again, a few times, so
    // the page never keeps showing a change as on trial or still waiting.
    const gap = Math.max(0, (pend.deadline - Math.floor(Date.now() / 1000) + 1) * 1000);
    const again = [gap, gap + 2000, gap + 5000].map((ms) => setTimeout(reload, ms));
    return () => { clearInterval(t); again.forEach(clearTimeout); };
  }, [pend]); // eslint-disable-line react-hooks/exhaustive-deps

  // Wait until nothing is on trial any more (kept, or put back).
  const settle = async () => {
    for (let i = 0; i < 100; i += 1) {
      await sleep(1000);
      const s = await fresh();
      reload(); // keep the screen in step with what NopeWarden says, so the countdown and list follow it
      if (!s.fw?.pending && !s.net?.pending) return s;
    }
    return stRef.current;
  };

  // Apply everything that is waiting: network first, then the firewall. Returns an error text, or ''.
  const commit = useCallback(async (code = '') => {
    if (busyRef.current) return '';
    busyRef.current = true; setBusy(true);
    try {
      let s = await fresh();
      for (const [k, label] of AREAS) {
        if (!s[k]?.dirty) continue;
        if (s[k]?.pending) s = await settle();
        try { await post(`/api/${k}/apply`, { code }); }
        catch (e) { return `Could not apply ${label}: ${e.message}`; }
        s = await settle();
      }
      return '';
    } catch (e) { return e.message; }
    finally { busyRef.current = false; setBusy(false); reload(); }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // The quick path: look for anything that deserves a second look, otherwise apply.
  const applyNow = useCallback(async () => {
    if (busyRef.current || !admin) { queued.current = true; return; }
    try {
      const s = await fresh();
      const kinds = AREAS.map(([k]) => k).filter((k) => s[k]?.dirty);
      if (!kinds.length) return;
      if (s.fw?.pending || s.net?.pending) { queued.current = true; return; }
      const j = await get('/api/staged');
      if ((j.items || []).length > 1 && kinds.length) {
        // More than this one change is waiting: nothing goes live without a look at the whole list.
        toast('Other changes are waiting too, so nothing was applied yet. Review them together.', 'warn');
        review(); return;
      }
      for (const k of kinds) {
        const pv = await post(`/api/${k}/preview`);
        if (!pv.ok) { toast((pv.problems || ['This change cannot be applied yet.'])[0], 'bad'); review(); return; }
        if (pv.warnings?.length) { toast('Saved. NopeWarden found something worth a look before it goes live.', 'warn'); review(); return; }
      }
      const err = await commit('');
      if (err) {
        if (/authenticator|code/i.test(err)) { toast('Saved. Enter your authenticator code to apply it.', 'warn'); review(); } else toast(err, 'bad');
      }
    } catch (e) { toast(e.message || 'Could not apply the change.', 'bad'); }
  }, [admin, commit, toast]);

  useEffect(() => {
    const on = (ev) => {
      if (!admin || ev.detail?.hold) { reload(); return; }
      clearTimeout(timer.current);
      timer.current = setTimeout(() => { applyNow(); }, 1200);
    };
    window.addEventListener('gh-staged', on);
    return () => { window.removeEventListener('gh-staged', on); clearTimeout(timer.current); };
  }, [admin, applyNow, reload]);

  // A change saved while another was on trial goes live as soon as that one is settled.
  useEffect(() => {
    if (!pend && queued.current && !busyRef.current) { queued.current = false; applyNow(); }
  }, [pend]); // eslint-disable-line react-hooks/exhaustive-deps

  const confirm = async () => {
    setActing(true);
    try { await post(`/api/${pendKind}/confirm`, { token: pend.token }); toast('Confirmed. The change is now permanent.'); reload(); }
    catch (e) { toast(e.message, 'bad'); } finally { setActing(false); }
  };
  const undo = async () => {
    setActing(true);
    try { await post(`/api/${pendKind}/rollback`); toast('Undone. The previous settings are back. The change is waiting on the Review page if you want it again.'); reload(); }
    catch (e) { toast(e.message, 'bad'); } finally { setActing(false); }
  };

  const value = { st, staged, dirtyKinds, pend, applyNow, commit, reload, busy, auto, setAutoPref };
  return (
    <Ctx.Provider value={value}>
      {children}
      <ChangeBar pend={pend} left={left} auto={auto} setAutoPref={setAutoPref} busy={acting} confirm={confirm} undo={undo} />
    </Ctx.Provider>
  );
}

// The bar at the bottom of the window: the countdown while a change is on trial.
function ChangeBar({ pend, left, auto, setAutoPref, busy, confirm, undo }) {
  if (!pend) return null;
  return (
    <div className="confirmbar countdown floating" role="alert">
      <div>
        <div className="count">{left ?? ''}s</div>
        <div className="sec">{auto ? 'Checking that you can still reach NopeWarden. If you can, the change is kept on its own.' : 'Does everything still work? Confirm to keep this change.'} If you cannot, the previous settings come back on their own.</div>
        <button type="button" className="link small" onClick={() => setAutoPref(!auto)}>{auto ? 'Ask me to confirm every time' : 'Keep changes automatically when I can still reach NopeWarden'}</button>
      </div>
      <div className="actions">
        <Button onClick={undo} disabled={busy}>Undo now</Button>
        <Button kind="primary" onClick={confirm} disabled={busy}>Keep this change</Button>
      </div>
    </div>
  );
}

// A small badge for the page header: how many changes wait, and a way to the Review page.
export function WaitingBadge() {
  const ch = useChanges();
  if (!ch || ch.pend || !ch.dirtyKinds.length) return null;
  const n = Math.max(1, (ch.staged?.items || []).length);
  return (
    <a className="waitbadge" href="#/review" title="Changes saved for later, not in use yet">
      <span className="navdot warn" aria-hidden="true" />{n} {n === 1 ? 'change' : 'changes'} waiting · Review
    </a>
  );
}

// The two ways to save a change that goes live: apply it now (the main button), or keep it on the
// Review page to commit later together with other changes.
export function SaveChoice({ onSave, busy, disabled, label = 'Save and apply' }) {
  return (
    <>
      <Button disabled={busy || disabled} onClick={() => onSave(true)}>Save for later</Button>
      <Button kind="primary" disabled={busy || disabled} onClick={() => onSave(false)}>{label}</Button>
    </>
  );
}
export const SAVED_LATER = 'Saved for later. It waits on the Review page until you commit it.';
