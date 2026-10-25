import React, { useMemo, useState } from 'react';
import { useData } from '../api.js';
import DeviceIcon from '../DeviceIcon.jsx';
import { Card, Empty, Pill, ToggleRow } from '../ui.jsx';
import { linkState } from '../linkstate.js';

const CONN = { wired: 'Wired', wifi: 'Wi-Fi', vpn: 'VPN' };
const SHOW = 18;

// The map shows what Gatehouse itself can see: its own ports, its Wi-Fi cards and its VPNs, and the
// devices it has seen on each. A device behind a switch or access point that Gatehouse does not
// manage appears under the port that switch is plugged into.
function PortGroup({ g, onOpen, showOffline }) {
  const [all, setAll] = useState(false);
  const list = g.devices.filter((d) => showOffline || d.online);
  const shown = all ? list : list.slice(0, SHOW);
  const online = g.devices.filter((d) => d.online).length;
  return (
    <div className="dm-port">
      <div className="dm-port-h">
        <div>
          <b>{g.port || 'Not seen on a port'}</b>
          {g.nets.size > 0 && <span className="muted"> · {[...g.nets].join(', ')}</span>}
        </div>
        <div className="inline">{g.conn && <Pill>{CONN[g.conn] || g.conn}</Pill>}<span className="small muted">{online} online{g.devices.length > online ? ` · ${g.devices.length - online} away` : ''}</span></div>
      </div>
      {g.conn === 'wired' && online > 1 && <div className="small muted">Several devices on one port usually means a switch or access point is plugged in here.</div>}
      <div className="dm-chips">
        {shown.map((d) => (
          <button type="button" key={d.id} className={`dm-chip ${d.online ? '' : 'off'}`} onClick={() => onOpen(d.id)} title={`${d.ip || ''} ${d.mac || ''}`.trim()}>
            <span className="devicon"><DeviceIcon d={d} size={16} /></span>
            <span className="name">{d.name || d.hostname || d.mac || 'unnamed'}</span>
          </button>
        ))}
        {!all && list.length > SHOW && <button type="button" className="link small" onClick={() => setAll(true)}>and {list.length - SHOW} more</button>}
        {!list.length && <span className="small muted">{g.devices.length ? 'Everything here is switched off or away.' : 'Nothing seen here.'}</span>}
      </div>
    </div>
  );
}

export default function DeviceMap({ devices, onOpen }) {
  const { data: wd } = useData('/api/net/wans', { topics: ['net.status'] });
  const { data: st } = useData('/api/net/status', { topics: ['net.status'], every: 5000 });
  const [showOffline, setShowOffline] = useState(false);
  const wans = (wd?.wans || []).filter((w) => w.enabled);
  const links = st?.links || {};
  const groups = useMemo(() => {
    const m = new Map();
    devices.forEach((d) => {
      const k = d.port || '';
      if (!m.has(k)) m.set(k, { port: k, conn: d.conn || '', nets: new Set(), devices: [] });
      if (d.network) m.get(k).nets.add(d.network);
      m.get(k).devices.push(d);
    });
    const arr = [...m.values()];
    arr.sort((a, b) => (a.port === '' ? 1 : b.port === '' ? -1 : a.port.localeCompare(b.port)));
    return arr;
  }, [devices]);
  return (
    <Card title="Map" desc="Gatehouse in the middle. It can only see its own ports, Wi-Fi cards and VPNs, so devices behind a switch show under the port it is plugged into."
      actions={<ToggleRow title="Show devices that are away" checked={showOffline} onChange={setShowOffline} />}>
      {!devices.length ? <Empty>No devices have joined yet.</Empty> : (
        <div className="dm">
          <div className="dm-core">
            <div className="dm-node">
              <b>Internet</b>
              {wans.length ? wans.map((w) => { const s = linkState(links[w.name]); return <div className="small" key={w.name}>{w.name} <Pill tone={s.tone}>{s.health === 'working' ? 'Working' : s.text}</Pill></div>; }) : <span className="small muted">Not set up</span>}
            </div>
            <div className="dm-line" aria-hidden="true" />
            <div className="dm-node gh"><b>Gatehouse</b><span className="small muted">{devices.filter((d) => d.online).length} devices online</span></div>
          </div>
          <div className="dm-ports">
            {groups.map((g) => <PortGroup key={g.port || 'none'} g={g} onOpen={onOpen} showOffline={showOffline} />)}
          </div>
        </div>
      )}
    </Card>
  );
}
