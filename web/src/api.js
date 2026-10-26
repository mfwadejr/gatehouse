import { useEffect, useRef, useState, useCallback, useSyncExternalStore } from 'react';

let csrf = '';
export const setCsrf = (t) => { csrf = t || ''; };
let installCode = '';
export const setInstallCode = (c) => { installCode = c || ''; };

export class ApiError extends Error {
  constructor(message, status) { super(message); this.status = status; }
}

export async function api(method, path, body) {
  const opts = { method, credentials: 'same-origin', headers: {} };
  if (csrf) opts.headers['X-CSRF-Token'] = csrf;
  if (installCode) opts.headers['X-Install-Code'] = installCode;
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  let res;
  try { res = await fetch(path, opts); }
  catch { throw new ApiError('Cannot reach Gatehouse. Check the network cable and try again.', 0); }
  let data = null;
  const text = await res.text();
  if (text) { try { data = JSON.parse(text); } catch { data = null; } }
  if (!res.ok) throw new ApiError((data && data.error) || `Something went wrong (${res.status}).`, res.status);
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
export function useStream(topics, handler) {
  const h = useRef(handler);
  h.current = handler;
  const key = topics.join(',');
  useEffect(() => {
    if (!key) return undefined;
    const es = new EventSource(`/api/stream?topics=${encodeURIComponent(key)}`);
    key.split(',').forEach((t) => {
      es.addEventListener(t, (ev) => {
        let p = null;
        try { p = JSON.parse(ev.data); } catch { p = null; }
        h.current(t, p);
      });
    });
    return () => es.close();
  }, [key]);
}
