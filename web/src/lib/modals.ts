import { MODAL_PICKER_SELECTOR } from './hotkeys';

/**
 * modals.ts is the live-DOM half of the hotkey grammar's modal guard
 * (prio3 review r2): the pure grammar in hotkeys.ts takes a `pickerOpen`
 * callback, and this module is what that callback is in the wiring.
 *
 * MODAL_PICKER_SELECTOR (defined beside the grammar so the two cannot
 * drift) recognizes modal/picker semantics structurally: menu, dialog and
 * listbox roles, aria-modal, and an open native dialog. The pre-existing
 * OptionPicker and PileModal data markers remain included as stable hooks.
 * That structural net means a new correctly marked-up picker is safe without
 * requiring another component-specific selector.
 *
 * The probe reads the live document, so it is DOM work the pure grammar
 * deliberately does not do.
 */
export function modalPickerOpen(): boolean {
  return typeof document !== 'undefined' && document.querySelector(MODAL_PICKER_SELECTOR) !== null;
}

/**
 * clickedOutside reports whether a click landed outside root (the Options
 * control's outside-click test). It reads the event's composed path, which
 * is fixed when dispatch starts, not root.contains(target): Svelte flushes a
 * click handler's state change in a microtask that runs before the window
 * listener, so a clicked control inside root ("Add key", "Rename") may
 * already be detached by then, and contains() would call it outside. Nor is
 * a detached target treated as inside: a genuinely outside target can vanish
 * too (a hand card that gets played).
 */
export function clickedOutside(event: Event, root: Node): boolean {
  return !event.composedPath().includes(root);
}
