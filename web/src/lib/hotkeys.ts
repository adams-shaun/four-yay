/**
 * hotkeys is the pure keyboard grammar behind the table's document-level
 * hotkeys (prio3): Space passes once, Enter arms End Turn, Shift+Enter arms
 * the hard skip, Escape cancels a live run, Ctrl+Shift+F toggles the
 * full-control preset. It is pure TypeScript — no DOM, no Svelte — so the
 * mapping and its guards are testable without a browser: the caller passes
 * the event-shaped object and a picker-open probe.
 *
 * The guards, in order:
 *  - a held Meta (Cmd) key: never a gorge hotkey — the OS/browser owns those;
 *  - an open modal picker (detected structurally by menu/dialog/listbox
 *    semantics, with the existing picker markers as compatibility hooks —
 *    see MODAL_PICKER_SELECTOR): its own keys govern, and a hotkey firing
 *    underneath it would act on a decision the player cannot currently
 *    see;
 *  - Escape is deliberately NOT guarded by the focus check: it is the panic
 *    key, and it must cancel a run wherever focus happens to be;
 *  - focus in an interactive element (button, link, input, textarea, select,
 *    contenteditable) before every other key: the element's own activation
 *    owns Space and Enter, firing the hotkey underneath would act twice, and
 *    Ctrl+Shift+F while TYPING is the find bar's grammar, never ours. This is
 *    one guard wider than the brief's input list on purpose: Space with focus
 *    on the PASS button would otherwise both activate the button and pass,
 *    posting the same intent twice;
 *  - Ctrl+Shift+F / Ctrl+Shift+O / Ctrl+Shift+[ / Ctrl+Shift+] before the
 *    plain-Ctrl check: Ctrl held ALONE is the hold-priority modifier for
 *    clicks (it is never a hotkey by itself — Ctrl+Shift alone is too easy to
 *    hit, hence the extra key). Ctrl+Shift+] / Ctrl+Shift+[ cycle the profile
 *    list (presets + saved profiles) forward and back; Ctrl+Shift+O toggles
 *    the Game Options panel. Ctrl+Shift+P is deliberately NOT used: Firefox
 *    opens a private window on it.
 */

export type HotkeyAction = 'pass' | 'end-turn' | 'hard-skip' | 'cancel-run' | 'toggle-full-control' | 'next-profile' | 'prev-profile' | 'toggle-options' | 'clear-sticky';

/**
 * MODAL_PICKER_SELECTOR is structural first: every open ARIA menu, dialog or
 * listbox suppresses table hotkeys, including a newly added picker that has
 * not learned a gorge-specific data marker yet. Native open dialogs and the
 * aria-modal contract are covered too. The existing OptionPicker/PileModal
 * markers remain as stable test and compatibility hooks.
 *
 * All matching roles currently belong to transient surfaces; there is no
 * always-mounted menu bar to exclude. If one is introduced, mark that
 * persistent element explicitly and exclude only it rather than weakening
 * this safety net.
 */
export const MODAL_PICKER_SELECTOR = [
  '[role="menu"]',
  '[role="dialog"]',
  '[role="listbox"]',
  '[aria-modal="true"]',
  'dialog[open]',
  '[data-option-picker]',
  '[data-pile-modal]',
].join(', ');

/** HotkeyEvent is the slice of KeyboardEvent the grammar reads, so tests can build it by hand. */
export interface HotkeyEvent {
  key: string;
  ctrlKey: boolean;
  shiftKey: boolean;
  metaKey: boolean;
  /**
   * code is the PHYSICAL key (`KeyboardEvent.code`, e.g. 'BracketRight'), so a
   * binding survives Shift changing the printable `key`: on a US layout
   * Ctrl+Shift+] reports key '}' but code 'BracketRight'. Optional because a
   * synthetic event may omit it; brackets fall back to the shifted `key`
   * values when it is absent.
   */
  code?: string;
  /** target is the event's original target; checked for interactive elements. */
  target?: EventTarget | null;
}

export function hotkeyAction(
  e: HotkeyEvent,
  pickerOpen: () => boolean = () => false,
): HotkeyAction | null {
  if (e.metaKey) return null;
  if (pickerOpen()) return null;
  if (e.key === 'Escape') return 'cancel-run';
  const t = e.target as { closest?: (sel: string) => unknown } | null | undefined;
  if (t && typeof t.closest === 'function' && t.closest('button, a, input, textarea, select, [contenteditable]')) return null;
  if (e.ctrlKey && e.shiftKey && (e.key === 'f' || e.key === 'F')) return 'toggle-full-control';
  if (e.ctrlKey && e.shiftKey && (e.key === 'O' || e.key === 'o')) return 'toggle-options';
  // Brackets are matched by PHYSICAL code first: Shift changes ] and [ to }
  // and { on a US layout, so `key` alone would never fire the shortcut. The
  // shifted `key` values are accepted as a fallback for a synthetic event
  // with no `code`.
  if (e.ctrlKey && e.shiftKey && (e.code === 'BracketRight' || e.key === '}' || e.key === ']')) return 'next-profile';
  if (e.ctrlKey && e.shiftKey && (e.code === 'BracketLeft' || e.key === '{' || e.key === '[')) return 'prev-profile';
  if (e.ctrlKey) return null;
  if (e.key === '5' && !e.shiftKey) return 'clear-sticky';
  if (e.key === ' ') return 'pass';
  if (e.key === 'Enter') return e.shiftKey ? 'hard-skip' : 'end-turn';
  return null;
}
