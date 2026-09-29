import type { CardView } from '../../protocol';
import type { Recipient, Transition, ZoneRef } from '../clientmodel/types';
import { TIMINGS, type MotionSpeed } from './settings';

/**
 * plan turns one batch of transitions (one view step) into timed motion
 * steps (spec sub-project 5). Pure: the DOM layer (dom.ts) resolves anchors
 * and plays the steps; the queue (queue.ts) orders batches.
 *
 * - A zone move is a flight. Consecutive moves of one object coalesce into a
 *   single flight from its first source to its last destination: by the time
 *   the batch plays, the intermediate zone (usually the stack) is gone.
 * - Beats (flights, damage, counters, life floats) are staggered in the
 *   transitions' own order.
 * - Past `cap` flights the batch collapses: one group flight per destination
 *   pile, carrying the count (a board wipe is one flight per graveyard).
 * - A flight into a pile bumps the pile when it lands.
 * - Damage floats a number and shakes the recipient. A life float is skipped
 *   for a seat that took damage in the batch (the damage float already says it).
 */

export interface Anchor {
  /** the card's own element when it is on screen */
  obj: number | null;
  zone: ZoneRef;
}

export type MotionStep =
  | { kind: 'flight'; objs: (number | null)[]; card: CardView | null; from: Anchor; to: Anchor; count: number; delay: number; duration: number }
  | { kind: 'float'; at: Recipient; life: boolean; text: string; tone: 'damage' | 'loss' | 'gain' | 'counter'; delay: number; duration: number }
  | { kind: 'shake'; at: Recipient; delay: number; duration: number }
  | { kind: 'bump'; zone: ZoneRef; delay: number; duration: number }
  | { kind: 'appear'; obj: number; delay: number; duration: number };

export interface BatchPlan {
  steps: MotionStep[];
  /** ms until the last step ends */
  duration: number;
}

export const DEFAULT_CAP = 8;

const PILES = new Set(['library', 'graveyard', 'exile', 'command']);

type Move = Extract<Transition, { kind: 'move' }>;
type Push = Extract<Transition, { kind: 'stack' }>;

interface FlightDraft {
  objs: (number | null)[];
  card: CardView | null;
  from: Anchor;
  to: Anchor;
}

const zoneKey = (z: ZoneRef) => `${z.seat ?? '-'}:${z.zone}`;

/** isPileLanding: the destination shows a count, not the card (a pile, or a hand the viewer cannot see into). */
function landsInPile(f: FlightDraft): boolean {
  return PILES.has(f.to.zone.zone) || (f.to.zone.zone === 'hand' && f.objs.every((o) => o === null));
}

