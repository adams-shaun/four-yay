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
 *  - focus in a text-entry control (input, textarea, select, contenteditable):
 *    EVERY other key is the control's — a digit typed into a profile name is
 *    text, and Ctrl+Shift+F while typing is the find bar's grammar, never
 *    ours;
 *  - focus on an activatable control (button, link, summary, role=button):
 *    only Space and Enter (NumpadEnter included) are the control's, because
 *    its native activation owns them — Space with focus on the PASS button
 *    would otherwise both activate the button and pass, posting the same
 *    intent twice. Every other chord still fires: closing the Options panel
 *    returns focus to its button, and a guard on every key there left every
 *    hotkey dead until the player clicked the board;
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

import { defaultKeymap, eventCode, matchKeymap, type KeyAction, type Keymap } from './keymap';

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

/** TEXT_ENTRY is focus that owns every key: typing, never a hotkey. */
const TEXT_ENTRY = 'input, textarea, select, [contenteditable]:not([contenteditable="false"])';
/** ACTIVATABLE is focus whose native activation owns Space and Enter only. */
const ACTIVATABLE = 'button, a[href], summary, [role="button"]';

type Closest = { closest?: (sel: string) => { matches?: unknown } | null };

/** focusOwnsKey reports whether the focused element, not the table, owns this key. */
function focusOwnsKey(e: HotkeyEvent): boolean {
  const t = e.target as Closest | null | undefined;
  if (!t || typeof t.closest !== 'function') return false;
  if (t.closest(TEXT_ENTRY)) return true;
  const control = t.closest(ACTIVATABLE);
  if (!control) return false;
  // Something that answers closest() but is not an Element cannot say what
  // it is; treat it as a text field, the direction that never fires.
  if (typeof control.matches !== 'function') return true;
  const code = eventCode(e);
  return code === 'Space' || code === 'Enter';
}

export function hotkeyAction(
  e: HotkeyEvent,
  pickerOpen: () => boolean = () => false,
  keymap: Keymap = DEFAULTS,
): HotkeyAction | null {
  if (e.metaKey) return null;
  if (pickerOpen()) return null;
  if (e.key === 'Escape') return 'cancel-run';
  if (focusOwnsKey(e)) return null;
  if (e.ctrlKey && !e.shiftKey) return null; // Ctrl alone is the hold-priority modifier, never a hotkey
  return matchKeymap(keymap, e);
}

/**
 * closesOptionsPanel is the open Game Options panel's own keyboard close:
 * Escape, or the toggle-options chord (as bound). The panel is a role=dialog,
 * so hotkeyAction's modal guard holds every table hotkey while it is open —
 * including the chord that opened it — and the route closes it through this
 * instead. The guard itself stays: with focus in the panel, Space must not
 * pass priority. Never while typing, while the Keys editor records a chord,
 * with Meta held, or on a press a hotkey already consumed (the one that
 * OPENED the panel reaches the route's listener too).
 */
export function closesOptionsPanel(
  e: HotkeyEvent & { defaultPrevented?: boolean },
  keymap: Keymap,
  capturing: boolean,
): boolean {
  if (e.key === 'Escape') return true;
  if (e.metaKey || capturing || e.defaultPrevented) return false;
  const t = e.target as Closest | null | undefined;
  if (t && typeof t.closest === 'function' && t.closest(TEXT_ENTRY)) return false;
  return matchKeymap(keymap, e) === 'toggle-options';
}
