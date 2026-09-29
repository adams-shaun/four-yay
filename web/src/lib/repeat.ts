import type { Decision, View } from '../protocol';
import { findCardAnywhere } from './board';
import { isManaColourChoice } from './sticky';

export interface RepeatPlan {
  sourceName: string;
  abilityText: string;
  target: number;
  done: number;
  ownStackIds: Set<number>;
}
export const REPEAT_MAX = 999;
export const REPEAT_BEAT_MS = 30;
export const REPEAT_PASSES_PER_ITERATION = 64;

export type RepeatHalt = 'done' | 'not_offered' | 'opponent_stack' | 'unanswered_decision'
  | 'game_over' | 'cancelled' | 'undo' | 'post_error' | 'payment';
export type RepeatStep =
  | { act: 'activate'; index: number }
  | { act: 'pass'; index: number }
  | { act: 'halt'; reason: RepeatHalt };

export function repeatTarget(n: number): number {
  return Number.isFinite(n) ? Math.max(1, Math.min(REPEAT_MAX, Math.trunc(n))) : 10;
}

/** The driver owns the counter, not the shared AUTO_PASS_CAP. */
export function repeatPassAllowed(passes: number): boolean {
  return passes < REPEAT_PASSES_PER_ITERATION;
}

/** One-pick mana colours and optional pay/decline prompts get sticky first
 * refusal. Actual payment windows and multi-mana allocations require the pilot. */
export function repeatPayment(d: Decision): boolean {
  return d.kind !== 'priority' && (!!d.mana_payment || d.options.some((o) =>
    (o.kind === 'mana' && !isManaColourChoice(d)) || o.kind === 'activate' || o.kind === 'autofill'));
}

/** Pure classification. Counters, sticky answers and external lifecycle halts
 * (cancel/undo/POST errors) belong to the driver. Never mutates the plan. */
export function repeatStep(plan: RepeatPlan, d: Decision, view: View, seat: number): RepeatStep {
  if (view.over) return { act: 'halt', reason: 'game_over' };
  if (d.kind !== 'priority') return { act: 'halt', reason: repeatPayment(d) ? 'payment' : 'unanswered_decision' };
  if (view.stack.some((s) => s.controller !== seat && !plan.ownStackIds.has(s.id))) {
    return { act: 'halt', reason: 'opponent_stack' };
  }
  if (view.stack.length) {
    const pass = d.options.find((o) => o.kind === 'pass');
    return pass ? { act: 'pass', index: pass.index } : { act: 'halt', reason: 'unanswered_decision' };
  }
  if (plan.done >= plan.target) return { act: 'halt', reason: 'done' };
  const option = d.options.find((o) => (o.kind === 'ability' || o.kind === 'activate') && o.label === plan.abilityText
    && o.obj !== undefined && findCardAnywhere(view, o.obj)?.name === plan.sourceName);
  return option ? { act: 'activate', index: option.index } : { act: 'halt', reason: 'not_offered' };
}
