import type { Decision, Option, PotentialAction, View } from '../protocol';
import { isActionKind } from './autopilot';
import { parseSymbol } from './mana';

/**
 * manualmana.ts is the ONE rule for when the auto-pay mode hides a manual
 * mana tap (spec §8, as amended 2026-09-26): "Manual `Activate … for mana`
 * options are hidden from the option list, the board badges and the hot
 * strip, unless the decision also offers a non-cast action that may need
 * mana. To pay a cast by hand the player switches the preference off."
 *
 * All three surfaces read manualManaHidden — SeatPanel.svelte's option list,
 * Table.svelte's boardOptions (the tile/hand/pile badges) and
 * HotButtonStrip.svelte's ACTIONS count — so they cannot drift apart again
 * (before this module each decided separately: the board kept the taps for
 * any non-cast option, the list and the strip hid them unconditionally, even
 * on a manual payment window).
 *
 * WHAT "A NON-CAST ACTION THAT MAY NEED MANA" MEANS — MEASURED, not assumed
 * (aph-web-autopay-policy, a Go probe through the real priority ask at
 * e77928ae9). The engine offers a mana-costed non-cast action FLOAT-FIRST,
 * exactly like a legacy cast: rules/legal.go prices its activated-ability
 * offers against the floating pool (offerCastable with hyp == nil). With an
 * untapped Plains and Mountain, an Equipment "Probe Blade" (K:Equip:1) beside
 * an untapped creature, and a firebreather ("{R}: +1/+0"), seat 0's
 * empty-pool main-1 priority decision
 * offered ONLY "Activate Plains for mana", "Activate Mountain for mana", pass
 * and concede; the Equip and the pump were absent from the options and
 * present in the seat's potential_actions as kind "ability". After the Plains
 * tap the next decision offered "Probe Blade: Equip 1" (kind "ability", cost
 * "1"); after the Mountain tap, the pump (cost "R"). The engine pins the same
 * model in rules/lander_token_offer_test.go
 * (TestLanderTokenAbilityOfferedOnlyWhenPoolFunded) and rules/potential_test.go
 * (TestPotentialActionsMountDoomDamageAbility, TestPotentialActionsEquipAbility).
 * Two consequences:
 *
 *  - Such an action is never on the decision while the pool is empty, so a
 *    rule reading only the options (the board's old hasNonCastAction) hides
 *    the very taps that would make it appear: the Equip becomes unreachable
 *    while auto-pay is on. This predicate therefore also reads the seat's own
 *    potential_actions (rules.PotentialActions: the SAME legal-offer walk
 *    priced against the pool the seat could float). A potential non-cast play
 *    the decision does not already offer is, by construction of that walk,
 *    withheld only because the floating pool cannot pay it yet — it needs
 *    mana.
 *  - Non-mana activated abilities are Kind "ability" (or "granted", "unlock",
 *    "turn_face_up", "specialize"), never "activate": on a priority decision
 *    "activate" is only the mana tap (rules/legal.go's mana-ability loop).
 *    KNOWN GAP (server side): rules.PotentialActions projects only "cast",
 *    "ability" and "play_land", so a float-gated unlock, turn-face-up,
 *    specialize or max-speed "granted" ability is invisible to the client on
 *    an empty pool and cannot keep the taps visible here. Any other non-cast
 *    potential kind already counts below, so widening the projection closes
 *    it without touching this module.
 *
 * An OFFERED non-cast action counts when its engine-stated cost may carry
 * mana: an "ability" option carries its offer-time cost in Option.cost (the
 * documented wire contract), and a cost with a mana pip counts (the mana may
 * be needed again: a second pump, a second equip). A cost with no pip (a {T}
 * ability, a loyalty ability) does not; an option whose cost is not on the
 * wire (a granted or alternate-cost ability, an unlock, a turn-face-up, a
 * specialize) counts conservatively — showing a tap that was not needed
 * costs a glance, hiding one that was makes an action unreachable. A land
 * drop never needs mana, and neither does a station (its cost is tapping
 * another creature). A CAST never counts, planned or not: §8 pays a cast by
 * hand by switching the preference off.
 *
 * Only a PRIORITY decision hides anything. The manual payment windows —
 * rules/cast.go's CR 601.2g window (where a PaymentFallback lands), the ward
 * and cumulative-upkeep windows — are choose decisions whose whole purpose is
 * the manual tap; hiding their taps left only Done, which abandons the
 * payment.
 *
 * A payment action carrying ZERO plans is still a cast and does not by itself
 * keep the taps visible: that is §8's rule as written, and main's behaviour on
 * all three surfaces. OPEN QUESTION: SeatPanel's "Suggested payment is
 * unavailable; use the manual mana controls" line (also §8) is only literally
 * true if such an action keeps the taps visible, which §8's single exception
 * does not list; which half of §8 yields is an operator decision, not this
 * module's. No live window reaches the shape today: the engine never publishes
 * a zero-plan action (rules/payment_plan.go skips a cast with no plan).
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

/** MANA_FREE_KINDS are the non-cast action kinds whose cost can never include mana: a land drop, and a station (tap another creature). */
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

/** offered reports whether the decision already offers this potential action (same kind, object and ability — cardoptions.laterByObj's identity). */
function offered(decision: Decision, action: PotentialAction): boolean {
  return decision.options.some((o) =>
    o.kind === action.kind && o.obj === action.obj && (o.ability ?? 0) === (action.ability ?? 0));
}

/** floatGatedNonCastPlay is the potential half: a non-cast potential play of the seat's own that the decision does not offer yet. */
function floatGatedNonCastPlay(decision: Decision, view: View, seat: number): boolean {
  const own = view.players?.find((p) => p.seat === seat);
  return (own?.potential_actions ?? []).some((action) =>
    action.kind !== 'cast' && !MANA_FREE_KINDS.has(action.kind) && !offered(decision, action));
}

/**
 * manualManaHidden reports whether the surfaces hide the decision's manual
 * mana taps (isManualManaOption) right now: only while the seat's auto-pay
 * preference is on, only on a priority decision, and only when no non-cast
 * action may need the mana — offered on the decision or, float-first,
 * potential for the seat (see the module comment for the measurement).
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
  if (view != null && seat != null && floatGatedNonCastPlay(decision, view, seat)) return false;
  return true;
}
