import { describe, expect, it } from 'vitest';
import { actionable, actionables, decide, emptyPriorityWindow, respondable, respondableFor, STEPS, STOPPABLE_STEPS, turnSide } from './autopilot';
import { applyPreset, defaultSettings, type PlaySettings, type StoppableStep, type StepStop } from './playsettings';
import type { CardView, Decision, Option, PlayerView, PotentialAction, View } from '../protocol';

/** view builds a View with only the fields decide reads: active (whose turn), step, stack, and the battlefield data the targets-me lookup reads. */
const view = (
  active: number,
  step: string,
  stack: { id: number; controller: number; kind?: string; targets?: { obj?: number; player: number; is_player: boolean }[] }[] = [],
  battlefield: { seat: number; cards: number[] }[] = [],
): View =>
  ({
    active,
    step,
    stack,
    players: battlefield.map((b) => ({ seat: b.seat, battlefield: b.cards.map((id) => ({ id, controller: b.seat })) })),
  }) as unknown as View;

const opt = (kind: string, at: number): Option => ({ index: at, kind, label: kind, player: 0 });

const priority = (options: Option[], min = 1, max = 1, kind = 'priority'): Decision =>
  ({ seq: 1, player: 0, kind, prompt: 'p', min, max, options } as Decision);

/** stackEntry builds one StackView-shaped entry; kind defaults to the view's "spell" (view/view.go:755). */
const stackEntry = (
  id: number,
  controller: number,
  kind = 'spell',
  targets: { obj?: number; player: number; is_player: boolean }[] = [],
) => ({ id, controller, kind, targets });

/** pot builds one projected PotentialAction: a real play the engine WOULD offer once the seat's mana floated. */
const pot = (kind: string, obj = 7): PotentialAction => ({ kind, obj, label: 'Cast Card' });

/** handCard builds one minimal CardView for a hand entry; only the fields the fixture reads are named. */
const handCard = (over: Partial<CardView>): CardView =>
  ({
    id: 1,
    name: 'Card',
    types: 'Instant',
    mana_cost: 'R',
    controller: 0,
    owner: 0,
    ...over,
  }) as CardView;

/** withHand returns the view with seat `seat`'s own hand, pool and availability attached (the viewer's own hidden zone). */
const withHand = (v: View, seat: number, over: Partial<PlayerView>): View => {
  const players = [...(v.players ?? [])];
  const at = players.findIndex((p) => p.seat === seat);
  const base = at >= 0 ? players[at] : { seat };
  const merged = {
    life: 20,
    lost: false,
    hand: [],
    battlefield: [],
    graveyard: [],
    exile: [],
    pool: {},
    command: [],
    commanders: [],
    commander_casts: [],
    ...base,
    ...over,
  } as PlayerView;
  if (at >= 0) players[at] = merged;
  else players.push(merged);
  return { ...v, players } as View;
};

const RESPONDABLE = [opt('pass', 0), opt('cast', 1), opt('concede', 2)];
const ONLY_MANA = [opt('activate', 0), opt('pass', 1), opt('concede', 2)];

const run = (decision: Decision, v: View, settings: PlaySettings = defaultSettings()) =>
  decide({ decision, view: v, seat: 0, settings });

/** runFfwd is decide() in the one-shot fast-forward mode: the stack rules are skipped, the step rules are not. */
const runFfwd = (decision: Decision, v: View, settings: PlaySettings = defaultSettings()) =>
  decide({ decision, view: v, seat: 0, settings, ffwd: true });

/** withSteps returns a copy of casual with one side's step rules replaced wholesale. */
const withSteps = (side: 'yours' | 'opponents', rules: Partial<Record<StoppableStep, StepStop>>): PlaySettings => {
  const s = applyPreset('casual');
  for (const [k, v] of Object.entries(rules)) s.steps[side][k as StoppableStep] = v as StepStop;
  return s;
};

describe('turnSide', () => {
  it('is yours on the seat\u2019s own turn and opponents otherwise', () => {
    expect(turnSide(view(0, 'main1'), 0)).toBe('yours');
    expect(turnSide(view(1, 'main1'), 0)).toBe('opponents');
    expect(turnSide(view(2, 'main1'), 0)).toBe('opponents');
  });
});

describe('STEPS / STOPPABLE_STEPS', () => {
  it('STEPS is the twelve wire step names in engine order', () => {
    expect(STEPS).toEqual([
      'untap', 'upkeep', 'draw', 'main1', 'begin-combat',
      'declare-attackers', 'declare-blockers', 'combat-damage', 'end-combat',
      'main2', 'end', 'cleanup',
    ]);
  });
  it('STOPPABLE_STEPS is the ten steps that grant priority (untap and cleanup excluded)', () => {
    expect(STOPPABLE_STEPS).toEqual([
      'upkeep', 'draw', 'main1', 'begin-combat',
      'declare-attackers', 'declare-blockers', 'combat-damage', 'end-combat',
      'main2', 'end',
    ]);
    expect(STOPPABLE_STEPS).toHaveLength(10);
  });
});

