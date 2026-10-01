import type { FocusOwnership } from './hotkeys';

/**
 * hotkeyhint is the ONE home for the transient "why didn't Space reach the
 * table?" cue (fb-20260929T080219Z). The hotkey grammar deliberately gives a
 * focused activatable control its own Space/Enter (hotkeys.ts
 * focusOwnership), so a player who Tabs to the PASS button and presses Space
 * gets no pass and — before this store — no explanation either. Both
 * capture-phase keydown listeners (HotButtonStrip, ViewHotkeys) call note()
 * when they suppress a key for the 'activatable-space-enter' reason; a
 * successful hotkey calls clear().
 *
 * The store keeps only the current hint and a monotonically increasing id, so
 * a later note() supersedes an earlier one and a presenter can tell a fresh
 * hint from a re-render of the same one. It is deliberately dumb: EXPIRY
 * belongs to the presenter component (a setTimeout in HotkeyHint.svelte),
 * never here — a timer in a module singleton would outlive the mount and
 * fire against a torn-down reactive tree. It imports only a TYPE, never
 * Svelte component code, so it stays safe for a pure-side test.
 */
export interface HotkeyHint {
  /** id increments on every note(), so a presenter can restart its expiry timer. */
  id: number;
  reason: FocusOwnership;
}

export class HotkeyHinter {
  /** current is the hint to show, or null when nothing is suppressed. */
  current = $state<HotkeyHint | null>(null);
  #id = 0;

  /** note records a suppression; a later note supersedes any earlier one. */
  note(reason: FocusOwnership): void {
    this.#id += 1;
    this.current = { id: this.#id, reason };
  }

  /** clear drops the hint — a successful hotkey, or the presenter's expiry. */
  clear(): void {
    this.current = null;
  }
}

/** The module singleton both hotkey listeners write and the chip reads. */
export const hotkeyHinter = new HotkeyHinter();
