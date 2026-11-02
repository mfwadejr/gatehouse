import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { get, post, setCsrf } from './api.js';

// The terminal itself, in its own page so the console's strict style rules stay as they are: the
// terminal emulator writes inline styles, and only this page is allowed to (see api.Middleware).
// The console shows it in a frame and passes the terminal's id in the address.

const b64 = {
  enc: (s) => { const u = new TextEncoder().encode(s); let t = ''; u.forEach((c) => { t += String.fromCharCode(c); }); return btoa(t); },
  dec: (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0)),
};

(async () => {
  const id = new URLSearchParams(window.location.search).get('id') || '';
  const root = document.getElementById('root');
  root.style.cssText = 'height:100vh;background:black;padding:6px;box-sizing:border-box';
  try { const me = await get('/api/auth/me'); setCsrf(me.csrf); } catch { root.textContent = 'Sign in again.'; return; }
  const term = new XTerm({ cursorBlink: true, fontSize: 14, fontFamily: 'ui-monospace, Menlo, Consolas, monospace', scrollback: 5000 });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(root);
  const resize = () => { try { fit.fit(); post(`/api/term/${id}/size`, { rows: term.rows, cols: term.cols }).catch(() => {}); } catch { /* not visible yet */ } };
  resize();
  window.addEventListener('resize', resize);
  let alive = true;
  let sending = Promise.resolve();
  term.onData((d) => { sending = sending.then(() => post(`/api/term/${id}/in`, { data: b64.enc(d) })).catch(() => {}); });
  term.focus();
  let since = 0;
  while (alive) {
    try {
      const r = await get(`/api/term/${id}/out?since=${since}`);
      since = r.next;
      if (r.data) term.write(b64.dec(r.data));
      if (r.closed) {
        term.write('\r\n\x1b[33m[The terminal has ended.]\x1b[0m\r\n');
        alive = false;
        window.parent.postMessage({ gatehouseTerminal: 'ended' }, window.location.origin);
      }
    } catch {
      await new Promise((res) => setTimeout(res, 1500));
    }
  }
})();
