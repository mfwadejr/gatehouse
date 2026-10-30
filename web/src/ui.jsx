import React, { createContext, useContext, useEffect, useId, useMemo, useState, useCallback, useRef } from 'react';
import { refreshAll, useFailing } from './api.js';

export const ICONS = {
  overview: 'M3 3h7v9H3zM14 3h7v5h-7zM14 12h7v9h-7zM3 16h7v5H3z',
  internet: 'M2 9a15 15 0 0120 0M5 12.5a10 10 0 0114 0M8.5 16a5 5 0 017 0M12 19.5h.01',
  networks: 'M9 3h6v5H9zM12 8v3M5 11h14M5 11v3M19 11v3M2 14h6v6H2zM16 14h6v6h-6z',
  wifi: 'M5 19v-3M10 19v-7M15 19V8M20 19V4',
  dns: 'M12 21a9 9 0 100-18 9 9 0 000 18zM3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18',
  dhcp: 'M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01',
  firewall: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z',
  services: 'M4 6h16M4 12h16M4 18h10M18 16v4M16 18h4',
  vpn: 'M6 11V8a6 6 0 1112 0v3M5 11h14v9H5zM12 15v2',
  devices: 'M3 5h14v10H3zM8 19h4M10 15v4M19 9h2v10h-2z',
  logs: 'M5 4h14v16H5zM9 9h6M9 13h6M9 17h3',
  settings: 'M4 7h10M18 7h2M4 17h2M10 17h10M14 5v4M6 15v4',
  check: 'M5 12l5 5 9-10',
  up: 'M12 19V5M5 12l7-7 7 7',
  down: 'M12 5v14M5 12l7 7 7-7',
  warn: 'M12 8v5M12 16.5h.01M10.3 3.9L2.4 18a2 2 0 001.7 3h15.8a2 2 0 001.7-3L13.7 3.9a2 2 0 00-3.4 0z',
  x: 'M6 6l12 12M18 6L6 18',
  plus: 'M12 5v14M5 12h14',
  trash: 'M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3',
  edit: 'M4 20h4L19 9l-4-4L4 16zM13 7l4 4',
  refresh: 'M20 11a8 8 0 00-14-4M4 4v4h4M4 13a8 8 0 0014 4M20 20v-4h-4',
  logo: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6zM9 12h6',
};

export function Icon({ name, size = 18, d }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={d || ICONS[name]} />
    </svg>
  );
}

// enter: Enter in a text box of this card does what its one green button does. It is opt-in, so a
// card with a separate dangerous button (like "Update now") never fires it from an unrelated box.
export function Card({ title, desc, actions, children, className = '', enter = false }) {
  const ref = useRef(null);
  return (
    <section className={`card ${className}`} ref={ref} onKeyDown={enter ? (e) => enterInScope(e, ref.current) : undefined}>
      {(title || actions) && (
        <div className="ch">
          <div>
            {title && <h2>{title}</h2>}
            {desc && <p className="desc">{desc}</p>}
          </div>
          {actions && <div className="actions">{actions}</div>}
        </div>
      )}
      {children}
    </section>
  );
}

export function Page({ title, sub, actions, children }) {
  const stale = useFailing();
  return (
    <>
      <header className="head">
        <div>
          <h1>{title}</h1>
          {sub && <p className="sub">{sub}</p>}
        </div>
        {actions && <div className="actions">{actions}</div>}
      </header>
      {stale && (
        <div className="stalenote small" role="status">
          Some of what is shown here could not be refreshed just now, so it may be out of date. Gatehouse keeps trying.{' '}
          <button type="button" className="link" onClick={refreshAll}>Try again now</button>
        </div>
      )}
      {children}
    </>
  );
}

export function Button({ kind = '', small, icon, children, ...rest }) {
  return (
    <button type="button" className={`btn ${kind} ${small ? 'small' : ''}`} {...rest}>
      {icon && <Icon name={icon} size={16} />}
      {children}
    </button>
  );
}

export function Switch({ checked, onChange, label, disabled }) {
  return (
    <button type="button" role="switch" aria-checked={!!checked} aria-label={label} disabled={disabled}
      className="switch" onClick={() => onChange(!checked)} />
  );
}

