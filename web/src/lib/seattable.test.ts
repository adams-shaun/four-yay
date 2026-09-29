import { describe, expect, it } from 'vitest';
import type { CardView, PlayerView, SeatInfo, View } from '../protocol';
import { focusSeat, lossCauses, seatRows, seatStateOf, stateLabel } from './seattable';

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

// seatCorner is gone: seat placement is lib/tablearrange's arrangeTable (tablearrange.test.ts).
