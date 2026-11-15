import { useCallback, useState } from 'react';

// One shared choice of time window (1 h, 24 h, 7 d) for the charts across the console, remembered in
// this browser. Live is a different kind of view (a rolling minute drawn as it happens, not stored
// history), so it is offered only where a page can show it and is remembered for that page alone.
// A page that cannot show Live always starts from the remembered window.
export const WINDOWS = ['1h', '24h', '7d'];
const WIN_KEY = 'nw.period';

const read = (k) => { try { return window.localStorage.getItem(k) || ''; } catch { return ''; } };
const write = (k, v) => { try { window.localStorage.setItem(k, v); } catch { /* the choice just is not remembered */ } };

export function usePeriod(page, { live = false, allowed = WINDOWS } = {}) {
  const win = () => { const w = read(WIN_KEY); return allowed.includes(w) ? w : allowed.includes('1h') ? '1h' : allowed[0]; };
  const [value, setValue] = useState(() => {
    if (live) { const mine = read(`nw.period.${page}`); if (mine === 'live' || allowed.includes(mine)) return mine; return 'live'; }
    return win();
  });
  const set = useCallback((v) => {
    setValue(v);
    if (live) write(`nw.period.${page}`, v);
    if (v !== 'live') write(WIN_KEY, v);
  }, [live, page]);
  const options = [...(live ? [{ value: 'live', label: 'Live' }] : []), ...allowed.map((w) => ({ value: w, label: w.replace(/^(\d+)([a-z])$/, '$1 $2') }))];
  return [value, set, options];
}
