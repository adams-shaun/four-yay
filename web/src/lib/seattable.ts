import type { PlayerView, SeatInfo, View } from '../protocol';
import { visibleHand } from './board';
import { countsFor } from './zones';

/**
 * seattable.ts is the projection behind the rail's one compact seat summary.
 * Rows are seats and the five icon/count cells are life, hand, library,
 * graveyard and exile. The three card-list zones are disclosed from that same
 * row in a modal; there is no second count panel or inline list.
 *
 * Everything here reads fields already on view.View. No wire change.
 */

/**
 * A seat's state as the table draws it. Two independent engine facts fold
 * into one value because a row can only carry one mark: `active` is whose
 * turn it is, `priority` is who the engine is actually waiting on, and
 * `acting` is both at once (the common case on your own main phase).
 * A seat that has lost is `lost` and nothing else — the turn order has
 * stopped meaning anything for it.
 */
export type SeatState = 'lost' | 'acting' | 'active' | 'priority' | 'idle';

/** One row of the rail's seat table: everything the row draws, resolved off the wire once. */
export interface SeatRow {
  seat: number;
  /** the table's name for the seat, falling back to the wire's own player name and then to "Seat N" — the resolution IdentityBar does, except that an EMPTY name counts as absent here: a blank cell in a table of four rows is worse than the placeholder */
  name: string;
  /** the deck, dropped when it would only repeat the name (the local fixture's seats do exactly that) */
  deck: string | null;
  colour: string;
  life: number;
  lost: boolean;
  /** hand SIZE — always on the wire even when the cards are not */
  hand: number;
  /** false when this viewer may not see the cards (a seat-scoped view of someone else's hand); the count is still true */
  handVisible: boolean;
  library: number;
  graveyard: number;
  exile: number;
  active: boolean;
  priority: boolean;
  state: SeatState;
  /** roster size: 0 on a constructed seat. The command zone itself is drawn on the board now (CommandArea), not from this row */
  commanders: number;
  /**
   * Why this seat is out, straight from the PlayerLost event's own Text
   * (commander damage, an empty-library draw, a concession, life at 0 or
   * less — never invented, and never "0 life": a player can lose with life
   * untouched, and forcing 0 would state something false). Null when the
   * seat has not lost, OR when it has but this client never saw the event
   * that said why (a spectator who joined after the loss, with no event
   * history behind the snapshot) — a known gap, not a guess papered over.
   */
  lostReason: string | null;
}

/**
 * lossCauses scans the transcript this client actually holds for
 * `player_lost` events and maps each seat to the cause text the engine gave
 * (rules/sba.go, rules/legal.go: "life total is 0 or less", "commander
 * damage (21 or more from one commander)", "drew from an empty library",
 * "conceded"). Player.Lost is monotone (events/apply.go), so at most one
 * such event exists per seat and the map never needs to pick a "latest".
 * Reads `event.text`/`event.player` off whatever EventBody shape the caller
 * holds (Rail is handed the DVR's own list) rather than importing the wire
 * Event type, so a lib test can pass a minimal fixture.
 */
export function lossCauses(events: { event: { kind: string; player: number; text?: string } }[]): Record<number, string> {
  const out: Record<number, string> = {};
  for (const e of events) {
    if (e.event.kind === 'player_lost' && e.event.text) out[e.event.player] = e.event.text;
  }
  return out;
}

/** seatStateOf folds the view's active/priority/lost facts into the one mark a row can carry. */
export function seatStateOf(view: View, p: PlayerView): SeatState {
  if (p.lost) return 'lost';
  const active = view.active === p.seat;
  const priority = view.priority === p.seat;
  if (active && priority) return 'acting';
  if (active) return 'active';
  if (priority) return 'priority';
  return 'idle';
}

/** stateLabel is the state as WORDS, for the row's accessible name. The table itself says it with a rule and a dot — the same vocabulary IdentityBar uses on the felt — so nothing is shouted twice. */
export function stateLabel(s: SeatState): string {
  switch (s) {
    case 'lost': return 'out of the game';
    case 'acting': return 'their turn, has priority';
    case 'active': return 'their turn';
    case 'priority': return 'has priority';
    default: return '';
  }
}

/** seatRows projects every seat of the view into a table row, in seat order. `causes` is this client's own lossCauses() map — optional so every existing caller (and every existing test) keeps working with every seat's lostReason simply null. */
export function seatRows(view: View, seats: SeatInfo[] = [], causes: Record<number, string> = {}): SeatRow[] {
  return (view.players ?? []).map((p) => {
    const counts = countsFor(p);
    const name = seats[p.seat]?.name || p.name || `Seat ${p.seat}`;
    const deck = seats[p.seat]?.deck;
    return {
      seat: p.seat,
      name,
      deck: deck && deck !== name ? deck : null,
      colour: seats[p.seat]?.colour ?? '',
      life: p.life,
      lost: p.lost,
      hand: counts.hand,
      handVisible: visibleHand(p) !== null,
      library: counts.library,
      graveyard: counts.graveyard,
      exile: counts.exile,
      active: view.active === p.seat,
      priority: view.priority === p.seat,
      state: seatStateOf(view, p),
      commanders: (p.commanders ?? []).length,
      lostReason: p.lost ? (causes[p.seat] ?? null) : null,
    };
  });
}

/**
 * focusSeat picks the seat whose focused rail details (currently floating
 * mana) are shown. Pile browsing is available directly from every summary row
 * and no longer depends on this focus.
 *
 * `selected` is the reader's explicit pick and always wins while it names a
 * seat that is still at the table. With no pick, the pane follows whoever is
 * most likely to be read: the viewer's own seat if this viewer has one,
 * otherwise the active player — which for an omniscient spectator (viewer 255,
 * view.NoSeat) is always the second branch.
 */
export function focusSeat(selected: number | null, view: View): number | null {
  const players = view.players ?? [];
  if (players.length === 0) return null;
  const at = (s: number) => players.some((p) => p.seat === s);
  if (selected !== null && at(selected)) return selected;
  if (at(view.viewer) && visibleHand(players.find((p) => p.seat === view.viewer)!) !== null) return view.viewer;
  if (at(view.active)) return view.active;
  return players[0].seat;
}
