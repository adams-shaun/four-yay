import type { View } from '../protocol';
import { STEPS } from './autopilot';
import { stepFullName } from './phases';

/**
 * passlabel is what the one gilt action button says (UI rework spec §3): it
 * NAMES what passing does — "Let Rhystic Study resolve" when the stack holds
 * something, "Move to Declare attackers" otherwise, "Pass the turn" at the
 * end of a turn — and becomes "Waiting" while a prompt needs an answer or
 * another seat has priority. It reads the view only; it decides nothing.
 */

export interface ActionLabel {
  /** the big word: Pass, or Waiting */
  title: string;
  /** what it does, or why it is waiting */
  sub: string;
  /** whether pressing it would pass */
  passes: boolean;
}

export interface PassLabelInput {
  view: View;
  /** the seat's pass option is live (a priority window this seat may pass) */
  passAvailable: boolean;
  /** a decision for this seat is waiting that pass cannot answer */
  prompt: boolean;
  /** the name of the seat the table is waiting on, when it is not this one */
  waitingOn: string | null;
}

/** nextStep is the wire step after `step`, or null at the end of the turn (or for an unknown step). */
export function nextStep(step: string): string | null {
  const order: readonly string[] = STEPS;
  const i = order.indexOf(step);
  if (i < 0 || i >= order.length - 1) return null;
  const next = order[i + 1];
  return next === 'cleanup' ? null : next;
}

export function passLabel(i: PassLabelInput): ActionLabel {
  if (i.passAvailable) {
    const top = i.view.stack.length > 0 ? i.view.stack[i.view.stack.length - 1] : null;
    if (top !== null) return { title: 'Pass', sub: `Let ${top.card?.name ?? top.name ?? 'the top object'} resolve`, passes: true };
    const next = nextStep(i.view.step);
    return { title: 'Pass', sub: next === null ? 'Pass the turn' : `Move to ${stepFullName(next)}`, passes: true };
  }
  if (i.prompt) return { title: 'Waiting', sub: 'Answer the prompt', passes: false };
  if (i.waitingOn !== null) return { title: 'Waiting', sub: `for ${i.waitingOn}`, passes: false };
  return { title: 'Waiting', sub: 'for the game', passes: false };
}
