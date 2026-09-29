import type { Decision, Option, View } from '../protocol';
import { checkBreakpoints, targetsSeat, type BreakpointHit } from './breakpoints';
import { castablesAfterTap, isResponseKind, respondableAfterTap } from './castable';
import type { OpponentObjectRule, OpponentTriggerRule, PlaySettings, StoppableStep } from './playsettings';
import { stackYieldKey } from './yields';

/**
 * autopilot is the "auto" decision logic for a seat: pass priority for the
 * player, stop where the player told it to, and auto-pass whenever there is
 * no action available. It is pure -- no I/O, no timers, no Svelte runes --
 * because a mis-firing autopasser loses games silently: it acts for the
 * player, at speed, where a wrong action is unrecoverable. The caller owns
 * the loop guard (track the answered seq) and the UI. This module only
 * answers one question: what should the seat do with this decision, right
 * now? Note the wire fact this module builds on: a priority decision's
 * options carry Kind ("pass", "cast", "ability", "concede", ...), so "no
 * action available" is mostly answerable client-side -- an option list whose
 * kinds are only "pass", "concede" and "activate" usually means the player
 * can do nothing that matters (the engine offers a mana tap for every
 * available source at every priority window; see actionable()). The ONE
 * exception is lib/castable's: the engine prices a cast against the FLOATING
 * pool only, so a hand card that becomes castable after tapping is offered
 * no cast option yet -- and a mana-only window that would reveal it is
 * exactly the window the player is about to need. actionable() consults
 * castableAfterTap so both auto-pass paths stop there.
 *
 * The SECOND exception is the response rules' own blind spot (fb-20260914T114244Z,
 * the same float-then-cast model seen from the other side): respondable()'s
 * option-kind test reads the window as dead exactly when the player is
 * holding a counterspell and the untapped lands to pay for it, because the
 * cast option does not exist until the mana floats. respondableFor() is the
 * arm the stack rules go through: respondable()'s kinds OR lib/castable's
 * potential-action scan, so a Mana Leak with two untapped Islands stops the
 * window and a sorcery does not (the server's own timing gates are already in
 * the projection, so nothing here re-derives them).
 *
 * The THIRD input is the seat's auto-pay preference (spec §8 as amended
 * 2026-09-26): while it is on, the decision's own payment_actions count, and a
 * payment action carrying a plan is a real play for the actionable tests — a
 * plan-only cast has no cast option yet, and the projection may not list it.
 * With the preference off nothing here reads payment_actions at all.
 *
 * decide() can only ever return an index pointing at an option whose kind
 * is "pass". It is structurally incapable of returning a "concede": the
 * verdict's index is always taken from the pass-option ref found by the
 * shape check, never from a position or a default.
 */

export type TurnSide = 'yours' | 'opponents';


export interface Stops {
  yours: Set<string>;
  opponents: Set<string>;
}

export type AutoVerdict =
  | { act: 'pass'; index: number }
  | { act: 'stop'; reason: StopReason; hit?: BreakpointHit };

export type StopReason =
  | 'disabled'
  | 'not-priority'
  | 'unexpected-shape'
  | 'opponent-object'
  | 'own-object'
  | 'stop-set'
  | 'breakpoint';

/** Client-side evidence captured alongside an automatic pass, not sent over the wire. */
export function passDiagnostics(decision: Decision, view: View, seat: number, verdict: string, yields?: ReadonlySet<string>, autoPayMana = false) {
  const top = view.stack.at(-1);
  return {
    verdict,
    optionKinds: [...new Set(decision.options.map((option) => option.kind))].sort(),
    actionableOptions: actionables(view, seat, decision, autoPayMana),
    castableAfterTap: castablesAfterTap(view, seat, decision),
    respondableOption: respondable(decision),
    respondableAfterTap: respondableAfterTap(view, seat),
    yieldsHit: !!top && !!yields?.has(stackYieldKey(top)),
  };
}

/** STEPS is the wire's twelve step names in engine order (state/ids.go). */
export const STEPS = [
  'untap', 'upkeep', 'draw', 'main1', 'begin-combat',
  'declare-attackers', 'declare-blockers', 'combat-damage', 'end-combat',
  'main2', 'end', 'cleanup',
] as const;

/**
 * STOPPABLE_STEPS is STEPS minus untap and cleanup, the two steps that
 * normally grant no priority. A stop there would be meaningless, so the UI
 * can only offer stops on these ten. Exported so the UI task cannot invent
 * its own list.
 */
