import React from 'react';
import { post, useData } from '../api.js';
import { Button, Card, Pill, fmt, useAction } from '../ui.jsx';

// Which lists blocking uses, and how fresh each is.
export default function BlockVersions() {
  const { data, reload } = useData('/api/blocks/versions', { every: 60000 });
  const { data: jobs, reload: reloadJobs } = useData('/api/schedules', { every: 30000 });
  const [run, busy] = useAction();
  if (!data) return null;
  const job = (jobs || []).find((x) => x.id === 'lists');
  const next = job?.next ? (job.next * 1000 <= Date.now() ? 'Due now.' : `Next: ${fmt.dt(job.next)}.`) : '';
  const now = () => run(() => post('/api/schedules/lists/run'), 'Lists updated.').then(() => { reload(); reloadJobs(); });
  const a = data.app_names;
  return (
    <Card title="Lists used for blocking" desc="What NopeWarden checks when it blocks an app, a category or a website.">
      <div className="list">
        <div className="item"><div><div className="name">App names</div><div className="meta">{a.apps} apps, {a.domains} website names.{a.version ? ` Version ${a.version}.` : ''} {a.note}</div></div></div>
        <div className="item"><div><div className="name">App kinds</div><div className="meta">{data.app_topics} categories made from the app names (Games, Video streaming and so on). No download.</div></div></div>
        {(data.topics || []).map((t) => (
          <div className="item" key={t.name}>
            <div><div className="name">{t.name} <Pill tone={t.status === 'ok' ? 'good' : t.status === 'error' ? 'warn' : ''}>{t.status === 'ok' ? 'Up to date' : t.status === 'error' ? 'Could not update' : t.status === 'not downloaded yet' ? 'Not in use yet' : t.status}</Pill></div>
              <div className="meta">{t.entries ? `${fmt.n(t.entries)} names. ` : ''}{t.status === 'error' ? `The last download did not work${t.last_ok ? `; the copy from ${fmt.ago(t.last_ok)} is still used` : ''}. NopeWarden tries again on its own. ` : ''}{t.last_ok ? `Last updated ${fmt.ago(t.last_ok)}.${job && job.spec !== 'off' && job.next ? ` ${next}` : ''}` : t.status === 'error' ? '' : t.status === 'not downloaded yet' ? 'Downloaded when a block uses it, then kept up to date every day.' : ''}</div></div>
          </div>
        ))}
      </div>
      {job && (
        <div className="inline" style={{ flexWrap: 'wrap' }}>
          <span className="small muted">{job.spec === 'off' ? 'Automatic updates are off.' : `Updated automatically: ${job.plain.charAt(0).toLowerCase()}${job.plain.slice(1)}. ${next}`} The machine's own clock is used.</span>
          <a className="link small" href="#/updates/schedule">Change when</a>
          <Button small icon="refresh" disabled={busy} onClick={now}>Run now</Button>
        </div>
      )}
      <p className="small muted">If a list cannot be read or a check fails, nothing is blocked because of it.</p>
    </Card>
  );
}
