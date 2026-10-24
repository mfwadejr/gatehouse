import React from 'react';

// Simple outline pictures for the kinds of device. A person can pick another one, or upload a
// picture of their own (shown from /api/device-icons/<id>).
const P = {
  computer: 'M3 5h18v11H3zM9 20h6M12 16v4',
  laptop: 'M5 6h14v9H5zM2 19h20',
  phone: 'M8 3h8a1 1 0 011 1v16a1 1 0 01-1 1H8a1 1 0 01-1-1V4a1 1 0 011-1zM11 18h2',
  tablet: 'M5 3h14a1 1 0 011 1v16a1 1 0 01-1 1H5a1 1 0 01-1-1V4a1 1 0 011-1zM11 18h2',
  tv: 'M3 6h18v11H3zM8 21h8M12 17v4',
  speaker: 'M7 3h10a1 1 0 011 1v16a1 1 0 01-1 1H7a1 1 0 01-1-1V4a1 1 0 011-1zM12 8.5a1 1 0 100 .01M12 17a3 3 0 100-.01',
  printer: 'M7 9V3h10v6M7 17H4V9h16v8h-3M7 14h10v7H7z',
  camera: 'M4 7h4l2-2h4l2 2h4v12H4zM12 17a3.5 3.5 0 100-.01',
  game: 'M6 8h12a4 4 0 014 4v2a3 3 0 01-5 2l-1-2H8l-1 2a3 3 0 01-5-2v-2a4 4 0 014-4zM8 10v3M6.5 11.5h3M16 11h.01M18 13h.01',
  watch: 'M8 7l1-4h6l1 4M8 17l1 4h6l1-4M7 7h10v10H7zM12 10v2l1.5 1',
  iot: 'M12 12a2 2 0 100-.01M7.8 7.8a6 6 0 000 8.4M16.2 7.8a6 6 0 010 8.4M5 5a10 10 0 000 14M19 5a10 10 0 010 14',
  server: 'M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01',
  network: 'M3 14h18v6H3zM7 17h.01M11 17h.01M12 14v-4M7 6a7 7 0 0110 0M9.5 8.5a3.5 3.5 0 015 0',
  storage: 'M5 5h14v6H5zM5 13h14v6H5zM8 8h.01M8 16h.01M12 8h4M12 16h4',
  device: 'M4 6h16v12H4zM9 21h6',
  vpn: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6zM9 12l2 2 4-4',
};

export const ICON_LABELS = { computer: 'Computer', laptop: 'Laptop', phone: 'Phone', tablet: 'Tablet', tv: 'TV', speaker: 'Speaker', printer: 'Printer', camera: 'Camera', game: 'Game console', watch: 'Watch', iot: 'Smart home', server: 'Server', network: 'Network gear', storage: 'Storage', device: 'Other' };

export default function DeviceIcon({ d, size = 22 }) {
  if (d.icon_id) return <img className="devicon-img" src={`/api/device-icons/${d.icon_id}`} width={size} height={size} alt="" />;
  const name = d.source === 'vpn' ? 'vpn' : d.icon || d.icon_guess || 'device';
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={P[name] || P.device} />
    </svg>
  );
}
