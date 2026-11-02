import { useCallback, useEffect, useState } from 'react';

// A page's tab lives in the address (#/page/tab, or #/page/tab/inner for a tab inside a tab), so
// links, search results and the back button can open the right one. `values` lists the tabs the page
// has; anything else in the address falls back to `first`.
const part = (n) => window.location.hash.replace(/^#\/?/, '').split('/')[n] || '';

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
    const head = window.location.hash.replace(/^#\/?/, '').split('/').slice(0, depth);
    if (head.length === depth) window.history.replaceState(null, '', `#/${[...head.slice(0, depth), v].join('/')}`);
  }, [depth]);
  return [tab, choose];
}
