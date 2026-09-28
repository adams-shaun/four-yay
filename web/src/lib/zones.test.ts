import { describe, expect, it } from 'vitest';
import { countsFor, zonesFor } from './zones';
import type { CardView, PlayerView } from '../protocol';

const card = (id: number, name: string): CardView => ({
  id, name, types: 'Creature', tapped: false, power: 0, toughness: 0, damage: 0, attacking: false,
  controller: 0, owner: 0, summon_sick: false, printing: { name }, token: `#${id}`,
});

// protocol.ts types PlayerView.graveyard/exile as CardView[] (non-nullable),
// but a Go nil slice serialises to JSON null — the same trap visibleHand in
// board.ts already documents — so the cast reproduces that real wire shape.
// size is the graveyard_size the wire claims; it may disagree with the
// array to prove the length is preferred where present. Exile has no
// `_size` field on the wire at all, so its fallback is 0.
const player = (gy: CardView[] | null, ex: CardView[] | null, size = 2): PlayerView => ({
  seat: 0, name: 'p', life: 20, lost: false, library_size: 30, hand_size: 7,
  graveyard_size: size,
  completed_dungeons: 0,
  hand: [], battlefield: [], graveyard: gy as CardView[], exile: ex as CardView[], pool: {}, command: [], commanders: [], commander_casts: [],
});

describe('zonesFor', () => {
  it('treats a wire-null graveyard/exile as an empty card list and falls back where a _size field exists', () => {
    const s = zonesFor(player(null, null, 5));
    expect(s.map((z) => z.zone)).toEqual(['graveyard', 'exile']);
    expect(s[0].cards).toEqual([]);
    expect(s[0].count).toBe(5); // graveyard_size fallback
    expect(s[1].cards).toEqual([]);
    expect(s[1].count).toBe(0); // exile has no _size field on the wire
  });

  it('returns both zones always, even at count 0, so a row layout does not jump', () => {
    const s = zonesFor(player([card(1, 'Bolt')], [], 1));
    expect(s.map((z) => z.zone)).toEqual(['graveyard', 'exile']);
    expect(s[1].cards).toEqual([]);
    expect(s[1].count).toBe(0);
    // the empty-everything seat still yields both rows
    expect(zonesFor(player([], [], 0)).map((z) => z.zone)).toEqual(['graveyard', 'exile']);
  });

  it('lists cards most-recently-added first: the last wire element is the top', () => {
    // zone order is the engine's append order (events.Move appends), so 3
    // arrived last and is the top of the graveyard.
    const gy = [card(1, 'A'), card(2, 'B'), card(3, 'C')];
    const ex = [card(4, 'D'), card(5, 'E')];
    const s = zonesFor(player(gy, ex, 3));
    expect(s[0].cards.map((c) => c.id)).toEqual([3, 2, 1]);
    expect(s[1].cards.map((c) => c.id)).toEqual([5, 4]);
  });

  it('count prefers the array length over the _size field where the array is present', () => {
    // graveyard_size says 7, but the array says 2: the array wins.
    expect(zonesFor(player([card(1, 'A'), card(2, 'B')], [], 7))[0].count).toBe(2);
    expect(zonesFor(player([], [], 7))[1].count).toBe(0);
  });
});

describe('countsFor', () => {
  it('reads library and hand from the _size fields, and the card-list zones from array length', () => {
    const c = countsFor(player([card(1, 'A'), card(2, 'B')], [card(3, 'C')], 9));
    expect(c.library).toBe(30);
    expect(c.hand).toBe(7);
    expect(c.graveyard).toBe(2); // array length, not graveyard_size 9
    expect(c.exile).toBe(1);
  });

  it('falls back to the _size field when a seat-scoped view redacts a zone to null', () => {
    const c = countsFor(player(null, null, 4));
    expect(c.graveyard).toBe(4);
    // exile has no `_size` field on the wire, so a null exile counts 0
    expect(c.exile).toBe(0);
  });
});
