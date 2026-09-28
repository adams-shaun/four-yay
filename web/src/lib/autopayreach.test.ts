import { describe, expect, it } from 'vitest';
import type { Decision, Option, PaymentAction, PotentialAction, View } from '../protocol';
import { actionables } from './autopilot';
import { offersPotential } from './castable';
import { isManualManaOption, isPlainManualTap, manualManaHidden } from './manualmana';

// "Auto never stops for something the panel hides" (aph-web-manual-only-plays,
// spec §8 as amended: manual mana taps are hidden under auto-pay only when
// every play the window can reach is reachable without them).
//
// For each fixture window, with the seat's auto-pay preference ON:
//  - every label actionables() returns -- the note Auto's smart stop and the
//    own-turn floor show -- is reachable on the surfaces, and
//  - so is every play the window can reach: each potential action and each
//    payment action.
// "Reachable" is what the three surfaces render from the SAME predicates they
// read (SeatPanel's list, Table's board badges and the hot strip all filter
// `manualManaHidden(...) && isPlainManualTap(option)`, and render a plan
// button for every payment action carrying a plan): a visible option, a plan
// button, or -- for a play the engine offers only once mana floats -- the
// manual taps being visible.

const SEAT = 0;

const opt = (index: number, kind: string, label: string, extra: Partial<Option> = {}): Option =>
  ({ index, kind, label, player: SEAT, ...extra });

const plains = opt(0, 'activate', 'Activate Plains for mana', { obj: 81 });
const island = opt(1, 'activate', 'Activate Island for mana', { obj: 82 });
const led = opt(2, 'activate', 'Activate Lion\'s Eye Diamond for mana', { obj: 83, cost: 'T Sac<1/CARDNAME> Discard<1/Hand>' });
const treasure = opt(3, 'activate', 'Activate Treasure for mana', { obj: 86, cost: 'T Sac<1/CARDNAME>' });
const confluence = opt(4, 'activate', 'Activate Mana Confluence for mana', { obj: 87, cost: 'T PayLife<1>' });
const pass = opt(8, 'pass', 'Pass priority');
const concede = opt(9, 'concede', 'Concede');

const zero: [number, number, number, number, number, number] = [0, 0, 0, 0, 0, 0];
const planFor = (id: string) => ({ version: 1, id, cost: { generic: 1, mana: zero }, activations: [], pool_spend: zero, pool_after: zero });
const payment = (object: number, label: string, base: number | null, plans = [planFor(`plan-${object}`)]): PaymentAction =>
  ({ id: `action-${object}`, cast: { object, face: 0, origin: 'hand' }, base_option_index: base, label, plans });

const priority = (options: Option[], payment_actions?: PaymentAction[]): Decision =>
  ({ seq: 70, player: SEAT, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options, ...(payment_actions ? { payment_actions } : {}) });

const view = (potential: PotentialAction[] = []): View =>
  ({
    viewer: SEAT, visibility: 'seat', turn: 3, round: 3, step: 'main1', phase: 'main1', active: SEAT, priority: SEAT,
    over: false, draw: false, winner: null, stack: [], pending: [],
    players: [{
      seat: SEAT, name: 'P', life: 20, lost: false, library_size: 40, hand_size: 0, graveyard_size: 0,
      completed_dungeons: 0,
      hand: [], battlefield: [], graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
      potential_actions: potential,
    }],
  }) as unknown as View;

/** surfaces is what the auto-pay surfaces render for one window, from the production predicates. */
function surfaces(d: Decision, v: View) {
  const hide = manualManaHidden(d, v, SEAT, true);
  const visible = d.options.filter((o) => o.kind !== 'concede' && !(hide && isPlainManualTap(o)));
  const planned = (d.payment_actions ?? []).filter((a) => a.plans.length > 0);
  const potential = v.players[0].potential_actions ?? [];
  const tapsVisible = !hide && d.options.some(isManualManaOption);
  const plannedCast = (a: PotentialAction) =>
    a.kind === 'cast' && !a.mode && planned.some((p) => p.cast.object === a.obj && p.label === a.label);
  const reachesPotential = (a: PotentialAction) => offersPotential(visible, a) || plannedCast(a) || tapsVisible;
  const reachesOption = (o: Option) => visible.includes(o) || planned.some((p) => p.base_option_index === o.index);
  function reachesLabel(label: string): boolean {
    const o = d.options.find((x) => x.label === label);
    if (o) return reachesOption(o);
    if (planned.some((p) => `${p.label} (with suggested mana)` === label)) return true;
    const after = /^(.*) \(after tapping\)$/.exec(label);
    const a = after ? potential.find((x) => x.label === after[1]) : undefined;
    return a !== undefined && reachesPotential(a);
  }
  const reachesPayment = (p: PaymentAction) => p.plans.length > 0 || tapsVisible;
  return { hide, potential, reachesLabel, reachesPotential, reachesPayment };
}

