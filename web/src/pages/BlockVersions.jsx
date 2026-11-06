import React from 'react';
import { useData } from '../api.js';
import { Card, Pill, fmt } from '../ui.jsx';

// Which lists blocking uses, and how fresh each is.
export default function BlockVersions() {
  const { data } = useData('/api/blocks/versions', { every: 60000 });
  if (!data) return null;
  const a = data.app_names;
  return (
    <Card title="Lists used for blocking" desc="What NopeWarden checks when it blocks an app, a topic or a website.">
      <div className="list">
        <div className="item"><div><div className="name">App names</div><div className="meta">{a.apps} apps, {a.domains} website names. Version {a.version}. {a.note}</div></div></div>
        <div className="item"><div><div className="name">App kinds</div><div className="meta">{data.app_topics} topics made from the app names (Games, Video streaming and so on). No download.</div></div></div>
        {(data.topics || []).map((t) => (
          <div className="item" key={t.name}>
            <div><div className="name">{t.name} <Pill tone={t.status === 'ok' ? 'good' : ''}>{t.status === 'ok' ? 'Up to date' : t.status}</Pill></div>
              <div className="meta">{t.entries ? `${fmt.n(t.entries)} names. ` : ''}{t.last_ok ? `Last updated ${fmt.ago(t.last_ok)}.` : 'Downloaded the first time a block uses it.'}</div></div>
          </div>
        ))}
        <div className="item"><div><div className="name">Traffic labels (thorough checking) {data.labels.installed ? <Pill tone={data.labels.connected ? 'good' : 'warn'}>{data.labels.connected ? 'Running' : data.labels.enabled ? 'Not connected' : 'Off'}</Pill> : <Pill>Not on this system</Pill>}</div><div className="meta">{data.labels.note}</div></div></div>
      </div>
      <p className="small muted">If a list cannot be read or a check fails, nothing is blocked because of it.</p>
    </Card>
  );
}
