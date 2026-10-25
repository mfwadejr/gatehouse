// One place that turns what the connection monitor measured into words, so the Internet page,
// the connection groups and the Overview always agree. A connection is never called healthy
// until something has actually answered.
export function linkState(l) {
  if (!l) return { tone: '', text: 'Not checked', detail: 'This connection is not being watched yet. Apply your settings to start checking it.', health: 'none' };
  if (!l.checked && l.health !== 'working') return { tone: '', text: 'Checking…', detail: 'Waiting for the first answer.', health: 'unknown' };
  if (l.health === 'no-internet') {
    const ms = l.gw_rtt ? ` (${Math.max(1, Math.round(l.gw_rtt / 1e6))} ms)` : '';
    return { tone: 'bad', text: 'No internet', detail: `The next device on this connection answers${ms}, but nothing beyond it does. Check your provider or the modem's own status.`, health: 'no-internet' };
  }
  if (l.health === 'no-link' || (l.up === false)) {
    return { tone: 'bad', text: 'No link', detail: 'Nothing answers on this connection. Check the cable, the modem power or the signal.', health: 'no-link' };
  }
  return { tone: 'good', text: l.active ? 'Working' : 'Standby', detail: '', health: 'working' };
}

// Short text for the delay and loss measured on a connection.
export function linkMeasure(l) {
  if (!l || !l.checked) return '';
  const bits = [];
  if (l.up && l.rtt) bits.push(`delay ${l.rtt / 1e6 < 10 ? (l.rtt / 1e6).toFixed(1) : Math.round(l.rtt / 1e6)} ms`);
  if (l.loss > 0) bits.push(`${l.loss} % lost`);
  return bits.join(' · ');
}
