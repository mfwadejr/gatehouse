import React, { useState } from 'react';
import { put, useData } from '../api.js';
import { Button, Field, Note, Select, useAction, confirmBox } from '../ui.jsx';

// Virtual Network Override: place one device on a VLAN network, wherever it is plugged in or
// connected. The caution is shown when it is ticked, as UniFi does, because the VLAN has to make
// sense on the way between the device and Gatehouse.
export default function VirtualOverride({ d, onSaved }) {
  const { data } = useData('/api/approval');
  const [run, busy] = useAction();
  const [pick, setPick] = useState('');
  const nets = data?.networks || [];
  const on = !!d.override;
  const set = (network, msg) => run(() => put(`/api/devices/${d.id}/override`, { network }), msg).then((ok) => { if (ok) onSaved(); });
  if (!nets.length && !on) {
    return (
      <Field label="Virtual Network Override" hint="Places this device on a VLAN network. You have no VLAN networks yet. Add one on the Networks page (give it a VLAN number) and it appears here." />
    );
  }
  return (
    <Field label="Virtual Network Override" hint="Assign this device to a specific VLAN, wherever it connects (cable or Wi-Fi). Gatehouse puts the device's traffic on that VLAN at its own port.">
      {on ? (
        <div className="stack">
          <p className="small">On <b>{d.override_network || d.override}</b>. <Button small disabled={busy} onClick={() => set('', 'Back on its own network.')}>Remove the override</Button></p>
          <p className="small muted">It takes effect when the device next asks for an address. Turn its Wi-Fi off and on, or unplug the cable for a moment, to make it immediate.</p>
        </div>
      ) : (
        <div className="inline" style={{ gap: 8, flexWrap: 'wrap' }}>
          <Select value={pick} onChange={setPick} options={[{ value: '', label: 'Choose a network' }, ...nets.map((n) => ({ value: n.iface, label: `${n.network} (VLAN ${n.vid})` }))]} />
          <Button small kind="primary" disabled={busy || !pick} onClick={async () => {
            const n = nets.find((x) => x.iface === pick);
            if (await confirmBox(`Proceed with caution. Gatehouse will place this device on ${n?.network} (VLAN ${n?.vid}) at its own port, so nothing needs to be tagged on switches between them. But if a switch or access point between the device and Gatehouse already puts this device, or its Wi-Fi name, on a different VLAN, remove that first or the two will fight. If the move does not work the device may lose its connection until you remove the override.`)) set(pick, 'Moved.');
          }}>Move it</Button>
        </div>
      )}
      <Note tone="">Works the same for wired and Wi-Fi devices. The VLAN network must be on the same port the device is plugged into, and the device has to ask for an address again to join it.</Note>
    </Field>
  );
}
