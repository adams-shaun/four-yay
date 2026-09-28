import { describe, expect, it } from 'vitest';
import type { CardView, PlayerView, SeatInfo, View } from '../protocol';
import { focusSeat, lossCauses, seatCorner, seatRows, seatStateOf, stateLabel } from './seattable';

const card = (id: number, name: string): CardView => ({
  id, name, types: 'Legendary Creature', tapped: false, power: 0, toughness: 0, damage: 0,
  attacking: false, controller: 0, owner: 0, summon_sick: false, printing: { name }, token: `#${id}`,
});

const player = (over: Partial<PlayerView> = {}): PlayerView => ({
  seat: 0, name: 'P0', life: 40, lost: false, library_size: 93, hand_size: 7, graveyard_size: 0,
  completed_dungeons: 0,
  hand: [], battlefield: [], graveyard: [], exile: [], pool: {},
  command: [], commanders: [], commander_casts: [], ...over,
});

const view = (over: Partial<View> = {}): View => ({
  viewer: 255, visibility: 'omniscient', turn: 4, round: 4, step: 'main1', phase: 'main1', active: 1, priority: 1,
  over: false, draw: false, winner: null, stack: [], pending: [],
  players: [player({ seat: 0, name: 'P0' }), player({ seat: 1, name: 'P1' })], ...over,
});

const seats: SeatInfo[] = [
  { name: 'Ari', deck: 'mono-red', colour: '#e5484d' },
  { name: 'Bo', deck: 'Bo', colour: '#22c55e' },
];

describe('seatStateOf', () => {
  it('active and priority together are one state, not two marks fighting for the same row', () => {
    const v = view({ active: 0, priority: 0 });
    expect(seatStateOf(v, v.players[0])).toBe('acting');
    expect(seatStateOf(v, v.players[1])).toBe('idle');
  });

  it('the turn and the wait are separable — the active seat can be waiting on someone else', () => {
    const v = view({ active: 0, priority: 1 });
    expect(seatStateOf(v, v.players[0])).toBe('active');
    expect(seatStateOf(v, v.players[1])).toBe('priority');
  });

  it('a seat that has lost reads as lost whatever the turn order says', () => {
    const v = view({ active: 0, priority: 0, players: [player({ seat: 0, lost: true }), player({ seat: 1 })] });
    expect(seatStateOf(v, v.players[0])).toBe('lost');
  });

  it('every state has a word for the accessible name, and idle has none to say', () => {
    expect(stateLabel('acting')).toBe('their turn, has priority');
    expect(stateLabel('active')).toBe('their turn');
    expect(stateLabel('priority')).toBe('has priority');
    expect(stateLabel('lost')).toBe('out of the game');
    expect(stateLabel('idle')).toBe('');
  });
});

