import type { Decision, Option, PaymentAction, PaymentPlan, PaymentSelection } from '../../protocol';
import { isPlainManualTap } from '../manualmana';
import { isActionKind } from '../autopilot';

/**
 * decision.ts holds the pure, rules-ignorant decision helpers the seat's
 * answer surfaces share (moved out of lib/seatpanel.svelte.ts by the prompt
 * system work, UI rework sub-project 4). Nothing here posts or holds state.
 */

/**
 * actedOption reports whether the posted `choices` (wire indices) contain at
 * least one real action on a priority decision — an option whose kind
 * passes isActionKind (autopilot.ts): neither pass, nor concede, nor
 * activate. The kind comes off the wire option itself, resolved by index; a
 * choice that names no option on the decision is not an action. This is the
 * arming test for pass-after-acting, and it is the per-choice counterpart of
 * `actionable`'s per-decision test — the SAME shared predicate (isActionKind),
 * so the two cannot drift apart again. The activate exclusion is the fix for
 * fb-3ab6d9da. The wire fact it rests on: in a PRIORITY decision, Kind
 * "activate" is only ever the tap-for-mana option (rules/legal.go's
 * availableManaAbilities loop — `add("activate", "Tap <name> for mana", id)`);
 * non-mana activated abilities are offered as Kind "ability" (legal.go's
 * ability loop), and the one other "activate" on the wire (cast.go's mid-cast
 * mana-source ask) sits on a non-priority decision, which the kind test above
 * already refuses. A mana tap therefore arms nothing: it is not "I am done
 * acting" — it is the prelude to acting in the NEXT window, the one where the
 * freshly floated mana makes the held spell affordable and the engine offers
 * the cast. Counting the tap as an action machine-passed exactly that window,
 * so the player never saw the spell become playable. An earlier version of
 * this comment claimed to mirror `actionable`'s kind test while inlining a
 * test that did not — now both call the one shared predicate.
 * (Moved here from actpass.ts, which prio3 deleted with the per-table keys.)
 * A planned cast posts no choices at all; its arming twin is actedPayment.
 */
export function actedOption(d: Decision, choices: number[]): boolean {
  if (d.kind !== 'priority') return false;
  return choices.some((i) => {
    const o = d.options.find((opt) => opt.index === i);
    return o !== undefined && isActionKind(o.kind);
  });
}

/**
 * actedPayment is actedOption's twin for the payment selector (spec §8:
 * "Auto-pay changes which witness an explicit cast uses; it does not
 * otherwise change Auto/Manual policy"). A planned cast posts `choices: []`
 * plus a payment selection, so actedOption alone never saw it and a cast paid
 * by its plan never armed pass-after-acting while the same cast clicked
 * through its legacy option did. The selection counts as an action exactly
 * when it resolves, by identity, to a payment action offered on this
 * priority decision and to one of that action's offered plans — the same
 * resolution submitPayment makes before it posts. Every payment action is a
 * cast, which isActionKind always counts, so no kind test is needed here.
 */
export function actedPayment(d: Decision, payment: PaymentSelection | null | undefined): boolean {
  if (d.kind !== 'priority' || !payment) return false;
  const action = d.payment_actions?.find((candidate) => candidate.id === payment.action_id);
  return action !== undefined && action.plans.some((plan) => plan.id === payment.plan.id);
}

/**
 * pickOption is the pure heart of the seat's selection logic: it applies one
 * click identified by the option's wire index to the current picked index set and returns the
 * resulting set. It is expressed ONLY in terms of the decision's own min/max
 * (applied elsewhere) and the option Group field, never in terms of what a
 * Group's members are (R-E4-2) — the panel never learns what a blocker is.
 *
 *  - A repeatable decision (`d.repeatable` — a CanRepeatModes$ Charm,
 *    CR 601.2b) is an ORDERED MULTISET over the distinct options: clicking an
 *    already-picked option appends another instance instead of toggling it
 *    off, so with CharmNum$ 3 over 2 legal modes the same mode can fill all
 *    three slots (the ask's Min may exceed the option count — exactly the
 *    shape where toggle-off would make the submit gate unreachable). The max
 *    is the only cap; a picked instance is removed through unpickOption.
 *  - Otherwise, if the clicked option is already picked, it is removed (toggle
 *    off).
 *  - Otherwise, if it carries a non-empty Group already represented in
 *    `picked`, that previously-picked group member is REPLACED by the new
 *    option: moving one blocker from attacker A to attacker B just works,
 *    and at most one option of a Group is ever held.
 *  - Otherwise it is appended.
 */