export const STOPPABLE_STEPS: readonly string[] = STEPS.filter((s) => s !== 'untap' && s !== 'cleanup');

/** turnSide reports whether the given seat is the turn's active player. */
export function turnSide(view: View, seat: number): TurnSide {
  return view.active === seat ? 'yours' : 'opponents';
}

/**
 * isActionKind is the ONE kind test for "this option kind is a real action":
 * an option that is neither pass, concede nor activate. Every classifier that
 * decides whether a priority window (or a posted choice on one) carries
 * something to do goes through this predicate, so the kinds excluded as
 * not-a-play are stated once and cannot drift apart:
 *
 *  - pass and concede are non-answers by definition;
 *  - a BARE-TAP activate is the tap-for-mana offer the engine hangs on every
 *    priority window that has an untapped source (rules/legal.go's
 *    availableManaAbilities loop), so counting it as an action makes almost
 *    every window "actionable" and defeats both the empty-window skip and the
 *    smart step rule — tapping mana with nothing to spend it on is not a play
 *    (fb-3ab6d9da: it also armed pass-after-acting, machine-passing the very
 *    window the floated mana unlocked). The one exception is the COSTLY
 *    activation (fb-20260917T192520Z): since the engine marks an activate
 *    option whose mana ability costs more than a bare tap with Option.cost,
 *    that option IS a play — it is the sac-for-mana / pay-life activation a
 *    ritual-combo deck needs the window for, and with a dead hand it is the
 *    window's ONLY action, which the floor then swallowed whole and the card
 *    was unreachable for the rest of the game. isCostlyManaActivation below
 *    is that exception; isActionKind itself stays a pure kind test so the
 *    pass-after-acting arming test (a tap arms nothing) keeps its old shape.
 *
 * The wire fact that makes the activate exclusion safe on every consumer: a
 * priority decision's activate kind is only ever the mana tap (non-mana
 * activated abilities are offered as kind "ability", legal.go's ability
 * loop), and the one other activate on the wire (cast.go's mid-cast
 * mana-source ask) sits on a non-priority decision those consumers already
 * refuse. respondable() below is deliberately NOT this predicate: it answers
 * a different question (could the seat interact with a resolving spell) and
 * admits only the response kinds (lib/castable isResponseKind).
 */
export function isActionKind(kind: string): boolean {
  return kind !== 'pass' && kind !== 'concede' && kind !== 'activate';
}

/**
 * isCostlyManaActivation is the ONE fb-led1 exception to the activate
 * exclusion above: an "activate" option carrying the engine's cost marker
 * (decision.Option.Cost, set at rules/legal.go's mana-ability offer loop when
 * the offered mana ability costs more than a bare tap — Lion's Eye Diamond's
 * {T}, Sacrifice; Mana Confluence's Pay 1 life). A bare tap (every plain
 * land) carries no marker and stays not-a-play, so the ordinary
 * mana-tap-everywhere shape is unchanged; a costly activation is a real play
 * the player must see the window for. The marker exists precisely so this
 * predicate can exist: CardView.Produces says what a source makes, never
 * what it costs to make it, so no client-side projection could tell the two
 * shapes apart before the engine spoke.
 */
export function isCostlyManaActivation(o: Option): boolean {
  return o.kind === 'activate' && !!o.cost;
}

/**
 * plannedCasts is the auto-pay arm of actionables() (spec §8 as amended
 * 2026-09-26, aph-web-autopass): with the seat's auto-pay preference ON, every
 * payment action carrying at least one plan is a real play. The seat panel,
 * the hand fan and the hot strip all offer it as a one-click cast paid by its
 * first plan — including a PLAN-ONLY cast, which has no legacy cast option
 * because the engine offers that option only once floating mana can pay it.
 * It is deliberately independent of potential_actions: a plan the server
 * offered is a play whether or not the projection also lists the card (the
 * Urborg-granted intrinsic the projection misses today). A payment action
 * with no plan is not a play (the panel offers nothing to click for it), and
 * only a priority decision carries payment actions the panel offers.
 *
 * With the preference OFF this is always empty: plan-only casts are not shown
 * then, and every classifier reading actionables() is exactly the policy of a
 * table without the auto_mana capability.
 */
function plannedCasts(decision: Decision, autoPayMana: boolean): string[] {
  if (!autoPayMana || decision.kind !== 'priority') return [];
  return (decision.payment_actions ?? [])
    .filter((action) => action.plans.length > 0)
    .map((action) => `${action.label} (with suggested mana)`);
}

