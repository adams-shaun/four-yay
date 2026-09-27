import { describe, expect, it } from 'vitest';
import { castableAfterTap, castablesAfterTap, offersPotential, respondableAfterTap } from './castable';
import type { Option, PlayerView, PotentialAction, View } from '../protocol';

/**
 * castable.test.ts pins the projection question the auto-pass logic asks
 * (lib/castable): the client no longer prices anything — the wire carries
 * PlayerView.potential_actions, the SERVER's own legal-offer walk priced
 * against the hypothetical tapped-out pool (rules.PotentialActions /
 * rules.PotentialMana). These tests pin what the client does with that
 * projection, including the shapes that motivated the rv2c rework: the client
 * could not price live cost modifiers, command-zone or flashback casts,
 * {T}-less activations, indeterminate sources or X spells, and every one of
 * those shapes arrives as a server-computed entry now.
 */

/** pot builds one PotentialAction. */
const pot = (kind: string, obj = 7): PotentialAction => ({ kind, obj, label: 'Cast Card' });

/** player builds one minimal PlayerView for seat 0. */
const player = (over: Partial<PlayerView>): PlayerView =>
  ({
    seat: 0,
    name: 'p0',
    life: 20,
    lost: false,
    library_size: 0,
    hand_size: 0,
    graveyard_size: 0,
    hand: [],
    battlefield: [],
    graveyard: [],
    exile: [],
    pool: {},
    command: [],
    commanders: [],
    commander_casts: [],
    ...over,
  }) as PlayerView;

/** view builds a View whose seat-0 player is `p` (and optionally a second seat — never carrying a projection). */
const view = (p: PlayerView, secondSeat = true): View =>
  ({
    viewer: 0,
    active: 0,
    step: 'main1',
    players: secondSeat ? [p, { seat: 1 } as PlayerView] : [p],
  }) as unknown as View;

describe('castableAfterTap — reading the server projection', () => {
  it('a mana-only window the engine would unlock (the Jitte/Medallion/Tron class) stops: potential_actions non-empty', () => {
    // Any of the eight rv2c shapes — a RaiseCost-raised cast the pool can no
    // longer pay, a ReduceCost-reduced cast it now can, a {T}-less Equip, an
    // X spell at 0, an indeterminate Tron source, a command-zone or flashback
    // cast — projects as whatever real plays the engine would offer. The
    // client reads the list, not the cost.
    const p = player({ potential_actions: [pot('cast', 57)] });
    expect(castableAfterTap(view(p), 0)).toBe(true);
    expect(castableAfterTap(view(p), 0)).toBe(true); // and again deterministically
  });

  it('an empty or absent projection passes: the engine would offer nothing', () => {
    const empty = player({ potential_actions: [] });
    expect(castableAfterTap(view(empty), 0)).toBe(false);
    const absent = player({});
    expect(castableAfterTap(view(absent), 0)).toBe(false);
  });

  it('a mana tap is never an action: the projection carries only real plays', () => {
    // The server never projects "activate" — but if a malformed projection
    // carried nothing but one, the client must still read the window empty.
    const tapsOnly = player({ potential_actions: [{ kind: 'activate', obj: 9, label: 'Tap for mana' } as PotentialAction] });
    expect(castableAfterTap(view(tapsOnly), 0)).toBe(false);
  });

  it('fails closed without the seat\u2019s own projection (a hidden-zone read is impossible client-side)', () => {
    // No player record for the seat at all.
    expect(castableAfterTap(view(player({})), 3)).toBe(false);
    // Another seat's PlayerView: the projection is the viewer's own seat's
    // only (view/view.go gates on p.ID == viewer), so seat 1 in this view has
    // none — even though the seat-0 player record carries one.
    expect(castableAfterTap(view(player({ potential_actions: [pot('cast')] })), 1)).toBe(false);
  });

  it('the projection is the whole decision: no hand, pool or availability field is consulted', () => {
    // The predecessor module re-derived castability from mana_cost + pool +
    // available + produces and drifted from the engine on every cost rule.
    // A hand full of cards the OLD pricing would have called castable (and
    // wrongly stopped for — the Jitte report) passes now, because the server
    // projected nothing.
    const hand = [0, 1, 2, 3, 4, 5].map((i) => ({
      id: 40 + i,
      name: `Card ${i}`,
      types: 'Instant',
      mana_cost: '2 W',
      printing: {},
      token: '',
      controller: 0,
      owner: 0,
    }));
    const p = player({ hand: hand as PlayerView['hand'], pool: { C: 1, W: 1 }, available: { R: 9 } });
    expect(castableAfterTap(view(p), 0)).toBe(false);
    // ...and one projected entry stops, even with a dead-looking pool.
    const p2 = player({ potential_actions: [pot('ability', 20)] });
    expect(castableAfterTap(view(p2), 0)).toBe(true);
  });
});

