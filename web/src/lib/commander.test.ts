import { describe, expect, it } from 'vitest';
import type { CardView, PlayerView } from '../protocol';
import {
  CRIT_CMD_DAMAGE, LETHAL_CMD_DAMAGE, TAX_PER_CAST,
  commandZoneOf, commanderDamageOf, nextCastCost, taxOf,
} from './commander';

const card = (id: number, name: string, manaCost?: string): CardView => ({
  id, name, types: 'Legendary Creature', mana_cost: manaCost,
  tapped: false, power: 0, toughness: 0, damage: 0, attacking: false,
  controller: 0, owner: 0, summon_sick: false, printing: { name }, token: `#${id}`,
});

/** players builds a seat's whole wire shape; optional zone lists and roster land where the caller put them. */
function players(...ps: Partial<PlayerView>[]): PlayerView[] {
  return ps.map((p, i) => ({
    seat: p.seat ?? i, name: p.name ?? `P${i}`, life: 20, lost: false,
    library_size: 30, hand_size: 7, graveyard_size: 0,
    completed_dungeons: 0,
    hand: p.hand ?? [], battlefield: p.battlefield ?? [], graveyard: p.graveyard ?? [],
    exile: p.exile ?? [], pool: {}, command: p.command ?? [], commanders: p.commanders ?? [],
    commander_casts: p.commander_casts ?? [], cmd_damage: p.cmd_damage,
  }));
}

describe('taxOf — the CR 903.8 arithmetic', () => {
  it('casts 0 pays no tax; each prior cast adds exactly {2}', () => {
    expect(taxOf(0)).toBe(0);
    expect(taxOf(1)).toBe(TAX_PER_CAST);
    expect(taxOf(2)).toBe(2 * TAX_PER_CAST);
    expect(taxOf(3)).toBe(6);
    expect(taxOf(5)).toBe(10);
  });
});

describe('nextCastCost — the cost the reader decides with', () => {
  it('zero casts returns the printed cost untouched', () => {
    expect(nextCastCost('1 W', 0)).toBe('1 W');
    expect(nextCastCost(undefined, 0)).toBe('');
  });
  it('the tax is an explicit addition on top of the printed cost, never folded away', () => {
    expect(nextCastCost('1 W', 2)).toBe('1 W 4');
    expect(nextCastCost('W W', 1)).toBe('W W 2');
  });
  it('the derived number is 2 × casts, not the raw count', () => {
    // casts = 3, tax must read 6 — the raw "3" would be the wrong number.
    expect(nextCastCost('1 W', 3)).toBe('1 W 6');
  });
});

describe('commandZoneOf — the rail command zone', () => {
  it('a commander sitting in the command zone is inZone; one on the battlefield is not', () => {
    const [cmd, soldier] = [card(1, 'Isamaru'), card(2, 'Soldier')];
    const [p] = players({
      commanders: [cmd, soldier], commander_casts: [2, 0],
      command: [cmd], battlefield: [soldier],
    });
    const c = commandZoneOf(p);
    expect(c).toHaveLength(2);
    expect(c[0].inZone).toBe(true);
    expect(c[0].zone).toBe('command');
    expect(c[1].inZone).toBe(false);
    expect(c[1].zone).toBe('battlefield');
  });

  it('each commander carries its own CR 903.8 tax — the derived number, not the count', () => {
    const [p] = players({ commanders: [card(1, 'Isamaru'), card(2, 'Zur')], commander_casts: [2, 5], command: [card(1, 'Isamaru'), card(2, 'Zur')] });
    const c = commandZoneOf(p);
    expect(c.map((x) => x.tax)).toEqual([4, 10]);
    expect(c.map((x) => x.casts)).toEqual([2, 5]);
  });

  it('a commander in the graveyard, exile or on the stack resolves to its own zone', () => {
    const [graveyard, exile, onStack, hidden] = [card(1, 'GY'), card(2, 'EX'), card(3, 'ST'), card(4, 'LIB')];
    const [p] = players({ commanders: [graveyard, exile, onStack, hidden], graveyard: [graveyard], exile: [exile] });
    const c = commandZoneOf(p, new Set([onStack.id]));
    expect(c.map((x) => x.zone)).toEqual(['graveyard', 'exile', 'stack', 'library']);
    expect(c.every((x) => !x.inZone)).toBe(true);
  });

  it('the zone is decided by the wire lists, never by label text', () => {
    // The battlefield fixture carries the same names a label-based matcher
    // could trip on; the state is decided purely by id membership.
    const [cmd] = [card(1, 'Isamaru')];
    const [p] = players({ commanders: [cmd], command: [cmd] });
    expect(commandZoneOf(p)[0].zone).toBe('command');
    const [p2] = players({ commanders: [cmd], battlefield: [cmd] });
    expect(commandZoneOf(p2)[0].zone).toBe('battlefield');
    const [p3] = players({ commanders: [cmd] });
    expect(commandZoneOf(p3)[0].zone).toBe('library');
  });

  it('an empty roster is an empty list — a Constructed game renders "no commanders", not a bug', () => {
    const [p] = players({});
    expect(commandZoneOf(p)).toEqual([]);
  });
});

