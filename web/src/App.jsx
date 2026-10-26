import React, { useCallback, useEffect, useState } from 'react';
import { ApiError, get, post, setCsrf, useData } from './api.js';
import { Icon, Loading } from './ui.jsx';
import { Enrol, Login, Setup } from './pages/Auth.jsx';
import Overview from './pages/Overview.jsx';
import Internet from './pages/Internet.jsx';
import Networks from './pages/Networks.jsx';
import Wifi from './pages/Wifi.jsx';
import DNS from './pages/DNS.jsx';
import DHCP from './pages/DHCP.jsx';
import VPN from './pages/VPN.jsx';
import Firewall from './pages/Firewall.jsx';
import Services from './pages/Services.jsx';
import Devices from './pages/Devices.jsx';
import Logs from './pages/Logs.jsx';
import Settings from './pages/Settings.jsx';
import Installer from './pages/Installer.jsx';
import Wizard from './pages/Wizard.jsx';

export const PAGES = [
  { id: 'overview', label: 'Overview', icon: 'overview', el: Overview },
  { id: 'internet', label: 'Internet', icon: 'internet', el: Internet },
  { id: 'networks', label: 'Networks', icon: 'networks', el: Networks },
  { id: 'wifi', label: 'Wi-Fi', icon: 'wifi', el: Wifi },
  { id: 'dns', label: 'DNS', icon: 'dns', el: DNS },
  { id: 'dhcp', label: 'DHCP', icon: 'dhcp', el: DHCP },
  { id: 'firewall', label: 'Firewall', icon: 'firewall', el: Firewall },
  { id: 'services', label: 'Services', icon: 'services', el: Services },
  { id: 'vpn', label: 'VPN', icon: 'vpn', el: VPN },
  { id: 'devices', label: 'Devices & access', icon: 'devices', el: Devices },
  { id: 'logs', label: 'Logs', icon: 'logs', el: Logs },
  { id: 'settings', label: 'Settings', icon: 'settings', el: Settings },
];

function useRoute() {
  const read = () => (window.location.hash.replace(/^#\/?/, '') || 'overview').split('/')[0];
  const [route, setRoute] = useState(read);
  useEffect(() => {
    const on = () => setRoute(read());
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  return route;
}

export const go = (id) => { window.location.hash = `#/${id}`; };

function Shell({ me, onSignOut, reloadMe }) {
  const route = useRoute();
  const page = PAGES.find((p) => p.id === route) || PAGES[0];
  const Page = page.el;
  const { data: fw } = useData('/api/fw/overview', { topics: ['fw.status'] });
  const { data: net } = useData('/api/net/status', { topics: ['net.status'] });
  const pending = (fw?.status?.pending || net?.status?.pending) ? true : false;
  const dirty = !!(fw?.status?.dirty || net?.status?.dirty);
  const initials = (me.user.username || '?').slice(0, 2).toUpperCase();
  return (
    <div className="shell">
      <aside className="side">
        <div className="brand"><Icon name="logo" size={24} /><span>Gatehouse</span></div>
        <nav className="nav" aria-label="Primary">
          {PAGES.map((p) => (
            <a key={p.id} href={`#/${p.id}`} className={p.id === page.id ? 'on' : ''} aria-current={p.id === page.id ? 'page' : undefined}>
              <Icon name={p.icon} />
              <span>{p.label}</span>
              {(p.id === 'firewall' || p.id === 'internet' || p.id === 'networks') && (dirty || pending) && <span className={`dot ${pending ? 'bad' : 'warn'} badge-dot`} title={pending ? 'A change is waiting for your confirmation' : 'Changes not applied yet'} />}
            </a>
          ))}
        </nav>
        <div className="me">
          <div className="avatar">{initials}</div>
          <div className="who">
            <span>{me.user.username}</span>
            <span className="small muted">{me.user.role === 'admin' ? 'Admin' : 'Viewer'}{me.user.totp_enabled ? ' · 2-step on' : ''}</span>
            <button className="link small" style={{ textAlign: 'left' }} onClick={onSignOut}>Sign out</button>
          </div>
        </div>
      </aside>
      <main className="main" id="main">
        <Page me={me} reloadMe={reloadMe} />
      </main>
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
  return <Shell me={state.me} onSignOut={signOut} reloadMe={boot} />;
}
