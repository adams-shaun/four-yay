import { describe, expect, it } from 'vitest';
import type { CardView } from '../protocol';
import { orderGroups, regionRows, slotUnits } from './boardregions';
import { defaultProfile, withPreset } from './layoutprofile';

function card(id: number, name: string, types: string, extra: Partial<CardView> = {}): CardView {
  return {
    id, name, types, printing: { name }, token: '', tapped: false, power: 0, toughness: 0, damage: 0,
    attacking: false, controller: 0, owner: 0, summon_sick: false, ...extra,
  } as CardView;
}

const bf = [
  card(30, 'Zombie', 'Creature — Zombie', { power: 2 }),
  card(10, 'Swamp', 'Basic Land — Swamp', { tapped: true }),
  card(11, 'Swamp', 'Basic Land — Swamp'),
  card(12, 'Island', 'Basic Land — Island'),
  card(20, 'Angel', 'Creature — Angel', { power: 4 }),
  card(40, 'Sol Ring', 'Artifact'),
  card(41, 'Sword', 'Artifact — Equipment', { attached_to: 30 }),
];

describe('regionRows — the board template', () => {
  it('Duel: creatures on the row nearest the centre, lands and others sharing the next row', () => {
    const rows = regionRows(bf, defaultProfile(), { stacking: true });
    expect(rows.map((r) => r.map((s) => s.key))).toEqual([['creatures'], ['lands', 'others']]);
    // lands by NAME (Island before Swamp); the Swamp pile merges tapped + untapped (lands rule)
    expect(rows[1][0].groups.map((g) => [g.cards[0].name, g.cards.length])).toEqual([['Island', 1], ['Swamp', 2]]);
    // the equipment rides under its host, never in a region of its own
    expect(rows[1][1].groups.map((g) => g.cards[0].name)).toEqual(['Sol Ring']);
  });

  it('entry order is the wire order, not the id', () => {
    const rows = regionRows(bf, defaultProfile(), { stacking: true });
    expect(rows[0][0].groups.map((g) => g.cards[0].name)).toEqual(['Zombie', 'Angel']);
  });

  it('power order is highest first, ties by entry', () => {
    const p = defaultProfile();
    p.regions.creatures.order = 'power';
    const extra = [...bf, card(50, 'Bear', 'Creature — Bear', { power: 2 })];
    expect(regionRows(extra, p, { stacking: true })[0][0].groups.map((g) => g.cards[0].name)).toEqual(['Angel', 'Zombie', 'Bear']);
  });

  it('stacking off gives every permanent its own tile', () => {
    const rows = regionRows(bf, defaultProfile(), { stacking: false });
    expect(rows[1][0].groups.map((g) => g.cards.length)).toEqual([1, 1, 1]);
  });

  it('Duel, 3 rows: creatures, others, lands top to bottom', () => {
    const rows = regionRows(bf, withPreset(defaultProfile(), 'duel3'), { stacking: true });
    expect(rows.map((r) => r.map((s) => s.key))).toEqual([['creatures'], ['others'], ['lands']]);
  });

  it('a side strip is the creatures alone, and an empty region is still a slot', () => {
    expect(regionRows(bf, defaultProfile(), { stacking: true, creaturesOnly: true }).map((r) => r.map((s) => s.key))).toEqual([['creatures']]);
    const rows = regionRows([], defaultProfile(), { stacking: true });
    expect(rows.map((r) => r.length)).toEqual([1, 2]);
  });

  it('a tapped tile counts wider than an untapped one', () => {
    const rows = regionRows([card(1, 'A', 'Creature', { tapped: true }), card(2, 'B', 'Creature')], defaultProfile(), { stacking: true });
    expect(slotUnits(rows[0][0])).toBeCloseTo(1 + 88 / 63);
  });

  it('orderGroups never reshuffles equal keys', () => {
    const entry = new Map([[5, 0], [3, 1], [9, 2]]);
    const g = (id: number) => ({ key: String(id), render: 'r' + id, cards: [card(id, 'Same', 'Land')] });
    expect(orderGroups([g(9), g(3), g(5)], 'name', entry).map((x) => x.cards[0].id)).toEqual([5, 3, 9]);
  });
});
