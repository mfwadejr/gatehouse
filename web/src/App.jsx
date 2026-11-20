import { AttentionProvider, useAttention, levelText } from './attention.jsx';
import React, { Suspense, lazy, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ApiError, get, post, setCsrf, useData } from './api.js';
import { Guard, Icon, Loading, Segmented } from './ui.jsx';
import { Logo } from './logo.jsx';
import { Enrol, Login, Setup } from './pages/Auth.jsx';
import FailbackPrompt from './pages/FailbackPrompt.jsx';
import Installer from './pages/Installer.jsx';
import Wizard from './pages/Wizard.jsx';
import { SearchBox, useSearchShortcut } from './search.jsx';
import { useTheme } from './theme.js';
import { ChangeProvider, WaitingBadge } from './changes.jsx';
import { ReleaseNotes } from './whatsnew.jsx';

const Overview = lazy(() => import('./pages/Overview.jsx'));
const Internet = lazy(() => import('./pages/Internet.jsx'));
const Networks = lazy(() => import('./pages/Networks.jsx'));
const DNS = lazy(() => import('./pages/DNS.jsx'));
const DHCP = lazy(() => import('./pages/DHCP.jsx'));
const VPN = lazy(() => import('./pages/VPN.jsx'));
const Policies = lazy(() => import('./pages/PoliciesPage.jsx'));
const Review = lazy(() => import('./pages/Review.jsx'));
const Firewall = lazy(() => import('./pages/Firewall.jsx'));
const Protection = lazy(() => import('./pages/Protection.jsx'));
const Threats = lazy(() => import('./pages/Threats.jsx'));
const Diagnostics = lazy(() => import('./pages/Diagnostics.jsx'));
const Devices = lazy(() => import('./pages/Devices.jsx'));
const Logs = lazy(() => import('./pages/Logs.jsx'));
const Updates = lazy(() => import('./pages/Updates.jsx'));
const Settings = lazy(() => import('./pages/Settings.jsx'));

export const PAGES = [
  { id: 'overview', label: 'Overview', icon: 'overview', el: Overview },
  { id: 'internet', label: 'Internet', icon: 'internet', el: Internet, group: 'Network' },
  { id: 'networks', label: 'Networks', icon: 'networks', el: Networks, group: 'Network' },
  { id: 'dns', label: 'DNS', icon: 'dns', el: DNS, group: 'Network' },
  { id: 'dhcp', label: 'DHCP', icon: 'dhcp', el: DHCP, group: 'Network' },
  { id: 'firewall', label: 'Firewall', icon: 'firewall', el: Firewall, group: 'Security' },
  { id: 'policies', label: 'Policies', icon: 'policies', el: Policies, group: 'Security' },
  { id: 'protection', label: 'Protection', icon: 'protection', el: Protection, group: 'Security' },
  { id: 'threats', label: 'Threats', icon: 'threats', el: Threats, group: 'Security' },
  { id: 'vpn', label: 'VPN', icon: 'vpn', el: VPN, group: 'Security' },
  { id: 'devices', label: 'Devices & access', icon: 'devices', el: Devices, group: 'People and devices' },
  { id: 'logs', label: 'Logs', icon: 'logs', el: Logs, group: 'System' },
  { id: 'diagnostics', label: 'Diagnostics', icon: 'diagnostics', el: Diagnostics, group: 'System' },
  { id: 'updates', label: 'Updates', icon: 'updates', el: Updates, group: 'System' },
  { id: 'settings', label: 'Settings', icon: 'settings', el: Settings, group: 'System' },
  { id: 'review', label: 'Review changes', icon: 'policies', el: Review, hidden: true },
];