describe('the widened projection (aph-web-manual-only-plays): every real play kind the offer walk emits', () => {
  // rules.PotentialActions now projects every play kind legalActionsPriced
  // emits (rules TestPotentialActionsProjectsEveryPlayKind): a float-gated
  // Room unlock, morph turn-face-up, specialize or max-speed granted ability
  // is a real play the seat reaches by floating mana, exactly like an Equip.
  const widened: PotentialAction[] = [
    { kind: 'unlock', obj: 93, label: 'Unlock Prop Room' },
    { kind: 'turn_face_up', obj: 94, label: 'Turn face up ({G})' },
    { kind: 'specialize', obj: 95, mode: '1', label: 'Specialize as White Form ({1})' },
    { kind: 'granted', obj: 96, label: 'Probe Engine: Draw a card.' },
  ];

  it('each new kind makes a mana-only window stop, and is named in the stop note', () => {
    for (const a of widened) {
      expect(castableAfterTap(view(player({ potential_actions: [a] })), 0), a.kind).toBe(true);
      expect(castablesAfterTap(view(player({ potential_actions: [a] })), 0)).toEqual([`${a.label} (after tapping)`]);
    }
  });

  it('a station (never mana-costed, so always offered when it is potential) is a play too', () => {
    expect(castableAfterTap(view(player({ potential_actions: [{ kind: 'station', obj: 98, label: 'Station Probe Ship' }] })), 0)).toBe(true);
  });

  it('an unknown kind is still never a play (the whitelist holds)', () => {
    expect(castableAfterTap(view(player({ potential_actions: [{ kind: 'mystery', obj: 1, label: 'Mystery' }] })), 0)).toBe(false);
  });

  it('the instant-speed new kinds (granted, turn face up) answer a stack object; the sorcery-speed ones do not', () => {
    const [unlock, turnUp, specialize, granted] = widened;
    expect(respondableAfterTap(view(player({ potential_actions: [granted] })), 0)).toBe(true);
    expect(respondableAfterTap(view(player({ potential_actions: [turnUp] })), 0)).toBe(true);
    expect(respondableAfterTap(view(player({ potential_actions: [unlock] })), 0)).toBe(false);
    expect(respondableAfterTap(view(player({ potential_actions: [specialize] })), 0)).toBe(false);
  });
});

describe('offersPotential — "the decision already offers this projected play"', () => {
  const o = (over: Partial<Option>): Option => ({ index: 0, kind: 'cast', label: 'Cast Opt', player: 0, obj: 22, ...over });

  it('matches kind, object, ability anchor, mode and label', () => {
    expect(offersPotential([o({})], { kind: 'cast', obj: 22, label: 'Cast Opt' })).toBe(true);
    expect(offersPotential([o({ kind: 'ability', ability: 1, label: 'Blade: Equip 1' })], { kind: 'ability', obj: 22, ability: 1, label: 'Blade: Equip 1' })).toBe(true);
    expect(offersPotential([o({ kind: 'specialize', mode: '2', label: 'Specialize as B' })], { kind: 'specialize', obj: 22, mode: '2', label: 'Specialize as B' })).toBe(true);
  });

  it('a different route of the same card is not the offered one', () => {
    // An alternative cost shares Mode "" with the ordinary cast (it is told
    // apart by AltCostIndex, which the projection does not carry): the label
    // is what separates them.
    expect(offersPotential([o({})], { kind: 'cast', obj: 22, label: 'Cast Opt (alternative cost)' })).toBe(false);
    expect(offersPotential([o({})], { kind: 'cast', obj: 22, mode: 'flashback', label: 'Cast Opt' })).toBe(false);
    expect(offersPotential([o({ kind: 'ability', ability: 0, label: 'Blade: Equip 1' })], { kind: 'ability', obj: 22, ability: 1, label: 'Blade: Equip 1' })).toBe(false);
    expect(offersPotential([o({ obj: 23 })], { kind: 'cast', obj: 22, label: 'Cast Opt' })).toBe(false);
  });
});

describe('castablesAfterTap — the descriptive twin (fb-20260916T225211Z)', () => {
  // Whatever makes castableAfterTap true is named here by construction (the
  // boolean IS this list's emptiness test): the labels are the server's own
  // offer labels for every real play in the projection.
  it('names every real play in the projection, in order, skipping the mana tap', () => {
    const p = player({
      potential_actions: [
        { kind: 'cast', obj: 40, label: 'Cast Lava Spike' },
        { kind: 'activate', obj: 9, label: 'Tap for mana' },
        { kind: 'ability', obj: 20, ability: 1, label: 'Equip Batterskull' },
      ],
    });
    expect(castablesAfterTap(view(p), 0)).toEqual(['Cast Lava Spike (after tapping)', 'Equip Batterskull (after tapping)']);
  });

  it('an empty or absent projection, or another seat’s, names nothing', () => {
    expect(castablesAfterTap(view(player({ potential_actions: [] })), 0)).toEqual([]);
    expect(castablesAfterTap(view(player({})), 0)).toEqual([]);
    expect(castablesAfterTap(view(player({ potential_actions: [pot('cast')] })), 1)).toEqual([]);
  });
});

describe('respondableAfterTap — the instant-speed half of the projection', () => {
  it('a potential cast or ability makes an opponent-object window respondable', () => {
    const cast = player({ potential_actions: [pot('cast')] });
    expect(respondableAfterTap(view(cast), 0)).toBe(true);
    const ability = player({ potential_actions: [pot('ability', 20)] });
    expect(respondableAfterTap(view(ability), 0)).toBe(true);
    // Rishadan Port's "11, T: tap target land" — an instant-speed mana-only
    // activation the old client skipped entirely — projects as an ability.
    const port = player({ potential_actions: [{ kind: 'ability', obj: 30, ability: 1, label: 'Port: Tap target land.' }] });
    expect(respondableAfterTap(view(port), 0)).toBe(true);
  });

  it('a land drop never makes a window respondable, and an empty projection does not either', () => {
    const land = player({ potential_actions: [pot('play_land')] });
    expect(respondableAfterTap(view(land), 0)).toBe(false);
    expect(respondableAfterTap(view(player({})), 0)).toBe(false);
    expect(respondableAfterTap(view(player({ potential_actions: [] }), false), 0)).toBe(false);
  });

  it('fails closed for a seat the projection does not belong to', () => {
    const p = player({ potential_actions: [pot('cast')] });
    expect(respondableAfterTap(view(p), 1)).toBe(false);
    expect(respondableAfterTap(view(p), 0)).toBe(true);
  });
});
