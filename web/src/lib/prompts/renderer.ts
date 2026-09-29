import type { Decision } from '../../protocol';
import { isSearchPick } from '../search';
import { isNamePick } from '../name-pick';
import { isDiscardPick } from '../discard';
import { manaWindow } from '../announcepay';
import { mulliganPhase } from './decision';

/**
 * RendererKind names the one renderer a decision is drawn by (UI rework
 * spec §4: "one renderer per decision kind"). It is resolved from wire facts
 * only — the decision kind first, then the option-shape families that cut
 * across kinds (a discard pick is a KModes or a KChoose; a library search
 * and a name pick are KChoose) — never from label text.
 *
 *  - mulligan: the London keep/bottom round (a mulligan whose option kinds
 *    this layout does not cover falls through to `list`, so no option is
 *    ever unreachable).
 *  - arrange / discard / search / name / payment: the card-face and picker
 *    families that already had their own layout.
 *  - priority: the action list under the ACTIONS strip; never the dock.
 *  - target: chips with art thumbnails, highlighted on the board.
 *  - attackers / blockers: assigned on the board, summarised in the dock.
 *  - list: every other kind — modes, choose, replacement, trigger_order,
 *    trigger_optional, commander_zone, starting_player — as numbered rows;
 *    single- vs multi-select comes from the decision's own min/max.
 */
export type RendererKind =
  | 'mulligan' | 'arrange' | 'discard' | 'search' | 'name' | 'payment'
  | 'priority' | 'target' | 'attackers' | 'blockers' | 'list';

/** rendererFor resolves a decision's renderer, in the precedence SeatPanel's old branch chain used. */
export function rendererFor(d: Decision): RendererKind {
  if (mulliganPhase(d) !== null) return 'mulligan';
  if (d.kind === 'arrange') return 'arrange';
  if (isDiscardPick(d)) return 'discard';
  if (isSearchPick(d)) return 'search';
  if (isNamePick(d)) return 'name';
  if (manaWindow(d) !== null) return 'payment';
  switch (d.kind) {
    case 'priority': return 'priority';
    case 'target': return 'target';
    case 'attackers': return 'attackers';
    case 'blockers': return 'blockers';
    default: return 'list';
  }
}

/**
 * Placement is where a renderer is mounted: the board-centred mulligan
 * panel, the ACTIONS strip/flyout, or the prompt dock (rail or floating).
 */
export type Placement = 'board' | 'flyout' | 'strip' | 'dock';

/**
 * dockAnswers reports whether the prompt dock is the answer surface for a
 * decision: every kind except priority (the action button, spec §4) and the
 * London mulligan (it keeps the board-centred panel: the opening hand needs
 * the board's width). While a dock is mounted, the ACTIONS strip defers
 * exactly these decisions to it.
 */
export function dockAnswers(d: Decision | null): boolean {
  if (d === null) return false;
  const r = rendererFor(d);
  return r !== 'priority' && r !== 'mulligan';
}
