import type { CardView, Decision, View } from '../../protocol';

/**
 * The normalized client model (spec 2026-09-28 "Client table UI rework",
 * sub-project 0): `ClientView` + `PendingDecision` + `Transition[]`. Every
 * protocol adapter produces these; components read only these, so moving to
 * the Manabrew protocol replaces an adapter, not the UI.
 *
 * `ClientView` and `PendingDecision` are aliases of the gorge wire shapes
 * today (Ruling in docs/superpowers/plans/2026-09-28-ui-motion.md): the gorge
 * view already is the shape the board reads, and a Manabrew adapter projects
 * its `gameView` into it. Transitions are the new, protocol-independent part.
 */
export type ClientView = View;
export type PendingDecision = Decision;

/** A zone a card can be seen moving between. `other` is anything the client does not draw (ceased, sideboard, planar deck, unknown). */
export type Zone = 'library' | 'hand' | 'battlefield' | 'graveyard' | 'exile' | 'stack' | 'command' | 'other';

/** ZoneRef names one seat's zone. The stack is shared; its `seat` is the controller when known. */
export interface ZoneRef {
  seat: number | null;
  zone: Zone;
}

/** A damage/counter recipient: an object on the board, or a seat. */
export type Recipient = { obj: number } | { seat: number };

/** seq is the event seq the transition came from, when an adapter knows it (events do; a snapshot diff does not). */
interface Base {
  seq?: number;
}

/**
 * Transition is one visible change between two views. `obj` is null for a
 * card the viewer cannot identify (an opponent's draw, a face-down move).
 */
export type Transition =
  | (Base & { kind: 'move'; obj: number | null; name?: string; card?: CardView | null; from: ZoneRef; to: ZoneRef })
  | (Base & { kind: 'appear'; obj: number | null; name?: string; to: ZoneRef })
  | (Base & { kind: 'tap'; obj: number; tapped: boolean })
  | (Base & { kind: 'damage'; to: Recipient; amount: number })
  | (Base & { kind: 'counter'; on: Recipient; counter: string; delta: number })
  | (Base & { kind: 'life'; seat: number; from: number; to: number })
  | (Base & { kind: 'stack'; op: 'push' | 'pop'; obj: number; source?: number; name?: string });

/**
 * ModelEvent is what an adapter emits per view change: a `step` is a live,
 * forward change whose transitions can animate; a `reset` is anything else
 * (a new match, a snapshot, a rewind, a DVR scrub) after which any motion in
 * flight is stale.
 */
export type ModelEvent =
  | { type: 'step'; prev: ClientView; next: ClientView; transitions: Transition[] }
  | { type: 'reset' };

export type ModelListener = (e: ModelEvent) => void;

/**
 * ClientModelSource is the adapter boundary. The gorge adapter is MatchState
 * (lib/match.svelte.ts); a Manabrew adapter would keep its own snapshot ring
 * and derive transitions with `diffViews` (snapshotdiff.ts). Listeners are
 * called synchronously BEFORE the new view is assigned, so a listener can
 * still measure the old DOM.
 */
export interface ClientModelSource {
  readonly view: ClientView | null;
  readonly clientDecision: PendingDecision | null;
  onModel(fn: ModelListener): () => void;
}

const ZONES: ReadonlySet<string> = new Set(['library', 'hand', 'battlefield', 'graveyard', 'exile', 'stack', 'command']);

/** asZone narrows a wire zone name; anything the client does not draw is `other`. */
export function asZone(name: string | undefined): Zone {
  return name !== undefined && ZONES.has(name) ? (name as Zone) : 'other';
}
