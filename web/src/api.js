import { useEffect, useRef, useState, useCallback, useSyncExternalStore } from 'react';

let csrf = '';
export const setCsrf = (t) => { csrf = t || ''; };
let installCode = '';
export const setInstallCode = (c) => { installCode = c || ''; };

export class ApiError extends Error {
  constructor(message, status, data) { super(message); this.status = status; this.data = data || null; }
}

// Several cards asking for the same thing at the same moment share one request.
const inflight = new Map();
export function api(method, path, body) {
  if (method !== 'GET') return writeCall(method, path, body);
  if (inflight.has(path)) return inflight.get(path).then((d) => (d && typeof d === 'object' ? structuredClone(d) : d));
  const p = apiCall(method, path, body);
  inflight.set(path, p);
  const done = () => inflight.delete(path);
  p.then(done, done);
  return p;
}

// A change to the firewall, the network or a policy is staged by the server. The console is told
// each time one is saved, so it can apply it at once (the default) or leave it for the Review page
// when the person chose "Save for later" (holdAnd).
const STAGING = /^\/api\/(fw|net|routes|vpn|dhcp|dns|wan|networks|blocks)/;
const NOT_STAGING = /\/(apply|confirm|rollback|preview|discard|check|test)$/;
let hold = false;
export async function holdAnd(fn) {
  hold = true;
  try { return await fn(); } finally { hold = false; }
}
async function writeCall(method, path, body) {
  const r = await apiCall(method, path, body);
  if ((STAGING.test(path) || (method === 'DELETE' && /^\/api\/devices\/\d+$/.test(path))) && !NOT_STAGING.test(path.split('?')[0])) window.dispatchEvent(new CustomEvent('gh-staged', { detail: { hold } }));
  return r;
}

async function apiCall(method, path, body) {
  const opts = { method, credentials: 'same-origin', headers: {} };
  if (csrf) opts.headers['X-CSRF-Token'] = csrf;
  if (installCode) opts.headers['X-Install-Code'] = installCode;
  // DNS changes from the console wait for the Review step (applied a moment later, or from Review).
  if (method !== 'GET' && (/^\/api\/(dns|blocks)(\/|$)/.test(path) || (method === 'DELETE' && /^\/api\/devices\/\d+$/.test(path)))) opts.headers['X-Stage'] = '1';
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  let res;
  try { res = await fetch(path, opts); }
  catch { throw new ApiError('Cannot reach NopeWarden. Check the network cable and try again.', 0); }
  let data = null;
  const text = await res.text();
  if (text) { try { data = JSON.parse(text); } catch { data = null; } }
  if (!res.ok) throw new ApiError((data && data.error) || `Something went wrong (${res.status}).`, res.status, data);
  return data;
}

// Fetch a file the server builds on request and hand it to the browser as a download.
export async function saveFile(method, path, body, filename) {
  const opts = { method, credentials: 'same-origin', headers: {} };
  if (csrf) opts.headers['X-CSRF-Token'] = csrf;
  if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const res = await fetch(path, opts);
  if (!res.ok) {
    let msg = `Something went wrong (${res.status}).`;
    try { msg = (await res.json()).error || msg; } catch { /* keep the default */ }
    throw new ApiError(msg, res.status);
  }
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url; a.download = filename; document.body.appendChild(a); a.click(); a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}

// Send a file as the body of a request.
export async function sendFile(path, file, headers = {}) {
  const h = { 'Content-Type': 'application/octet-stream', ...headers };
  if (csrf) h['X-CSRF-Token'] = csrf;
  const res = await fetch(path, { method: 'POST', credentials: 'same-origin', headers: h, body: file });
  const text = await res.text();
  let data = null;
  try { data = JSON.parse(text); } catch { data = null; }
  if (!res.ok) throw new ApiError((data && data.error) || `Something went wrong (${res.status}).`, res.status);
  return data;
}

export const get = (p) => api('GET', p);
export const put = (p, b) => api('PUT', p, b);
export const post = (p, b) => api('POST', p, b ?? {});
export const del = (p) => api('DELETE', p);
export const patch = (p, b) => api('PATCH', p, b);

// Which things on the screen could not be refreshed just now. A page shows one small note when
// anything is on this list, so a failed refresh is never silent.
const failing = new Set();
const watchers = new Set();
let failingNow = false;
export function setFailing(key, bad) {
  const had = failing.has(key);
  if (bad === had) return;
  if (bad) failing.add(key); else failing.delete(key);
  failingNow = failing.size > 0;
  watchers.forEach((f) => f());
}
export function useFailing() {
  return useSyncExternalStore((cb) => { watchers.add(cb); return () => watchers.delete(cb); }, () => failingNow);
}
// Fetch and hand the answer on; a failure is remembered under `key` instead of being swallowed.
export function getWatched(key, path, then) {
  return get(path).then((d) => { setFailing(key, false); then(d); }, () => setFailing(key, true));
}
export const refreshAll = () => window.dispatchEvent(new Event('gh-refresh'));