describe('seatRows', () => {
  it('projects one row per seat in seat order with the counts the columns draw', () => {
    const v = view({
      players: [
        player({ seat: 0, life: 37, hand_size: 6, library_size: 88, graveyard: [card(1, 'Bolt'), card(2, 'Shock')], exile: [card(3, 'Path')] }),
        player({ seat: 1, life: 40 }),
      ],
    });
    const rows = seatRows(v, seats);
    expect(rows.map((r) => r.seat)).toEqual([0, 1]);
    expect(rows[0]).toMatchObject({ name: 'Ari', deck: 'mono-red', colour: '#e5484d', life: 37, hand: 6, library: 88, graveyard: 2, exile: 1 });
    expect(rows[1]).toMatchObject({ name: 'Bo', life: 40, hand: 7, graveyard: 0, exile: 0 });
  });

  it('the deck is dropped when it would only repeat the seat name', () => {
    expect(seatRows(view(), seats)[1].deck).toBeNull();
  });

  it('falls back to the wire name, then to Seat N — an empty name counts as absent, never as a blank cell', () => {
    const v = view({ players: [player({ seat: 0, name: 'wire-name' }), player({ seat: 1, name: '' })] });
    const rows = seatRows(v, []);
    expect(rows[0].name).toBe('wire-name');
    expect(rows[1].name).toBe('Seat 1');
  });

  it('a hidden hand keeps its true size and is marked not visible — the count is public, the cards are not', () => {
    // A Go nil slice serialises to JSON null; that is what a seat-scoped view
    // sends for someone else's hand (board.ts visibleHand).
    const v = view({ players: [player({ seat: 0, hand: null as unknown as CardView[], hand_size: 5 })] });
    const [row] = seatRows(v, []);
    expect(row.hand).toBe(5);
    expect(row.handVisible).toBe(false);
  });

  it('carries the roster size so a constructed row never asks for a command zone', () => {
    const v = view({ players: [player({ seat: 0, commanders: [card(9, 'Isamaru')] }), player({ seat: 1 })] });
    const rows = seatRows(v, []);
    expect(rows[0].commanders).toBe(1);
    expect(rows[1].commanders).toBe(0);
  });

  it('reads a null graveyard through the _size fallback rather than counting an absent array', () => {
    const v = view({ players: [player({ seat: 0, graveyard: null as unknown as CardView[], graveyard_size: 4 })] });
    expect(seatRows(v, [])[0].graveyard).toBe(4);
  });

  it('a live seat carries no elimination cause, whatever the events say', () => {
    const v = view({ players: [player({ seat: 0, lost: false })] });
    const rows = seatRows(v, [], { 0: 'life total is 0 or less' });
    expect(rows[0].lostReason).toBeNull();
  });

  it('a lost seat surfaces the cause this client actually saw — never a fake "0 life"', () => {
    const v = view({ players: [player({ seat: 0, life: 39, lost: true })] });
    const rows = seatRows(v, [], { 0: 'commander damage (21 or more from one commander)' });
    expect(rows[0].life).toBe(39); // the true, untouched life total — not forced to 0
    expect(rows[0].lostReason).toBe('commander damage (21 or more from one commander)');
  });

  it('a lost seat with no matching event in this client\'s history reads lost with no invented cause', () => {
    const v = view({ players: [player({ seat: 0, lost: true })] });
    expect(seatRows(v, [])[0].lostReason).toBeNull();
  });
});

describe('lossCauses', () => {
  it('maps a seat to the PlayerLost event\'s own Text', () => {
    const events = [
      { event: { kind: 'life_change', player: 0, text: undefined } },
      { event: { kind: 'player_lost', player: 2, text: 'drew from an empty library' } },
    ];
    expect(lossCauses(events)).toEqual({ 2: 'drew from an empty library' });
  });

  it('a player_lost event with no text (a malformed or fuzz event) is not recorded as a cause', () => {
    const events = [{ event: { kind: 'player_lost', player: 1, text: '' } }];
    expect(lossCauses(events)).toEqual({});
  });

  it('an empty transcript yields no causes at all', () => {
    expect(lossCauses([])).toEqual({});
  });
});

describe('focusSeat', () => {
  it('an explicit pick wins', () => {
    expect(focusSeat(1, view())).toBe(1);
  });

  it('a pick naming a seat that is not at this table is ignored, not rendered as a blank pane', () => {
    expect(focusSeat(7, view())).toBe(1); // falls through to the active player
  });

  it('an omniscient spectator (viewer 255) follows the active player', () => {
    expect(focusSeat(null, view({ active: 1 }))).toBe(1);
    expect(focusSeat(null, view({ active: 0 }))).toBe(0);
  });

  it('a seated viewer defaults to their own seat, not to whoever is acting', () => {
    const v = view({
      viewer: 0, visibility: 'seat', active: 1, priority: 1,
      players: [player({ seat: 0 }), player({ seat: 1, hand: null as unknown as CardView[] })],
    });
    expect(focusSeat(null, v)).toBe(0);
  });

  it('a viewer id that is a seat but whose hand is redacted falls through to the active player', () => {
    const v = view({
      viewer: 0, visibility: 'public', active: 1,
      players: [player({ seat: 0, hand: null as unknown as CardView[] }), player({ seat: 1, hand: null as unknown as CardView[] })],
    });
    expect(focusSeat(null, v)).toBe(1);
  });

  it('an active seat that is not in the player list still resolves to a real seat', () => {
    expect(focusSeat(null, view({ active: 9 }))).toBe(0);
  });

  it('a view with no players has no seat to focus', () => {
    expect(focusSeat(null, view({ players: [] }))).toBeNull();
  });
});

