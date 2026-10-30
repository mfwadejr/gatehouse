import React, { useState } from 'react';
import { post } from '../api.js';
import { Button, useAction, confirmBox } from '../ui.jsx';

// The question that follows a failover: the main unit is back and healthy, and the other unit is
// running the network. Nothing moves until a choice is made here. "Decide later" hides the
// question on this screen for a day; it comes back by itself and the network keeps running.
const KEY = 'gh.failback.later';
const hidden = () => { try { return Number(localStorage.getItem(KEY) || 0) > Date.now(); } catch { return false; } };
const hideFor = (ms) => { try { localStorage.setItem(KEY, String(Date.now() + ms)); } catch { /* nothing to remember it with */ } };

export default function FailbackPrompt({ ha, onChanged, canHide = true }) {
  const [run, busy] = useAction();
  const [, again] = useState(0);
  const fb = ha?.failback;
  if (!fb || (canHide && hidden())) return null;
  const back = () => run(async () => {
    if (!(await confirmBox(`Give the network back to the main unit at ${fb.peer_addr}? This unit steps aside and connections pause for a few seconds. If the main unit does not take over, this unit carries on by itself.`, { title: 'Fail back', yes: 'Give it back' }))) return;
    await post('/api/ha/failback', { code: '' });
    onChanged?.();
  }, 'Switching to the main unit. Reload this page in a few seconds; it may be at the same address.');
  const keep = () => run(async () => {
    if (!(await confirmBox(`Keep this unit running the network and make it the main unit? The unit at ${fb.peer_addr} becomes the standby and stays that way. Nothing moves now.`, { title: 'Make this unit the main unit', yes: 'Make it the main unit', danger: false }))) return;
    await post('/api/ha/promote', { code: '' });
    onChanged?.();
  }, 'This unit is now the main unit.');
  return (
    <div className="note warn" role="status" style={{ margin: 'var(--space-3)' }}>
      <div>
        <strong>The main unit is back.</strong> The unit at {fb.peer_addr} is online and healthy. This unit ({fb.own_addr}) is still running the network. What would you like to do?
        {fb.ready !== true && <div className="small" style={{ marginTop: 4 }}>Not yet: {fb.why}</div>}
        <div className="inline" style={{ marginTop: 8 }}>
          <Button kind="primary" disabled={busy || fb.ready !== true} onClick={back}>Fail back to the main unit</Button>
          <Button disabled={busy} onClick={keep}>Keep this unit as the main unit</Button>
          {canHide && <Button disabled={busy} onClick={() => { hideFor(24 * 3600 * 1000); again((n) => n + 1); }}>Decide later</Button>}
        </div>
      </div>
    </div>
  );
}