export function pickOption(d: Decision, index: number, picked: number[]): number[] {
  const opt = optionAt(d, index);
  if (opt === undefined) return [...picked];
  if (d.repeatable) {
    if (picked.length >= d.max) return [...picked];
    return [...picked, index];
  }
  const at = picked.indexOf(index);
  if (at >= 0) return picked.filter((i) => i !== index);
  const g = opt.group;
  if (g) {
    // The per-Group cap: groupLimit raises the exclusivity marker from "at
    // most one" to "at most N" (Decision.GroupLimit, Forge's EACH per-type
    // ChangeNum), and groupLimits raises ONE named group above even that
    // (Decision.GroupLimits, a per-defender attack ceiling). At the default
    // cap the group is already represented, so the pick REPLACES that member;
    // above it the pick appends until the cap is full, then replaces the
    // oldest member.
    const cap =
      d.groupLimits?.[g] ?? (d.groupLimit && d.groupLimit > 1 ? d.groupLimit : 1);
    const members = picked.filter((i) => optionAt(d, i)?.group === g);
    if (members.length >= cap) {
      // Replace: drop the oldest group member, keep the rest's click order,
      // and put the freshly picked option at the end.
      return picked.filter((i) => i !== members[0]).concat(index);
    }
  }
  return [...picked, index];
}

/**
 * unpickOption removes ONE instance of an option from a repeatable pick
 * multiset — the picked-so-far chips' remove affordance. The LAST occurrence
 * in click order goes (the click that added it is the most recent intent),
 * the other instances keep their order; an unpicked index is a no-op.
 */
export function unpickOption(index: number, picked: number[]): number[] {
  const at = picked.lastIndexOf(index);
  if (at < 0) return [...picked];
  return picked.filter((_, i) => i !== at);
}

/** primaryOf resolves the "primary" option by kind — pass/resolve — never by position (R-E4-1). */
export function primaryOf(d: Decision): Option | null {
  for (const o of d.options) {
    if (o.kind === 'pass' || o.kind === 'resolve') return o;
  }
  return null;
}

export function isConcede(o: Option): boolean {
  return o.kind === 'concede';
}

/** paymentActionForBase keeps the additive payment offer attached to its
 * legacy cast.  It deliberately compares the wire index, never display text:
 * labels are presentation and may change without changing the action. */
export function paymentActionForBase(d: Decision, index: number): PaymentAction | undefined {
  return d.payment_actions?.find((action) => action.base_option_index === index);
}

/** paymentPlanSummary is presentation only.  The offered witness remains the
 * value sent to the server; this string never participates in selection. */
export function paymentPlanSummary(plan: PaymentPlan): string {
  if (plan.activations.length === 0) return 'Use floating mana';
  const produced = plan.activations.flatMap((step) => step.produces.map((n, i) => n > 0 ? `${n > 1 ? n : ''}${['W', 'U', 'B', 'R', 'G', 'C'][i]}` : '').filter(Boolean));
  return `Tap ${plan.activations.length} ${plan.activations.length === 1 ? 'source' : 'sources'} for ${produced.join(' + ') || 'mana'}`;
}

/** optionAt resolves a wire option by its own index, never by array position (R-E4-1). */
export function optionAt(d: Decision, index: number): Option | undefined {
  return d.options.find((o) => o.index === index);
}

/**
 * MANA_FOLLOW_UP_KINDS is the one eligibility rule for arming the card
 * follow-up expectation, measured from the engine: the only decision
 * resolveCardFollowUp can open (2-6 all-'mana' options on one object) is
 * posed by rules/mana_activation.go, and only two answers lead straight
 * into it for the SAME object:
 *
 * - `activate` -- activateManaFor, entered only from an `activate` option,
 *   whatever decision carries it: the priority window (rules/legal.go), the
 *   CR 601.2g mid-cast mana window, and the ward, cumulative-upkeep and
 *   unless-payment windows (all `choose` decisions). It poses the stage-1
 *   ability pick (`mana` options on the source) or goes straight to
 *   askManaColor (a Treasure's colour ask on the source).
 * - `mana` -- the stage-1 ability pick; answering an Any / Combo Any /
 *   Chosen ability poses askManaColor's stage-2 wheel on the same source
 *   (fb-e079def5: Talisman, Vivid Marsh).
 *
 * The rule reads option kinds, never the decision kind, because the same
 * `activate` shape appears on priority and on choose windows. Nothing else
 * reaches a same-object mana ask: an `ability` option goes on the stack (mana
 * abilities are offered only as `activate`), and target (`permanent`/
 * `player`), arrange (`card`), cast, sacrifice and mode answers do not arm.
 */
const MANA_FOLLOW_UP_KINDS: ReadonlySet<string> = new Set(['activate', 'mana']);

/**
 * followUpArm returns the expectation a hand post arms: the first answered
 * option whose kind can hand back a same-object mana ask
 * (MANA_FOLLOW_UP_KINDS) and that carries an obj. The obj is read from the
 * answered option itself (R-E4-1), never rebuilt from position. Anything
 * else returns null, which disarms.
 */
export function followUpArm(d: Decision, choices: number[]): { seq: number; obj: number } | null {
  for (const index of choices) {
    const option = optionAt(d, index);
    if (option !== undefined && MANA_FOLLOW_UP_KINDS.has(option.kind) && option.obj !== undefined) {
      return { seq: d.seq, obj: option.obj };
    }
  }
  return null;
}