/**
 * actionables is actionable()'s descriptive twin (fb-20260916T225211Z): the
 * SAME scan, returned as the human labels of what made the window actionable
 * — an action-kind option's own wire label ("Cast Deadly Rollick (alternative
 * cost)"), the costly mana activation's label ("Activate Lion's Eye Diamond
 * for mana", fb-20260917T192520Z), else — only while the seat's auto-pay
 * preference is on — plannedCasts()'s labels for the casts an offered plan
 * pays in one click ("Cast Opt (with suggested mana)"), else castablesAfterTap's
 * labels for the float-then-cast shape ("Cast Lava Spike (after tapping)").
 * actionable() below is this list's emptiness test, so a smart step stop and
 * the note that explains it read ONE predicate by construction: whatever made
 * the stop fire is named here, verbatim. The arms are actionable()'s arms, in
 * the same order; the first non-empty one names the window.
 *
 * autoPayMana is the SEAT's auto-pay preference (SeatPanelState.autoPayMana),
 * never the table's auto_mana capability. It is an explicit argument, threaded
 * through actionable(), emptyPriorityWindow() and decide() alike, so the
 * empty-window floor and Auto's step and own-turn tests keep sharing this one
 * scan. Left out it is false: the capability-less policy.
 */
export function actionables(view: View, seat: number, decision: Decision, autoPayMana = false): string[] {
  const labels = decision.options
    .filter((o) => isActionKind(o.kind) || isCostlyManaActivation(o))
    .map((o) => o.label);
  if (labels.length > 0) return labels;
  const planned = plannedCasts(decision, autoPayMana);
  if (planned.length > 0) return planned;
  return castablesAfterTap(view, seat, decision);
}

/**
 * actionable reports whether a priority decision offers the player a real
 * action. The kind test covers the obvious shapes: an option that is neither
 * pass, concede nor activate (cast, ability, play_land, ...) — isActionKind
 * above, shared with actedOption's per-choice arming test. The engine
 * offers an "activate" (tap for mana) option for every available mana source
 * at every priority window (rules/legal.go's availableManaAbilities loop), so
 * counting those taps as actions would make almost every window "actionable"
 * and defeat both the empty-window skip and the smart step rule -- tapping
 * mana with nothing to spend it on is not a play.
 *
 * The one exception to that kind test is the float-then-cast payment model:
 * the engine prices a cast against the FLOATING pool only, so the window
 * AFTER a land drop (or any window with untapped sources but no floating
 * mana) carries nothing but taps even when the player is holding a spell they
 * are about to want to cast -- Lava Spike after the turn-1 Mountain. For that
 * shape actionable() consults lib/castable's castableAfterTap: a mana-only
 * window stops when any nonland card in the seat's own hand becomes castable
 * once Available joins Pool. A window whose card is already affordable from
 * the pool carries a cast option and is caught by the kind test, so the
 * helper never double-counts; a window with no untapped source, or a hand of
 * unaffordable cards, is still "empty" and still passes.
 *
 * view/seat name WHOSE hand and mana are read: the seat the stop rules are
 * deciding for. The helper itself never asks whose turn it is -- it fires
 * wherever the caller's own step rules consult it.
 *
 * autoPayMana is the seat's auto-pay preference (see actionables): while it
 * is on, a payment action carrying a plan is a real action too (the
 * plan-only cast), so a window whose ONLY play is a planned cast is
 * actionable; off, the test is exactly the capability-less one.
 */
export function actionable(decision: Decision, view: View, seat: number, autoPayMana = false): boolean {
  return actionables(view, seat, decision, autoPayMana).length > 0;
}

/**
 * respondable reports whether the player could respond to something on the
 * stack: an option of a response kind (lib/castable isResponseKind — a cast,
 * a non-mana ability, a max-speed granted ability, or a morph turn-face-up,
 * the special action CR 708.6 allows any time its controller has priority).
 * It deliberately excludes play_land (a land drop is never a response —
 * lands are sorcery-speed and cannot interact with a resolving spell) and
 * activate (see actionable above — every mana source offers a tap at every
 * window). Used by decide()'s stack rules so an opponent object on the
 * stack only stops a player who can actually answer it, not one who can
 * merely tap a land. The projection half (respondableAfterTap) reads the
 * same kind test.
 */
export function respondable(decision: Decision): boolean {
  return decision.options.some((o) => isResponseKind(o.kind));
}

