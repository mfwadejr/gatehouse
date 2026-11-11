import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

// The site's own dropdown, time, date and date-range controls. A browser's native list, clock and
// calendar cannot be styled, so none of them is used anywhere: check-controls.mjs fails the build
// if a raw select or a native date or time input appears in a screen.

const cx = (...c) => c.filter(Boolean).join(' ');

// A box that opens under (or above) its trigger, closes on an outside click or Escape and keeps
// the focus where the person put it.
function usePopup(open, setOpen, triggerRef) {
  const popRef = useRef(null);
  const [pos, setPos] = useState(null);
  const place = useCallback(() => {
    const t = triggerRef.current;
    if (!t) return;
    const r = t.getBoundingClientRect();
    const h = popRef.current ? popRef.current.offsetHeight : 0;
    const below = window.innerHeight - r.bottom, up = h && below < h + 8 && r.top > below;
    setPos({ left: Math.max(8, Math.min(r.left, window.innerWidth - (popRef.current?.offsetWidth || r.width) - 8)), top: up ? Math.max(8, r.top - h - 4) : r.bottom + 4, minWidth: r.width });
  }, [triggerRef]);
  useLayoutEffect(() => { if (open) place(); }, [open, place]);
  useEffect(() => { if (open) place(); });
  useEffect(() => {
    if (!open) return undefined;
    const down = (e) => { if (!popRef.current?.contains(e.target) && !triggerRef.current?.contains(e.target)) setOpen(false); };
    const key = (e) => { if (e.key === 'Escape') { e.stopPropagation(); setOpen(false); triggerRef.current?.focus(); } };
    const move = (e) => { if (!popRef.current?.contains(e.target)) place(); };
    document.addEventListener('mousedown', down);
    document.addEventListener('keydown', key, true);
    window.addEventListener('resize', place);
    window.addEventListener('scroll', move, true);
    return () => { document.removeEventListener('mousedown', down); document.removeEventListener('keydown', key, true); window.removeEventListener('resize', place); window.removeEventListener('scroll', move, true); };
  }, [open, place, setOpen, triggerRef]);
  const popup = (children, props = {}) => open && createPortal(
    <div ref={popRef} className={cx('pop', props.className)} style={{ left: pos?.left ?? 0, top: pos?.top ?? 0, minWidth: props.fit ? pos?.minWidth : undefined, visibility: pos ? 'visible' : 'hidden' }} role={props.role} aria-label={props['aria-label']} id={props.id} onKeyDown={props.onKeyDown} tabIndex={-1}>{children}</div>, document.body);
  return { popup, popRef };
}

// ---- Select --------------------------------------------------------------------------------
const flat = (options) => options.flatMap((o) => (o.group ? o.options : [typeof o === 'string' ? { value: o, label: o } : o]));