describe('seatCorner — the seat → position mapping (Task ui17)', () => {
  it('a 1v1 viewed by seat 0 puts the viewer at the bottom and the opponent on top', () => {
    expect(seatCorner(0, 2, 0)).toBe('bottom');
    expect(seatCorner(1, 2, 0)).toBe('top');
  });

  it('a 1v1 viewed by seat 1 puts seat 1 at the bottom and seat 0 on top — relative, not absolute', () => {
    expect(seatCorner(1, 2, 1)).toBe('bottom');
    expect(seatCorner(0, 2, 1)).toBe('top');
  });

  it('a 1v1 spectator (NoSeat, or any viewer id not at the table) is deterministic: seat 0 at the bottom', () => {
    // NoSeat is 255
    expect(seatCorner(0, 2, 255)).toBe('bottom');
    expect(seatCorner(1, 2, 255)).toBe('top');
    // a viewer id that names no seat at this table behaves the same
    expect(seatCorner(0, 2, 9)).toBe('bottom');
    expect(seatCorner(1, 2, 9)).toBe('top');
  });

  // Task ui22 retitled this and dropped one line. The old title claimed the
  // 3/4-seat layout does "NOT depend on the viewer", and its last assertion
  // passed a REAL SEATED viewer (seats 3, viewer 2) and required the
  // un-rotated corners -- so that assertion did not state a rule, it pinned
  // the deferral recorded in seatCorner's old doc comment. What survives is
  // the part that is still true and still worth guarding: the SPECTATOR
  // layout, and the seat-0 viewer that coincides with it, are exactly the
  // historic clockwise arrangement and must not drift. A seated viewer's
  // layout is asserted below by property (bottom corner + bijection), which
  // covers seats 3 viewer 2 more strongly than the deleted line did.
  it('the spectator 3- and 4-player layout is the historic clockwise one', () => {
    expect([0, 1, 2, 3].map((s) => seatCorner(s, 4, 0))).toEqual(['bl', 'tl', 'tr', 'br']);
    expect([0, 1, 2, 3].map((s) => seatCorner(s, 4, 255))).toEqual(['bl', 'tl', 'tr', 'br']);
    expect([0, 1, 2].map((s) => seatCorner(s, 3, 0))).toEqual(['bl', 'tl', 'tr']);
    expect([0, 1, 2].map((s) => seatCorner(s, 3, 255))).toEqual(['bl', 'tl', 'tr']);
  });
});

// Task ui22: 3 and 4 seats are now re-anchored to the viewer the same way 1v1
// always was — rotate the bottom-left-then-clockwise cycle so the seated
// viewer lands at `bl`. Assert the two properties that matter, not a list of
// hard-coded corners: (1) a seated viewer's own seat is always at the bottom
// corner, and (2) the mapping is a bijection (no two seats share a corner,
// which is exactly the property a rotation bug breaks).
describe('seatCorner — 3/4 seats anchor to the viewer (Task ui22)', () => {
  const cycle = ['bl', 'tl', 'tr', 'br'] as const;

  it('a seated viewer always lands at the bottom corner, and the mapping is a bijection, for every (seats, viewer)', () => {
    for (const seats of [2, 3, 4]) {
      // the corner set this seat count must exactly tile: bottom/top for 1v1,
      // the leading `seats` entries of the clockwise bl/tl/tr/br cycle otherwise.
      const expected = new Set(seats === 2 ? ['bottom', 'top'] : cycle.slice(0, seats));
      const bottom = seats === 2 ? 'bottom' : 'bl';

      // every seated viewer
      for (let viewer = 0; viewer < seats; viewer++) {
        const corners = Array.from({ length: seats }, (_, seat) => seatCorner(seat, seats, viewer));
        // property 1: the viewer's own seat is the bottom corner.
        expect(corners[viewer], `seats=${seats} viewer=${viewer}: viewer should be ${bottom}`).toBe(bottom);
        // property 2: a bijection — every corner used exactly once, no two seats sharing.
        expect(new Set(corners).size, `seats=${seats} viewer=${viewer}: corners ${corners} not a bijection`).toBe(seats);
        expect(new Set(corners), `seats=${seats} viewer=${viewer}: corners ${corners} != ${[...expected]}`).toEqual(expected);
      }

      // the spectator case must not move: seat 0 bottom, then clockwise.
      for (const viewer of [255, 9]) {
        expect(Array.from({ length: seats }, (_, seat) => seatCorner(seat, seats, viewer)), `seats=${seats} spectator ${viewer}`)
          .toEqual([...expected]);
      }
    }
  });
});
