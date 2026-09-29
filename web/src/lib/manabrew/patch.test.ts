import { describe, expect, it } from 'vitest';
import { applyPatch, PatchError } from './patch';
import { allRecords, stateOf } from './testdata/fixture';

describe('applyPatch', () => {
  const gv = stateOf(allRecords[0]);

  it('merges plain fields, replaces with $v, and treats null as a value', () => {
    const next = applyPatch(gv, { turn: 5, step: { $v: 'main1' }, winnerId: null });
    expect(next.turn).toBe(5);
    expect(next.step).toBe('main1');
    expect(next.winnerId).toBeNull();
    expect(gv.turn).toBe(0); // not mutated
  });

  it('edits keyed arrays: players by id, zones by zone/owner, cards by id', () => {
    const hand = gv.zones.find((z) => z.zone === 'hand' && z.ownerId === 'player-0')!;
    const first = hand.cards[0];
    const next = applyPatch(gv, {
      players: { $k: { 'player-1': { life: 17 } } },
      zones: { $k: { 'hand/player-0': { count: hand.count - 1, cards: { $k: { [first.id]: { $d: true } } } } } },
    });
    expect(next.players.find((p) => p.id === 'player-1')!.life).toBe(17);
    const h2 = next.zones.find((z) => z.zone === 'hand' && z.ownerId === 'player-0')!;
    expect(h2.count).toBe(hand.count - 1);
    expect(h2.cards.map((c) => c.id)).toEqual(hand.cards.slice(1).map((c) => c.id));
  });

  it('adds a new keyed element and reorders with $o', () => {
    const next = applyPatch(gv, { players: { $k: { 'player-9': { $v: { id: 'player-9', name: 'X', life: 1 } } }, $o: ['player-9', 'player-1', 'player-0'] } });
    expect(next.players.map((p) => p.id)).toEqual(['player-9', 'player-1', 'player-0']);
  });

  it('refuses what it cannot apply', () => {
    expect(() => applyPatch(gv, 3)).toThrow(PatchError);
    expect(() => applyPatch(gv, { turn: { $k: {} } })).toThrow(PatchError);
    expect(() => applyPatch(gv, { players: { $o: ['nobody'] } })).toThrow(PatchError);
  });
});
