import { describe, expect, it } from 'vitest';
import type { Decision, Option, PaymentAction, PotentialAction, View } from '../protocol';
import { isManualManaOption, manualManaHidden } from './manualmana';

// Spec §8 (as amended 2026-09-26): with the seat's auto-pay preference on,
// manual "Activate … for mana" options are hidden from the option list, the
// board badges and the hot strip, "unless the decision also offers a non-cast
// action that may need mana". manualManaHidden is the ONE predicate all three
// surfaces read. The shapes below are the measured wire (aph-web-autopay-policy
// Go probe at e77928ae9): a mana-costed non-cast action is offered FLOAT-FIRST,
// so on an empty pool it is absent from the options and present only in the
// seat's potential_actions.

const opt = (index: number, kind: string, label: string, extra: Partial<Option> = {}): Option =>
  ({ index, kind, label, player: 0, ...extra });

const plains = opt(0, 'activate', 'Activate Plains for mana', { obj: 81 });
const mountain = opt(1, 'activate', 'Activate Mountain for mana', { obj: 82 });
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

  it('a float-gated CAST is not a non-cast action: §8 pays a cast by hand by switching the preference off', () => {
    const d = priority([plains, mountain, pass, concede]);
    expect(manualManaHidden(d, view([{ kind: 'cast', obj: 30, mode: 'flashback', label: 'Cast Think Twice (flashback)' }]), 0, true)).toBe(true);
  });

  it('a payment action carrying zero plans is still a cast; it does not by itself keep manual mana visible (§8 as written)', () => {
    // OPEN QUESTION (see manualmana.ts): kept at §8's single exception and at
    // main's behaviour on all three surfaces, although the panel's "use the
    // manual mana controls" line reads as if the taps were visible. The
    // engine never publishes a zero-plan action (rules/payment_plan.go skips
    // a nil plan), so no live window reaches this shape; flipping it is a
    // one-line change in manualManaHidden plus this expectation.
    const d = priority([plains, mountain, pass, concede], [planned(null, [])]);
    expect(manualManaHidden(d, view(), 0, true)).toBe(true);
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
