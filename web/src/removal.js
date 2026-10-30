import { get } from './api.js';
import { confirmBox } from './ui.jsx';

// Asks before a network is removed, and says in plain words everything that goes with it, so
// nothing is left behind and nothing disappears without the person knowing.
// url is the removal summary of the network; lead is the first line.
export async function confirmRemoval(url, lead, { zoneRemoval = false } = {}) {
  let imp = null;
  try { imp = await get(url); } catch { /* the summary is a courtesy; the question is still asked */ }
  const lines = [lead];
  if (imp) {
    const goes = [];
    (imp.scopes || []).forEach((s) => goes.push(`the address range ${s.range ? `${s.range}` : s.subnet} (${s.name})`));
    if (imp.remove_zone && imp.zone) goes.push(`the firewall network "${imp.zone}"`);
    if (imp.rules > 0) goes.push(`${imp.rules} exception rule${imp.rules === 1 ? '' : 's'} that mention it`);
    if (goes.length) lines.push(`This also removes: ${goes.join(', ')}.`);
    if (imp.kept) lines.push(imp.kept);
    if (zoneRemoval && !imp.management && imp.other_ports?.length) lines.push(`${imp.other_ports.join(', ')} keeps its address but is no longer part of any firewall network.`);
    if (zoneRemoval && imp.management) lines.push('This network can manage Gatehouse, so its address range stays and devices on it keep working.');
  }
  return confirmBox(lines.join('\n'), { title: 'Remove this network?', yes: 'Remove' });
}
