import type { Decision, Option, PaymentAction, PotentialAction, View } from '../protocol';
import { isActionKind, isCostlyManaActivation } from './autopilot';
import { offersPotential } from './castable';
import { parseSymbol } from './mana';

/**
 * manualmana.ts is the ONE rule for when the auto-pay mode hides a manual
 * mana tap (spec §8, as amended by aph-web-manual-only-plays): "Manual mana
 * taps are hidden under auto-pay only when every play the window can reach is
 * reachable without them."
 *
 * All three surfaces read manualManaHidden, and apply it to exactly the
 * options isPlainManualTap names — SeatPanel.svelte's option list,
 * Table.svelte's boardOptions (the tile/hand/pile badges) and
 * HotButtonStrip.svelte's ACTIONS count — so they cannot drift apart again
 * (before aph-web-autopay-policy each decided separately: the board kept the
 * taps for any non-cast option, the list and the strip hid them
 * unconditionally, even on a manual payment window).
 *
 * WHAT HIDING MAY NEVER TAKE AWAY. A play the window can reach is reachable
 * without the manual taps when it is a visible option, or a cast its own
 * offered plan pays (the payment action's plan button, the hand card's CAST
 * shortcut, the hot strip). Everything else the engine offers only once mana
 * floats, so while any such play exists the taps stay:
 *
 *  - a potential play (the seat's potential_actions, rules.PotentialActions:
 *    the SAME legal-offer walk priced against the pool the seat could float)
 *    that the decision does not already offer (castable.offersPotential) and
 *    that no offered plan pays. A potential play the decision does not offer
 *    is, by construction of that walk, withheld only because the floating
 *    pool cannot pay it yet. This covers every non-cast kind the server
 *    projects (an Equip or pump "ability", a max-speed "granted" ability, a
 *    Room "unlock", a morph "turn_face_up", a "specialize") and every cast no
 *    plan pays: X spells, flashback, kicker/optional and alternative costs —
 *    anything the planner excludes. A plan pays exactly the ordinary cast
 *    (rules/payment_plan.go PaymentActionsForPriority: Mode "", AltCostIndex
 *    0), whose offer label the payment action carries verbatim, so a planned
 *    card's OTHER routes keep the taps too. A land drop and a station never
 *    need mana (MANA_FREE_KINDS);
 *  - an offered non-cast action whose engine-stated cost may carry mana: an
 *    "ability" option carries its offer-time cost in Option.cost (the
 *    documented wire contract), and a cost with a mana pip counts (the mana
 *    may be needed again: a second pump, a second equip). A cost with no pip
 *    (a {T} ability, a loyalty ability) does not; an option whose cost is not
 *    on the wire (a granted or alternate-cost ability, an unlock, a
 *    turn-face-up, a specialize) counts conservatively — showing a tap that
 *    was not needed costs a glance, hiding one that was makes a play
 *    unreachable;
 *  - a payment action carrying ZERO plans: its cast is reachable only by hand,
 *    which is what the seat panel's "Suggested payment is unavailable; use the
 *    manual mana controls" line tells the player (the engine never publishes
 *    one today — rules/payment_plan.go skips a cast with no plan — but the
 *    line must be true when it does).
 *
 * MEASURED, not assumed (aph-web-autopay-policy, a Go probe through the real
 * priority ask at e77928ae9): the engine offers a mana-costed play
 * FLOAT-FIRST. With an untapped Plains and Mountain, an Equipment "Probe
 * Blade" (K:Equip:1) beside an untapped creature, and a firebreather ("{R}:
 * +1/+0"), seat 0's empty-pool main-1 priority decision offered ONLY the two
 * mana taps, pass and concede; the Equip and the pump were present only in the
 * seat's potential_actions as kind "ability", and each appeared on the next
 * decision once its mana floated. The engine pins the same model in
 * rules/lander_token_offer_test.go and rules/potential_test.go, and — for the
 * special actions and granted abilities rules.PotentialActions projects since
 * aph-web-manual-only-plays — rules/potential_kinds_test.go.
 *
 * COSTLY MANA ACTIVATIONS ARE PLAYS, NOT TAPS. An "activate" mana option
 * carrying a cost on the wire (autopilot.isCostlyManaActivation: Lion's Eye
 * Diamond, a Treasure, Mana Confluence) is never hidden: it is a play of its
 * own (a sacrifice, a life payment) and Auto already stops for it. Only a
 * plain tap (isPlainManualTap) is ever hidden.
 *
 * Only a PRIORITY decision hides anything. The manual payment windows —
 * rules/cast.go's CR 601.2g window (where a PaymentFallback lands), the ward
 * and cumulative-upkeep windows — are choose decisions whose whole purpose is
 * the manual tap; hiding their taps left only Done, which abandons the
 * payment.
 *
 * web/src/lib/autopayreach.test.ts pins the consequence: for a table of
 * windows, every label Auto's actionables() stops for, and every play the
 * window can reach, is on a surface while auto-pay is on.
 *
 * Pure functions only: no Svelte, no I/O.
 */