/**
 * respondableFor is the predicate decide()'s stack rules go through: could
 * this seat respond to the object on the stack if it stopped NOW. It is
 * respondable()'s option-kind test OR lib/castable's potential-action scan
 * (respondableAfterTap): the engine prices a cast against the floating pool
 * only, so a window where the player holds a castable counterspell and the
 * untapped lands to pay for it still offers nothing but mana taps -- and
 * reading that window as "nothing to respond with" silently eats exactly the
 * response the if-respondable rules exist to protect (the Kitesail
 * Apprentice / Mana Leak report). The projection half carries the engine's
 * own timing gates already: a sorcery-speed play is only potential in a
 * sorcery window, so a sorcery that merely becomes affordable after tapping
 * never stops an opponent-spell window (it could not respond even if it
 * floated first).
 *
 * Kept as one function (not an inline OR at each arm) so the three arms
 * cannot drift apart and the next if-respondable consumer inherits the fix.
 * The auto-pay preference is deliberately not an input here: spec §8 makes a
 * plan-bearing payment action count for the actionable tests (actionables()'s
 * planned arm), not for these stack rules.
 */
export function respondableFor(view: View, seat: number, decision: Decision): boolean {
  return respondable(decision) || respondableAfterTap(view, seat);
}

/**
 * emptyPriorityWindow reports the one window shape the panel skips even when
 * auto is OFF: a plain single-pick priority window with nothing actionable
 * on it. "Actionable" is actionable()'s test -- pass, concede and a bare-tap
 * activate do not count as option kinds (a mana tap is offered at every window
 * and is not a play), EXCEPT that a mana-only window whose hand holds a card
 * that becomes castable after tapping IS actionable now (lib/castable: the
 * float-then-cast payment model hides the cast behind the tap), and so is a
 * window whose only action is a COSTLY mana activation carrying the engine's
 * cost marker (fb-20260917T192520Z — Lion's Eye Diamond with a dead hand). So this
 * covers the only-pass-and-concede shape and the mana-only shape whose hand
 * is dead mana-wise, and deliberately does NOT cover the post-land window
 * holding a spell the player is about to want -- that window stops, in
 * manual mode too (skipEmpty on). There is nothing to decide in the covered
 * shapes -- the player's only non-suicidal answer is the pass, so stopping
 * to collect it is a click that carries no information. This is deliberately
 * the SAME shape test decide()
 * applies before its own !actionable branch (single-pick, exactly one pass
 * option), factored out rather than restated, so the manual-mode skip can
 * never come to a different conclusion than auto would.
 *
 * It returns the pass option's index, or null when the window is not that
 * shape. Like decide(), it is structurally incapable of pointing at a
 * concede: the index always comes from the pass option that was found.
 *
 * autoPayMana is the seat's auto-pay preference, handed to actionable() so
 * this floor and decide() read the same test (spec §8, aph-web-autopass):
 * while it is on, a window whose only play is a plan-only cast is NOT empty,
 * whether or not potential_actions lists the card (the gaps audit's third
 * probe: the floor used to pass it); off, the floor is the capability-less
 * one and passes that window exactly as before.
 */
export function emptyPriorityWindow(decision: Decision, view: View, seat: number, autoPayMana = false): number | null {
  if (decision.kind !== 'priority') return null;
  if (decision.min !== 1 || decision.max !== 1) return null;
  const passOptions = decision.options.filter((o) => o.kind === 'pass');
  if (passOptions.length !== 1) return null;
  if (actionable(decision, view, seat, autoPayMana)) return null;
  return passOptions[0].index;
}

/**
 * opponentRuleFor maps a StackView.Kind (view/view.go sets exactly three:
 * "spell" for a spell object, "trigger" for one minted by TriggerPush,
 * "ability" for any other ability object) to the settings' matching rule.
 * A kind the view does not define today is treated as if-respondable — the
 * old guard's answer for any opponent object — so a future kind fails
 * toward stopping, never toward silently passing.
 */
function opponentRuleFor(settings: PlaySettings, kind: string): OpponentObjectRule | OpponentTriggerRule {
  switch (kind) {
    case 'spell': return settings.opponentSpell;
    case 'ability': return settings.opponentAbility;
    case 'trigger': return settings.opponentTrigger;
    default: return 'if-respondable';
  }
}