/**
 * Tone is how loudly the panel presents its state, and it is resolved from
 * option KINDS alone — never a label, never a position (R-E4-1).
 *
 * `offered` is a window this seat may decline: the decision carries a `pass`
 * option, so doing nothing is a legal answer and the game moves on without
 * you. `initiative` is a decision the game is blocked on — a target, a
 * mulligan, a block assignment, a mode — where there is no pass and nothing
 * happens anywhere at the table until this seat answers. Those two deserve
 * different colours because they demand different things of the player, and
 * painting them alike is what made the old panel unreadable across a room.
 */
export type Tone = 'initiative' | 'offered' | 'idle';

export function toneOf(d: Decision | null): Tone {
  if (d === null) return 'idle';
  return d.options.some((o) => o.kind === 'pass') ? 'offered' : 'initiative';
}

/**
 * triggerOrderPermutation is the SHAPE contract every auto-order path shares
 * (fb-trigorder1 generalised the prio6 check): a trigger_order decision whose
 * min == max == options.length (the engine's permutation contract, Ruling U2)
 * with at least two options — a one-trigger ask is never posed. The check is
 * grounded in the real wire shape, measured on a live decision
 * (rules/trigger_queue.go's askTriggerOrder): each option is `kind: "trigger"`
 * with `label: "<source name>: <TriggerDescription>"`.
 */
export function triggerOrderPermutation(d: Decision): boolean {
  if (d.kind !== 'trigger_order') return false;
  if (d.min !== d.max || d.max !== d.options.length) return false;
  return d.options.length >= 2;
}

/**
 * identicalTriggerOrder reports whether a trigger_order decision's EVERY
 * option describes the same trigger — the same source name and the same
 * text (prio6) — on top of the shared triggerOrderPermutation shape
 * contract.
 *
 * Caveat, measured rather than assumed away: the label carries no target
 * information, so two identical-name/text triggers aimed at DIFFERENT
 * targets also read as identical. That is the brief's definition
 * deliberately — between two copies of the same trigger the order is
 * immaterial — and it is stated here so nobody mistakes the test for a
 * target-aware one.
 */
export function identicalTriggerOrder(d: Decision): boolean {
  if (!triggerOrderPermutation(d)) return false;
  const first = d.options[0].label;
  return d.options.every((o) => o.label === first);
}

/**
 * MulliganPhase names which half of the London round a `mulligan` decision is
 * in, so the seat panel can lay it out. The two halves are told apart by their
 * option KINDS — `keep`/`mulligan` in the first, `bottom` in the second — and
 * never by option count, position or label text (FL-101).
 *
 * A mulligan decision carrying any other kind returns null and falls back to
 * the generic option list, so an option this layout does not understand is
 * still reachable rather than silently dropped.
 */
export type MulliganPhase =
  | { phase: 'keep'; choices: Option[] }
  | { phase: 'bottom'; cards: Option[] }
  | null;

export function mulliganPhase(d: Decision | null): MulliganPhase {
  if (d === null || d.kind !== 'mulligan' || d.options.length === 0) return null;
  if (d.options.every((o) => o.kind === 'keep' || o.kind === 'mulligan')) {
    return { phase: 'keep', choices: d.options };
  }
  if (d.options.every((o) => o.kind === 'bottom')) {
    return { phase: 'bottom', cards: d.options };
  }
  return null;
}

/**
 * answersByToggle reports whether the panel answers this decision's options
 * by TOGGLING them into `picked` (committed with Submit) rather than through
 * click(): the mulligan BOTTOM half and the arrange ask (MulliganPrompt's
 * bottom row and ArrangePrompt, components/prompts/). Both can be
 * min==max==1, where click() would post the first pick irreversibly (see
 * toggle()). Every non-panel answer path — the pick-N hotkeys — routes
 * through this one predicate so it can never answer differently.
 */
export function answersByToggle(d: Decision | null): boolean {
  return d !== null && (d.kind === 'arrange' || mulliganPhase(d)?.phase === 'bottom');
}

/**
 * genericListOptions is the generic option list's rows, in order: the
 * priority list and the numbered rows of components/prompts/ draw exactly
 * these as option buttons. It leaves out what
 * the panel draws elsewhere or hides — concede, the pass/resolve `primary`
 * (its own button), a cast whose payment action stands in for it
 * (`paymentBases`), and plain manual taps while Auto Mana hides them.
 */
export function genericListOptions(
  d: Decision,
  primary: Option | null,
  paymentBases: ReadonlySet<number>,
  hideManualMana: boolean,
): Option[] {
  return d.options.filter((opt) =>
    !isConcede(opt)
    && opt.index !== primary?.index
    && !paymentBases.has(opt.index)
    && !(hideManualMana && isPlainManualTap(opt)));
}