export function ToggleRow({ title, desc, checked, onChange, disabled }) {
  return (
    <div className="toggle-row">
      <div>
        <div className="t">{title}</div>
        {desc && <div className="d">{desc}</div>}
      </div>
      <Switch checked={checked} onChange={onChange} label={title} disabled={disabled} />
    </div>
  );
}

export function Field({ label, hint, children }) {
  const id = useId();
  const child = React.isValidElement(children) && !children.props.id ? React.cloneElement(children, { id }) : children;
  return (
    <div className="field">
      {label && <label htmlFor={id}>{label}</label>}
      {child}
      {hint && <span className="hint">{hint}</span>}
    </div>
  );
}

// A text box always keeps its own look ("in"); a caller's class is added to it, never instead of it.
export const inClass = (extra) => ['in', ...String(extra || '').split(/\s+/).filter((c) => c && c !== 'in')].join(' ');

export function Input({ value, onChange, className, ...rest }) {
  return <input value={value ?? ''} onChange={(e) => onChange(e.target.value)} {...rest} className={inClass(className)} />;
}
export function NumberInput({ value, onChange, className, ...rest }) {
  return <input type="number" value={value ?? ''} onChange={(e) => onChange(e.target.value === '' ? 0 : Number(e.target.value))} {...rest} className={inClass(className)} />;
}
export function Select({ value, onChange, options, className, ...rest }) {
  return (
    <select value={value ?? ''} onChange={(e) => onChange(e.target.value)} {...rest} className={inClass(className)}>
      {options.map((o) => (typeof o === 'string' ? <option key={o} value={o}>{o}</option> : <option key={o.value} value={o.value}>{o.label}</option>))}
    </select>
  );
}

export function Segmented({ value, onChange, options }) {
  return (
    <div className="seg" role="group">
      {options.map((o) => (
        <button key={o.value} type="button" disabled={o.disabled} className={value === o.value ? 'on' : ''} onClick={() => onChange(o.value)}>{o.label}</button>
      ))}
    </div>
  );
}

export function Tabs({ value, onChange, tabs }) {
  return (
    <div className="tabs" role="tablist">
      {tabs.map((t) => (
        <button key={t.value} role="tab" aria-selected={value === t.value} className={value === t.value ? 'on' : ''} onClick={() => onChange(t.value)}>{t.label}</button>
      ))}
    </div>
  );
}

export function Pill({ tone = '', children }) {
  return <span className={`pill ${tone}`}>{tone && <span className={`dot ${tone === 'good' ? '' : tone === 'warn' ? 'warn' : 'bad'}`} />}{children}</span>;
}

export function Note({ tone = '', title, children }) {
  return (
    <div className={`note ${tone}`} role={tone === 'bad' ? 'alert' : undefined}>
      {tone && <Icon name={tone === 'good' ? 'check' : 'warn'} size={18} />}
      <div>
        {title && <strong>{title}</strong>}
        {children}
      </div>
    </div>
  );
}

export function Problems({ list }) {
  if (!list || !list.length) return null;
  return (
    <Note tone="bad" title={list.length > 1 ? 'Fix these first' : 'Fix this first'}>
      <ul>{list.map((p, i) => <li key={i}>{p}</li>)}</ul>
    </Note>
  );
}

