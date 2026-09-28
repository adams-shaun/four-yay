import { describe, expect, it } from 'vitest';
import type { Decision, Option, PaymentAction, PotentialAction, View } from '../protocol';
import { isManualManaOption, isPlainManualTap, manualManaHidden } from './manualmana';

// Spec §8 (as amended by aph-web-manual-only-plays): with the seat's auto-pay
// preference on, "manual mana taps are hidden under auto-pay only when every
// play the window can reach is reachable without them". manualManaHidden is
// the ONE predicate all three surfaces read, and isPlainManualTap the one
// per-option test they apply it to. The shapes below are the measured wire
// (aph-web-autopay-policy Go probe at e77928ae9): a mana-costed play is
// offered FLOAT-FIRST, so on an empty pool it is absent from the options and
// present only in the seat's potential_actions.

const opt = (index: number, kind: string, label: string, extra: Partial<Option> = {}): Option =>
  ({ index, kind, label, player: 0, ...extra });

const plains = opt(0, 'activate', 'Activate Plains for mana', { obj: 81 });
const mountain = opt(1, 'activate', 'Activate Mountain for mana', { obj: 82 });
const led = opt(2, 'activate', 'Activate Lion\'s Eye Diamond for mana', { obj: 83, cost: 'T Sac<1/CARDNAME> Discard<1/Hand>' });
const treasure = opt(3, 'activate', 'Activate Treasure for mana', { obj: 86, cost: 'T Sac<1/CARDNAME>' });
const confluence = opt(4, 'activate', 'Activate Mana Confluence for mana', { obj: 87, cost: 'T PayLife<1>' });
const pass = opt(8, 'pass', 'Pass priority');
const concede = opt(9, 'concede', 'Concede');

const plan = {
  version: 1, id: 'plan-a', cost: { generic: 0, mana: [0, 0, 0, 0, 0, 0] as [number, number, number, number, number, number] },
  activations: [], pool_spend: [0, 0, 0, 0, 0, 0] as [number, number, number, number, number, number],
  pool_after: [0, 0, 0, 0, 0, 0] as [number, number, number, number, number, number],
};
const planned = (base: number | null, plans = [plan]): PaymentAction =>
  ({ id: 'action-a', cast: { object: 22, face: 0, origin: 'hand' }, base_option_index: base, label: 'Cast Opt', plans });

const priority = (options: Option[], payment_actions?: PaymentAction[]): Decision =>
  ({ seq: 43, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options, ...(payment_actions ? { payment_actions } : {}) });

/** view is seat 0's own view; potential_actions rides on its own PlayerView only (view/view.go). */
const view = (potential?: PotentialAction[], seat = 0): View =>
  ({
    viewer: seat, visibility: 'seat', turn: 3, round: 3, step: 'main1', phase: 'main1', active: seat, priority: seat,
    over: false, draw: false, winner: null, stack: [], pending: [],
    players: [{
      seat, name: 'P', life: 20, lost: false, library_size: 40, hand_size: 0, graveyard_size: 0,
      completed_dungeons: 0,
      hand: [], battlefield: [], graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
      ...(potential ? { potential_actions: potential } : {}),
    }],
  }) as unknown as View;

// The measured empty-pool window: two lands, the Equip {1} and the {R} pump
// are NOT options — they are potential "ability" actions only.
const equipPotential: PotentialAction = { kind: 'ability', obj: 84, label: 'Probe Blade: Equip 1' };
const pumpPotential: PotentialAction = { kind: 'ability', obj: 85, label: 'Probe Firebreather: CARDNAME gets +1/+0 until end of turn.' };

