import React from 'react';
import { useData } from '../api.js';
import { Card, Pill } from '../ui.jsx';

// Temperatures, and fans where the box has a fan chip. A box that reports nothing shows no card, and a
// box with no fan chip says nothing about fans.
const TONE = { ok: 'good', warm: 'warn', hot: 'bad' };
const WORD = { ok: 'Fine', warm: 'Warm', hot: 'Hot' };

export default function Hardware() {
  const { data } = useData('/api/hardware', { every: 5000 });
  if (!data || (!data.temps?.length && !data.has_fans)) return null;
  return (
    <Card title="Hardware" desc="Temperatures and fans, read from this machine. You are told in the Logs if the processor runs hot or a fan stops.">
      <div className="list">
        {data.temps.map((t) => (
          <div className="item" key={t.name}>
            <div><div className="name">{t.name}</div><div className="meta">Warm from {Math.round(t.high - 10)}°C, hot from {Math.round(t.high)}°C{t.crit ? `, shuts down near ${Math.round(t.crit)}°C` : ''}.</div></div>
            <div className="spread"><b>{Math.round(t.c)}°C</b><Pill tone={TONE[t.state]}>{WORD[t.state]}</Pill></div>
          </div>
        ))}
        {data.has_fans && data.fans.map((f) => (
          <div className="item" key={f.name}>
            <div><div className="name">{f.name}</div><div className="meta">{f.state === 'turning' ? 'Turning. The speed is as the board reports it and may not be exact.' : 'It was turning and has stopped. Check it is plugged in and not blocked.'}</div></div>
            <div className="spread">{f.state === 'turning' && <b>{f.rpm.toLocaleString()} rpm</b>}<Pill tone={f.state === 'turning' ? 'good' : 'bad'}>{f.state === 'turning' ? 'Turning' : 'Stopped'}</Pill></div>
          </div>
        ))}
        {data.has_fans && !data.fans.length && <div className="item"><div><div className="name">Fans</div><div className="meta">None of the fans on this board are reporting a speed.</div></div></div>}
      </div>
    </Card>
  );
}
