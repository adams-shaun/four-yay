import type { CardView, StackView } from '../../protocol';
import type { ClientView, ModelListener, Transition, Zone, ZoneRef } from './types';

/**
 * snapshotdiff derives transitions by comparing two views by object id.
 *
 * This is the Manabrew half of sub-project 0: that protocol sends whole
 * `gameView` snapshots with no event log, and a card's `id` is stable across
 * zones, so moved A→B, tapped, damage, counters, life and stack push/pop all
 * fall out of a keyed comparison. It runs over `ClientView` — the future
 * adapter projects `gameView` first — and it is also the gorge adapter's
 * fallback when the step's events have not arrived (lib/clientmodel/gorge.ts).
 *
 * Gorge views omit hidden cards entirely (an opponent's hand, every library),
 * so a card that appears from, or vanishes into, a hidden zone is placed by
 * the owner's hand/library COUNTS, and a count change nothing visible
 * explains becomes an anonymous (`obj: null`) move — an opponent's draw.
 * Output order is deterministic: views are walked in player order, then zone
 * order, then card order; no map iteration reaches the result unordered.
 */

export interface Located {
  zone: Zone;
  seat: number;
  card: CardView;
}

const CARD_ZONES = ['hand', 'battlefield', 'graveyard', 'exile', 'command'] as const;

/** locate indexes every visible card by id: its zone and seat. A spell on the stack is located at 'stack' under its controller; ability stack objects are not cards and are left to stackAbilities. */
export function locate(v: ClientView | null): Map<number, Located> {
  const at = new Map<number, Located>();
  if (!v) return at;
  for (const p of v.players) {
    for (const z of CARD_ZONES) {
      for (const c of p[z] ?? []) at.set(c.id, { zone: z, seat: z === 'battlefield' ? c.controller : p.seat, card: c });
    }
  }
  for (const s of v.stack) {
    if (s.kind === 'spell' && s.card) at.set(s.id, { zone: 'stack', seat: s.controller, card: s.card });
  }
  return at;
}

function stackAbilities(v: ClientView): StackView[] {
  return v.stack.filter((s) => s.kind !== 'spell');
}

/** seatOf reads where a card is in a view, or null when it is not visible there. */
export function seatOf(v: ClientView | null, obj: number | undefined): number | null {
  if (obj === undefined || obj === 0 || !v) return null;
  return locate(v).get(obj)?.seat ?? null;
}

interface Counts {
  lib: number;
  hand: number;
}

function counts(v: ClientView): Map<number, Counts> {
  const m = new Map<number, Counts>();
  for (const p of v.players) m.set(p.seat, { lib: p.library_size, hand: p.hand_size });
  return m;
}

/** handVisible: the viewer's own hand (or an omniscient view's) lists its cards; an opponent's does not. */
function handListed(v: ClientView, seat: number): boolean {
  const p = v.players.find((q) => q.seat === seat);
  return !!p && (p.hand ?? []).length === p.hand_size;
}

const ref = (seat: number | null, zone: Zone): ZoneRef => ({ seat, zone });