describe('isManualManaOption — the one manual mana tap test', () => {
  it('is an "activate" option whose label ends "for mana" (priority, cast-payment, ward and cumulative-upkeep windows)', () => {
    expect(isManualManaOption(plains)).toBe(true);
    expect(isManualManaOption(opt(0, 'activate', 'Tap Island for mana'))).toBe(true);
    expect(isManualManaOption(opt(0, 'activate', 'Activate Lion\'s Eye Diamond for mana', { cost: 'T Sac<1/CARDNAME>' }))).toBe(true);
  });

  it('is never a non-mana ability, a cast, or a pass', () => {
    expect(isManualManaOption(opt(0, 'ability', 'Probe Blade: Equip 1', { obj: 84, cost: '1' }))).toBe(false);
    expect(isManualManaOption(opt(0, 'cast', 'Cast Opt'))).toBe(false);
    expect(isManualManaOption(pass)).toBe(false);
  });
});

describe('isPlainManualTap — only a bare tap is ever hidden (a costly mana activation is a play of its own)', () => {
  it('a plain land tap is hideable', () => {
    expect(isPlainManualTap(plains)).toBe(true);
    expect(isPlainManualTap(mountain)).toBe(true);
  });

  it('Lion\u2019s Eye Diamond, a Treasure and Mana Confluence carry a cost on the wire and are never hidden', () => {
    // autopilot.isCostlyManaActivation's marker (Option.cost on an activate):
    // Auto already stops for these as real plays, so hiding them left Auto
    // stopping for something the panel did not show.
    expect(isPlainManualTap(led)).toBe(false);
    expect(isPlainManualTap(treasure)).toBe(false);
    expect(isPlainManualTap(confluence)).toBe(false);
  });

  it('is never anything but a manual mana option', () => {
    expect(isPlainManualTap(opt(0, 'ability', 'Probe Blade: Equip 1', { obj: 84, cost: '1' }))).toBe(false);
    expect(isPlainManualTap(opt(0, 'cast', 'Cast Opt'))).toBe(false);
    expect(isPlainManualTap(pass)).toBe(false);
  });
});

