// The morning summary, put together by fixed rules from the same list as the menu dots and from
// yesterday's numbers. No AI: the same inputs always give the same words.

const plural = (n, one, many) => `${n.toLocaleString()} ${n === 1 ? one : many}`;
const minutes = (s) => (s < 90 ? 'a minute or so' : s < 3600 ? `${Math.round(s / 60)} minutes` : `${(s / 3600).toFixed(1).replace(/\.0$/, '')} hours`);

export function morningSummary(items, y) {
  const list = items || [];
  const bad = list.filter((i) => i.level === 'bad');
  const warn = list.filter((i) => i.level === 'warn');
  const info = list.filter((i) => i.level === 'info');
  let head;
  if (bad.length) head = `${plural(bad.length, 'thing is', 'things are')} broken${warn.length ? ` and ${warn.length} more need${warn.length === 1 ? 's' : ''} you` : ''}.`;
  else if (warn.length) head = `${plural(warn.length, 'thing needs', 'things need')} you.`;
  else head = 'Everything is running normally.';
  const tone = bad.length ? 'bad' : warn.length ? 'warn' : 'good';
  const top = [...bad, ...warn].slice(0, 3);
  const suggest = info.length ? `${plural(info.length, 'suggestion', 'suggestions')} waiting, none of them urgent.` : '';

  const lines = [];
  if (y) {
    const partial = y.since && y.since > y.from + 3600;
    if (partial) lines.push(`NopeWarden has only been recording since ${new Date(y.since * 1000).toLocaleString([], { weekday: 'long', hour: '2-digit', minute: '2-digit' })}, so yesterday's numbers are partial.`);
    if (!y.since || y.since < y.to) {
      lines.push(y.outages === 0 ? 'The internet stayed up all day yesterday.' : `The internet dropped ${plural(y.outages, 'time', 'times')} yesterday, ${minutes(y.down_seconds)} in total.`);
      if (y.devices_peak > 0) lines.push(`Up to ${plural(y.devices_peak, 'device', 'devices')} were online.`);
      if (y.dns_total > 0) lines.push(`${plural(y.dns_total, 'lookup was', 'lookups were')} answered, ${Math.round((100 * y.dns_blocked) / y.dns_total)}% of them blocked.`);
      lines.push(y.blocked_in > 0 ? `${plural(y.blocked_in, 'attempt', 'attempts')} from the internet ${y.blocked_in === 1 ? 'was' : 'were'} blocked, from ${plural(y.probe_addrs, 'address', 'addresses')}.` : 'Nothing from the internet was blocked.');
      if (y.alerts > 0) lines.push(`Threat watching raised ${plural(y.alerts, 'alert', 'alerts')}.`);
    }
  }
  return { head, tone, top, suggest, lines };
}