export function Select({ value, onChange, options, className, disabled, id, style, ...rest }) {
  const [open, setOpen] = useState(false);
  const [hi, setHi] = useState(0);
  const btn = useRef(null);
  const list = useRef(null);
  const typed = useRef({ s: '', t: 0 });
  const items = useMemo(() => flat(options), [options]);
  const cur = items.findIndex((o) => String(o.value) === String(value ?? ''));
  const label = cur >= 0 ? items[cur].label : '';
  const { popup } = usePopup(open, setOpen, btn);
  const uid = useRef(`sel-${Math.random().toString(36).slice(2, 8)}`).current;
  useEffect(() => { if (open) setHi(Math.max(0, cur)); }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (open) list.current?.querySelector('[data-hi="1"]')?.scrollIntoView({ block: 'nearest' }); }, [open, hi]);
  const pick = (o) => { setOpen(false); btn.current?.focus(); if (String(o.value) !== String(value ?? '')) onChange(o.value); };
  const onKey = (e) => {
    const k = e.key;
    if (k === 'ArrowDown' || k === 'ArrowUp') { e.preventDefault(); if (!open) { setOpen(true); return; } setHi((h) => Math.min(items.length - 1, Math.max(0, h + (k === 'ArrowDown' ? 1 : -1)))); }
    else if (k === 'Home' && open) { e.preventDefault(); setHi(0); }
    else if (k === 'End' && open) { e.preventDefault(); setHi(items.length - 1); }
    else if (k === 'Enter' || k === ' ') { if (open && items[hi]) { e.preventDefault(); pick(items[hi]); } else if (!open) { e.preventDefault(); setOpen(true); } }
    else if (k === 'Tab') setOpen(false);
    else if (k.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const now = Date.now();
      typed.current = { s: (now - typed.current.t < 700 ? typed.current.s : '') + k.toLowerCase(), t: now };
      const at = items.findIndex((o) => String(o.label).toLowerCase().startsWith(typed.current.s));
      if (at >= 0) { if (open) setHi(at); else onChange(items[at].value); }
    }
  };
  let n = -1;
  const row = (o) => { n += 1; const i = n; return (
    <div key={`${o.value}-${i}`} role="option" id={`${uid}-${i}`} aria-selected={String(o.value) === String(value ?? '')} data-hi={i === hi ? '1' : undefined} className={cx('opt', i === hi && 'hi', String(o.value) === String(value ?? '') && 'on')}
      onMouseEnter={() => setHi(i)} onMouseDown={(e) => e.preventDefault()} onClick={() => pick(o)}>{o.label}</div>
  ); };
  return (
    <>
      <button type="button" ref={btn} id={id} disabled={disabled} className={cx('in', 'selbtn', className)} style={style} role="combobox" aria-haspopup="listbox" aria-expanded={open} aria-controls={open ? `${uid}-list` : undefined} aria-activedescendant={open ? `${uid}-${hi}` : undefined}
        onClick={() => !disabled && setOpen((o) => !o)} onKeyDown={onKey} {...rest}>
        <span className="selval">{label || ' '}</span><span className="selcaret" aria-hidden="true">▾</span>
      </button>
      {popup(<div ref={list} id={`${uid}-list`} role="listbox" className="optlist" aria-label={rest['aria-label']}>
        {options.map((o) => (o.group
          ? <div key={o.group} role="group" aria-label={o.group}><div className="optgroup">{o.group}</div>{o.options.map(row)}</div>
          : row(typeof o === 'string' ? { value: o, label: o } : o)))}
      </div>, { fit: true, className: 'selpop' })}
    </>
  );
}

// ---- helpers for dates and times -------------------------------------------------------------
const p2 = (n) => String(n).padStart(2, '0');
export const ymd = (d) => `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`;
export const parseYmd = (s) => { const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(s || ''); return m ? new Date(+m[1], +m[2] - 1, +m[3]) : null; };
const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const DOW = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];
const showDate = (s) => { const d = parseYmd(s); return d ? `${MONTHS[d.getMonth()].slice(0, 3)} ${d.getDate()}, ${d.getFullYear()}` : ''; };
const goodTime = (s) => /^([01]?\d|2[0-3]):[0-5]\d$/.test(s);
const normTime = (s) => { const [h, m] = s.split(':'); return `${p2(+h)}:${m}`; };

