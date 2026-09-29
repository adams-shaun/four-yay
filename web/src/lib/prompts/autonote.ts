import type { StopReason } from '../autopilot';

/**
 * AUTO_PASS_CAP bounds how many priority windows auto may pass in an
 * unbroken run before it switches itself off. A runaway autopasser is not a
 * cosmetic bug: it hammers the server and it passes the game away in
 * silence. Forty is roughly two turn cycles of an uneventful four-seat
 * game — long enough that a normal quiet stretch never trips it, short
 * enough that a stuck loop is caught in seconds.
 */
export const AUTO_PASS_CAP = 40;

/**
 * AutoOffReason is why auto is no longer running, as distinct from
 * StopReason (why auto declined THIS window but stays armed). The two are
 * separate vocabularies because they need separate words on screen: one is
 * "waiting for you here", the other is "auto is off now".
 */
export type AutoOffReason = 'loop' | 'cap';

/**
 * AutoNote is the one line the panel shows about what auto is doing. It is
 * an enum-shaped value, never a string to print: autoNoteText turns it into
 * words, so no StopReason identifier can reach the screen.
 */
export type AutoNote =
  | { kind: 'off' }
  | { kind: 'paused' }
  | { kind: 'skip-off'; reason: AutoOffReason }
  | { kind: 'skipped'; count: number }
  | { kind: 'armed' }
  | { kind: 'passing'; count: number }
  /**
   * detail (fb-20260916T225211Z) carries the actionable option labels that
   * made a stop-set window stop-worthy — autoNoteText folds it into the
   * note text, so the player reads WHAT the window offered, not just that a
   * stop they set fired. Absent (or empty) for every other reason and for a
   * 'forced' stop with nothing to do — the base wording is complete there.
   */
  | { kind: 'waiting'; reason: StopReason; detail?: string }
  | { kind: 'stopped'; reason: AutoOffReason }
  | { kind: 'end-turn-armed' }
  | { kind: 'end-turn-passing'; count: number }
  | { kind: 'end-turn-stopped'; reason: StopReason | AutoOffReason }
  | { kind: 'skip-turn-armed' }
  | { kind: 'skip-turn-passing'; count: number }
  | { kind: 'skip-turn-stopped'; reason: StopReason | AutoOffReason }
  | { kind: 'resolve-all-armed' }
  | { kind: 'resolve-all-passing'; count: number }
  | { kind: 'resolve-all-stopped'; reason: StopReason | AutoOffReason }
  | { kind: 'act-passed'; count: number };

const WAITING_TEXT: Record<StopReason, string> = {
  'disabled': 'Auto is off.',
  'not-priority': 'Auto is waiting: this decision needs you, not a pass.',
  'unexpected-shape': 'Auto is waiting: it does not recognise this window.',
  'stop-set': 'Auto stopped here: you set a stop on this step.',
  'opponent-object': "Auto stopped here: an opponent's object is on the stack and you can respond.",
  'own-object': 'Auto stopped here: your own object is on the stack and you can respond.',
  'breakpoint': 'Auto paused here: a pause you set fired.',
};

const OFF_TEXT: Record<AutoOffReason, string> = {
  'loop': 'Auto switched itself off: the same decision came back after it answered. Press the Auto switch to rearm it.',
  'cap': `Auto switched itself off after ${AUTO_PASS_CAP} passes in a row. Press the Auto switch to rearm it.`,
};

/**
 * RUN_*_TEXT re-words a stop reason for the one-shot runs, which are not
 * "waiting" — a stop ENDS an End Turn / hard-skip run. The wording is
 * reason-loyal (the same fact the Auto wording states), only re-anchored.
 */
const RUN_WAITING_TEXT: Record<StopReason, string> = {
  'disabled': 'the Auto switch is off.',
  'not-priority': 'this decision needs you, not a pass.',
  'unexpected-shape': 'it does not recognise this window.',
  'stop-set': 'you set a stop on this step.',
  'opponent-object': "an opponent's object is on the stack and you can respond.",
  'own-object': 'your own object is on the stack and you can respond.',
  'breakpoint': 'a pause you set fired.',
};
const RUN_OFF_TEXT: Record<AutoOffReason, string> = {
  'loop': 'the same decision came back after it answered.',
  'cap': `it passed ${AUTO_PASS_CAP} windows in a row.`,
};

