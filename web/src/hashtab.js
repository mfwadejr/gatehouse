import { useCallback, useEffect, useState } from 'react';

// A page's tab lives in the address (#/page/tab, or #/page/tab/inner for a tab inside a tab), so
// links, search results and the back button can open the right one. `values` lists the tabs the page
// has; anything else in the address falls back to `first`.
const part = (n) => window.location.hash.replace(/^#\/?/, '').split('?')[0].split('/')[n] || '';

// A search can ride in the address too (#/logs/log?q=word), so it can be shared.
export const hashQuery = (name) => new URLSearchParams(window.location.hash.split('?')[1] || '').get(name) || '';
export function setHashQuery(name, value) {
  const [base, qs] = window.location.hash.split('?');
  const p = new URLSearchParams(qs || '');
  if (value) p.set(name, value); else p.delete(name);
  const next = `${base}${p.toString() ? `?${p}` : ''}`;
  if (next !== window.location.hash) window.history.replaceState(null, '', next);
}

export function useHashTab(page, values, first, depth = 1) {
  const read = useCallback(() => {
    const here = part(0) === page || depth > 1 ? part(depth) : '';
    return values.includes(here) ? here : first;
  }, [page, depth, first, values]);
  const [tab, setTab] = useState(read);
  useEffect(() => {
    const on = () => { if (depth > 1 || part(0) === page) setTab(read()); };
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, [page, depth, read]);
  const choose = useCallback((v) => {
    setTab(v);
    const head = window.location.hash.replace(/^#\/?/, '').split('?')[0].split('/').slice(0, depth);
    if (head.length === depth) window.history.replaceState(null, '', `#/${[...head.slice(0, depth), v].join('/')}`);
  }, [depth]);
  return [tab, choose];
}

// A link such as #/routes?edit=7 (the Policies page uses them) opens that item's edit window once
// the list has loaded. `find` picks the item from the list; the address is cleaned afterwards.
export function useWantedEdit(list, find, open) {
  useEffect(() => {
    const want = hashQuery('edit');
    if (!want || !list) return;
    const it = (list || []).find((x) => find(x, want));
    if (it) open(it);
    setHashQuery('edit', '');
  }); // eslint-disable-line react-hooks/exhaustive-deps
}
