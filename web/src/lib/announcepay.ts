import type { Decision, ManaPaymentWindow, Option, PaymentAction, PaymentCost, View } from '../protocol';
import { findCardAnywhere } from './board';

/**
 * announcepay.ts is the client half of announce-then-pay
 * (docs/superpowers/specs/2026-09-27-announce-then-pay.md). Pure functions
 * only: which casts a seat with Auto-pay OFF may announce, and how the
 * announced CR 601.2g window ("select mana" prompt) is regrouped for display.
 *
 * R-E4-2 (the client is rules-ignorant): castability is the engine's planner
 * verdict already on the wire (a payment action with at least one plan), and
 * every button posts an option the server offered, by its own index (R-E4-1).
 * The one exception is ManaBrew, whose wire cannot carry payment plans: it
 * offers a plan-less action that the announce route answers (see
 * castableActions below).
 */

const SLOTS = ['W', 'U', 'B', 'R', 'G', 'C'] as const;

/** announceActions are casts a seat with Auto-pay OFF may announce: priority
 *  payment actions with no legacy cast option. Plan-less actions are used by
 *  ManaBrew, whose wire cannot carry payment plans; announcing opens the
 *  select-mana window. A cast the pool already pays keeps its legacy option. */
export function announceActions(d: Decision | null, autoManaAvailable: boolean, autoPayMana: boolean): PaymentAction[] {
  if (d === null || d.kind !== 'priority' || !autoManaAvailable || autoPayMana) return [];
  return (d.payment_actions ?? []).filter((a) => a.base_option_index === undefined || a.base_option_index === null);
}

/** castableActions are every offered payment action CAST can reach from a hand
 *  card. With Auto-pay OFF a plan-less ManaBrew action is reachable through
 *  announce-then-pay (submitAnnounce); with Auto-pay ON castAction submits the
 *  suggested plan and has no route for a plan-less action, so offering it
 *  would render a dead CAST button. Exclude it there. */
export function castableActions(d: Decision | null, autoManaAvailable: boolean, autoPayMana: boolean): PaymentAction[] {
  if (d === null || d.kind !== 'priority' || !autoManaAvailable) return [];
  const actions = d.payment_actions ?? [];
  return autoPayMana ? actions.filter((a) => a.plans.length > 0) : actions;
}

/** manaWindow is the announced window, or null for any other decision. */
export function manaWindow(d: Decision | null): (Decision & { mana_payment: ManaPaymentWindow }) | null {
  if (d === null || d.kind !== 'choose' || !d.mana_payment) return null;
  return d as Decision & { mana_payment: ManaPaymentWindow };
}

/** paymentCostText renders a PaymentCost in the Forge notation ManaSymbols
 *  reads ("1 B B U"); '' for nothing. */
export function paymentCostText(c: PaymentCost | null | undefined): string {
  if (!c) return '';
  const pips: string[] = [];
  if (c.generic > 0) pips.push(String(c.generic));
  c.mana.forEach((n, i) => {
    for (let k = 0; k < n; k++) pips.push(SLOTS[i]);
  });
  return pips.join(' ');
}

/** manaAmountText renders a W/U/B/R/G/C vector as pips ("U U R"). */
export function manaAmountText(m: readonly number[] | null | undefined): string {
  return paymentCostText(m ? { generic: 0, mana: [...m] as PaymentCost['mana'] } : null);
}

/** manaOptionPips reads a "mana" option's production pips out of its label
 *  ("Add U" -> "U", "Add CC" -> "C C"), or null when the production is not a
 *  plain pip string (any colour, a cost prefix, a combination) and the label
 *  itself must be shown. */
export function manaOptionPips(label: string): string | null {
  const m = /^Add ([WUBRGC]+)$/.exec(label);
  return m ? m[1].split('').join(' ') : null;
}

export interface ManaSourceRow {
  obj: number;
  name: string;
  options: Option[];
  /** suggested marks a source Auto-fill would activate. */
  suggested: boolean;
}

/** manaSourceRows groups the window's "mana" options by source, in the order
 *  the engine offered them (never re-sorted: the offer order is the board
 *  order the engine walks). */
export function manaSourceRows(d: Decision, view: View | null): ManaSourceRow[] {
  const rows: ManaSourceRow[] = [];
  const at = new Map<number, ManaSourceRow>();
  const suggested = new Set(d.mana_payment?.autofill ?? []);
  for (const o of d.options) {
    if (o.kind !== 'mana' || o.obj === undefined || o.obj === null) continue;
    let row = at.get(o.obj);
    if (row === undefined) {
      const card = view ? findCardAnywhere(view, o.obj) : null;
      row = { obj: o.obj, name: card?.name ?? `Source ${o.obj}`, options: [], suggested: suggested.has(o.obj) };
      at.set(o.obj, row);
      rows.push(row);
    }
    row.options.push(o);
  }
  return rows;
}

/** manaOptionAccessibleName is one mana button's accessible name: the source
 *  and what the activation makes ("Tap Badlands for R"). */
export function manaOptionAccessibleName(sourceName: string, o: Option): string {
  const pips = manaOptionPips(o.label);
  if (pips !== null) return `Tap ${sourceName} for ${pips.split(' ').join('')}`;
  return `${sourceName}: ${o.label}`;
}

/** windowAction finds the window's single option of a control kind. */
export function windowAction(d: Decision, kind: 'autofill' | 'undo_tap' | 'cancel_cast' | 'done'): Option | null {
  return d.options.find((o) => o.kind === kind) ?? null;
}

/** owesNothing reports an owed cost of zero (the pay-life grant's Pay case). */
export function owesNothing(c: PaymentCost): boolean {
  return c.generic === 0 && c.mana.every((n) => n === 0);
}
