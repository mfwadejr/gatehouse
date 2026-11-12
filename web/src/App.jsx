import { AttentionProvider, useAttention, levelText } from './attention.jsx';
import React, { Suspense, lazy, useCallback, useEffect, useState } from 'react';
import { ApiError, get, post, setCsrf, useData } from './api.js';
import { Guard, Icon, Loading } from './ui.jsx';
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
const Routes = lazy(() => import('./pages/Routes.jsx'));
const Policies = lazy(() => import('./pages/Policies.jsx'));
const Review = lazy(() => import('./pages/Review.jsx'));
const Firewall = lazy(() => import('./pages/Firewall.jsx'));
const Protection = lazy(() => import('./pages/Protection.jsx'));
const Threats = lazy(() => import('./pages/Threats.jsx'));
const Diagnostics = lazy(() => import('./pages/Diagnostics.jsx'));
const Services = lazy(() => import('./pages/Services.jsx'));
const Devices = lazy(() => import('./pages/Devices.jsx'));
const Profiles = lazy(() => import('./pages/Profiles.jsx'));
const Logs = lazy(() => import('./pages/Logs.jsx'));
const Updates = lazy(() => import('./pages/Updates.jsx'));
const Settings = lazy(() => import('./pages/Settings.jsx'));

export const PAGES = [
  { id: 'overview', label: 'Overview', icon: 'overview', el: Overview },
  { id: 'internet', label: 'Internet', icon: 'internet', el: Internet, group: 'Network' },
  { id: 'networks', label: 'Networks', icon: 'networks', el: Networks, group: 'Network' },
  { id: 'dns', label: 'DNS', icon: 'dns', el: DNS, group: 'Network' },
  { id: 'dhcp', label: 'DHCP', icon: 'dhcp', el: DHCP, group: 'Network' },
  { id: 'routes', label: 'Routes', icon: 'routes', el: Routes, group: 'Network' },
  { id: 'policies', label: 'Policies', icon: 'policies', el: Policies, group: 'Network' },
  { id: 'firewall', label: 'Firewall', icon: 'firewall', el: Firewall, group: 'Security' },
  { id: 'protection', label: 'Protection', icon: 'protection', el: Protection, group: 'Security' },
  { id: 'threats', label: 'Threats', icon: 'protection', el: Threats, group: 'Security' },
  { id: 'services', label: 'Services', icon: 'services', el: Services, group: 'Security' },
  { id: 'vpn', label: 'VPN', icon: 'vpn', el: VPN, group: 'Security' },
  { id: 'profiles', label: 'Profiles', icon: 'devices', el: Profiles, group: 'People and devices' },
  { id: 'devices', label: 'Devices & access', icon: 'devices', el: Devices, group: 'People and devices' },
  { id: 'logs', label: 'Logs', icon: 'logs', el: Logs, group: 'System' },
  { id: 'diagnostics', label: 'Diagnostics', icon: 'diagnostics', el: Diagnostics, group: 'System' },
  { id: 'updates', label: 'Updates', icon: 'updates', el: Updates, group: 'System' },
  { id: 'settings', label: 'Settings', icon: 'settings', el: Settings, group: 'System' },
  { id: 'review', label: 'Review changes', icon: 'policies', el: Review, hidden: true },
];

// The Threats page used to be a tab of Protection. Old links and bookmarks still land in the right place.
const fixOldLink = () => {
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
  const dirty = !!(fw?.status?.dirty || net?.status?.dirty);
  const [finding, setFinding] = useState(false);
  const [notes, setNotes] = useState(false);
  const [theme, setTheme] = useTheme();
  const [open, setOpen] = useState(false);
  useEffect(() => { setOpen(false); }, [route]);
  const { data: upd } = useData('/api/system/update', { every: 600000 });
  useSearchShortcut(useCallback(() => setFinding(true), []));
  const initials = (me.user.username || '?').slice(0, 2).toUpperCase();
  return (
    <div className="shell">
      <aside className={`side ${open ? 'open' : ''}`}>
        <div className="brand"><Logo size={32} /><span>NopeWarden</span></div>
        <button type="button" className="srchbtn" onClick={() => setFinding(true)} aria-label="Search"><Icon name="search" /><span>Search</span><kbd>/</kbd></button>
        <button type="button" className="menubtn" onClick={() => setOpen((o) => !o)} aria-expanded={open} aria-controls="primary-nav"><Icon name={open ? 'x' : 'menu'} /><span>Menu</span></button>
        <nav className="nav" id="primary-nav" aria-label="Primary">
          {PAGES.filter((p) => !p.hidden).map((p, i, vis) => (
            <React.Fragment key={p.id}>
            {p.group && p.group !== vis[i - 1]?.group && <span className="navgroup">{p.group}</span>}
            <a href={`#/${p.id}`} className={p.id === page.id ? 'on' : ''} aria-current={p.id === page.id ? 'page' : undefined}>
              <Icon name={p.icon} />
              <span>{p.label}</span>
              {(p.id === 'firewall' || p.id === 'internet' || p.id === 'networks') && (dirty || pending)
                ? <span className={`navdot ${pending ? '' : 'warn'}`} title={pending ? 'A change is waiting for your confirmation' : 'Changes not applied yet'} />
                : attn.byPage[p.id] && <span className={`navdot ${attn.byPage[p.id]}`} role="img" aria-label={`${levelText[attn.byPage[p.id]]}: ${attn.items.filter((x) => x.to === p.id).map((x) => x.t).join('. ')}`} title={attn.items.filter((x) => x.to === p.id).map((x) => x.t).join('\n')} />}
            </a>
            </React.Fragment>
          ))}
        </nav>
        <div className="me">
          <div className="avatar">{initials}</div>
          <div className="who">
            <span title={me.user.username}>{me.user.username}</span>
            <span className="small muted" title={`${me.user.role === 'admin' ? 'Admin' : 'Viewer'}${me.user.totp_enabled ? ' · 2-step on' : ''}`}>{me.user.role === 'admin' ? 'Admin' : 'Viewer'}{me.user.totp_enabled ? ' · 2-step' : ''}</span>
          </div>
          <button type="button" className="themebtn" onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')} aria-label={theme === 'light' ? 'Switch to the dark look' : 'Switch to the light look'} title={theme === 'light' ? 'Switch to the dark look' : 'Switch to the light look'}><Icon name={theme === 'light' ? 'moon' : 'sun'} size={16} /></button>
          <button type="button" className="signoutbtn" onClick={onSignOut} aria-label="Sign out" title="Sign out"><Icon name="logout" size={16} /></button>
          {upd?.current && (attn.items.some((x) => x.id === 'updates')
            ? <a className="link small verline" href="#/updates" title="An update is ready">Version {upd.current} · Update available</a>
            : <button type="button" className="link small verline" onClick={() => setNotes(true)}>Version {upd.current} · What's new</button>)}
        </div>
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