/** autoNoteText renders an AutoNote as plain words. No enum identifier ever reaches the screen. */
export function autoNoteText(note: AutoNote): string {
  switch (note.kind) {
    case 'off':
      return 'Auto is off. You answer every window that offers you something to do.';
    case 'paused':
      return 'Undo paused automatic passing so it cannot re-answer the window you rewound to. Press the Auto switch (or apply a preset) to start it again.';
    case 'skip-off':
      return `${OFF_TEXT[note.reason]} Empty windows are no longer skipped either.`;
    case 'skipped':
      return note.count === 1
        ? 'Passed 1 window where you had nothing to do.'
        : `Passed ${note.count} windows where you had nothing to do.`;
    case 'armed':
      return 'Auto is on. It passes windows where you have nothing to do, and stops at your stops.';
    case 'passing':
      return note.count === 1
        ? 'Auto passed 1 priority window.'
        : `Auto passed ${note.count} priority windows.`;
    case 'waiting':
      // stop-set with actionable labels (fb-20260916T225211Z): the base line
      // alone read "you set a stop" without saying WHY the window was worth
      // stopping at — the exact gap the Deadly Rollick free-cast report is
      // about. The labels come from actionables(), the same predicate the
      // smart step rule consulted, so the note cannot name something the
      // stop did not actually stop for. Derived from the base string (the
      // trailing full stop is dropped, the clause spliced in) so the wording
      // stays in one place.
      if (note.reason === 'breakpoint' && note.detail) return `Auto paused here: ${note.detail}.`;
      if (note.reason === 'stop-set' && note.detail) {
        return `${WAITING_TEXT[note.reason].replace(/\.$/, '')} and you can act — ${note.detail}.`;
      }
      return WAITING_TEXT[note.reason];
    case 'stopped':
      return OFF_TEXT[note.reason];
    case 'end-turn-armed':
      return 'End Turn: passing the rest of this turn — it still stops for opponent plays. Esc cancels.';
    case 'end-turn-passing':
      return note.count === 1
        ? 'End Turn passed 1 priority window.'
        : `End Turn passed ${note.count} priority windows.`;
    case 'end-turn-stopped':
      return `End Turn stopped: ${
        note.reason in RUN_WAITING_TEXT
          ? RUN_WAITING_TEXT[note.reason as StopReason]
          : RUN_OFF_TEXT[note.reason as AutoOffReason]
      }`;
    case 'skip-turn-armed':
      return 'Skipping turn — Esc to stop.';
    case 'skip-turn-passing':
      return note.count === 1
        ? 'Skipping turn: passed 1 priority window.'
        : `Skipping turn: passed ${note.count} priority windows.`;
    case 'skip-turn-stopped':
      return `Skipping turn stopped: ${
        note.reason in RUN_WAITING_TEXT
          ? RUN_WAITING_TEXT[note.reason as StopReason]
          : RUN_OFF_TEXT[note.reason as AutoOffReason]
      }`;
    case 'resolve-all-armed':
      return 'Resolve All: passing the stack as it stands — a NEW opponent play or a decision that needs you stops it. Esc cancels.';
    case 'resolve-all-passing':
      return note.count === 1
        ? 'Resolve All passed 1 priority window.'
        : `Resolve All passed ${note.count} priority windows.`;
    case 'resolve-all-stopped':
      return `Resolve All stopped: ${
        note.reason in RUN_WAITING_TEXT
          ? RUN_WAITING_TEXT[note.reason as StopReason]
          : RUN_OFF_TEXT[note.reason as AutoOffReason]
      }`;
    case 'act-passed':
      return note.count === 1
        ? 'Passed 1 priority window after your action.'
        : `Passed ${note.count} priority windows after your action.`;
  }
}

/** runStopNote is a one-shot run's stopped note, worded in that run's own register. */
export function runStopNote(mode: 'end-turn' | 'hard-skip' | 'resolve-all', reason: StopReason | AutoOffReason): AutoNote {
  const kind = mode === 'end-turn'
    ? 'end-turn-stopped'
    : mode === 'resolve-all'
    ? 'resolve-all-stopped'
    : 'skip-turn-stopped';
  return { kind, reason } as AutoNote;
}
