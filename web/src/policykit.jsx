import React from 'react';
import { post, put, useData } from './api.js';
import { AddrPicker, ScheduleEditor, ServicePicker } from './AddrPicker.jsx';
import { pauseChoices, whenText } from './pages/Profiles.jsx';
import { Button, Field, Select, ToggleRow } from './ui.jsx';

// The building blocks every policy form is made from, so a firewall rule, a route, a block or a
// port forward asks for who, where, what and when in the same way and the same words.

// usePickers loads what the pickers offer: devices, groups, networks, saved port lists and the
// services Gatehouse knows by name.
export function usePickers() {
  const { data, reload } = useData('/api/fw/pickers');
  return { pickers: data, reload };
}

export function WhoField({ value, onChange, pickers, reload, networks = true, label = 'Who', hint }) {
  return (
    <Field label={label} hint={hint || 'Any mix of networks, devices, device groups and addresses. Leave empty for everyone.'}>
      <AddrPicker side="src" value={value} onChange={onChange} pickers={pickers} reload={reload} networks={networks} />
    </Field>
  );
}

export function WhereField({ value, onChange, pickers, reload, label = 'Where to', hint }) {
  return (
    <Field label={label} hint={hint || 'Any mix of websites, addresses, ranges and countries. Leave empty for anywhere.'}>
      <AddrPicker side="dst" value={value} onChange={onChange} pickers={pickers} reload={reload} />
    </Field>
  );
}

export function ServiceField({ value, onChange, pickers, reload, label = 'What for', hint }) {
  return (
    <Field label={label} hint={hint || 'Services such as NTP or NNTPS, or ports like tcp/563. Leave empty for any traffic.'}>
      <ServicePicker value={value} onChange={onChange} pickers={pickers} reload={reload} />
    </Field>
  );
}

export function WhenField({ value, onChange, label = 'When', hint }) {
  return (
    <Field label={label} hint={hint || 'Always, or only during certain hours.'}>
      <ScheduleEditor value={value} onChange={onChange} />
    </Field>
  );
}

export function OnToggle({ checked, onChange, what = 'This' }) {
  return <ToggleRow title="Turned on" desc={`Turn off to keep ${what.toLowerCase()} without using it.`} checked={checked !== false} onChange={onChange} />;
}

// PauseMenu is the same everywhere: a drop-down of durations, or Resume while paused.
export function PauseMenu({ until, name, onPause, disabled }) {
  if (until * 1000 > Date.now()) return <Button small kind="primary" disabled={disabled} onClick={() => onPause(0)}>Resume now</Button>;
  return (
    <Select value="" onChange={(v) => v && onPause(Number(v))} aria-label={`Pause ${name}`} disabled={disabled}
      options={[{ value: '', label: 'Pause…' }, ...pauseChoices().map((c) => ({ value: String(c.m), label: c.t }))]} />
  );
}

export { whenText };

// PolicyControls is the one row of actions every policy has: Pause, Turn off or on, Edit, Remove.
// A caller leaves out what its kind of policy cannot do.
export function PolicyControls({ name, enabled, pausedUntil, onPause, onToggle, onEdit, onRemove, busy }) {
  return (
    <>
      {onPause && (enabled || pausedUntil * 1000 > Date.now()) && <PauseMenu until={pausedUntil} name={name} onPause={onPause} disabled={busy} />}
      {onToggle && <Button small disabled={busy} onClick={onToggle}>{enabled ? 'Turn off' : 'Turn on'}</Button>}
      {onEdit && <Button small icon="edit" onClick={onEdit}>Edit</Button>}
      {onRemove && <Button small kind="danger" icon="trash" onClick={onRemove}>Remove</Button>}
    </>
  );
}

export { post, put };
