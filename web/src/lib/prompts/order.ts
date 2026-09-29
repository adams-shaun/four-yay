import type { Decision, Option } from '../../protocol';
import { searchOptions } from '../search';
import { NAME_PICK_RENDER_LIMIT, nameOptions } from '../name-pick';
import { manaSourceRows, windowAction } from '../announcepay';
import { genericListOptions, mulliganPhase, primaryOf } from './decision';
import { rendererFor } from './renderer';

/** NO_BASES is the empty paymentBases every non-priority decision has. */
const NO_BASES: ReadonlySet<number> = new Set<number>();

/** OrderContext is the display-only state a rendered order depends on: the picker filter the search and name-pick layouts share. */
export interface OrderContext {
  filter: string;
}

/**
 * orderedOptions is the options a decision's renderer draws as numbered
 * rows, in exactly the order they appear on screen (UI rework spec §1:
 * "Pick 1–9 acts only where the on-screen order is the decision's order …
 * until sub-project 4 numbers options by rendered order"). Each renderer
 * numbers its rows from this list and the pick-N hotkeys resolve digit N
 * through it, so the digit on a row and the key that answers it cannot
 * disagree.
 *
 *  - priority: null. Priority is the action button, not a numbered list
 *    (its pass, concede and payment actions all sit outside the list).
 *  - mulligan keep: the keep/mulligan buttons; bottom: the hand's cards.
 *  - arrange, discard: d.options unchanged.
 *  - search: sorted A→Z and filtered by the typed text (searchOptions).
 *  - name: filtered, and capped at the render limit (nameOptions).
 *  - payment: the mana abilities grouped by source in offer order (the
 *    panel's rows), then Auto-fill, Pay, Undo last tap and Cancel cast —
 *    the panel's own button order.
 *  - everything else: the generic rows — every option except a separately
 *    drawn pass/resolve primary and concede.
 */
export function orderedOptions(d: Decision | null, ctx: OrderContext): Option[] | null {
  if (d === null) return null;
  switch (rendererFor(d)) {
    case 'priority':
      return null;
    case 'mulligan': {
      const m = mulliganPhase(d);
      if (m === null) return null;
      return m.phase === 'keep' ? m.choices : m.cards;
    }
    case 'arrange':
    case 'discard':
      return d.options;
    case 'search':
      return searchOptions(d, ctx.filter);
    case 'name':
      return nameOptions(d, ctx.filter).slice(0, NAME_PICK_RENDER_LIMIT);
    case 'payment': {
      const out: Option[] = manaSourceRows(d, null).flatMap((row) => row.options);
      for (const kind of ['autofill', 'done', 'undo_tap', 'cancel_cast'] as const) {
        const o = windowAction(d, kind);
        if (o !== null) out.push(o);
      }
      return out;
    }
    default:
      return genericListOptions(d, primaryOf(d), NO_BASES, false);
  }
}

/** renderedOrder is orderedOptions as wire indexes. */
export function renderedOrder(d: Decision | null, ctx: OrderContext): number[] | null {
  return orderedOptions(d, ctx)?.map((o) => o.index) ?? null;
}

/** MAX_DIGIT is the last numbered row: the pick hotkeys stop at 9. */
export const MAX_DIGIT = 9;

/** digitMap numbers the first nine rows of an order, 1-based, keyed by wire index. Rows past nine carry no digit. */
export function digitMap(order: readonly number[] | null): Map<number, number> {
  const out = new Map<number, number>();
  (order ?? []).slice(0, MAX_DIGIT).forEach((index, i) => out.set(index, i + 1));
  return out;
}
