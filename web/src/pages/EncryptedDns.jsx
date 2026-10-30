import React from 'react';
import { post, put, useData } from '../api.js';
import { Button, Card, Note, Segmented, Select, useAction } from '../ui.jsx';

// Encrypted DNS as one switch for Gatehouse's own lookups. It writes the same list of DNS servers
// shown below it; Custom leaves your own list alone. The previous list is kept for one-click undo.
export default function EncryptedDns({ onChanged }) {
  const { data, reload } = useData('/api/dns/encrypted');
  const [run, busy] = useAction();
  if (!data) return null;
  const top = data.mode.startsWith('provider:') ? 'provider' : data.mode;
  const set = (mode, text) => run(async () => { await put('/api/dns/encrypted', { mode }); reload(); onChanged?.(); }, text || 'Saved. The list of servers below is updated.');
  return (
    <Card title="Encrypted DNS" desc="Hides your website lookups from your internet provider by sending them to a DNS provider over an encrypted link.">
      <div className="stack">
        <Segmented value={top} onChange={(v) => set(v === 'provider' ? 'provider:cloudflare' : v)}
          options={[{ value: 'off', label: 'Off' }, { value: 'automatic', label: 'Automatic' }, { value: 'provider', label: 'Pick a provider' }, { value: 'custom', label: 'Custom' }]} />
        {top === 'provider' && (
          <Select value={data.mode.slice(9)} onChange={(v) => set(`provider:${v}`)} options={data.providers.map((p) => ({ value: p.id, label: `${p.label}: ${p.desc}` }))} />
        )}
        {top === 'off' && <Note tone="warn" title="Not encrypted">Lookups travel in the clear, so your internet provider can see which sites you visit. Use this only if encrypted DNS does not work on your connection.</Note>}
        {top === 'automatic' && <span className="small muted">Cloudflare and Quad9, encrypted. If one is down, the other answers.</span>}
        {top === 'custom' && <span className="small muted">You manage the list of servers below yourself.</span>}
        {data.can_undo && <div><Button small disabled={busy} onClick={() => run(async () => { await post('/api/dns/encrypted/undo'); reload(); onChanged?.(); }, 'Put back the previous servers.')}>Undo the last change</Button></div>}
      </div>
    </Card>
  );
}
