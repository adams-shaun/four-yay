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
 *  - Ctrl without Shift: Ctrl held ALONE is the hold-priority modifier for
 *    clicks, so it is never a hotkey and never a binding (Ctrl+Shift alone is
 *    too easy to hit, hence the extra key). Ctrl+Shift+P is deliberately NOT
 *    a default: Firefox opens a private window on it.
 *
 * After the guards, the chord's meaning comes from the keymap (lib/keymap):
 * a rebindable table matched on the PHYSICAL key. The defaults reproduce the
 * original grammar — Ctrl+Shift+] / Ctrl+Shift+[ cycle the profile list,
 * Ctrl+Shift+O toggles the Game Options panel — and add the keymap's newer
 * actions.
 */

import { defaultKeymap, matchKeymap, type KeyAction, type Keymap } from './keymap';

export type HotkeyAction = KeyAction;

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
   * synthetic event may omit it; keymap.eventCode derives one from `key`
   * when it is absent.
   */
  code?: string;
  altKey?: boolean;
  /** target is the event's original target; checked for interactive elements. */
  target?: EventTarget | null;
}

const DEFAULTS = defaultKeymap();

export function hotkeyAction(
  e: HotkeyEvent,
  pickerOpen: () => boolean = () => false,
  keymap: Keymap = DEFAULTS,
): HotkeyAction | null {
  if (e.metaKey) return null;
  if (pickerOpen()) return null;
  if (e.key === 'Escape') return 'cancel-run';
  const t = e.target as { closest?: (sel: string) => unknown } | null | undefined;
  if (t && typeof t.closest === 'function' && t.closest('button, a, input, textarea, select, [contenteditable]')) return null;
  if (e.ctrlKey && !e.shiftKey) return null; // Ctrl alone is the hold-priority modifier, never a hotkey
  return matchKeymap(keymap, e);
}
