// The NopeWarden badge: the same purple-blue shield as the website's tab icon. Its colours are the brand
// tokens (tokens/tokens.json); the favicon and logo files are generated from the same ones.
export function Logo({ size = 24 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none" aria-hidden="true">
      <defs><linearGradient id="nw-logo-grad" x1="0" y1="0" x2="1" y2="1"><stop style={{ stopColor: 'var(--brand-badge-start)' }} /><stop offset="1" style={{ stopColor: 'var(--brand-badge-end)' }} /></linearGradient></defs>
      <path d="M16 2.5 5 6.5v8.2c0 6.6 4.5 12.2 11 14.8 6.5-2.6 11-8.2 11-14.8V6.5L16 2.5Z" fill="url(#nw-logo-grad)" />
      <path d="M11 15.5h10" style={{ stroke: 'var(--brand-badge-mark)' }} strokeWidth="2.6" strokeLinecap="round" />
    </svg>
  );
}