// The Threats page used to be a tab of Protection. Old links and bookmarks still land in the right place.
const fixOldLink = () => {
  // Routes became a tab of Policies.
  const r = window.location.hash.match(/^#\/?routes(\?.*)?$/);
  if (r) window.history.replaceState(null, '', `#/policies/routes${r[1] || ''}`);
  // Services became a tab of Firewall, and Profiles a tab of Devices & access.
  const sv = window.location.hash.match(/^#\/?services(\?.*)?$/);
  if (sv) window.history.replaceState(null, '', `#/firewall/services${sv[1] || ''}`);
  const pf = window.location.hash.match(/^#\/?(?:profiles|dns\/groups)(\?.*)?$/);
  if (pf) window.history.replaceState(null, '', `#/devices/profiles${pf[1] || ''}`);
  // Old links to the Device groups tab, when it was called Groups & lists, still work: the tab keeps its address.
  const m = window.location.hash.match(/^#\/?protection\/threats(?:\/([a-z]+))?(\?.*)?$/);
  if (m) window.history.replaceState(null, '', `#/threats${m[1] ? `/${m[1]}` : ''}${m[2] || ''}`);
};

function useRoute() {
  fixOldLink();
  const read = () => (window.location.hash.replace(/^#\/?/, '').split('?')[0] || 'overview').split('/')[0];
  const [route, setRoute] = useState(read);
  useEffect(() => {
    const on = () => { fixOldLink(); setRoute(read()); };
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  return route;
}

// The person's corner of the menu: their name opens a short list of things that are about them or the
// whole console (account, look, help, what is new, sign out), instead of loose buttons.
function ProfileMenu({ me, theme, setTheme, version, updateReady, onNotes, onSignOut, initials }) {
  const [open, setOpen] = useState(false);
  const box = useRef(null);
  const trig = useRef(null);
  useEffect(() => {
    if (!open) return undefined;
    const away = (e) => { if (box.current && !box.current.contains(e.target)) setOpen(false); };
    const key = (e) => { if (e.key === 'Escape') { setOpen(false); trig.current?.focus(); } };
    document.addEventListener('mousedown', away);
    document.addEventListener('keydown', key);
    return () => { document.removeEventListener('mousedown', away); document.removeEventListener('keydown', key); };
  }, [open]);
  const role = `${me.user.role === 'admin' ? 'Admin' : 'Viewer'}${me.user.totp_enabled ? ' · 2-step' : ''}`;
  const pick = (fn) => () => { setOpen(false); fn(); };
  return (
    <div className="me" ref={box}>
      <button type="button" className="me-trigger" ref={trig} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <span className="avatar">{initials}</span>
        <span className="who">
          <span title={me.user.username}>{me.user.username}</span>
          <span className="small muted" title={role}>{role}</span>
        </span>
        {updateReady && <span className="navdot" role="img" aria-label="An update is ready" title="An update is ready" />}
        <Icon name="chevup" size={16} />
      </button>
      {open && (
        <div className="me-menu" role="menu">
          <button type="button" role="menuitem" onClick={pick(() => go('settings/account'))}>Account &amp; sign-in</button>
          <div className="me-row" role="group" aria-label="Appearance"><span>Appearance</span><Segmented value={theme === 'light' ? 'light' : 'dark'} onChange={setTheme} options={[{ value: 'light', label: 'Light' }, { value: 'dark', label: 'Dark' }]} /></div>
          <a role="menuitem" href="/help/" target="_blank" rel="noopener" onClick={() => setOpen(false)}>Help</a>
          {updateReady
            ? <button type="button" role="menuitem" onClick={pick(() => go('updates'))}>Update available</button>
            : <button type="button" role="menuitem" onClick={pick(onNotes)}>What's new</button>}
          {version && <div className="me-ver small muted">Version {version}</div>}
          <button type="button" role="menuitem" className="me-out" onClick={pick(onSignOut)}><Icon name="logout" size={16} /> Sign out</button>
        </div>
      )}
    </div>
  );
}

export const go = (id) => { window.location.hash = `#/${id}`; };

function Shell({ me, onSignOut, reloadMe }) {
  const route = useRoute();
  const attn = useAttention();
  const page = PAGES.find((p) => p.id === route) || PAGES[0];
  const Page = page.el;
  const { data: fw } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const { data: net } = useData('/api/net/status', { topics: ['net.status'] });
  const { data: ha, reload: reloadHa } = useData('/api/ha', { every: 15000 });
  const pending = (fw?.status?.pending || net?.status?.pending) ? true : false;
  const dirty = !!((fw?.status?.dirty && !fw.status.refresh_only) || net?.status?.dirty);
  const [finding, setFinding] = useState(false);
  const [notes, setNotes] = useState(false);
  const [theme, setTheme] = useTheme();
  const [open, setOpen] = useState(false);
  useEffect(() => { setOpen(false); }, [route]);
  // The highlight behind the current page slides to the new one. It is measured from the real link, so
  // it follows resizes, zoom and the group headings; the first placement does not animate.
  const navRef = useRef(null);
  const [pill, setPill] = useState(null);
  const place = useCallback(() => {
    const on = navRef.current?.querySelector('a.on');
    if (!on) { setPill(null); return; }
    setPill((p) => (p && p.top === on.offsetTop && p.height === on.offsetHeight ? p : { top: on.offsetTop, height: on.offsetHeight, seen: !!p }));
  }, []);
  useLayoutEffect(() => { place(); }, [place, route, open, attn.items.length]);
  useEffect(() => {
    if (!navRef.current || typeof ResizeObserver === 'undefined') return undefined;
    const ro = new ResizeObserver(place);
    ro.observe(navRef.current);
    return () => ro.disconnect();
  }, [place]);
  const { data: upd } = useData('/api/system/update', { every: 600000 });
  useSearchShortcut(useCallback(() => setFinding(true), []));
  const initials = (me.user.username || '?').slice(0, 2).toUpperCase();
  return (
    <div className="shell">
      <aside className={`side ${open ? 'open' : ''}`}>
        <div className="brand"><Logo size={32} /><span>NopeWarden</span></div>
        <button type="button" className="srchbtn" onClick={() => setFinding(true)} aria-label="Search"><Icon name="search" /><span>Search</span><kbd>/</kbd></button>
        <button type="button" className="menubtn" onClick={() => setOpen((o) => !o)} aria-expanded={open} aria-controls="primary-nav"><Icon name={open ? 'x' : 'menu'} /><span>Menu</span></button>
        <nav className="nav" id="primary-nav" aria-label="Primary" ref={navRef}>
          {pill && <span className={`nav-pill ${pill.seen ? 'slides' : ''}`} aria-hidden="true" style={{ transform: `translateY(${pill.top}px)`, height: pill.height }} />}
          {PAGES.filter((p) => !p.hidden).map((p, i, vis) => (
            <React.Fragment key={p.id}>
            {p.group && p.group !== vis[i - 1]?.group && <span className="navgroup">{p.group}</span>}
            <a href={`#/${p.id}`} className={p.id === page.id ? 'on' : ''} aria-current={p.id === page.id ? 'page' : undefined}>
              <Icon name={p.icon} />
              <span>{p.label}</span>
              {(p.id === 'firewall' || p.id === 'internet' || p.id === 'networks') && (dirty || pending)
                ? <span className={`navdot ${pending ? '' : 'warn'}`} title={pending ? 'A change is waiting for your confirmation' : 'Changes not applied yet'} />
                : attn.byPage[p.id] && <span className={`navdot ${attn.byPage[p.id]}`} role="img" aria-label={`${levelText[attn.byPage[p.id]]}: ${attn.items.filter((x) => String(x.to).split('/')[0] === p.id).map((x) => x.t).join('. ')}`} title={attn.items.filter((x) => String(x.to).split('/')[0] === p.id).map((x) => x.t).join('\n')} />}
            </a>
            </React.Fragment>
          ))}
        </nav>
        <ProfileMenu me={me} theme={theme} setTheme={setTheme} version={upd?.current} updateReady={attn.items.some((x) => x.id === 'updates')} onNotes={() => setNotes(true)} onSignOut={onSignOut} initials={initials} />
      </aside>
      <main className="main" id="main">
        {ha?.standby && <div className="note warn" role="status" style={{ margin: 'var(--space-3)' }}><div><strong>This is the standby unit.</strong> Settings are read-only here; make changes on the active unit{ha.peer_addr ? ` (${ha.peer_addr})` : ''}. <a href="#/settings">High availability</a></div></div>}
        <FailbackPrompt ha={ha} onChanged={reloadHa} />
        <WaitingBadge />
        <Guard key={page.id} what="This page"><Suspense fallback={<Loading />}><Page me={me} reloadMe={reloadMe} /></Suspense></Guard>
      </main>
      {finding && <SearchBox onClose={() => setFinding(false)} go={go} />}
      {notes && <ReleaseNotes onClose={() => setNotes(false)} />}
    </div>
  );
}

export default function App() {
  const [state, setState] = useState({ kind: 'boot' });
  const [prefill, setPrefill] = useState(null);

  const boot = useCallback(async () => {
    try {
      const inst = await get('/api/install/info');
      if (inst?.installer) { setState({ kind: 'installer', info: inst }); return; }
    } catch { /* not the installer */ }
    try {
      const s = await get('/api/setup');
      if (s.needed) { setState({ kind: 'setup' }); return; }
    } catch { /* fall through to sign-in */ }
    try {
      const me = await get('/api/auth/me');
      setCsrf(me.csrf);
      if (me.needs_enrol) { setState({ kind: 'enrol' }); return; }
      if (me.user?.role === 'admin') {
        try {
          const wz = await get('/api/setup/wizard');
          if (wz.pending) { setState({ kind: 'wizard', me, data: wz }); return; }
        } catch { /* no wizard to show */ }
      }
      setState({ kind: 'app', me });
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) setState({ kind: 'login' });
      else setState({ kind: 'login' });
    }
  }, []);
  useEffect(() => { boot(); }, [boot]);

  const signOut = async () => { try { await post('/api/auth/logout'); } catch { /* ignore */ } setCsrf(''); setState({ kind: 'login' }); };

  if (state.kind === 'boot') return <div className="auth"><Loading /></div>;
  if (state.kind === 'installer') return <Installer info={state.info} />;
  if (state.kind === 'setup') return <Setup onDone={(u, p) => { setPrefill({ u, p }); setState({ kind: 'login' }); }} />;
  if (state.kind === 'login') return <Login prefill={prefill} onSession={() => { setPrefill(null); boot(); }} />;
  if (state.kind === 'wizard') return <Wizard me={state.me} data={state.data} onDone={boot} />;
  if (state.kind === 'enrol') return <Enrol onDone={boot} />;
  return <AttentionProvider><ChangeProvider me={state.me}><Shell me={state.me} onSignOut={signOut} reloadMe={boot} /></ChangeProvider></AttentionProvider>;
}
