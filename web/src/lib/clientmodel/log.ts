import type { Recipient, Transition } from './types';

/**
 * transitionLine renders one transition as a plain log line. It is the log
 * source for a snapshot-only adapter (Manabrew, until its `display` channel
 * lands): gorge already has server-written lines and uses only the zone chip
 * (events.ts moveChip). Returns null for a transition not worth a line (a
 * tap, a move into nowhere).
 */
export function transitionLine(t: Transition, seatName: (seat: number) => string): string | null {
  const who = (seat: number | null) => (seat === null ? 'Someone' : seatName(seat));
  const thing = (name: string | undefined) => name ?? 'a card';
  const at = (r: Recipient) => ('seat' in r ? seatName(r.seat) : `#${r.obj}`);
  switch (t.kind) {
    case 'move':
      if (t.to.zone === 'other') return null;
      if (t.from.zone === 'library' && t.to.zone === 'hand') return `${who(t.to.seat)} draws ${t.obj === null ? 'a card' : thing(t.name)}`;
      return `${thing(t.name)} ${t.from.zone} → ${t.to.zone}`;
    case 'appear':
      return `${thing(t.name)} enters the ${t.to.zone}`;
    case 'damage':
      return `${at(t.to)} is dealt ${t.amount} damage`;
    case 'life':
      return `${seatName(t.seat)} ${t.to > t.from ? 'gains' : 'loses'} ${Math.abs(t.to - t.from)} life (${t.from} → ${t.to})`;
    case 'counter':
      return `${at(t.on)} ${t.delta > 0 ? 'gets' : 'loses'} ${Math.abs(t.delta)} ${t.counter} counter${Math.abs(t.delta) === 1 ? '' : 's'}`;
    case 'stack':
      return t.op === 'push' ? `${thing(t.name)} goes on the stack` : `${thing(t.name)} leaves the stack`;
    case 'tap':
      return null;
  }
}