// Load data and keep it fresh. Refetches when `topics` push an event, and on a slow timer.
export function useData(path, { topics = [], every = 0, enabled = true } = {}) {
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [updatedAt, setUpdatedAt] = useState(0);
  const alive = useRef(true);
  const me = useRef(`${Math.random()}`);
  const reload = useCallback(async () => {
    if (!enabled) return;
    try { const d = await get(path); if (alive.current) { setData(d); setError(null); setUpdatedAt(Date.now()); setFailing(me.current, false); } }
    catch (e) { if (alive.current) { setError(e); setFailing(me.current, true); } }
    finally { if (alive.current) setLoading(false); }
  }, [path, enabled]);
  useEffect(() => { alive.current = true; setLoading(true); reload(); return () => { alive.current = false; setFailing(me.current, false); }; }, [reload]);
  useEffect(() => { const f = () => { if (!document.hidden) reload(); }; window.addEventListener('gh-refresh', f); return () => window.removeEventListener('gh-refresh', f); }, [reload]);
  // Polling stops while the tab is hidden and catches up the moment it is shown again.
  useEffect(() => {
    if (!every || !enabled) return undefined;
    const tick = () => { if (!document.hidden) reload(); };
    const t = setInterval(tick, every);
    const vis = () => { if (!document.hidden) reload(); };
    document.addEventListener('visibilitychange', vis);
    return () => { clearInterval(t); document.removeEventListener('visibilitychange', vis); };
  }, [every, reload, enabled]);
  const key = topics.join(',');
  useStream(key ? key.split(',') : [], () => { if (!document.hidden) reload(); });
  return { data, error, loading, reload, setData, updatedAt };
}

// Subscribe to server-sent events. The handler receives (topic, payload).
// One live connection for the whole console. Every part of the page that wants live updates
// registers its topics here, and a single EventSource carries the union of them. Browsers allow
// only a handful of connections to one address, so one stream per card starves ordinary requests.
const subs = new Map(); // topic -> Set of handlers
let source = null;
let sourceKey = '';
let rebuildTimer = null;

function rebuildStream() {
  rebuildTimer = null;
  const topics = [...subs.keys()].filter((t) => subs.get(t).size).sort();
  const key = topics.join(',');
  if (key === sourceKey) return;
  if (source) { source.close(); source = null; }
  sourceKey = key;
  if (!key) return;
  const es = new EventSource(`/api/stream?topics=${encodeURIComponent(key)}`);
  topics.forEach((t) => {
    es.addEventListener(t, (ev) => {
      let p = null;
      try { p = JSON.parse(ev.data); } catch { p = null; }
      [...(subs.get(t) || [])].forEach((fn) => fn(t, p));
    });
  });
  source = es;
}

export function useStream(topics, handler) {
  const h = useRef(handler);
  h.current = handler;
  const key = topics.join(',');
  useEffect(() => {
    if (!key) return undefined;
    const mine = key.split(',');
    const fn = (t, p) => h.current(t, p);
    mine.forEach((t) => { if (!subs.has(t)) subs.set(t, new Set()); subs.get(t).add(fn); });
    if (!rebuildTimer) rebuildTimer = setTimeout(rebuildStream, 80);
    return () => {
      mine.forEach((t) => { const set = subs.get(t); if (set) { set.delete(fn); if (!set.size) subs.delete(t); } });
      if (!rebuildTimer) rebuildTimer = setTimeout(rebuildStream, 80);
    };
  }, [key]);
}

// The traffic feed for the internet connections. The first request brings the last ten minutes;
// after that each poll asks for only the newest few samples and adds what is new, so a poll every
// second costs a few hundred bytes instead of a full ten minutes.
export function useThroughput(every = 1000) {
  const [state, setState] = useState({ data: null, updatedAt: 0 });
  const held = useRef(null);
  useEffect(() => {
    let alive = true;
    const TAIL = 20;
    const load = async () => {
      if (document.hidden) return;
      try {
        const cur = held.current;
        const r = await get(cur ? `/api/net/throughput?tail=${TAIL}` : '/api/net/throughput');
        if (!alive) return;
        let next = r;
        if (cur) {
          const fresh = r.seq - cur.seq;
          const names = Object.keys(r.links || {});
          const same = names.length === Object.keys(cur.links).length && names.every((n) => cur.links[n]);
          if (fresh < 0 || fresh > TAIL || !same) { held.current = null; return load(); }
          const links = {};
          names.forEach((n) => {
            const grow = (a, b) => (fresh > 0 ? [...a, ...b.slice(-fresh)].slice(-900) : a);
            links[n] = { ...r.links[n], rx: grow(cur.links[n].rx || [], r.links[n].rx || []), tx: grow(cur.links[n].tx || [], r.links[n].tx || []) };
          });
          next = { ...r, links };
        }
        held.current = next;
        setState({ data: next, updatedAt: Date.now() });
      } catch { /* the next poll tries again */ }
    };
    load();
    const t = setInterval(load, every);
    const vis = () => { if (!document.hidden) load(); };
    document.addEventListener('visibilitychange', vis);
    return () => { alive = false; clearInterval(t); document.removeEventListener('visibilitychange', vis); };
  }, [every]);
  return state;
}