describe('commanderDamageOf — the CR 903.10 clock', () => {
  // dealer seat 1 and seat 2's commanders are what the taker's wire keys by.
  const taker = (damage: Record<string, number> | undefined): PlayerView => players({
    seat: 0, name: 'Taker', cmd_damage: damage,
  })[0];
  const table = [
    players({ seat: 1, commanders: [card(10, 'Isamaru')] })[0],
    players({ seat: 2, commanders: [card(11, 'Zur')] })[0],
  ];

  // The wire values are pinned literally so a mutation of the constant is
  // caught: whatever LETHAL_CMD_DAMAGE reads, a real 20 must be survivable
  // and a real 21 must not be (CR 903.10).
  it('21 from any single commander is lethal, 20 is not', () => {
    const low = commanderDamageOf(taker({ '10': 20 }), table);
    expect(low[0].lethal).toBe(false);
    expect(low[0].crit).toBe(true);
    const lethal = commanderDamageOf(taker({ '10': 21 }), table);
    expect(lethal[0].lethal).toBe(true);
    const over = commanderDamageOf(taker({ '10': 25 }), table);
    expect(over[0].lethal).toBe(true);
    expect(LETHAL_CMD_DAMAGE).toBe(21);
  });

  it('19 is the danger line: two or fewer points from lethal', () => {
    const at = commanderDamageOf(taker({ '10': 19 }), table);
    expect(at[0].crit).toBe(true);
    expect(at[0].lethal).toBe(false);
    const calm = commanderDamageOf(taker({ '10': 18 }), table);
    expect(calm[0].crit).toBe(false);
    expect(CRIT_CMD_DAMAGE).toBe(19);
  });

  it('commander damage is per commander — 11 from two commanders is two entries, never one 22', () => {
    const d = commanderDamageOf(taker({ '10': 11, '11': 11 }), table);
    expect(d).toHaveLength(2);
    const amounts = d.map((x) => x.amount);
    // each commander keeps its own 11; no entry ever presents the summed 22
    // that would misread as lethal (the classic wrong implementation).
    expect(amounts).toEqual([11, 11]);
    expect(amounts).not.toContain(22);
    expect(d[0].lethal).toBe(false);
    expect(d[1].lethal).toBe(false);
  });

  it('21 from ONE commander is a single entry at 21 and lethal — even when a different commander also dealt 11', () => {
    const d = commanderDamageOf(taker({ '10': LETHAL_CMD_DAMAGE, '11': 11 }), table);
    expect(d).toHaveLength(2);
    expect(d.map((x) => x.lethal)).toEqual([true, false]);
  });

  it('resolves the dealer name and seat from the match-wide rosters, and sorts by dealer seat', () => {
    const d = commanderDamageOf(taker({ '10': 5, '11': 9 }), table);
    expect(d[0]).toMatchObject({ name: 'Isamaru', fromSeat: 1, amount: 5 });
    expect(d[1]).toMatchObject({ name: 'Zur', fromSeat: 2, amount: 9 });
  });

  it('no damage on the wire means no clock', () => {
    expect(commanderDamageOf(taker(undefined), table)).toEqual([]);
    expect(commanderDamageOf(taker({}), table)).toEqual([]);
  });
});
