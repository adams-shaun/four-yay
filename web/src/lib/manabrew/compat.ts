import { safeStorage } from '../storage';
import type { AgentPrompt, AvailableAction, ClientMessage } from './wire';

/**
 * compat.ts is the ManaBrew compatibility rule: gorge gives priority at every
 * step, while ManaBrew's own engine never poses a chooseAction the player can
 * do nothing with (0 of 196 in the mbdelta transcripts, against gorge's 1001
 * of 1353). So that a/b tests compare like with like, the adapter answers
 * such a prompt with a pass before it is ever shown.
 *
 * "Nothing to do" is an empty action list, or one holding only mana
 * abilities (and undoing mana already made): mana with nothing to spend it
 * on is not a play (operator ruling 2026-09-29). A cast or a non-mana
 * ability means the prompt is shown as usual.
 *
 * A client-side setting, default ON, active only on the ManaBrew wire;
 * native play is untouched.
 */

export const COMPAT_KEY = 'gorge.mbcompat.v1';

export function loadCompat(store: Storage | null = safeStorage()): boolean {
  try {
    return store?.getItem(COMPAT_KEY) !== 'off';
  } catch {
    return true;
  }
}

export function saveCompat(on: boolean, store: Storage | null = safeStorage()): void {
  try {
    if (on) store?.removeItem(COMPAT_KEY);
    else store?.setItem(COMPAT_KEY, 'off');
  } catch {
    // A browser refusing site data keeps the default.
  }
}

function idle(a: AvailableAction): boolean {
  return a.isManaAbility === true || a.type === 'undoMana';
}

/** nothingToDo reports whether a prompt is a chooseAction whose only actions are mana abilities (or none). */
export function nothingToDo(p: AgentPrompt): boolean {
  return p.input.type === 'chooseAction' && p.input.actions.every(idle);
}

/** compatPass is the automatic answer to such a prompt: pass priority. */
export function compatPass(p: AgentPrompt): ClientMessage {
  return { kind: 'response', promptId: p.promptId, action: { type: 'chooseAction', output: { type: 'pass', exhaustStack: false } } };
}
