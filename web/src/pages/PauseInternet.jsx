import React, { useState } from 'react';
import { post, useData } from '../api.js';
import { Button, Modal, Note, Segmented, useAction } from '../ui.jsx';
import { pauseChoices } from './Profiles.jsx';

// Pause one internet connection, or all of them, for a while. It says what will happen first: whether
// traffic moves to another connection or the internet stops, and whether the person managing this device
// is probably connected through the connection being paused. Only the way out stops; networks inside, the
// console, DHCP and local DNS keep working. The connection starts again by itself.
export default function PauseInternet({ ids, all, title, onClose, onDone }) {
  const [run, busy] = useAction();
  const [m, setM] = useState(60);
  const q = all ? 'all=1' : `ids=${ids.join(',')}`;
  const { data: chk } = useData(`/api/internet/pause-check?${q}`);
  const risky = !!chk && (chk.cuts_all || chk.you);
  const go = async () => {
    if (await run(() => post('/api/internet/pause', { ids, all, minutes: m }), 'Paused.')) { onDone?.(); onClose(); }
  };
  return (
    <Modal title={title} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button kind={risky ? 'danger' : 'primary'} onClick={go} disabled={busy || !chk}>{risky ? 'Pause anyway' : 'Pause'}</Button></>}>
      <div className="stack">
        <p className="small">Nothing leaves through {all ? 'any internet connection' : 'this connection'} while it is paused. Your own networks keep working, including this console, DHCP and local names. It starts again by itself when the time is up.</p>
        <Segmented value={m} onChange={setM} options={pauseChoices().map((c) => ({ value: c.m, label: c.t }))} />
        {chk && !chk.cuts_all && chk.working > 0 && <Note title="Traffic moves to another connection">{chk.left} other connection{chk.left === 1 ? '' : 's'} {chk.left === 1 ? 'is' : 'are'} working and will carry everything meanwhile.</Note>}
        {chk && chk.cuts_all && <Note tone="warn" title="The internet stops">{chk.working === 0 ? 'No connection is working now, and pausing keeps it that way.' : 'No other connection is working, so nothing can reach the internet until the pause ends.'} Existing downloads and calls stop too.</Note>}
        {chk?.you && <Note tone="bad" title="This may cut you off">{chk.you_why} If this is the connection you are using, you will lose your session until the pause ends.</Note>}
      </div>
    </Modal>
  );
}