export function diffViews(prev: ClientView, next: ClientView): Transition[] {
  const before = locate(prev);
  const after = locate(next);
  const cPrev = counts(prev);
  const cNext = counts(next);
  // Budget of hidden-zone count changes still unexplained, per seat.
  const libOut = new Map<number, number>();
  const handOut = new Map<number, number>();
  const handIn = new Map<number, number>();
  const libIn = new Map<number, number>();
  for (const [seat, a] of cPrev) {
    const b = cNext.get(seat);
    if (!b) continue;
    libOut.set(seat, Math.max(0, a.lib - b.lib));
    libIn.set(seat, Math.max(0, b.lib - a.lib));
    const hidden = !handListed(prev, seat) || !handListed(next, seat);
    handOut.set(seat, hidden ? Math.max(0, a.hand - b.hand) : 0);
    handIn.set(seat, hidden ? Math.max(0, b.hand - a.hand) : 0);
  }
  const take = (m: Map<number, number>, seat: number): boolean => {
    const n = m.get(seat) ?? 0;
    if (n <= 0) return false;
    m.set(seat, n - 1);
    return true;
  };

  const out: Transition[] = [];
  // Stack pops of abilities first (they resolve before what follows them is seen).
  const abPrev = stackAbilities(prev);
  const abNext = stackAbilities(next);
  const abNextIds = new Set(abNext.map((s) => s.id));
  const abPrevIds = new Set(abPrev.map((s) => s.id));
  for (const s of abPrev) if (!abNextIds.has(s.id)) out.push({ kind: 'stack', op: 'pop', obj: s.id, source: s.source, name: s.name });

  // Moves and disappearances, in prev's order.
  for (const [id, a] of before) {
    const b = after.get(id);
    if (b) {
      if (a.zone !== b.zone || a.seat !== b.seat) out.push({ kind: 'move', obj: id, name: b.card.name, card: b.card, from: ref(a.seat, a.zone), to: ref(b.seat, b.zone) });
      continue;
    }
    // Gone from view: into a hidden zone of its owner, or ceased.
    const owner = a.card.owner;
    if (take(handIn, owner)) out.push({ kind: 'move', obj: id, name: a.card.name, card: a.card, from: ref(a.seat, a.zone), to: ref(owner, 'hand') });
    else if (take(libIn, owner)) out.push({ kind: 'move', obj: id, name: a.card.name, card: a.card, from: ref(a.seat, a.zone), to: ref(owner, 'library') });
    else out.push({ kind: 'move', obj: id, name: a.card.name, card: a.card, from: ref(a.seat, a.zone), to: ref(null, 'other') });
  }
  // Appearances, in next's order.
  for (const [id, b] of after) {
    if (before.has(id)) continue;
    const owner = b.card.owner;
    if (b.zone === 'hand' && take(libOut, owner)) out.push({ kind: 'move', obj: id, name: b.card.name, card: b.card, from: ref(owner, 'library'), to: ref(b.seat, b.zone) });
    else if (b.zone !== 'hand' && take(handOut, owner)) out.push({ kind: 'move', obj: id, name: b.card.name, card: b.card, from: ref(owner, 'hand'), to: ref(b.seat, b.zone) });
    else if (b.zone !== 'hand' && take(libOut, owner)) out.push({ kind: 'move', obj: id, name: b.card.name, card: b.card, from: ref(owner, 'library'), to: ref(b.seat, b.zone) });
    else out.push({ kind: 'appear', obj: id, name: b.card.name, to: ref(b.seat, b.zone) });
  }
  // Hidden draws nothing visible explains: library → hand by counts alone.
  for (const p of next.players) {
    const n = Math.min(libOut.get(p.seat) ?? 0, handIn.get(p.seat) ?? 0);
    for (let i = 0; i < n; i++) out.push({ kind: 'move', obj: null, from: ref(p.seat, 'library'), to: ref(p.seat, 'hand') });
  }
  // Ability pushes.
  for (const s of abNext) if (!abPrevIds.has(s.id)) out.push({ kind: 'stack', op: 'push', obj: s.id, source: s.source, name: s.name });

  // Per-object state on cards visible in both.
  for (const [id, b] of after) {
    const a = before.get(id);
    if (!a) continue;
    if (a.zone === 'battlefield' && b.zone === 'battlefield' && a.card.tapped !== b.card.tapped) out.push({ kind: 'tap', obj: id, tapped: b.card.tapped });
  }
  for (const [id, b] of after) {
    const a = before.get(id);
    if (!a || a.zone !== 'battlefield' || b.zone !== 'battlefield') continue;
    if (b.card.damage > a.card.damage) out.push({ kind: 'damage', to: { obj: id }, amount: b.card.damage - a.card.damage });
    const keys = [...new Set([...Object.keys(a.card.counters ?? {}), ...Object.keys(b.card.counters ?? {})])].sort();
    for (const k of keys) {
      const d = (b.card.counters?.[k] ?? 0) - (a.card.counters?.[k] ?? 0);
      if (d !== 0) out.push({ kind: 'counter', on: { obj: id }, counter: k, delta: d });
    }
  }
  out.push(...lifeTransitions(prev, next));
  return out;
}

/** lifeTransitions is every seat whose life total changed, in player order. */
export function lifeTransitions(prev: ClientView, next: ClientView): Transition[] {
  const out: Transition[] = [];
  for (const p of next.players) {
    const q = prev.players.find((x) => x.seat === p.seat);
    if (q && q.life !== p.life) out.push({ kind: 'life', seat: p.seat, from: q.life, to: p.life });
  }
  return out;
}

/**
 * SnapshotTransitionSource is the reusable core of a snapshot-only adapter
 * (Manabrew): push each projected view in arrival order and it emits a step
 * with the diffed transitions, or a reset for the first view or a view that
 * `sameGame` says belongs to a different game. It keeps no ring itself; the
 * adapter's DVR ring is its own concern.
 */
export class SnapshotTransitionSource {
  #prev: ClientView | null = null;
  #listeners = new Set<ModelListener>();

  constructor(private readonly sameGame: (a: ClientView, b: ClientView) => boolean = () => true) {}

  onModel(fn: ModelListener): () => void {
    this.#listeners.add(fn);
    return () => this.#listeners.delete(fn);
  }

  push(next: ClientView): void {
    const prev = this.#prev;
    this.#prev = next;
    const e = prev && this.sameGame(prev, next) ? { type: 'step' as const, prev, next, transitions: diffViews(prev, next) } : { type: 'reset' as const };
    for (const fn of this.#listeners) fn(e);
  }

  reset(): void {
    this.#prev = null;
    for (const fn of this.#listeners) fn({ type: 'reset' });
  }
}
