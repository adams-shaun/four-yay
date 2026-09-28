import type { View } from '../protocol';

/**
 * autolog is the client-local, non-engine log of automatic passes (prio5).
 *
 * WHAT IT IS NOT: it is not the event-sourced log. The transcript's real
 * lines are server-described events (DvrState.events, host/describe.go);
 * those are hash-chained and replayed, and nothing here touches them — no
 * event is emitted, nothing is posted to the server, and a note exists only
 * in the browser tab that made the pass. It exists because Forge's players
 * found invisible instant passing disorienting: with casual pacing the seat
 * passes dozens of windows a turn, and `settings.logAutoPasses` (prio2,
 * editable since prio4) is the player's "show me what was skipped".
 *
 * SHAPE: one note per automatic pass, appended in pass order, each carrying
 * the engine turn it was made in so a reader can place it. The transcript
 * has no grouping primitive — its rows are event-seq-keyed scrub buttons and
 * a local note has no seq — so notes are one line each, and the list is
 * CAPPED: the oldest notes fall off past AUTO_LOG_CAP, so a long auto-passed
 * stretch (up to AUTO_PASS_CAP = 40 windows in one unbroken run) can add at
 * most AUTO_LOG_CAP lines to the log and can never flood it.
 */

export interface AutoPassDiagnostics {
  verdict: string;
  optionKinds: string[];
  actionableOptions: string[];
  castableAfterTap: string[];
  respondableOption: boolean;
  respondableAfterTap: boolean;
  yieldsHit: boolean;
  /** The seat-redacted view captured at pass time; serialized only when expanded. */
  view: View;
}

export interface AutoPassLog {
  /** monotonic local id, the transcript's each-key (notes have no seq). */
  id: number;
  /** the engine turn the pass was made in (view.turn at dispatch). */
  turn: number;
  /** the rendered note, e.g. "Auto-passed: Ana's end step". */
  text: string;
  /** Optional advanced detail for an automatic pass. */
  diagnostics?: AutoPassDiagnostics;
}

/**
 * AUTO_LOG_CAP bounds the local note list; the oldest note is dropped when
 * it is exceeded. Forty matches the seat panel's own runaway cap, so one
 * full cap-tripping run fits and everything before it scrolls off.
 */
export const AUTO_LOG_CAP = 40;

let nextId = 1;

/** pushAutoPassLog appends one note (or replaces the list when empty) and caps it, dropping the OLDEST entries first. */
export function pushAutoPassLog(notes: readonly AutoPassLog[], text: string, turn: number, cap = AUTO_LOG_CAP, diagnostics?: AutoPassDiagnostics): AutoPassLog[] {
  const out = [...notes, { id: nextId++, turn, text, ...(diagnostics ? { diagnostics } : {}) }];
  return out.length > cap ? out.slice(out.length - cap) : out;
}

/**
 * AutoPassKind is which machine path made the pass — the same six the seat
 * panel counts separately (autoPassed, emptySkipped, actPassed, runPassed).
 */
export type AutoPassKind = 'auto' | 'empty' | 'act' | 'end-turn' | 'hard-skip' | 'resolve-all';

/**
 * stepLabel turns the wire's step name (state/ids.go: "main1", "end",
 * "declare-attackers", ...) into the words the log note uses. "main 2" and
 * "end step" read as English; the generic case just un-hyphenates.
 */
export function stepLabel(step: string): string {
  switch (step) {
    case 'main1': return 'main 1';
    case 'main2': return 'main 2';
    case 'draw': return 'draw step';
    case 'end': return 'end step';
    case 'end-combat': return 'end of combat';
    default: return step.replace(/-/g, ' ');
  }
}

/**
 * autoPassLogText words one automatic pass for the log. The stack case names
 * the object that is about to resolve (the TOP of the stack — the same
 * object decide()'s stack rules and the seat panel read); the step case
 * names whose turn and which step. Both wordings are the two the brief
 * asks to distinguish ("Auto-passed: Lightning Bolt resolving" /
 * "Auto-passed: opponent's end step"); the one-shot runs say so in their own
 * register ("End turn: passed main 2").
 */
export function autoPassLogText(kind: AutoPassKind, view: View, seat: number): string {
  const step = stepLabel(view.step);
  if (kind === 'end-turn') return `End turn: passed ${step}`;
  if (kind === 'hard-skip') return `Skip turn: passed ${step}`;
  if (kind === 'resolve-all') {
    const top = view.stack.length > 0 ? view.stack[view.stack.length - 1] : null;
    return top !== null ? `Resolve all: passed ${top.name || 'an ability'} resolving` : `Resolve all: passed ${step}`;
  }
  const top = view.stack.length > 0 ? view.stack[view.stack.length - 1] : null;
  if (top !== null) return `Auto-passed: ${top.name || 'an ability'} resolving`;
  const who = view.active === seat
    ? 'your'
    : `${view.players?.find((p) => p.seat === view.active)?.name ?? 'opponent'}'s`;
  return `Auto-passed: ${who} ${step}`;
}