// ---- Time of day ----------------------------------------------------------------------------
export function TimeField({ value, onChange, className, disabled, id, ...rest }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState(value || '');
  const btn = useRef(null);
  const { popup } = usePopup(open, setOpen, btn);
  useEffect(() => { setText(value || ''); }, [value, open]);
  const [h, m] = goodTime(value || '') ? value.split(':').map(Number) : [null, null];
  const set = (nh, nm) => onChange(`${p2(nh ?? 0)}:${p2(nm ?? 0)}`);
  const commit = () => { if (goodTime(text)) { onChange(normTime(text)); setOpen(false); btn.current?.focus(); } };
  const colRef = (el) => { el?.querySelector('.on')?.scrollIntoView({ block: 'center' }); };
  return (
    <>
      <button type="button" ref={btn} id={id} disabled={disabled} className={cx('in', 'selbtn', 'timebtn', className)} aria-haspopup="dialog" aria-expanded={open} onClick={() => !disabled && setOpen((o) => !o)} {...rest}>
        <span className="selval">{value || '--:--'}</span><span className="selcaret" aria-hidden="true">◷</span>
      </button>
      {popup(
        <div className="timepop">
          <input className="in mono" value={text} inputMode="numeric" placeholder="HH:MM" aria-label="Time, 24-hour" onChange={(e) => setText(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); commit(); } }} />
          <div className="cols">
            <div className="col" role="listbox" aria-label="Hour" ref={colRef}>{Array.from({ length: 24 }, (_, i) => <button type="button" key={i} role="option" aria-selected={i === h} className={cx('opt', i === h && 'on')} onClick={() => set(i, m)}>{p2(i)}</button>)}</div>
            <div className="col" role="listbox" aria-label="Minute" ref={colRef}>{Array.from({ length: 12 }, (_, i) => i * 5).concat(m !== null && m % 5 ? [m] : []).sort((a, b) => a - b).map((i) => <button type="button" key={i} role="option" aria-selected={i === m} className={cx('opt', i === m && 'on')} onClick={() => set(h, i)}>{p2(i)}</button>)}</div>
          </div>
          <div className="actions"><button type="button" className="btn small" onClick={() => { onChange(''); setOpen(false); }}>Clear</button><button type="button" className="btn small primary" onClick={() => { if (goodTime(text)) commit(); else setOpen(false); }}>Done</button></div>
        </div>, { role: 'dialog', 'aria-label': 'Choose a time' })}
    </>
  );
}

// ---- Calendar ---------------------------------------------------------------------------------
function Calendar({ month, setMonth, isOn, inRange, onPick, onHover, ok }) {
  const first = new Date(month.getFullYear(), month.getMonth(), 1);
  const start = new Date(first); start.setDate(1 - first.getDay());
  const days = Array.from({ length: 42 }, (_, i) => { const d = new Date(start); d.setDate(start.getDate() + i); return d; });
  const today = ymd(new Date());
  const shift = (n) => setMonth(new Date(month.getFullYear(), month.getMonth() + n, 1));
  const years = []; for (let y = month.getFullYear() - 6; y <= month.getFullYear() + 3; y += 1) years.push({ value: String(y), label: String(y) });
  return (
    <div className="cal">
      <div className="calhead">
        <button type="button" className="btn small" aria-label="Previous month" onClick={() => shift(-1)}>‹</button>
        <div className="calpick">
          <Select value={String(month.getMonth())} onChange={(v) => setMonth(new Date(month.getFullYear(), +v, 1))} options={MONTHS.map((n, i) => ({ value: String(i), label: n }))} aria-label="Month" />
          <Select value={String(month.getFullYear())} onChange={(v) => setMonth(new Date(+v, month.getMonth(), 1))} options={years} aria-label="Year" />
        </div>
        <button type="button" className="btn small" aria-label="Next month" onClick={() => shift(1)}>›</button>
      </div>
      <div className="calgrid" role="grid">
        {DOW.map((d) => <div key={d} className="dow" role="columnheader">{d}</div>)}
        {days.map((d) => { const k = ymd(d); return (
          <button type="button" key={k} role="gridcell" disabled={ok ? !ok(k) : false} aria-selected={isOn(k)} aria-label={showDate(k)} className={cx('day', d.getMonth() !== month.getMonth() && 'off', k === today && 'today', inRange?.(k) && 'inr', isOn(k) && 'on')} onMouseEnter={() => onHover?.(k)} onClick={() => onPick(k)}>{d.getDate()}</button>
        ); })}
      </div>
    </div>
  );
}

