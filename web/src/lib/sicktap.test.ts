import { describe, expect, it } from 'vitest';
import type { CardView } from '../protocol';
import { SICK_TAP_REASON, sickUntappableManaSource } from './sicktap';

// The predicate table mirrors rules/mana_activation.go tapFlagsSick's gate
// one row per condition (see sicktap.ts for the mirror comment). Each row
// is the Elvish-Mystic shape (fb-20260928T161557Z-2d23d432) with exactly one
// fact flipped, so a row that stops differing from the base case fails the
// setup loudly rather than passing vacuously.

const anyColour = { colour: [0, 0, 0, 0, 1, 0] as [number, number, number, number, number, number], any: false };

const card = (over: Partial<CardView> = {}): CardView => ({
  id: 16, name: 'Elvish Mystic', types: 'Creature', mana_cost: 'G',
  printing: { name: 'Elvish Mystic' }, token: '', tapped: false, power: 1, toughness: 1,
  damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: true,
  produces: anyColour,
  ...over,
});

const BASE = card();

describe('sickUntappableManaSource — the muted tap affordance predicate', () => {
  it('the base case is the reported shape: a sick, untapped, non-haste creature with a mana ability', () => {
    // preconditions: the base row really is sick, really untapped, really
    // has a mana ability, and really carries no haste — the row only means
    // something if every gate it clears is genuinely cleared.
    expect(BASE.summon_sick).toBe(true);
    expect(BASE.tapped).toBe(false);
    expect(BASE.produces).not.toBeNull();
    expect(BASE.keywords).toBeUndefined();
    expect(sickUntappableManaSource(BASE)).toBe(true);
  });

  it('a healthy (non-sick) creature with a mana ability gets nothing', () => {
    expect(sickUntappableManaSource(card({ summon_sick: false }))).toBe(false);
  });

  it('a sick creature with haste gets nothing (the engine\u2019s kwhHaste escape)', () => {
    // haste on the wire is DERIVED and capitalised ("Haste"); granted haste
    // must read exactly like printed haste.
    expect(sickUntappableManaSource(card({ keywords: ['Haste'] }))).toBe(false);
    expect(sickUntappableManaSource(card({ keywords: ['flying', 'Haste'] }))).toBe(false);
  });

  it('a parameterised keyword whose head is not haste never hides the glyph', () => {
    // the head-word split must not swallow a keyword that merely CONTAINS
    // the letters — and the setup differs from the haste row above.
    expect(sickUntappableManaSource(card({ keywords: ['protection from red'] }))).toBe(true);
  });

  it('a sick creature with NO mana ability (produces null) gets nothing — the dim and chip already cover it', () => {
    const noMana = card({ produces: null, name: 'Grizzly Bears' });
    expect(noMana.produces).toBeNull(); // setup: really no mana ability
    expect(sickUntappableManaSource(noMana)).toBe(false);
  });

  it('a tapped sick creature gets nothing — there is nothing left to tap', () => {
    expect(sickUntappableManaSource(card({ tapped: true }))).toBe(false);
  });

  it('a sick NONCREATURE with a mana ability (a freshly played land) gets nothing — fb-20260917T004545Z unchanged', () => {
    const land = card({ types: 'Land', name: 'Forest', mana_cost: '', power: 0, toughness: 0 });
    expect(land.types.includes('Creature')).toBe(false); // setup: really a noncreature
    expect(sickUntappableManaSource(land)).toBe(false);
  });

  it('the reason string names the sickness and the wait, for the tooltip and accessible name', () => {
    expect(SICK_TAP_REASON).toBe('summoning sick — untappable until your next turn');
  });
});
