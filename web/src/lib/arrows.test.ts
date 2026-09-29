import { describe, expect, it } from 'vitest';
import { arrowsFor, hoverArrowFor, previewArrowsFor } from './arrows';
import type { CardView, View } from '../protocol';
import type { CardOptions } from './cardoptions';

const card = (id: number, extra: Partial<CardView> = {}): CardView => ({ id, name: `c${id}`, types: 'Creature', tapped: false, power: 1, toughness: 1, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false, printing: { name: `c${id}` }, token: `#${id}`, ...extra });

describe('arrowsFor', () => {
  it('draws target, attack and block arrows in a fixed order', () => {
    const view = {
      players: [
        { seat: 0, battlefield: [card(1, { attacking: true, attacking_player: 1, blocked_by: [2] })], hand: null, graveyard: [], exile: [], pool: null },
        { seat: 1, battlefield: [card(2)], hand: null, graveyard: [], exile: [], pool: null },
      ],
      stack: [{ id: 9, kind: 'spell', name: 'Bolt', text: '', controller: 1, targets: [{ player: 0, is_player: true, label: 'any' }, { obj: 1, player: 0, is_player: false }] }],
      pending: [],
    } as unknown as View;
    expect(arrowsFor(view)).toEqual([
      { from: { obj: 9 }, to: { seat: 0 }, kind: 'target' },
      { from: { obj: 9 }, to: { obj: 1 }, kind: 'target' },
      { from: { obj: 1 }, to: { seat: 1 }, kind: 'attack' },
      { from: { obj: 2 }, to: { obj: 1 }, kind: 'block' },
    ]);
  });

  it('a counterspell on the stack targets the spell beneath it — the stack-to-stack pair the rail renders (fb-20260914T121642Z)', () => {
    // view.stack lists bottom first, so the bolt (890) sits BELOW the
    // counterspell (900) in the rail; the arrow runs from the upper tile to
    // the lower one. No new wire or target projection was needed for the
    // arrow: it was always computed — the defect was the overlay's clipped
    // host (pinned by Arrows.geometry.test.ts).
    const view = {
      players: [
        { seat: 0, battlefield: [], hand: null, graveyard: [], exile: [], pool: null },
        { seat: 1, battlefield: [], hand: null, graveyard: [], exile: [], pool: null },
      ],
      stack: [
        { id: 890, kind: 'spell', name: 'Lightning Bolt', text: '', controller: 1, targets: [] },
        { id: 900, kind: 'spell', name: 'Counterspell', text: '', controller: 0, targets: [{ obj: 890, player: 1, is_player: false }] },
      ],
      pending: [],
    } as unknown as View;
    expect(arrowsFor(view)).toEqual([
      { from: { obj: 900 }, to: { obj: 890 }, kind: 'target' },
    ]);
  });

  it('returns no arrows for an empty stack and no combat', () => {
    const view = {
      players: [
        { seat: 0, battlefield: [card(1)], hand: null, graveyard: [], exile: [], pool: null },
        { seat: 1, battlefield: [card(2)], hand: null, graveyard: [], exile: [], pool: null },
      ],
      stack: [],
      pending: [],
    } as unknown as View;
    expect(arrowsFor(view)).toEqual([]);
  });

  it('emits one target arrow per target on a spell with several targets, players and objects mixed', () => {
    const view = {
      players: [
        { seat: 0, battlefield: [card(1)], hand: null, graveyard: [], exile: [], pool: null },
        { seat: 1, battlefield: [card(2)], hand: null, graveyard: [], exile: [], pool: null },
      ],
      stack: [{
        id: 9, kind: 'spell', name: 'Chain Lightning', text: '', controller: 0,
        targets: [
          { player: 1, is_player: true, label: 'any' },
          { obj: 1, player: 0, is_player: false },
          { obj: 2, player: 1, is_player: false },
        ],
      }],
      pending: [],
    } as unknown as View;
    expect(arrowsFor(view)).toEqual([
      { from: { obj: 9 }, to: { seat: 1 }, kind: 'target' },
      { from: { obj: 9 }, to: { obj: 1 }, kind: 'target' },
      { from: { obj: 9 }, to: { obj: 2 }, kind: 'target' },
    ]);
  });

  it('emits one block arrow per blocker when an attacker has several', () => {
    const view = {
      players: [
        { seat: 0, battlefield: [card(1, { attacking: true, attacking_player: 1, blocked_by: [2, 3, 4] })], hand: null, graveyard: [], exile: [], pool: null },
        { seat: 1, battlefield: [card(2), card(3), card(4)], hand: null, graveyard: [], exile: [], pool: null },
      ],
      stack: [],
      pending: [],
    } as unknown as View;
    expect(arrowsFor(view)).toEqual([
      { from: { obj: 1 }, to: { seat: 1 }, kind: 'attack' },
      { from: { obj: 2 }, to: { obj: 1 }, kind: 'block' },
      { from: { obj: 3 }, to: { obj: 1 }, kind: 'block' },
      { from: { obj: 4 }, to: { obj: 1 }, kind: 'block' },
    ]);
  });

  it('emits no attack or block arrows when there is no combat', () => {
    const view = {
      players: [
        { seat: 0, battlefield: [card(1)], hand: null, graveyard: [], exile: [], pool: null },
        { seat: 1, battlefield: [card(2)], hand: null, graveyard: [], exile: [], pool: null },
      ],
      stack: [{ id: 9, kind: 'spell', name: 'Bolt', text: '', controller: 0, targets: [{ obj: 2, player: 1, is_player: false }] }],
      pending: [],
    } as unknown as View;
    expect(arrowsFor(view)).toEqual([
      { from: { obj: 9 }, to: { obj: 2 }, kind: 'target' },
    ]);
  });
});