// ---- A day --------------------------------------------------------------------------------------
export function DateField({ value, onChange, className, disabled, id, min, max, ...rest }) {
  const [open, setOpen] = useState(false);
  const btn = useRef(null);
  const { popup } = usePopup(open, setOpen, btn);
  const [month, setMonth] = useState(() => parseYmd(value) || new Date());
  useEffect(() => { if (open) { const d = parseYmd(value) || new Date(); setMonth(new Date(d.getFullYear(), d.getMonth(), 1)); } }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const ok = (k) => (!min || k >= min) && (!max || k <= max);
  return (
    <>
      <button type="button" ref={btn} id={id} disabled={disabled} className={cx('in', 'selbtn', 'datebtn', className)} aria-haspopup="dialog" aria-expanded={open} onClick={() => !disabled && setOpen((o) => !o)} {...rest}>
        <span className="selval">{showDate(value) || 'Pick a date'}</span><span className="selcaret" aria-hidden="true">▾</span>
      </button>
      {popup(
        <>
          <Calendar ok={ok} month={month} setMonth={setMonth} isOn={(k) => k === value} onPick={(k) => { if (ok(k)) { onChange(k); setOpen(false); btn.current?.focus(); } }} />
          <div className="actions"><button type="button" className="btn small" onClick={() => { onChange(''); setOpen(false); }}>Clear</button><button type="button" className="btn small" onClick={() => { const t = ymd(new Date()); if (ok(t)) { onChange(t); setOpen(false); } }}>Today</button></div>
        </>, { role: 'dialog', 'aria-label': 'Choose a date' })}
    </>
  );
}

// ---- A day and a time of day (value like 2026-11-11T14:30) -----------------------------------------
export function DateTimeField({ value, onChange, className, disabled, ...rest }) {
  const [d, t] = (value || '').split('T');
  const join = (nd, nt) => (nd ? `${nd}T${nt || '00:00'}` : '');
  return (
    <div className="dtf">
      <DateField value={d || ''} onChange={(nd) => onChange(join(nd, t))} className={className} disabled={disabled} {...rest} />
      <TimeField value={t ? t.slice(0, 5) : ''} onChange={(nt) => onChange(d ? join(d, nt) : '')} disabled={disabled || !d} aria-label="Time" />
    </div>
  );
}

// ---- A range of days (from and to, both included) --------------------------------------------------
export function DateRange({ from, to, onChange, className, disabled, id, min, max, ...rest }) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState(null);
  const [hover, setHover] = useState(null);
  const btn = useRef(null);
  const { popup } = usePopup(open, setOpen, btn);
  const [month, setMonth] = useState(() => parseYmd(to || from) || new Date());
  useEffect(() => { if (open) { const d = parseYmd(to || from) || new Date(); setMonth(new Date(d.getFullYear(), d.getMonth(), 1)); setAnchor(null); } }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const a = anchor || from, b = anchor ? (hover || anchor) : to;
  const lo = a && b ? (a < b ? a : b) : a, hi = a && b ? (a < b ? b : a) : a;
  const ok = (k) => (!min || k >= min) && (!max || k <= max);
  const pick = (k) => {
    if (!ok(k)) return;
    if (!anchor) { setAnchor(k); setHover(k); return; }
    const x = anchor < k ? anchor : k, y = anchor < k ? k : anchor;
    setAnchor(null); setOpen(false); onChange(x, y); btn.current?.focus();
  };
  const label = from && to ? (from === to ? showDate(from) : `${showDate(from)} to ${showDate(to)}`) : 'Pick a range';
  return (
    <>
      <button type="button" ref={btn} id={id} disabled={disabled} className={cx('in', 'selbtn', 'datebtn', className)} aria-haspopup="dialog" aria-expanded={open} onClick={() => !disabled && setOpen((o) => !o)} {...rest}>
        <span className="selval">{label}</span><span className="selcaret" aria-hidden="true">▾</span>
      </button>
      {popup(
        <>
          <Calendar ok={ok} month={month} setMonth={setMonth} isOn={(k) => k === lo || k === hi} inRange={(k) => lo && hi && k > lo && k < hi} onPick={pick} onHover={(k) => anchor && setHover(k)} />
          <p className="small muted calnote">{anchor ? 'Now pick the last day.' : 'Pick the first day, then the last.'}</p>
        </>, { role: 'dialog', 'aria-label': 'Choose a range of days' })}
    </>
  );
}