export function planBatch(transitions: readonly Transition[], speed: MotionSpeed, opts: { cap?: number } = {}): BatchPlan {
  if (speed === 'off' || transitions.length === 0) return { steps: [], duration: 0 };
  const t = TIMINGS[speed];
  const cap = opts.cap ?? DEFAULT_CAP;

  // Coalesce moves per object; keep each object's first slot.
  const lastMove = new Map<number, Move>();
  for (const x of transitions) if (x.kind === 'move' && x.obj !== null) lastMove.set(x.obj, x);
  const damagedSeats = new Set<number>();
  for (const x of transitions) if (x.kind === 'damage' && 'seat' in x.to) damagedSeats.add(x.to.seat);

  type Beat = { flight: FlightDraft } | { other: Transition };
  const beats: Beat[] = [];
  const flown = new Set<number>();
  for (const x of transitions) {
    if (x.kind === 'move') {
      if (x.obj !== null) {
        if (flown.has(x.obj)) continue;
        flown.add(x.obj);
      }
      const last = x.obj !== null ? lastMove.get(x.obj)! : x;
      if (last.to.zone === 'other') continue;
      if (x.from.zone === 'other') {
        if (x.obj !== null) beats.push({ other: { kind: 'appear', obj: x.obj, to: last.to } });
        continue;
      }
      if (zoneKey(x.from) === zoneKey(last.to)) continue;
      beats.push({ flight: { objs: [x.obj], card: last.card ?? x.card ?? null, from: { obj: x.obj, zone: x.from }, to: { obj: x.obj, zone: last.to } } });
    } else if (x.kind === 'stack' && x.op === 'push' && x.source !== undefined) {
      beats.push({ flight: pushFlight(x) });
    } else {
      beats.push({ other: x });
    }
  }

  // Cap: collapse every flight into one group flight per destination.
  const flights = beats.filter((b): b is { flight: FlightDraft } => 'flight' in b);
  let ordered: Beat[] = beats;
  if (flights.length > cap) {
    const groups = new Map<string, FlightDraft>();
    ordered = [];
    for (const b of beats) {
      if (!('flight' in b)) {
        ordered.push(b);
        continue;
      }
      const k = zoneKey(b.flight.to.zone);
      const g = groups.get(k);
      if (g) {
        g.objs.push(...b.flight.objs);
        continue;
      }
      const lead: FlightDraft = { ...b.flight, objs: [...b.flight.objs], to: { obj: null, zone: b.flight.to.zone } };
      groups.set(k, lead);
      ordered.push({ flight: lead });
    }
  }

  const steps: MotionStep[] = [];
  const bumps = new Map<string, MotionStep & { kind: 'bump' }>();
  let slot = 0;
  for (const b of ordered) {
    const delay = slot * t.stagger;
    if ('flight' in b) {
      const f = b.flight;
      steps.push({ kind: 'flight', objs: f.objs, card: f.card, from: f.from, to: f.to, count: f.objs.length, delay, duration: t.flight });
      if (landsInPile(f)) bumps.set(zoneKey(f.to.zone), { kind: 'bump', zone: f.to.zone, delay: delay + t.flight, duration: t.bump });
      slot++;
      continue;
    }
    const x = b.other;
    switch (x.kind) {
      case 'damage':
        steps.push({ kind: 'float', at: x.to, life: 'seat' in x.to, text: `−${x.amount}`, tone: 'damage', delay, duration: t.float });
        steps.push({ kind: 'shake', at: x.to, delay, duration: t.shake });
        slot++;
        break;
      case 'counter': {
        steps.push({ kind: 'float', at: x.on, life: false, text: counterText(x.counter, x.delta), tone: 'counter', delay, duration: t.float });
        slot++;
        break;
      }
      case 'life':
        if (damagedSeats.has(x.seat)) break;
        steps.push({ kind: 'float', at: { seat: x.seat }, life: true, text: `${x.to > x.from ? '+' : '−'}${Math.abs(x.to - x.from)}`, tone: x.to > x.from ? 'gain' : 'loss', delay, duration: t.float });
        slot++;
        break;
      case 'appear':
        if (x.obj !== null) {
          steps.push({ kind: 'appear', obj: x.obj, delay, duration: t.flight });
          slot++;
        }
        break;
      default:
        break; // taps render as the board's own state; pops have nothing to fly
    }
  }
  steps.push(...bumps.values());
  const duration = steps.reduce((m, s) => Math.max(m, s.delay + s.duration), 0);
  return { steps, duration };
}

/** An ability going on the stack flies from its source permanent to its stack entry. */
function pushFlight(x: Push): FlightDraft {
  return { objs: [x.obj], card: null, from: { obj: x.source!, zone: { seat: null, zone: 'battlefield' } }, to: { obj: x.obj, zone: { seat: null, zone: 'stack' } } };
}

const COUNTER_LABELS: Record<string, string> = { P1P1: '+1/+1', M1M1: '−1/−1', LOYALTY: 'loyalty', DEFENSE: 'defense' };

/** counterLabel spells an engine counter name for people: P1P1 is +1/+1, LOYALTY is loyalty. */
export function counterLabel(name: string): string {
  return COUNTER_LABELS[name.toUpperCase()] ?? name.toLowerCase().replace(/_/g, ' ');
}

/** counterText is a counter change's float: "+1/+1", "+1/+1 ×2", "−2 loyalty", "+1 poison". */
export function counterText(name: string, delta: number): string {
  const n = Math.abs(delta);
  const label = counterLabel(name);
  if (/^[+−]\d/.test(label)) return delta > 0 ? `${label}${n > 1 ? ` ×${n}` : ''}` : `−${n} × ${label}`;
  return `${delta > 0 ? '+' : '−'}${n} ${label}`;
}