/**
 * isManualManaOption is the one test for "this option is a manual mana tap":
 * an "activate" option whose label ends "for mana" — the priority window's
 * "Activate <name> for mana", the CR 601.2g window's same label, and the ward
 * and cumulative-upkeep windows' "Tap <name> for mana".
 */
export function isManualManaOption(option: Pick<Option, 'kind' | 'label'>): boolean {
  return option.kind === 'activate' && / for mana$/i.test(option.label);
}

/**
 * isPlainManualTap is the one per-option test the three surfaces apply
 * manualManaHidden to: a manual mana option (isManualManaOption) that costs
 * no more than a bare tap. A costly activation (the engine's Option.cost
 * marker, autopilot.isCostlyManaActivation) is a play of its own and is never
 * hidden.
 */
export function isPlainManualTap(option: Option): boolean {
  return isManualManaOption(option) && !isCostlyManaActivation(option);
}

/** MANA_FREE_KINDS are the play kinds whose cost can never include mana: a land drop, and a station (tap another creature). */
const MANA_FREE_KINDS: ReadonlySet<string> = new Set(['play_land', 'station']);

/** costMayNeedMana reads an engine-stated Forge-notation cost: absent means unknown (conservatively yes); present, yes iff some token is a mana pip. */
function costMayNeedMana(cost: string | undefined): boolean {
  const trimmed = cost?.trim() ?? '';
  if (trimmed === '') return true;
  return trimmed.split(/\s+/).some((token) => parseSymbol(token).kind !== 'unknown');
}

/** offeredNonCastMayNeedMana is the offered half: a real, non-cast action whose cost may carry mana. */
function offeredNonCastMayNeedMana(option: Option): boolean {
  if (!isActionKind(option.kind) || option.kind === 'cast' || MANA_FREE_KINDS.has(option.kind)) return false;
  return costMayNeedMana(option.cost);
}

/**
 * paidByPlan reports whether an offered plan pays this potential cast: a
 * payment action carrying a plan for the same object, whose label is the
 * cast's own offer label (the planner plans the ordinary cast only, Mode "").
 */
function paidByPlan(actions: readonly PaymentAction[], action: PotentialAction): boolean {
  if (action.kind !== 'cast' || (action.mode ?? '') !== '') return false;
  return actions.some((pa) => pa.plans.length > 0 && pa.cast.object === action.obj && pa.label === action.label);
}

/** needsManualMana is the potential half: a play of the seat's own that only floating mana by hand reaches. */
function needsManualMana(decision: Decision, action: PotentialAction): boolean {
  if (MANA_FREE_KINDS.has(action.kind)) return false;
  if (offersPotential(decision.options, action)) return false;
  return !paidByPlan(decision.payment_actions ?? [], action);
}

/**
 * manualManaHidden reports whether the surfaces hide the decision's plain
 * manual mana taps (isPlainManualTap) right now: only while the seat's
 * auto-pay preference is on, only on a priority decision, and only when every
 * play the window can reach is reachable without them (see the module comment
 * for what keeps them).
 *
 * autoPayMana is the SEAT preference (SeatPanelState.autoPayMana, which is
 * never true unless the table offers auto-pay). view and seat name whose
 * potential_actions are read — the viewer's own row, the only one the server
 * projects them on; a missing view or seat reads none.
 */
export function manualManaHidden(
  decision: Decision | null,
  view: View | null | undefined,
  seat: number | null | undefined,
  autoPayMana: boolean,
): boolean {
  if (!autoPayMana || decision === null || decision.kind !== 'priority') return false;
  if (decision.options.some(offeredNonCastMayNeedMana)) return false;
  if ((decision.payment_actions ?? []).some((pa) => pa.plans.length === 0)) return false;
  const own = view != null && seat != null ? view.players?.find((p) => p.seat === seat) : undefined;
  if ((own?.potential_actions ?? []).some((action) => needsManualMana(decision, action))) return false;
  return true;
}
