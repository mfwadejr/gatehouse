import { useEffect, useState } from 'react';

// The look of the console (dark or light), kept per browser. It is applied the moment the console
// loads, so a saved choice shows on every page and every visit, not only after opening Settings.
const key = 'gh.theme';
export const readTheme = () => { try { return localStorage.getItem(key) === 'light' ? 'light' : 'dark'; } catch { return 'dark'; } };
export function applyTheme(t) {
  document.documentElement.dataset.theme = t;
  try { localStorage.setItem(key, t); } catch { /* ignore */ }
  window.dispatchEvent(new Event('gh-theme'));
}
document.documentElement.dataset.theme = readTheme();

export function useTheme() {
  const [theme, set] = useState(readTheme);
  useEffect(() => {
    const on = () => set(readTheme());
    window.addEventListener('gh-theme', on);
    return () => window.removeEventListener('gh-theme', on);
  }, []);
  return [theme, applyTheme];
}