export function Spark({ values = [], width = 88, height = 30, area = true, tone = '', fluid = false, ceil }) {
  const path = useMemo(() => {
    if (values.length < 2) return null;
    const max = ceil || Math.max(...values, 1), min = Math.min(...values, 0);
    const span = max - min || 1;
    const pts = values.map((v, i) => [(i / (values.length - 1)) * width, height - 2 - ((v - min) / span) * (height - 4)]);
    const line = pts.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(1)} ${p[1].toFixed(1)}`).join(' ');
    return { line, area: `${line} L${width} ${height} L0 ${height} Z` };
  }, [values, width, height, ceil]);
  const idle = !values.some((v) => v > 0);
  return (
    <svg className={`spark ${tone} ${fluid ? 'fluid' : ''} ${idle ? 'idle' : ''}`} {...(fluid ? {} : { width, height })} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio={fluid ? 'none' : undefined} aria-hidden="true">
      <line className="gridline" x1="0" x2={width} y1={height - 0.5} y2={height - 0.5} />
      {path && area && <path className="area" d={path.area} />}
      {path && <path className="line" d={path.line} />}
    </svg>
  );
}

// A labelled chart: a scale on the left, time along the bottom, and a legend saying what the
// line is, so an empty or flat chart still reads as "nothing happened" rather than "broken".
export function Trend({ values = [], unit = '', series = '', seconds = 60, height = 90, empty = 'Nothing in this period' }) {
  const peak = Math.max(0, ...values);
  const top = peak === 0 ? 1 : peak < 10 ? Math.ceil(peak * 10) / 10 : Math.ceil(peak);
  const label = (n) => `${n % 1 ? n.toFixed(1) : n}${unit}`;
  return (
    <figure className="trend-chart">
      <div className="tc-body">
        <div className="tc-y" aria-hidden="true"><span>{label(top)}</span><span>{label(top / 2)}</span><span>0</span></div>
        <div className="tc-plot" style={{ height }}>
          <div className="tc-grid" aria-hidden="true"><i /><i /><i /></div>
          <Spark values={values} width={320} height={height} fluid ceil={top} />
          {peak === 0 && <span className="tc-empty">{empty}</span>}
        </div>
      </div>
      <div className="tc-x small muted" aria-hidden="true"><span>{seconds} s ago</span><span>{seconds / 2} s</span><span>now</span></div>
      {series && <figcaption className="tc-legend small muted"><span className="swatch" />{series}</figcaption>}
    </figure>
  );
}

export function Tile({ label, value, unit, foot, spark, tone }) {
  return (
    <div className="card tile">
      <span className="lbl">{label}</span>
      <div className="line">
        <div><span className="val">{value}</span>{unit && <span className="unit">{unit}</span>}</div>
        {spark && <Spark values={spark} tone={tone} />}
      </div>
      {foot && <span className="foot">{foot}</span>}
    </div>
  );
}

export function Bar({ pct, thin }) {
  return <div className={`bar ${thin ? 'thin' : ''}`} role="img" aria-label={`${Math.round(pct)} percent`}><i style={{ width: `${Math.min(100, Math.max(0, pct))}%` }} /></div>;
}

export function Empty({ children }) { return <div className="empty">{children}</div>; }
export function Loading() { return <div className="stack" aria-busy="true"><div className="skeleton" /><div className="skeleton" /></div>; }

// ---------- toasts ----------
const ToastCtx = createContext(() => {});
export const useToast = () => useContext(ToastCtx);
export function ToastHost({ children }) {
  const [items, setItems] = useState([]);
  const push = useCallback((text, tone = '') => {
    const id = Math.random();
    setItems((x) => [...x, { id, text, tone }]);
    setTimeout(() => setItems((x) => x.filter((i) => i.id !== id)), tone === 'bad' ? 8000 : 4000);
  }, []);
  return (
    <ToastCtx.Provider value={push}>
      {children}
      <DialogHost />
      <div className="toasts" role="status" aria-live="polite">{items.map((i) => <div key={i.id} className={`toast ${i.tone}`} onClick={() => setItems((x) => x.filter((t) => t.id !== i.id))} title="Click to dismiss">{i.text}</div>)}</div>
    </ToastCtx.Provider>
  );
}

// ---------- Enter key ----------
// Pressing Enter in a field, or anywhere on a dialog or step that has nothing focused, does what
// the green button does. A focused button, link or multi-line box keeps its own Enter.
export function enterToPrimary(e, root) {
  if (e.key !== 'Enter' || e.defaultPrevented || e.isComposing || e.shiftKey || !root) return;
  const t = e.target;
  const tag = t?.tagName;
  if (tag === 'BUTTON' || tag === 'A' || tag === 'TEXTAREA' || t?.closest?.('form')) return;
  if (tag === 'INPUT' && ['checkbox', 'radio'].includes(t.type)) return;
  const b = root.querySelector('.btn.primary:not([disabled])');
  if (b) { e.preventDefault(); b.click(); }
}

// Enter in a text box of a card or page (not in a dialog, which handles its own Enter) does what the
// card's one green button does. With no green button ready, or more than one, it does nothing, so a
// filter box or a half-filled form never fires something by accident.
export function enterInScope(e, root) {
  const t = e.target;
  if (e.key !== 'Enter' || e.defaultPrevented || e.isComposing || e.shiftKey || !root || t?.tagName !== 'INPUT') return;
  if (['checkbox', 'radio', 'search', 'file', 'button', 'submit'].includes(t.type) || t.dataset?.noenter != null) return;
  if (t.closest('.modal, [role="dialog"], form')) return;
  const ready = [...root.querySelectorAll('.btn.primary:not([disabled])')].filter((b) => !b.closest('.ch, .modal, [role="dialog"]'));
  if (ready.length !== 1) return;
  e.preventDefault();
  ready[0].click();
}

// A form whose Enter key submits it (see Button type="submit").
export function Form({ onSubmit, children, ...rest }) {
  return <form {...rest} onSubmit={(e) => { e.preventDefault(); onSubmit?.(e); }}>{children}</form>;
}

// ---------- modal ----------
export function Modal({ title, onClose, children, footer }) {
  const ref = useRef(null);
  // The page behind a dialog refreshes itself and hands it a new onClose each time. The dialog must
  // not treat that as "opened again" (it would grab focus back from the field being typed in).
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    const prev = document.activeElement;
    const onKey = (e) => { if (e.key === 'Escape') close.current(); };
    document.addEventListener('keydown', onKey);
    ref.current?.querySelector('input,select,textarea,button')?.focus();
    return () => { document.removeEventListener('keydown', onKey); prev?.focus?.(); };
  }, []);
  return (
    <div className="overlay" role="dialog" aria-modal="true" aria-label={title}>
      <div className="scrim" onClick={onClose} />
      <div className="modal" ref={ref} onKeyDown={(e) => enterToPrimary(e, ref.current)}>
        <h2>{title}</h2>
        {children}
        {footer && <div className="actions" style={{ justifyContent: 'flex-end' }}>{footer}</div>}
      </div>
    </div>
  );
}

// ---------- confirm and notice dialogs ----------
// Used instead of the browser's grey pop-ups. They work from anywhere with `await confirmBox(text)`,
// which gives true or false, and `await noticeBox(text)`, which waits for OK. The first button is
// the safe one and has the focus, so a stray Enter never deletes anything.
let showDialog = null;
export function confirmBox(message, { title = 'Are you sure?', yes = 'Yes, do it', no = 'Cancel', danger = true } = {}) {
  if (!showDialog) return Promise.resolve(window.confirm(message));
  return new Promise((resolve) => showDialog({ kind: 'confirm', message, title, yes, no, danger, resolve }));
}
export function noticeBox(message, { title = 'Please note' } = {}) {
  if (!showDialog) { window.alert(message); return Promise.resolve(); }
  return new Promise((resolve) => showDialog({ kind: 'notice', message, title, resolve }));
}
function DialogHost() {
  const [d, setD] = useState(null);
  const queue = useRef([]);
  useEffect(() => {
    showDialog = (x) => setD((cur) => { if (cur) { queue.current.push(x); return cur; } return x; });
    return () => { showDialog = null; };
  }, []);
  if (!d) return null;
  const done = (v) => { d.resolve(v); const next = queue.current.shift(); setD(next || null); };
  return (
    <Modal title={d.title} onClose={() => done(false)}
      footer={d.kind === 'confirm'
        ? <><Button onClick={() => done(false)}>{d.no}</Button><Button kind={d.danger ? 'danger' : 'primary'} onClick={() => done(true)}>{d.yes}</Button></>
        : <Button kind="primary" onClick={() => done(true)}>OK</Button>}>
      {String(d.message).split('\n').map((l, i) => <p key={i}>{l}</p>)}
    </Modal>
  );
}

// Runs an async action, shows its error as a toast, and tracks busy state.
export function useAction() {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const run = useCallback(async (fn, okText) => {
    setBusy(true);
    try { const r = await fn(); if (okText) toast(okText); return r; }
    catch (e) { toast(e.message || 'Something went wrong.', 'bad'); return undefined; }
    finally { setBusy(false); }
  }, [toast]);
  return [run, busy];
}

export const fmt = {
  n: (v) => (v == null ? '–' : Number(v).toLocaleString()),
  d1: (v) => (v == null ? '–' : (Math.round(v * 10) / 10).toLocaleString()),
  ago: (ts) => {
    if (!ts) return 'never';
    const s = Math.max(0, Math.floor(Date.now() / 1000 - ts));
    if (s < 10) return 'now';
    if (s < 60) return `${s}s ago`;
    if (s < 3600) return `${Math.floor(s / 60)}m ago`;
    if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
    return `${Math.floor(s / 86400)}d ago`;
  },
  time: (ts) => new Date(ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
  dt: (ts) => new Date(ts * 1000).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }),
};