export function decide(args: {
  decision: Decision;
  view: View;
  seat: number;
  /** settings carries every rule; playsettings.ts holds the values (presets), this module only consumes them. */
  settings: PlaySettings;
  /**
   * ffwd marks the one-shot fast-forward run (absent/false = persistent
   * Auto). Pressing FFWD is itself the player's explicit "I have no more
   * actions to take", so the stack rules below are skipped on this path:
   * the pass IS the consent that guard otherwise has to assume for an
   * unattended autopasser. The step rules still apply — the c2f4db8f
   * contract: a set stop stops a fast-forward too — and the caller's pass
   * cap still bounds the run.
   */
  ffwd?: boolean;
  /**
   * yields is the game-scoped set of "always pass for this ability" keys
   * (prio6, lib/yields.ts): when the TOP stack entry's key is in the set,
   * the opponent-object rule is skipped for it — the player has already
   * said, for this game, that they never stop for that ability. The step
   * rules still apply: a yield is per ability, never per step.
   */
  yields?: ReadonlySet<string>;
  /**
   * baselineStack is the Resolve All run's arm-time stack (prio6): the ids
   * of the objects that were already on the stack when the run started.
   * Resolve All's whole point is to play through the stack AS IT STANDS, so
   * an opponent object that was present at arm time does not stop the run
   * (the rule checks are skipped for it); a NEW opponent object — an id not
   * in the baseline — stops per the caller's settings, exactly as a plain
   * End Turn would. Resolve All never stops for the seat's own objects,
   * whether they were present at arm time or were added while resolving;
   * only a NEW opponent object is subject to a stack stop rule.
   */
  baselineStack?: ReadonlySet<number>;
  /**
   * skipOwnTurnFloor marks a run the player explicitly consented to blow
   * through their own turn (the one-shot runs: End Turn / Skip Turn /
   * Resolve All — runSettings() already zeroes their step rules), so the
   * own-turn main-phase floor below is bypassed for them. Persistent Auto
   * and every other caller keep the default (false), and ffwd keeps it too:
   * a fast-forward honours the floor — it is one press, the player is
   * present, and the floor only fires where there is a real play
   * (fb-20260917T231311Z-e392fcc0).
   */
  skipOwnTurnFloor?: boolean;
  /**
   * autoPayMana is the SEAT's auto-pay preference (SeatPanelState.autoPayMana),
   * never the table's auto_mana capability: the capability only makes the
   * switch available, and a player who never turns it on gets exactly the
   * capability-less policy (spec §8 as amended 2026-09-26). It reaches this
   * classification only through actionables()' planned arm: a payment action
   * carrying a plan is a real play, so the 'smart' step rule and the own-turn
   * main-phase floor stop for a plan-only cast exactly as they stop for an
   * offered cast option.
   * It is not a pass policy of its own: a window merely CARRYING a plan is not
   * held for that reason alone (no blanket guard; see SeatPanelState.derivePass).
   */
  autoPayMana?: boolean;
  /**
   * breakpointsFired maps a breakpoint key to the decision seq it first
   * stopped at (SeatPanelState owns it). A key fired at an earlier seq does
   * not stop again; the same seq does, so re-deriving one window is stable.
   */
  breakpointsFired?: ReadonlyMap<string, number>;
}): AutoVerdict {
  const { decision, view, seat, settings, ffwd = false, yields = null, baselineStack = null, skipOwnTurnFloor = false, autoPayMana = false, breakpointsFired = new Map<string, number>() } = args;

  // Safety first: auto NEVER answers anything but a plain single-pick
  // priority decision with exactly one pass option. Target, blockers,
  // attackers, mulligan, modes, trigger_order, trigger_optional and choose
  // always stop, whatever the settings say — acting on a decision it does
  // not fully understand is exactly how an autopasser loses a game silently.
  if (decision.kind !== 'priority') return { act: 'stop', reason: 'not-priority' };
  if (decision.min !== 1 || decision.max !== 1) return { act: 'stop', reason: 'unexpected-shape' };
  const passOptions = decision.options.filter((o) => o.kind === 'pass');
  if (passOptions.length !== 1) return { act: 'stop', reason: 'unexpected-shape' };
  const pass = passOptions[0];

  // 1. Master switch. FFWD outranks it: a one-shot run is explicit consent
  // even when persistent auto is off.
  if (!settings.autoPass && !ffwd) return { act: 'stop', reason: 'disabled' };

  // 1b. Breakpoints: the player's pause-on-X rules (UI rework spec §1). They
  // stop ffwd and the one-shot runs too — like a step stop, they are the
  // player's own ask. The top-object rules respect a yield or a Resolve All
  // baseline: those are explicit consent for that object.
  const bpTop = view.stack.length > 0 ? view.stack[view.stack.length - 1] : null;
  const skipTop = bpTop !== null && ((baselineStack?.has(bpTop.id) ?? false) || (yields?.has(stackYieldKey(bpTop)) ?? false));
  const hit = checkBreakpoints({ view, seat, bp: settings.breakpoints, fired: breakpointsFired, seq: decision.seq, skipTop });
  if (hit !== null) return { act: 'stop', reason: 'breakpoint', hit };

  // 2. Stack rules, on the TOP of the stack only (the object that resolves
  // next). ffwd passes through all of them (pressing FFWD is consent).
  // Resolve All's baseline passes through them too, but ONLY for the
  // objects that were on the stack when the run was armed: the run is the
  // player's "resolve what is already there". A yield skips the
  // opponent-object rule for its one key, whatever run is asking.
  const top = view.stack.length > 0 ? view.stack[view.stack.length - 1] : null;
  if (top !== null && !ffwd && !(baselineStack?.has(top.id) ?? false)) {
    if (top.controller !== seat) {
      if (yields?.has(stackYieldKey(top))) {
        // yielded: the player always passes for this ability this game.
      } else {
        const rule = opponentRuleFor(settings, top.kind);
        if (rule === 'always') return { act: 'stop', reason: 'opponent-object' };
        if (rule === 'if-respondable' && respondableFor(view, seat, decision)) return { act: 'stop', reason: 'opponent-object' };
        if (rule === 'targets-me-if-respondable' && respondableFor(view, seat, decision) && targetsSeat(view, seat, top)) {
          return { act: 'stop', reason: 'opponent-object' };
        }
        // 'never' (and a rule the arms above did not meet) falls through.
      }
    } else if (baselineStack === null) {
      // The seat's own object on top is governed by ownObjects, independent
      // of the auto-pay preference: stop only when 'if-respondable' has a
      // response; otherwise pass so the object resolves before step stops.
      if (settings.ownObjects === 'if-respondable' && respondableFor(view, seat, decision)) {
        return { act: 'stop', reason: 'own-object' };
      }
      return { act: 'pass', index: pass.index };
    }
  }

  // 3. Step rule for the current turn side and step — this applies to ffwd
  // too (the c2f4db8f contract: a set stop stops a fast-forward). 'forced'
  // stops whenever priority is posed, even with nothing to do; 'smart' stops
  // only when the window offers a real action (actionable(): a mana-only
  // window still passes UNLESS tapping would make a hand card castable —
  // lib/castable, the post-land Lava Spike window — or, with the auto-pay
  // preference on, an offered plan pays a cast in one click); 'off' — or a
  // step outside the ten stoppable ones — falls through.
  const side = turnSide(view, seat);
  const stepRule = settings.steps[side][view.step as StoppableStep] ?? 'off';
  if (stepRule === 'forced' || (stepRule === 'smart' && actionable(decision, view, seat, autoPayMana))) {
    return { act: 'stop', reason: 'stop-set' };
  }

  // 3b. The own-turn main-phase floor (fb-20260917T231311Z-e392fcc0): with
  // Auto on and the step rule 'off', a plain pass never consulted the
  // potential-mana hand scan at all — on the player's own main phase that
  // means the machine plays their turn even while they hold a castable
  // card, and because passing priority on an empty stack in main1 advances
  // the step (the engine's MTGO convention), the player's main phase is
  // gone. So: on the ACTIVE seat's own main1/main2 (both sorcery-speed
  // windows, where a castable-in-hand is real — upkeep/draw/begin-combat
  // are NOT floored, where castableAfterTap's timing-blind scan would name
  // sorceries the engine does not even offer), when the verdict would be a
  // pass and the same oracle the smart rule reads (actionables(), one scan,
  // with the same auto-pay preference) is non-empty, stop instead — the
  // window is surfaced with the same actionable-labels note the smart rule
  // already produces. One-shot runs (skipOwnTurnFloor) and opponent turns
  // pass through as before.
  if (
    !skipOwnTurnFloor &&
    (view.step === 'main1' || view.step === 'main2') &&
    turnSide(view, seat) === 'yours' &&
    actionables(view, seat, decision, autoPayMana).length > 0
  ) {
    return { act: 'stop', reason: 'stop-set' };
  }

  // 4. Pass.
  return { act: 'pass', index: pass.index };
}