describe('previewArrowsFor', () => {
  const options = (source?: number): CardOptions => ({
    source,
    byObj: new Map([
      [9, [{ index: 4, kind: 'permanent', label: 'Self', obj: 9, player: 0 }]],
      [12, [{ index: 8, kind: 'permanent', label: 'Bear', obj: 12, player: 1 }]],
    ]),
    byPlayer: new Map([
      [1, [{ index: 15, kind: 'player', label: 'Ari', player: 1 }]],
    ]),
    picked: [], tone: 'initiative', post: () => {},
  });

  it('emits object and player candidate arrows from a named decision source', () => {
    expect(previewArrowsFor(options(9))).toEqual([
      { from: { obj: 9 }, to: { obj: 12 }, kind: 'target-preview' },
      { from: { obj: 9 }, to: { seat: 1 }, kind: 'target-preview' },
    ]);
  });

  it('emits none without a source', () => {
    expect(previewArrowsFor(options())).toEqual([]);
  });

  it('skips the source object itself instead of making a zero-length arrow', () => {
    const onlySelf = options(9);
    onlySelf.byObj = new Map([[9, onlySelf.byObj.get(9)!]]);
    onlySelf.byPlayer = new Map();
    expect(previewArrowsFor(onlySelf)).toEqual([]);
  });
});

describe('hoverArrowFor', () => {
  const opts = { source: 7, byObj: new Map(), byPlayer: new Map(), picked: [], tone: 'initiative', post: () => {} } as unknown as CardOptions;
  it('draws from the decision source to the hovered end, or from the option\'s own start', () => {
    expect(hoverArrowFor(opts, { from: null, to: { obj: 9 } })).toEqual({ from: { obj: 7 }, to: { obj: 9 }, kind: 'target-hover' });
    expect(hoverArrowFor(null, { from: { obj: 3 }, to: { seat: 1 } })).toEqual({ from: { obj: 3 }, to: { seat: 1 }, kind: 'target-hover' });
  });
  it('draws nothing with no hover or no start', () => {
    expect(hoverArrowFor(opts, null)).toBeNull();
    expect(hoverArrowFor(null, { from: null, to: { obj: 9 } })).toBeNull();
  });
});