describe('decide', () => {
  it('stops with disabled when autoPass is off, before any rule is consulted', () => {
    const s = withSteps('yours', {}); // casual, but every step rule off
    s.autoPass = false;
    expect(run(priority(RESPONDABLE), view(0, 'main1'), s)).toEqual({ act: 'stop', reason: 'disabled' });
    // ffwd outranks the master switch: the run proceeds to the step rules
    expect(runFfwd(priority(RESPONDABLE), view(0, 'draw'), s)).toEqual({ act: 'pass', index: 0 });
  });

  it('stops a non-priority decision (never auto-answers a non-priority ask), even with autoPass on', () => {
    const d = priority([opt('pass', 0), opt('concede', 1)], 1, 1, 'target');
    expect(run(d, view(0, 'main1'))).toEqual({ act: 'stop', reason: 'not-priority' });
    expect(runFfwd(d, view(0, 'draw'))).toEqual({ act: 'stop', reason: 'not-priority' });
  });

  it.each(['attackers', 'blockers', 'mulligan', 'trigger_order', 'trigger_optional', 'choose', 'modes'])(
    'stops every non-priority %s ask',
    (kind) => {
      // The decision carries a pass option, so a mutated decide that
      // answered non-priority decisions would pass here instead of stopping.
      const d = priority([opt('pass', 0), opt('concede', 1)], 1, 1, kind);
      expect(run(d, view(0, 'main1'))).toEqual({ act: 'stop', reason: 'not-priority' });
    },
  );

  it('stops when min or max is not 1 (unexpected-shape)', () => {
    expect(run(priority([opt('pass', 0), opt('concede', 1)], 0, 1), view(0, 'draw')))
      .toEqual({ act: 'stop', reason: 'unexpected-shape' });
    expect(run(priority([opt('pass', 0), opt('cast', 1), opt('concede', 2)], 1, 2), view(0, 'draw')))
      .toEqual({ act: 'stop', reason: 'unexpected-shape' });
  });

  it('stops when there are two pass options (unexpected-shape)', () => {
    const d = priority([opt('pass', 0), opt('pass', 1), opt('concede', 2)]);
    expect(run(d, view(0, 'draw'))).toEqual({ act: 'stop', reason: 'unexpected-shape' });
  });

  it('stops when there is no pass option (unexpected-shape)', () => {
    const d = priority([opt('cast', 0), opt('concede', 1)]);
    expect(run(d, view(0, 'draw'))).toEqual({ act: 'stop', reason: 'unexpected-shape' });
  });

  // --- stack rules: opponent objects (casual: if-respondable / targets-me-if-respondable) ---

  it('casual: an opponent spell on top with a cast available stops (if-respondable)', () => {
    const d = priority(RESPONDABLE);
    expect(run(d, view(0, 'draw', [stackEntry(9, 1, 'spell')]))).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('casual: an opponent spell on top with nothing to respond with passes (if-respondable)', () => {
    const d = priority(ONLY_MANA);
    expect(run(d, view(0, 'draw', [stackEntry(9, 1, 'spell')]))).toEqual({ act: 'pass', index: 1 });
  });

  // --- the if-respondable arms see the tap-then-cast hand (fb-20260914T114244Z):
  // the engine prices a cast against the FLOATING pool only, so a window where the
  // player holds a castable counterspell and the untapped lands to pay for it
  // offers nothing but mana taps — respondable()'s option-kind test read that
  // window as dead and silently ate the response the rule exists to protect.

  it('casual: the report shape — opponent spell on top, mana-only window, Mana Leak + 2 untapped Islands in view STOPS', () => {
    const d = priority(ONLY_MANA); // activate + pass + concede — the window as the engine offers it, pool empty
    // rv2c: the server projects the walk — the Mana Leak the seat could pay
    // once its Islands float — and the client reads that, not a printed cost.
    const v = withHand(view(0, 'draw', [stackEntry(9, 1, 'spell')]), 0, {
      potential_actions: [pot('cast', 1)],
    });
    expect(run(d, v)).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('casual: the same shape with NO hand still passes (the empty-hand pin above stays valid)', () => {
    const d = priority(ONLY_MANA);
    // rv2c: no projection — the engine would unlock nothing here.
    const v = view(0, 'draw', [stackEntry(9, 1, 'spell')]);
    expect(run(d, v)).toEqual({ act: 'pass', index: 1 });
  });

  it('casual: a SORCERY castable after tapping does NOT stop an opponent-spell window (the timing filter)', () => {
    const d = priority(ONLY_MANA);
    // rv2c: the server's own timing gates keep a sorcery out of a response
    // window's projection — the client's old types-line timing filter is gone.
    const v = view(0, 'draw', [stackEntry(9, 1, 'spell')]);
    expect(run(d, v)).toEqual({ act: 'pass', index: 1 });
  });

  it('casual: a FLASH creature castable after tapping stops an opponent-spell window', () => {
    const d = priority(ONLY_MANA);
    const v = withHand(view(0, 'draw', [stackEntry(9, 1, 'spell')]), 0, {
      potential_actions: [pot('cast')],
    });
    expect(run(d, v)).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('casual: a play the engine would NOT unlock still passes (the money half lives server-side now)', () => {
    const d = priority(ONLY_MANA);
    const v = view(0, 'draw', [stackEntry(9, 1, 'spell')]);
    expect(run(d, v)).toEqual({ act: 'pass', index: 1 });
  });

  it('casual: a targets-me opponent trigger + mana-only window + castable instant in hand STOPS (targets-me-if-respondable)', () => {
    const d = priority(ONLY_MANA);
    const v = withHand(view(0, 'draw', [stackEntry(9, 1, 'trigger', [{ player: 0, is_player: true }])]), 0, {
      potential_actions: [pot('cast')],
    });
    expect(run(d, v)).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('casual: a targets-me opponent trigger + mana-only window + castable SORCERY in hand passes (timing, not money)', () => {
    const d = priority(ONLY_MANA);
    // rv2c: a sorcery is never in a response window's projection.
    const v = view(0, 'draw', [stackEntry(9, 1, 'trigger', [{ player: 0, is_player: true }])]);
    expect(run(d, v)).toEqual({ act: 'pass', index: 1 });
  });

  it('casual: a trigger NOT targeting me passes even with a castable instant in hand (targets-me still gates the trigger arm)', () => {
    const d = priority(ONLY_MANA);
    const v = withHand(view(0, 'draw', [stackEntry(9, 1, 'trigger', [{ player: 1, is_player: true }])]), 0, {
      potential_actions: [pot('cast')],
    });
    expect(run(d, v)).toEqual({ act: 'pass', index: 1 });
  });

  it("full-control's ownObjects if-respondable: an OWN object on top + mana-only window + castable instant STOPS (own-object)", () => {
    const d = priority(ONLY_MANA);
    const s = { ...applyPreset('full-control'), autoPass: true };
    const v = withHand(view(0, 'draw', [stackEntry(9, 0, 'spell')]), 0, {
      potential_actions: [pot('cast')],
    });
    expect(run(d, v, s)).toEqual({ act: 'stop', reason: 'own-object' });
  });

  it('respondableFor is respondable OR the projection scan, and never asks another seat\u2019s projection', () => {
    const d = priority(ONLY_MANA);
    // rv2c: the seat's own projection (the engine's walk) is the only hand
    // the scan ever reads.
    const holding = withHand(view(0, 'draw'), 0, { potential_actions: [pot('cast')] });
    expect(respondableFor(holding, 0, d)).toBe(true);
    expect(respondableFor(view(0, 'draw'), 0, d)).toBe(false);
    expect(respondableFor(holding, 1, d)).toBe(false); // the projection rides the viewer's own seat only
    const withCast = priority([opt('pass', 0), opt('cast', 1), opt('concede', 2)]);
    expect(respondableFor(view(0, 'draw'), 0, withCast)).toBe(true);
    // A response window never projects a sorcery-speed play (the server's
    // timing gates), so a projection-less view is not respondable either.
    const projectionless = view(0, 'draw');
    expect(respondableFor(projectionless, 0, d)).toBe(false);
  });

  it('casual: an opponent ability on top with a cast available stops (if-respondable)', () => {
    const d = priority(RESPONDABLE);
    expect(run(d, view(0, 'draw', [stackEntry(9, 1, 'ability')]))).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('casual: an opponent trigger targeting MY creature + respondable stops (targets-me-if-respondable)', () => {
    const d = priority(RESPONDABLE);
    // my creature (id 5) on my battlefield, targeted by the opponent's trigger
    const v = view(0, 'draw', [stackEntry(9, 1, 'trigger', [{ obj: 5, player: 0, is_player: false }])], [{ seat: 0, cards: [5] }]);
    expect(run(d, v)).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('casual: an opponent trigger targeting an opponent creature + respondable passes', () => {
    const d = priority(RESPONDABLE);
    // the trigger targets the opponent's own creature (id 5, controller 1)
    const v = view(0, 'draw', [stackEntry(9, 1, 'trigger', [{ obj: 5, player: 1, is_player: false }])], [{ seat: 1, cards: [5] }]);
    expect(run(d, v)).toEqual({ act: 'pass', index: 0 });
  });

  it('casual: an opponent trigger targeting ME (a player target) + respondable stops', () => {
    const d = priority(RESPONDABLE);
    const v = view(0, 'draw', [stackEntry(9, 1, 'trigger', [{ player: 0, is_player: true }])]);
    expect(run(d, v)).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('casual: an opponent trigger targeting the opponent passes even when respondable', () => {
    const d = priority(RESPONDABLE);
    const v = view(0, 'draw', [stackEntry(9, 1, 'trigger', [{ player: 1, is_player: true }])]);
    expect(run(d, v)).toEqual({ act: 'pass', index: 0 });
  });

  // --- targets-me counts MY objects ON THE STACK (prio7) ---

  it('casual: an opponent trigger targeting MY spell lower on the stack + respondable stops', () => {
    const d = priority(RESPONDABLE);
    // my spell (id 8, controller 0) under the opponent's trigger (id 9) that targets it
    const v = view(0, 'draw', [
      stackEntry(8, 0, 'spell'),
      stackEntry(9, 1, 'trigger', [{ obj: 8, player: 0, is_player: false }]),
    ]);
    expect(run(d, v)).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('casual: an opponent trigger targeting the OPPONENT’s own spell on the stack passes', () => {
    const d = priority(RESPONDABLE);
    const v = view(0, 'draw', [
      stackEntry(8, 1, 'spell'),
      stackEntry(9, 1, 'trigger', [{ obj: 8, player: 1, is_player: false }]),
    ]);
    expect(run(d, v)).toEqual({ act: 'pass', index: 0 });
  });

  it('casual: an opponent trigger targeting MY ability on the stack + respondable stops', () => {
    const d = priority(RESPONDABLE);
    const v = view(0, 'draw', [
      stackEntry(8, 0, 'ability'),
      stackEntry(9, 1, 'trigger', [{ obj: 8, player: 0, is_player: false }]),
    ]);
    expect(run(d, v)).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('casual: an opponent trigger targeting MY card in my graveyard + respondable stops (public-zone controller)', () => {
    const d = priority(RESPONDABLE);
    const v = view(
      0, 'draw',
      [stackEntry(9, 1, 'trigger', [{ obj: 5, player: 0, is_player: false }])],
      [{ seat: 0, cards: [] }],
    );
    (v.players as { seat: number; graveyard: { id: number; controller: number }[] }[])[0].graveyard = [{ id: 5, controller: 0 }];
    expect(run(d, v)).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('no-tells: the same opponent trigger targeting the opponent\u2019s own creature stops (always)', () => {
    const d = priority(RESPONDABLE);
    const s = applyPreset('no-tells');
    const v = view(0, 'draw', [stackEntry(9, 1, 'trigger', [{ obj: 5, player: 1, is_player: false }])], [{ seat: 1, cards: [5] }]);
    expect(run(d, v, s)).toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('no-tells: an opponent spell + NOT respondable still stops (always)', () => {
    const d = priority(ONLY_MANA);
    expect(run(d, view(0, 'draw', [stackEntry(9, 1, 'spell')]), applyPreset('no-tells')))
      .toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('no-tells: an opponent spell with an empty option side still stops even when only mana is offered', () => {
    const d = priority([opt('pass', 0), opt('concede', 1)]);
    expect(run(d, view(0, 'draw', [stackEntry(9, 1, 'spell')]), applyPreset('no-tells')))
      .toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('ffwd passes through the stack rules: an opponent spell under no-tells still passes the one-shot run', () => {
    const d = priority(RESPONDABLE);
    const s = applyPreset('no-tells');
    expect(runFfwd(d, view(0, 'draw', [stackEntry(9, 1, 'spell')]), s)).toEqual({ act: 'pass', index: 0 });
  });

  it('only the TOP of the stack is classified: my own spell over the opponent\u2019s does not trigger the opponent rule (casual)', () => {
    const d = priority(RESPONDABLE);
    const v = view(0, 'draw', [stackEntry(8, 1, 'spell'), stackEntry(9, 0, 'spell')]);
    expect(run(d, v)).toEqual({ act: 'pass', index: 0 });
  });

  // --- stack rules: own objects ---

  it('auto_mana: my own spell on top resolves through the Main 1 smart stop (ownObjects never)', () => {
    const d = priority(RESPONDABLE);
    expect(decide({ decision: d, view: view(0, 'main1', [stackEntry(9, 0, 'spell')]), seat: 0, settings: defaultSettings(), autoManaAvailable: true }))
      .toEqual({ act: 'pass', index: 0 });
  });

  it('without auto_mana, an own spell keeps the baseline smart-step stop', () => {
    const d = priority(RESPONDABLE);
    expect(run(d, view(0, 'main1', [stackEntry(9, 0, 'spell')]))).toEqual({ act: 'stop', reason: 'stop-set' });
  });

  it('full-control: my own spell on top + respondable stops (ownObjects if-respondable)', () => {
    const d = priority(RESPONDABLE);
    // full-control's autoPass is false (the preset is manual play); the rule
    // set is what is under test here, so the master switch is turned on over
    // it — the state a player is in when they run auto with full-control's
    // rules.
    const s = { ...applyPreset('full-control'), autoPass: true };
    expect(run(d, view(0, 'draw', [stackEntry(9, 0, 'spell')]), s)).toEqual({ act: 'stop', reason: 'own-object' });
  });

  it('own object on top with nothing to respond with passes even through a forced step (ownObjects if-respondable)', () => {
    const d = priority(ONLY_MANA);
    // The own-object setting decides the whole window.  With no response,
    // "Stop if I can respond" passes; a forced step must not turn it into an
    // unexpected second own-object stop.
    const s = withSteps('yours', { draw: 'forced' });
    s.ownObjects = 'if-respondable';
    expect(decide({ decision: d, view: view(0, 'draw', [stackEntry(9, 0, 'ability')]), seat: 0, settings: s, autoManaAvailable: true }))
      .toEqual({ act: 'pass', index: 1 });
  });

  // --- step rules ---

  it('casual: a smart step with only a mana activate passes (not actionable)', () => {
    const d = priority(ONLY_MANA);
    const s = withSteps('yours', { main1: 'smart' });
    expect(run(d, view(0, 'main1'), s)).toEqual({ act: 'pass', index: 1 });
  });

  it('casual: a smart step with a real action stops', () => {
    const d = priority(RESPONDABLE);
    const s = withSteps('yours', { main1: 'smart' });
    expect(run(d, view(0, 'main1'), s)).toEqual({ act: 'stop', reason: 'stop-set' });
  });

  it('a forced step stops even with nothing to do', () => {
    const d = priority(ONLY_MANA);
    const s = withSteps('opponents', { end: 'forced' });
    expect(run(d, view(1, 'end'), s)).toEqual({ act: 'stop', reason: 'stop-set' });
  });

  it('an off step with a dead mana-only window falls through to a pass (the floor is quiet when nothing is playable)', () => {
    // fb-20260917T231311Z-e392fcc0: the old leaf asserted a plain fall-through
    // with a cast option on the window — but a cast option IS an action, so the
    // own-turn main-phase floor stops that shape now. The fall-through survives
    // for the mana-only window with an unreadable/dead hand.
    const d = priority(ONLY_MANA);
    const s = withSteps('yours', { main1: 'off' });
    expect(run(d, view(0, 'main1'), s)).toEqual({ act: 'pass', index: 1 });
  });

  it('a step rule on the wrong turn side does not apply', () => {
    const d = priority(RESPONDABLE);
    const s = withSteps('yours', { main1: 'forced' });
    // their turn: the YOURS rule is inert
    expect(run(d, view(1, 'main1'), s)).toEqual({ act: 'pass', index: 0 });
  });

  it('a step outside the ten stoppable ones (untap, cleanup) never stops', () => {
    const d = priority(RESPONDABLE);
    expect(run(d, view(0, 'untap'))).toEqual({ act: 'pass', index: 0 });
    expect(run(d, view(0, 'cleanup'))).toEqual({ act: 'pass', index: 0 });
  });

  it('the step rule applies to ffwd too (the c2f4db8f contract)', () => {
    const d = priority(RESPONDABLE);
    const s = withSteps('yours', { main1: 'smart' });
    expect(runFfwd(d, view(0, 'main1'), s)).toEqual({ act: 'stop', reason: 'stop-set' });
    const forced = withSteps('yours', { main1: 'forced' });
    expect(runFfwd(priority(ONLY_MANA), view(0, 'main1'), forced)).toEqual({ act: 'stop', reason: 'stop-set' });
  });

  it('full-control with ffwd still runs (ffwd outranks autoPass off) but a forced step stops it', () => {
    const s = applyPreset('full-control'); // all steps forced, autoPass false
    expect(runFfwd(priority(RESPONDABLE), view(0, 'main1'), s)).toEqual({ act: 'stop', reason: 'stop-set' });
  });

  // --- own-turn main-phase floor (fb-20260917T231311Z-e392fcc0) ---
  //
  // rv2c merged over b4234e28: the castability oracle the floor reads is no
  // longer a client-side mana scan of hand+lands — it is the seat's own
  // projected potential_actions (castable.ts reads the server's offer walk
  // and fails closed to empty without it). The seq-1260 fixtures keep the
  // incident's hand and battlefield as context; the PROJECTION is the
  // operative input, carrying the two Burning Wish casts the engine would
  // unlock once the lands' mana floated.

  const seqLand = (id: number, name: string, colour: [number, number, number, number, number, number]): CardView =>
    ({ id, name, types: 'Land', mana_cost: '', controller: 0, owner: 0, produces: { colour, any: false } }) as CardView;
  const SEQ_ISLAND = seqLand(6, 'Island', [0, 1, 0, 0, 0, 0]);
  const SEQ_SWAMP = seqLand(1, 'Swamp', [0, 0, 1, 0, 0, 0]);
  const SEQ_MOUNTAIN = seqLand(7, 'Mountain', [0, 0, 0, 1, 0, 0]);
  const SEQ_VOLC = seqLand(3, 'Volcanic Island', [0, 1, 0, 1, 0, 0]);
  const SEQ_BW_1 = handCard({ id: 33, name: 'Burning Wish', types: 'Sorcery', mana_cost: '1 R' });
  const SEQ_BW_2 = handCard({ id: 34, name: 'Burning Wish', types: 'Sorcery', mana_cost: '1 R' });
  const SEQ_AD_NAUSEAM = handCard({ id: 43, name: 'Ad Nauseam', types: 'Instant', mana_cost: '3 B B' });

  /** seqBW is one projected Burning Wish cast: the offer's obj is the hand card's id, the label the server's own. */
  const seqBW = (id: number): PotentialAction => ({ kind: 'cast', obj: id, label: 'Cast Burning Wish' });

  /** seq1260Options is the incident decision's exact option list: four mana-tap activates, the pass at index 4. */
  const seq1260Options: Option[] = [
    { index: 0, kind: 'activate', label: 'Activate Island for mana', player: 0, obj: 6 },
    { index: 1, kind: 'activate', label: 'Activate Swamp for mana', player: 0, obj: 1 },
    { index: 2, kind: 'activate', label: 'Activate Mountain for mana', player: 0, obj: 7 },
    { index: 3, kind: 'activate', label: 'Activate Volcanic Island for mana', player: 0, obj: 3 },
    { index: 4, kind: 'pass', label: 'Pass priority', player: 0 },
    { index: 5, kind: 'concede', label: 'Concede', player: 0 },
  ];

  /**
   * seqView rebuilds the incident state (turn 11 main1, seat 0 active): four untapped lands, the reported hand,
   * empty pool, and seat 0's own projection — by default the two Burning Wish casts the incident hand unlocks.
   */
  const seqView = (step = 'main1', active = 0, actions: PotentialAction[] = [seqBW(33), seqBW(34)]): View =>
    withHand(view(active, step, [], [{ seat: 0, cards: [] }]), 0, {
      hand: [SEQ_BW_1, SEQ_AD_NAUSEAM, SEQ_BW_2],
      battlefield: [SEQ_ISLAND, SEQ_SWAMP, SEQ_MOUNTAIN, SEQ_VOLC],
      pool: {},
      potential_actions: actions,
    });

  /** seqOff is the incident settings: Auto on (default), the own-main steps off. */
  const seqOff = () => withSteps('yours', { main1: 'off', main2: 'off' });

  it('own-turn main1 off + a castable hand stops (the seq-1260 replica) and the oracle names both Burning Wishes', () => {
    const s = seqOff();
    const d = priority(seq1260Options);
    expect(run(d, seqView(), s)).toEqual({ act: 'stop', reason: 'stop-set' });
    expect(actionables(seqView(), 0, d)).toEqual([
      'Cast Burning Wish (after tapping)',
      'Cast Burning Wish (after tapping)',
    ]);
  });

  it('own-turn main2 off + the same hand stops (both main phases are floored)', () => {
    expect(run(priority(seq1260Options), seqView('main2'), seqOff())).toEqual({ act: 'stop', reason: 'stop-set' });
  });

  it('own-turn main1 off + a dead hand still passes (the seq-1053 shape: the engine projects no play for the unpayable hand)', () => {
    const d = priority([
      { index: 0, kind: 'activate', label: 'Activate Mountain for mana', player: 0, obj: 7 },
      { index: 1, kind: 'pass', label: 'Pass priority', player: 0 },
      { index: 2, kind: 'concede', label: 'Concede', player: 0 },
    ]);
    // rv2c: the projection is the castability oracle — the seq-1053 incident's
    // hand (only Mountain untapped) unlocks nothing, so the engine projects
    // no cast and the floor stays quiet.
    const v = seqView('main1', 0, []);
    expect(run(d, v, seqOff())).toEqual({ act: 'pass', index: 1 });
  });

  it('opponent-turn main1 off + the castable hand still passes (the floor is own-turn scoped)', () => {
    expect(run(priority(seq1260Options), seqView('main1', 1), seqOff())).toEqual({ act: 'pass', index: 4 });
  });

  it('own-turn main1 off + castable hand with skipOwnTurnFloor still passes (one-shot consent bypasses the floor)', () => {
    const d = priority(seq1260Options);
    expect(decide({ decision: d, view: seqView(), seat: 0, settings: seqOff(), skipOwnTurnFloor: true }))
      .toEqual({ act: 'pass', index: 4 });
  });

  it('own-turn upkeep off + a castable sorcery still passes (the floor is main-phase scoped)', () => {
    const d = priority([
      { index: 0, kind: 'activate', label: 'Activate Island for mana', player: 0, obj: 6 },
      { index: 1, kind: 'activate', label: 'Activate Mountain for mana', player: 0, obj: 7 },
      { index: 2, kind: 'pass', label: 'Pass priority', player: 0 },
      { index: 3, kind: 'concede', label: 'Concede', player: 0 },
    ]);
    // A sorcery IS projected here, so this leaf isolates the step scoping:
    // the window has a real play and the upkeep pass must survive the floor
    // anyway (the engine itself gates sorcery-speed offers by step, so an
    // upkeep projection of a sorcery is the timing-safe shape).
    const vCastable = seqView('upkeep', 0, [seqBW(33)]);
    expect(run(d, vCastable, seqOff())).toEqual({ act: 'pass', index: 2 });
    expect(actionables(vCastable, 0, d).length).toBeGreaterThan(0);
  });

  // --- structural invariant ---

  it('property: every pass verdict points at a pass option, over generated option-list permutations', () => {
    const KINDS = ['pass', 'concede', 'cast', 'ability'];
    const lists: Option[][] = [];
    const gen = (prefix: Option[], depth: number) => {
      if (depth === 5) return;
      for (const k of KINDS) {
        const at = prefix.length;
        const next = [...prefix, { index: at, kind: k, label: k, player: 0 } as Option];
        lists.push(next);
        gen(next, depth + 1);
      }
    };
    gen([], 1);
    // Two-pass lists (shape must stop them, but assert the invariant anyway).
    for (const n of [2, 3, 4]) {
      const list: Option[] = [];
      for (let i = 0; i < n; i++) list.push({ index: i, kind: i < 2 ? 'pass' : 'cast', label: String(i), player: 0 });
      lists.push(list);
    }
    for (const active of [0, 1]) {
      for (const step of ['main1', 'draw', 'declare-blockers', 'cleanup']) {
        for (const stackEntryVariant of ['none', 'mine', 'theirs', 'their-trigger-targeting-me'] as const) {
          const stack = stackEntryVariant === 'none' ? []
            : stackEntryVariant === 'mine' ? [stackEntry(9, 0, 'spell')]
            : stackEntryVariant === 'theirs' ? [stackEntry(9, 1, 'spell')]
            : [stackEntry(9, 1, 'trigger', [{ obj: 5, player: 0, is_player: false }])];
          const v = view(active, step, stack, [{ seat: 0, cards: [5] }]);
          for (const settings of [applyPreset('casual'), applyPreset('no-tells'), applyPreset('full-control')]) {
            for (const list of lists) {
              // Both modes must hold the structural invariant: a pass verdict
              // always points at a pass option. On the ffwd path the stack
              // rules are skipped, so the stack branch is reachable as a pass.
              for (const ffwd of [false, true]) {
                const d = priority(list);
                const out = ffwd
                  ? decide({ decision: d, view: v, seat: 0, settings, ffwd })
                  : decide({ decision: d, view: v, seat: 0, settings });
                if (out.act !== 'pass') continue;
                const o = d.options[out.index];
                expect(o, `pass index ${out.index} on ${JSON.stringify(list.map((x) => x.kind))} must be a pass option`).toBeDefined();
                expect(o.kind, `kind of option at pass index ${out.index} on ${JSON.stringify(list.map((x) => x.kind))}`).toBe('pass');
              }
            }
          }
        }
      }
    }
  });

  it('actionable is false exactly when every option kind is pass, concede or activate AND no hand card is castable after tapping', () => {
    // SEMANTICS CHANGED (fb-20260914T014141Z): the old pin — "false exactly when every
    // option kind is pass/concede/activate" — no longer holds. A mana-only window
    // whose hand holds a card the seat could cast after tapping (lib/castable,
    // the post-land Lava Spike window) is now actionable: the engine prices a
    // cast against the floating pool only, so the cast option does not exist
    // YET, and auto-passing the tap would eat exactly the window the player
    // wants. These pins keep the view hand-less (the empty hand fails closed),
    // so every line below is the OLD semantics on the kind test alone.
    const noHand = view(0, 'main1');
    expect(actionable(priority([opt('pass', 0), opt('concede', 1)]), noHand, 0)).toBe(false);
    expect(actionable(priority([opt('pass', 0)]), noHand, 0)).toBe(false);
    expect(actionable(priority([opt('activate', 0), opt('pass', 1), opt('concede', 2)]), noHand, 0)).toBe(false);
    expect(actionable(priority([opt('pass', 0), opt('cast', 1), opt('concede', 2)]), noHand, 0)).toBe(true);
    expect(actionable(priority([opt('ability', 0)]), noHand, 0)).toBe(true);
    expect(actionable(priority([opt('play_land', 0), opt('activate', 1), opt('pass', 2), opt('concede', 3)]), noHand, 0)).toBe(true);
  });

  it('actionable on a mana-only window: a projected play makes it TRUE, an empty projection does not', () => {
    const d = priority(ONLY_MANA);
    // rv2c: the server projected the walk — a play the seat could pay once
    // its untapped sources floated.
    const stop = withHand(view(0, 'main1'), 0, { potential_actions: [pot('cast')] });
    expect(actionable(d, stop, 0)).toBe(true);
    // Same window, empty projection: the engine would unlock nothing, not
    // actionable.
    const pass = view(0, 'main1');
    expect(actionable(d, pass, 0)).toBe(false);
    // A castable-from-the-pool hand carries the cast option already (kind
    // test); the helper must not make the window MORE than actionable — same
    // verdict either way.
    const floating = view(0, 'main1');
    const castable = priority([opt('pass', 0), opt('cast', 1), opt('concede', 2)]);
    expect(actionable(castable, floating, 0)).toBe(true);
  });

  it('respondable is true exactly when a cast or ability option is offered (not play_land, not activate)', () => {
    expect(respondable(priority([opt('pass', 0), opt('concede', 1)]))).toBe(false);
    expect(respondable(priority([opt('activate', 0), opt('pass', 1), opt('concede', 2)]))).toBe(false);
    expect(respondable(priority([opt('play_land', 0), opt('activate', 1), opt('pass', 2), opt('concede', 3)]))).toBe(false);
    expect(respondable(priority([opt('pass', 0), opt('cast', 1), opt('concede', 2)]))).toBe(true);
    expect(respondable(priority([opt('ability', 0)]))).toBe(true);
  });

  it('a mana-only window with an empty projection is not actionable and emptyPriorityWindow returns the pass index', () => {
    const d = priority(ONLY_MANA);
    const v = view(0, 'main1');
    expect(actionable(d, v, 0)).toBe(false);
    expect(emptyPriorityWindow(d, v, 0)).toBe(1);
  });

  it('a mana-only window with a projected play is NOT an empty priority window', () => {
    const d = priority(ONLY_MANA);
    const v = withHand(view(0, 'main1'), 0, { potential_actions: [pot('cast')] });
    expect(emptyPriorityWindow(d, v, 0)).toBe(null);
  });

  // --- fb-20260917T192520Z-26136705: a costly mana activation is an action ---

  it("fb-led1: a window whose ONLY action is a costly mana activation (Lion's Eye Diamond) is actionable and emptyPriorityWindow returns null", () => {
    const d = priority([
      { index: 0, kind: 'activate', label: "Activate Lion's Eye Diamond for mana", player: 0, cost: "T Sac<1/Lion's Eye Diamond>" },
      opt('pass', 1),
      opt('concede', 2),
    ]);
    const v = view(0, 'main1'); // no players: the hand is dead mana-wise, exactly the reporter's shape
    expect(actionables(v, 0, d)).toEqual(["Activate Lion's Eye Diamond for mana"]);
    expect(actionable(d, v, 0)).toBe(true);
    expect(emptyPriorityWindow(d, v, 0)).toBe(null);
  });

  it('fb-led1: the same window whose activate option is a bare tap (no cost marker) is still empty — the floor passes it', () => {
    const d = priority(ONLY_MANA);
    const v = view(0, 'main1');
    expect(emptyPriorityWindow(d, v, 0)).toBe(1);
  });

  // --- the post-land window (fb-20260914T014141Z): smart stops now catch the tap-then-cast shape ---

  it('casual decide(): a post-land mana-only window with a castable-after-tap card in hand STOPS (stop-set)', () => {
    const d = priority(ONLY_MANA); // activate + pass + concede — exactly the window after the turn-1 Mountain
    // rv2c: the server projected the Lava Spike cast (the Mountain floats
    // first); the client reads the projection.
    const v = withHand(view(0, 'main1'), 0, { potential_actions: [pot('cast', 5)] });
    const s = withSteps('yours', { main1: 'smart' });
    expect(run(d, v, s)).toEqual({ act: 'stop', reason: 'stop-set' });
  });

  it('casual decide(): the server projection, not a per-card produces join, decides the own-main window', () => {
    // rv2c: the predecessor joined the decision's activate offers to
    // battlefield CardView.Produces projections (and priced Indeterminate
    // sources — Tron, a Cradle — at zero). The server projection carries
    // every shape, including the multi-colour and any-colour ones, and the
    // client reads it directly.
    const d = priority(ONLY_MANA);
    const s = withSteps('yours', { main1: 'smart' });
    const stoppable = withHand(view(0, 'main1'), 0, { potential_actions: [pot('cast')] });
    expect(run(d, stoppable, s)).toEqual({ act: 'stop', reason: 'stop-set' });

    const passing = view(0, 'main1');
    expect(run(d, passing, s)).toEqual({ act: 'pass', index: 1 });
  });

  it('casual decide(): a projected activation stops, but an activation the engine withholds does not project at all', () => {
    const d = priority([{ ...opt('activate', 0), obj: 7 }, opt('pass', 1), opt('concede', 2)]);
    const s = withSteps('yours', { main1: 'smart' });
    // rv2c: the payable shape (including the effective-cost one — a printed
    // 2 T reduced by a live ReduceCost static arrives already reduced) is a
    // server-projected ability.
    const payable = withHand(view(0, 'main1'), 0, { potential_actions: [pot('ability', 8)] });
    expect(run(d, payable, s)).toEqual({ act: 'stop', reason: 'stop-set' });

    // The engine withholds the unpayable and the summoning-sick shapes
    // outright (rules/legal.go's own gates), so their windows carry no
    // projection and pass — no client-side ability_costs reading left.
    const sacrifice = view(0, 'main1');
    expect(run(d, sacrifice, s)).toEqual({ act: 'pass', index: 1 });
    const sick = view(0, 'main1');
    expect(run(d, sick, s)).toEqual({ act: 'pass', index: 1 });
  });

  it('rv2c (replaces the "only uncastable cards in hand passes" pin): an X spell the engine prices at 0 projects, and an empty projection still passes', () => {
    const d = priority(ONLY_MANA);
    const s = withSteps('yours', { main1: 'smart' });
    // The engine prices X at 0 (CR 107.3b) and offers the Blaze from the
    // floating {R} — the projection carries it, so the old client-side pin
    // (a variable pip is unaffordable BY DEFINITION) is gone: the window now
    // stops, because the player CAN act.
    const withX = withHand(view(0, 'main1'), 0, { hand: [handCard({ mana_cost: 'X R' })], potential_actions: [pot('cast')] });
    expect(run(d, withX, s)).toEqual({ act: 'stop', reason: 'stop-set' });
    // An empty projection still passes — the pin's surviving half, now a
    // server fact instead of a client derivation.
    const empty = withHand(view(0, 'main1'), 0, { hand: [handCard({ mana_cost: 'X R' })] });
    expect(run(d, empty, s)).toEqual({ act: 'pass', index: 1 });
  });

  it('the projection stop does not fire on another seat\u2019s projection (the helper fails closed) and never depends on turnSide', () => {
    const d = priority(ONLY_MANA);
    // The projection is the viewer's own seat's only (view/view.go gates on
    // p.ID == viewer); a view without it fails closed.
    const projectionless = view(0, 'main1');
    const s = withSteps('yours', { main1: 'smart' });
    expect(run(d, projectionless, s)).toEqual({ act: 'pass', index: 1 });
    // The stop is about the SEAT's own projected plays, not whose turn it is:
    // actionable() is TRUE for the same shape on the opponent's turn (the
    // helper is turn-side-blind), but casual has no opponents-side main-phase
    // rule at all, so decide() still passes — the stop fires only where the
    // caller's step rules consult it.
    const mine = withHand(view(1, 'main1'), 0, { potential_actions: [pot('cast')] });
    expect(actionable(d, mine, 0)).toBe(true);
    expect(run(d, mine, s)).toEqual({ act: 'pass', index: 1 });
  });

  it('an opponent spell on top does not stop a window that only offers a land drop (not respondable)', () => {
    const d = priority([opt('activate', 0), opt('play_land', 1), opt('pass', 2), opt('concede', 3)]);
    expect(respondable(d)).toBe(false);
    expect(run(d, view(0, 'draw', [stackEntry(9, 1, 'spell')]))).toEqual({ act: 'pass', index: 2 });
  });

  // ---- prio6: always-yield per ability, and the Resolve All baseline ----
  // A yield key is `${controller}:${name}:${text}` — the same three facts
  // the stack tile renders (lib/yields.ts). The entry below is shaped like
  // the real wire StackView for an ability: name is the SOURCE's face name,
  // text the ability's own text (view/view.go's abilityName/abilityText).
  const namedEntry = (
    id: number,
    controller: number,
    name: string,
    text: string,
    kind = 'trigger',
    targets: { obj?: number; player: number; is_player: boolean }[] = [],
  ) => ({ id, controller, kind, name, text, targets });
  // artist targets me (a player target on seat 0), so every one of casual's
  // opponent-rule shapes genuinely stops for it before the yield is granted.
  const artist = namedEntry(9, 1, 'Blood Artist', 'Whenever a creature dies, each opponent loses 1 life', 'trigger', [{ player: 0, is_player: true }]);
  // other is a spell (kind spell), so casual's opponentSpell (if-respondable) stops for it.
  const other = namedEntry(10, 1, 'Soul Warden', 'Whenever a creature enters, its controller gains 1 life', 'spell');
  const artistKey = '1:Blood Artist:Whenever a creature dies, each opponent loses 1 life';

  it('a yielded key skips the opponent-object stop for THAT entry, but not for another entry', () => {
    const d = priority(RESPONDABLE);
    // Without the yield, casual stops (opponent spell, if-respondable).
    expect(run(d, view(0, 'draw', [artist]))).toEqual({ act: 'stop', reason: 'opponent-object' });
    // With the artist's key yielded, the opponent-object rule is skipped
    // for it and the window passes. A DIFFERENT key changes nothing.
    expect(decide({ decision: d, view: view(0, 'draw', [artist]), seat: 0, settings: defaultSettings(), yields: new Set([artistKey]) }))
      .toEqual({ act: 'pass', index: 0 });
    expect(decide({ decision: d, view: view(0, 'draw', [other]), seat: 0, settings: defaultSettings(), yields: new Set([artistKey]) }))
      .toEqual({ act: 'stop', reason: 'opponent-object' });
  });

  it('a yield overrides every opponent rule shape — always, if-respondable, targets-me — but not a step stop', () => {
    const d = priority(RESPONDABLE);
    const yields = new Set([artistKey]);
    for (const rule of ['always', 'if-respondable', 'targets-me-if-respondable'] as const) {
      const s = defaultSettings();
      s.opponentTrigger = rule;
      expect(decide({ decision: d, view: view(0, 'draw', [artist]), seat: 0, settings: s, yields }))
        .toEqual({ act: 'pass', index: 0 });
    }
    // The step rules still apply: a yield is per ability, never per step.
    const stopped = withSteps('yours', { draw: 'forced' });
    expect(decide({ decision: d, view: view(0, 'draw', [artist]), seat: 0, settings: stopped, yields }))
      .toEqual({ act: 'stop', reason: 'stop-set' });
  });

  it('a yield does not make the own-object rule skip: the key names the controller, so an own entry never matches', () => {
    const d = priority(RESPONDABLE);
    const mine = namedEntry(9, 0, 'Blood Artist', 'Whenever a creature dies, each opponent loses 1 life');
    const s = defaultSettings();
    s.ownObjects = 'if-respondable';
    expect(decide({ decision: d, view: view(0, 'draw', [mine]), seat: 0, settings: s, yields: new Set([artistKey]) }))
      .toEqual({ act: 'stop', reason: 'own-object' });
  });

  it('the Resolve All baseline passes through arm-time opponent objects but stops on a NEW opponent object', () => {
    const d = priority(RESPONDABLE);
    const settings = defaultSettings();
    // The arm-time entry (id 9) is on the baseline: the run plays through it.
    expect(decide({ decision: d, view: view(0, 'draw', [artist]), seat: 0, settings, baselineStack: new Set([9]) }))
      .toEqual({ act: 'pass', index: 0 });
    // A NEW opponent object (id 10) stops per the settings — the same stop
    // a plain End Turn would take.
    expect(decide({ decision: d, view: view(0, 'draw', [artist, other]), seat: 0, settings, baselineStack: new Set([9]) }))
      .toEqual({ act: 'stop', reason: 'opponent-object' });
    // And when the settings say never to stop for opponent spells, the new
    // object does not stop either — the run honours the player's own rules
    // for what arrived after the press.
    settings.opponentSpell = 'never';
    expect(decide({ decision: d, view: view(0, 'draw', [artist, other]), seat: 0, settings, baselineStack: new Set([9]) }))
      .toEqual({ act: 'pass', index: 0 });
  });

  it('Resolve All skips the own-object rule for both arm-time and NEW own objects, but not a step stop', () => {
    const d = priority(RESPONDABLE);
    const mine = namedEntry(9, 0, 'Blood Artist', 'Whenever a creature dies, each opponent loses 1 life');
    const newMine = namedEntry(10, 0, 'Young Pyromancer', 'Whenever you cast an instant or sorcery spell, create a token');
    const s = defaultSettings();
    s.ownObjects = 'if-respondable';
    expect(decide({ decision: d, view: view(0, 'draw', [mine]), seat: 0, settings: s, baselineStack: new Set([9]) }))
      .toEqual({ act: 'pass', index: 0 });
    // A seat-owned trigger pushed during the run is not one of Resolve All's
    // stop conditions, even under Full Control and when a response is offered.
    expect(decide({ decision: d, view: view(0, 'draw', [mine, newMine]), seat: 0, settings: s, baselineStack: new Set([9]) }))
      .toEqual({ act: 'pass', index: 0 });
    const stopped = withSteps('yours', { draw: 'forced' });
    expect(decide({ decision: d, view: view(0, 'draw', [artist]), seat: 0, settings: stopped, baselineStack: new Set([9]) }))
      .toEqual({ act: 'stop', reason: 'stop-set' });
  });
});

describe('actionables — the labels behind actionable() (fb-20260916T225211Z)', () => {
  // The Deadly Rollick window: one real cast the smart step rule stopped for,
  // and the note had to be able to name it.
  it('names each action-kind option by its wire label, and actionable is its emptiness test', () => {
    const d = priority([
      { index: 0, kind: 'cast', label: 'Cast Deadly Rollick (alternative cost)', player: 0 },
      opt('pass', 1),
      opt('concede', 2),
      opt('activate', 3),
    ]);
    // The mana tap is offered but is not a play: only the cast is named.
    expect(actionables(view(0, 'main1'), 0, d)).toEqual(['Cast Deadly Rollick (alternative cost)']);
    expect(actionable(d, view(0, 'main1'), 0)).toBe(true);
  });

  it('on a mana-only window it names the server-projected potential play; an empty projection is not actionable', () => {
    const d = priority(ONLY_MANA);
    const v = withHand(view(0, 'main1'), 0, { potential_actions: [{ kind: 'cast', obj: 7, label: 'Cast Lava Spike' }] });
    expect(actionables(v, 0, d)).toEqual(['Cast Lava Spike (after tapping)']);
    expect(actionable(d, v, 0)).toBe(true);

    const dead = withHand(view(0, 'main1'), 0, { potential_actions: [] });
    expect(actionables(dead, 0, d)).toEqual([]);
    expect(actionable(d, dead, 0)).toBe(false);
  });

  it('names a projected battlefield activation behind the tap', () => {
    const d = priority([{ ...opt('activate', 0), obj: 7 }, opt('pass', 1), opt('concede', 2)]);
    const v = withHand(view(0, 'main1'), 0, {
      potential_actions: [{ kind: 'ability', obj: 8, ability: 0, label: 'Activate Ability Rock' }],
    });
    expect(actionables(v, 0, d)).toEqual(['Activate Ability Rock (after tapping)']);
    expect(actionable(d, v, 0)).toBe(true);

    // A cost the engine itself withholds (a sacrifice component with nothing
    // to sacrifice) simply never enters the projection -- names nothing and
    // leaves the window not actionable, the fail-closed arm.
    const sacrifice = withHand(view(0, 'main1'), 0, { potential_actions: [] });
    expect(actionables(sacrifice, 0, d)).toEqual([]);
    expect(actionable(d, sacrifice, 0)).toBe(false);
  });
});