describe('manualManaHidden — the predicate table (spec §8)', () => {
  it('hides manual mana on a priority decision offering only casts and manual mana', () => {
    const d = priority([plains, opt(7, 'cast', 'Cast Opt', { obj: 22 }), pass, concede], [planned(7)]);
    expect(manualManaHidden(d, view(), 0, true)).toBe(true);
    // A plan-only cast (no legacy option) is the same shape.
    expect(manualManaHidden(priority([plains, mountain, pass, concede], [planned(null)]), view(), 0, true)).toBe(true);
  });

  it('never hides anything while the preference is off', () => {
    const d = priority([plains, opt(7, 'cast', 'Cast Opt', { obj: 22 }), pass, concede], [planned(7)]);
    expect(manualManaHidden(d, view(), 0, false)).toBe(false);
    expect(manualManaHidden(d, view([equipPotential]), 0, false)).toBe(false);
    expect(manualManaHidden(null, view(), 0, false)).toBe(false);
  });

  it('MEASURED float-first: an Equip {1} offered only in potential_actions keeps manual mana visible on the empty-pool window', () => {
    // The probe's live decision, verbatim in shape: only the two taps, pass
    // and concede. The equip is reachable only by floating mana first.
    const d = priority([plains, mountain, pass, concede]);
    expect(manualManaHidden(d, view([equipPotential]), 0, true)).toBe(false);
    expect(manualManaHidden(d, view([pumpPotential]), 0, true)).toBe(false);
    // With no such potential play the same window hides them.
    expect(manualManaHidden(d, view(), 0, true)).toBe(true);
  });

  it('an offered non-cast action with a mana cost keeps manual mana visible (the equip once {W} floats)', () => {
    const equip = opt(2, 'ability', 'Probe Blade: Equip 1', { obj: 84, ability: 0, cost: '1' });
    const d = priority([mountain, equip, pass, concede]);
    expect(manualManaHidden(d, view([equipPotential]), 0, true)).toBe(false);
    expect(manualManaHidden(d, view(), 0, true)).toBe(false);
  });

  it('an offered ability whose engine-stated cost carries no mana does not count (a {T} ability, a loyalty ability)', () => {
    const tapOnly = opt(2, 'ability', 'Mother of Runes: protection', { obj: 90, ability: 0, cost: 'T' });
    const loyalty = opt(3, 'ability', 'Walker: draw', { obj: 91, ability: 1, cost: 'AddCounter<1/LOYALTY>' });
    const d = priority([plains, tapOnly, loyalty, pass, concede]);
    // Its own potential twin is the SAME ability, already offered: not a
    // float-gated play either.
    expect(manualManaHidden(d, view([{ kind: 'ability', obj: 90, label: 'Mother of Runes: protection' }]), 0, true)).toBe(true);
  });

  it('an offered non-cast action whose cost is not on the wire counts conservatively (granted/alternate-cost abilities, unlock, turn face up, specialize)', () => {
    for (const o of [
      opt(2, 'ability', 'Granted: pump', { obj: 92, svar: 'Pump' }),
      opt(2, 'ability', 'Probe Blade: Equip (alternate cost)', { obj: 84, ability: 0, alt_cost_index: 1 }),
      opt(2, 'unlock', 'Unlock Door', { obj: 93 }),
      opt(2, 'turn_face_up', 'Turn face up (2 G)', { obj: 94 }),
      opt(2, 'specialize', 'Specialize as X (1)', { obj: 95 }),
      opt(2, 'granted', 'Raceway: pump', { obj: 96 }),
    ]) {
      expect(manualManaHidden(priority([plains, o, pass, concede]), view(), 0, true), o.kind + ' ' + o.label).toBe(false);
    }
  });

  it('a land drop never needs mana, and neither does a station', () => {
    const d = priority([plains, opt(2, 'play_land', 'Play Forest', { obj: 97 }), opt(3, 'station', 'Station Probe Ship', { obj: 98 }), pass, concede]);
    expect(manualManaHidden(d, view([{ kind: 'play_land', obj: 97, label: 'Play Forest' }]), 0, true)).toBe(true);
  });

  it('an UNPLANNED float-gated cast keeps manual mana visible: a flashback cast has no plan to pay it', () => {
    const d = priority([plains, mountain, pass, concede]);
    expect(manualManaHidden(d, view([{ kind: 'cast', obj: 30, mode: 'flashback', label: 'Cast Think Twice (flashback)' }]), 0, true)).toBe(false);
  });

  it('an X spell the planner never plans keeps manual mana visible', () => {
    // rules/payment_plan.go publishes no payment action for a cast it cannot
    // plan (X, kicker, alternative and optional costs, flashback, ...): the
    // projection is the only trace of it, and floating mana the only route.
    const d = priority([plains, mountain, pass, concede]);
    expect(manualManaHidden(d, view([{ kind: 'cast', obj: 31, label: 'Cast Fireball' }]), 0, true)).toBe(false);
  });

  it('a potential cast its OWN plan pays hides them: the plan button reaches it', () => {
    const d = priority([plains, mountain, pass, concede], [planned(null)]);
    expect(manualManaHidden(d, view([{ kind: 'cast', obj: 22, label: 'Cast Opt' }]), 0, true)).toBe(true);
  });

  it('a planned card\u2019s OTHER cast routes are not planned: kicker and alternative-cost variants keep the taps', () => {
    // The plan pays exactly the ordinary cast (Mode "", AltCostIndex 0 --
    // PaymentActionsForPriority), whose label the payment action carries
    // verbatim; any other route of the same card is reachable only by hand.
    const d = priority([plains, mountain, pass, concede], [planned(null)]);
    const plain = { kind: 'cast', obj: 22, label: 'Cast Opt' };
    expect(manualManaHidden(d, view([plain, { kind: 'cast', obj: 22, mode: 'optionalcost', label: 'Cast Opt (optional cost)' }]), 0, true)).toBe(false);
    expect(manualManaHidden(d, view([plain, { kind: 'cast', obj: 22, label: 'Cast Opt (alternative cost)' }]), 0, true)).toBe(false);
  });

  it('a cast the floating pool already pays is a visible option, so it does not keep them', () => {
    const bolt = opt(5, 'cast', 'Cast Lightning Bolt', { obj: 40 });
    const d = priority([plains, bolt, pass, concede]);
    expect(manualManaHidden(d, view([{ kind: 'cast', obj: 40, label: 'Cast Lightning Bolt' }]), 0, true)).toBe(true);
  });

  it('a payment action carrying zero plans keeps manual mana visible (the panel says "use the manual mana controls")', () => {
    // Resolves aph-web-autopay-policy's open question under the amended §8:
    // a cast no plan pays is reachable only by floating mana by hand. The
    // engine never publishes this shape today (rules/payment_plan.go skips a
    // nil plan); the panel's own line must still be true when it does.
    const d = priority([plains, mountain, pass, concede], [planned(null, [])]);
    expect(manualManaHidden(d, view(), 0, true)).toBe(false);
  });

  it('every widened non-cast potential kind keeps them: unlock, turn face up, specialize, granted', () => {
    const d = priority([plains, mountain, pass, concede]);
    for (const a of [
      { kind: 'unlock', obj: 93, label: 'Unlock Prop Room' },
      { kind: 'turn_face_up', obj: 94, label: 'Turn face up ({G})' },
      { kind: 'specialize', obj: 95, mode: '1', label: 'Specialize as White Form ({1})' },
      { kind: 'granted', obj: 96, label: 'Probe Engine: Draw a card.' },
    ]) {
      expect(manualManaHidden(d, view([a]), 0, true), a.kind).toBe(false);
    }
    // A station is never mana-costed: its potential twin changes nothing.
    expect(manualManaHidden(d, view([{ kind: 'station', obj: 98, label: 'Station Probe Ship' }]), 0, true)).toBe(true);
  });

  it('a costly mana activation alone does not keep the plain taps (it is shown itself, and needs none of them)', () => {
    const d = priority([plains, led, pass, concede]);
    expect(manualManaHidden(d, view(), 0, true)).toBe(true);
    expect(isPlainManualTap(led)).toBe(false);
  });

  it('only the viewing seat’s own projection counts; a spectator or another seat’s view carries none', () => {
    const d = priority([plains, mountain, pass, concede]);
    expect(manualManaHidden(d, view([equipPotential], 1), 0, true)).toBe(true);
    expect(manualManaHidden(d, null, 0, true)).toBe(true);
    expect(manualManaHidden(d, view([equipPotential]), null, true)).toBe(true);
  });

  it('never hides the manual payment windows: every non-priority decision keeps its mana taps', () => {
    // rules/cast.go's CR 601.2g window (carrying a PaymentFallback after a
    // plan fell back) and the ward / cumulative-upkeep windows are KChoose
    // decisions whose whole purpose is manual mana; hiding their taps left
    // only Done, which abandons the payment.
    const castWindow: Decision = {
      seq: 50, player: 0, kind: 'choose', prompt: 'Activate mana abilities to pay for Opt', min: 1, max: 1, source: 22,
      options: [opt(0, 'activate', 'Activate Island for mana', { obj: 41 }), opt(1, 'done', 'Done')],
      payment_fallback: { plan_id: 'plan-a', reason: 'source_changed' },
    };
    const wardWindow: Decision = {
      seq: 51, player: 0, kind: 'choose', prompt: 'Pay ward {2}', min: 1, max: 1,
      options: [opt(0, 'activate', 'Tap Island for mana', { obj: 41 }), opt(1, 'done', 'Done')],
    };
    expect(manualManaHidden(castWindow, view(), 0, true)).toBe(false);
    expect(manualManaHidden(wardWindow, view(), 0, true)).toBe(false);
  });
});
