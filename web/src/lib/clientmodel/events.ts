import type { Event } from '../../protocol';
import { locate, type Located } from './snapshotdiff';
import { asZone, type ClientView, type Transition, type ZoneRef } from './types';

/**
 * events maps explicit gorge events to transitions (sub-project 0, gorge
 * adapter). Objects keep their ObjID across zone moves (events/apply.go
 * MoveZone), so a move's seats are read from the views on either side of the
 * step: the card's place in `prev` for the source, in `next` for the
 * destination, and the event's own player when the card is hidden in both.
 *
 * Kinds the board cannot show (priority, notes, decisions, mana) map to
 * nothing. A negative `damage` amount is cleanup clearing marked damage, not
 * a hit, and is dropped.
 */

function place(at: Map<number, Located>, obj: number | undefined, zone: string | undefined, fallback: number): ZoneRef {
  const z = asZone(zone);
  const l = obj ? at.get(obj) : undefined;
  if (l && l.zone === z) return { seat: l.seat, zone: z };
  return { seat: fallback, zone: z };
}

export function transitionsFromEvents(events: readonly Event[], prev: ClientView | null, next: ClientView | null): Transition[] {
  const before = locate(prev);
  const after = locate(next);
  const out: Transition[] = [];
  for (const e of events) {
    const obj = e.obj && e.obj > 0 ? e.obj : undefined;
    switch (e.kind) {
      case 'move_zone':
      case 'draw':
      case 'stack_push': {
        if (!e.from || !e.to) {
          if (e.kind === 'stack_push' && obj !== undefined) out.push({ kind: 'stack', op: 'push', obj, seq: e.seq });
          break;
        }
        const card = (obj !== undefined ? (after.get(obj) ?? before.get(obj))?.card : undefined) ?? null;
        out.push({
          kind: 'move',
          obj: obj ?? null,
          ...(card ? { name: card.name, card } : {}),
          from: place(before, obj, e.from, e.player),
          to: place(after, obj, e.to, e.player),
          seq: e.seq,
        });
        break;
      }
      case 'tap':
      case 'untap':
        if (obj !== undefined) out.push({ kind: 'tap', obj, tapped: e.kind === 'tap', seq: e.seq });
        break;
      case 'damage':
        if ((e.amount ?? 0) > 0) out.push({ kind: 'damage', to: obj !== undefined ? { obj } : { seat: e.player }, amount: e.amount!, seq: e.seq });
        break;
      case 'counter':
        if (obj !== undefined && e.counter && e.amount) out.push({ kind: 'counter', on: { obj }, counter: e.counter, delta: e.amount, seq: e.seq });
        break;
      case 'player_counter':
        if (e.counter && e.amount) out.push({ kind: 'counter', on: { seat: e.player }, counter: e.counter, delta: e.amount, seq: e.seq });
        break;
      case 'token_create':
        out.push({ kind: 'appear', obj: null, to: { seat: e.player, zone: 'battlefield' }, seq: e.seq });
        break;
      default:
        // Every ability-object push (trigger_push, ability_push,
        // keyword_trigger_push, delayed_push, ...) mints a stack object.
        if (e.kind.endsWith('_push') && obj !== undefined) out.push({ kind: 'stack', op: 'push', obj, seq: e.seq });
    }
  }
  return out;
}

const ZONE_LABEL: Record<string, string> = {
  library: 'library', hand: 'hand', battlefield: 'battlefield', graveyard: 'grave', exile: 'exile', stack: 'stack', command: 'command',
};

/**
 * moveChip is the transcript's zone chip for one event ("hand → stack"), or
 * null when the event is not a visible move. It reads the same mapping the
 * motion queue plays, so the log and the board agree on what moved.
 */
export function moveChip(e: Event): string | null {
  if (e.kind !== 'move_zone' && e.kind !== 'draw' && e.kind !== 'stack_push') return null;
  const [t] = transitionsFromEvents([e], null, null);
  if (!t || t.kind !== 'move' || t.from.zone === t.to.zone) return null;
  const a = ZONE_LABEL[t.from.zone];
  const b = ZONE_LABEL[t.to.zone];
  return a && b ? `${a} → ${b}` : null;
}