interface Window {
  name: string;
  decision: Decision;
  view: View;
  /** hidden pins which way the fixture resolves, so the table cannot pass by never hiding. */
  hidden: boolean;
}

const opt22 = { kind: 'cast', obj: 22, label: 'Cast Opt' };

const WINDOWS: Window[] = [
  {
    name: 'a plan-only cast beside plain lands (the plan button pays it)',
    decision: priority([plains, island, pass, concede], [payment(22, 'Cast Opt', null)]),
    view: view([opt22]),
    hidden: true,
  },
  {
    name: 'an X spell the planner does not plan',
    decision: priority([plains, island, pass, concede]),
    view: view([{ kind: 'cast', obj: 31, label: 'Cast Fireball' }]),
    hidden: false,
  },
  {
    name: 'a flashback card in the graveyard beside a planned hand cast',
    decision: priority([plains, island, pass, concede], [payment(22, 'Cast Opt', null)]),
    view: view([opt22, { kind: 'cast', obj: 30, mode: 'flashback', label: 'Cast Think Twice (flashback)' }]),
    hidden: false,
  },
  {
    name: 'the kicked route of a planned card',
    decision: priority([plains, island, pass, concede], [payment(22, 'Cast Opt', null)]),
    view: view([opt22, { kind: 'cast', obj: 22, mode: 'optionalcost', label: 'Cast Opt (optional cost)' }]),
    hidden: false,
  },
  {
    name: 'Lion’s Eye Diamond with a dead hand',
    decision: priority([plains, led, pass, concede]),
    view: view(),
    hidden: true,
  },
  {
    name: 'a Treasure beside a planned cast',
    decision: priority([plains, treasure, pass, concede], [payment(22, 'Cast Opt', null)]),
    view: view([opt22]),
    hidden: true,
  },
  {
    name: 'Mana Confluence alone',
    decision: priority([confluence, pass, concede]),
    view: view(),
    hidden: true,
  },
  {
    name: 'a float-gated Equip',
    decision: priority([plains, island, pass, concede]),
    view: view([{ kind: 'ability', obj: 84, label: 'Probe Blade: Equip 1' }]),
    hidden: false,
  },
  {
    name: 'a float-gated Room unlock (a widened projection kind)',
    decision: priority([plains, island, pass, concede]),
    view: view([{ kind: 'unlock', obj: 93, label: 'Unlock Prop Room' }]),
    hidden: false,
  },
  {
    name: 'a float-gated morph turn-face-up (a widened projection kind)',
    decision: priority([plains, island, pass, concede]),
    view: view([{ kind: 'turn_face_up', obj: 94, label: 'Turn face up ({G})' }]),
    hidden: false,
  },
  {
    name: 'a cast the floating pool already pays (a visible legacy option)',
    decision: priority([plains, opt(5, 'cast', 'Cast Lightning Bolt', { obj: 40 }), pass, concede]),
    view: view([{ kind: 'cast', obj: 40, label: 'Cast Lightning Bolt' }]),
    hidden: true,
  },
  {
    name: 'a planned cast whose legacy option is offered too (its base is replaced by the plan button)',
    decision: priority([plains, opt(6, 'cast', 'Cast Opt', { obj: 22 }), pass, concede], [payment(22, 'Cast Opt', 6)]),
    view: view([opt22]),
    hidden: true,
  },
  {
    // The engine's payment candidates come from the same walk as the
    // projection, so a zero-plan action's cast is projected too.
    name: 'a payment action carrying zero plans',
    decision: priority([plains, island, pass, concede], [payment(22, 'Cast Opt', null, [])]),
    view: view([opt22]),
    hidden: false,
  },
  {
    name: 'a land drop beside a planned cast',
    decision: priority([plains, opt(7, 'play_land', 'Play Forest', { obj: 97 }), pass, concede], [payment(22, 'Cast Opt', null)]),
    view: view([opt22, { kind: 'play_land', obj: 97, label: 'Play Forest' }]),
    hidden: true,
  },
];

describe('auto-pay reachability: every play Auto stops for, and every play the window can reach, is on a surface', () => {
  for (const w of WINDOWS) {
    it(w.name, () => {
      const s = surfaces(w.decision, w.view);
      expect(s.hide, 'hidden').toBe(w.hidden);
      const labels = actionables(w.view, SEAT, w.decision, true);
      expect(labels.length, 'every fixture window is one Auto stops for').toBeGreaterThan(0);
      for (const label of labels) expect(s.reachesLabel(label), `actionables label ${label}`).toBe(true);
      for (const a of s.potential) expect(s.reachesPotential(a), `potential ${a.kind} ${a.label}`).toBe(true);
      for (const p of w.decision.payment_actions ?? []) expect(s.reachesPayment(p), `payment ${p.label}`).toBe(true);
    });
  }

  it('the table exercises both directions', () => {
    expect(WINDOWS.some((w) => w.hidden)).toBe(true);
    expect(WINDOWS.some((w) => !w.hidden)).toBe(true);
  });
});
