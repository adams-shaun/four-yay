/**
 * keymap is the pure, rebindable binding table behind the table's hotkeys
 * (UI rework spec §1). hotkeys.ts keeps the guards (Meta, modal pickers,
 * focused inputs, the Escape panic key) and asks matchKeymap which action a
 * chord means. Bindings match the PHYSICAL key (KeyboardEvent.code), so a
 * chord survives Shift changing the printable key. Ctrl without Shift is
 * reserved: Ctrl held alone is the hold-priority click modifier.
 */
import type { HotkeyEvent } from './hotkeys';

type Nine = 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9;
export type KeyAction =
  | 'pass' | 'end-turn' | 'hard-skip' | 'cancel-run' | 'undo' | 'resolve-all'
  | 'confirm' | `pick-${Nine}` | 'attack-all' | 'no-blocks' | 'auto-pay'
  | 'toggle-full-control' | 'next-profile' | 'prev-profile' | `profile-${Nine}`
  | 'toggle-options' | 'show-keys';

const NINE: Nine[] = [1, 2, 3, 4, 5, 6, 7, 8, 9];
const PICKS = NINE.map((n) => `pick-${n}` as const);
const PROFILES = NINE.map((n) => `profile-${n}` as const);

export const ACTION_GROUPS: readonly { title: string; actions: readonly KeyAction[] }[] = [
  { title: 'Priority', actions: ['pass', 'end-turn', 'hard-skip', 'cancel-run', 'undo', 'resolve-all'] },
  { title: 'Decisions', actions: ['confirm', ...PICKS, 'attack-all', 'no-blocks', 'auto-pay'] },
  { title: 'Profiles', actions: ['toggle-full-control', 'next-profile', 'prev-profile', ...PROFILES] },
  { title: 'View', actions: ['toggle-options', 'show-keys'] },
];
export const KEY_ACTIONS: readonly KeyAction[] = ACTION_GROUPS.flatMap((g) => g.actions);

export const ACTION_LABELS: Record<KeyAction, string> = {
  'pass': 'Pass priority once',
  'end-turn': 'End turn (pass until something needs me)',
  'hard-skip': 'Skip the rest of the turn',
  'cancel-run': 'Stop an End Turn / Skip / Resolve All run',
  'undo': 'Undo',
  'resolve-all': 'Resolve the whole stack',
  'confirm': 'Confirm the current choice (Done)',
  ...Object.fromEntries(PICKS.map((a, i) => [a, `Pick option ${i + 1}`])),
  'attack-all': 'Attack with all (select every creature that can attack)',
  'no-blocks': 'Declare no blocks',
  'auto-pay': 'Auto-pay (Auto-fill the mana being paid)',
  'toggle-full-control': 'Toggle full control',
  'next-profile': 'Next flow profile',
  'prev-profile': 'Previous flow profile',
  ...Object.fromEntries(PROFILES.map((a, i) => [a, `Switch to flow profile ${i + 1}`])),
  'toggle-options': 'Open or close game options',
  'show-keys': 'Show the keyboard shortcuts',
} as Record<KeyAction, string>;

export interface Binding {
  code: string;
  ctrl: boolean;
  shift: boolean;
  alt: boolean;
}
export type Keymap = Record<KeyAction, Binding[]>;

export const MAX_BINDINGS = 2;
export const KEYMAP_KEY = 'gorge.keymap.v1';

const b = (code: string, mods: Partial<Omit<Binding, 'code'>> = {}): Binding => ({ code, ctrl: false, shift: false, alt: false, ...mods });
const CS = { ctrl: true, shift: true };

export function defaultKeymap(): Keymap {
  const k = Object.fromEntries(KEY_ACTIONS.map((a) => [a, [] as Binding[]])) as Keymap;
  k['pass'] = [b('Space'), b('Space', { shift: true })]; // Shift+Space passed before the keymap too
  k['end-turn'] = [b('Enter')];
  k['hard-skip'] = [b('Enter', { shift: true })];
  k['cancel-run'] = [b('Escape')];
  k['undo'] = [b('KeyZ', CS)];
  k['toggle-full-control'] = [b('KeyF', CS)];
  k['toggle-options'] = [b('KeyO', CS)];
  k['next-profile'] = [b('BracketRight', CS)];
  k['prev-profile'] = [b('BracketLeft', CS)];
  k['show-keys'] = [b('Slash', { shift: true })];
  // The prompt system's quick answers (UI rework spec §1/§4). Shifted, so
  // ordinary typing is still never a hotkey; bare letters stay free.
  k['attack-all'] = [b('KeyA', { shift: true })];
  k['no-blocks'] = [b('KeyN', { shift: true })];
  k['auto-pay'] = [b('KeyP', { shift: true })];
  NINE.forEach((n) => {
    k[`pick-${n}`] = [b(`Digit${n}`)];
    k[`profile-${n}`] = [b(`Digit${n}`, CS)];
  });
  return k;
}

const SHIFTED: Record<string, string> = { '}': 'BracketRight', ']': 'BracketRight', '{': 'BracketLeft', '[': 'BracketLeft', '?': 'Slash', '/': 'Slash' };

/** eventCode is the event's physical code, or one derived from `key` for a synthetic event that omits it. */
export function eventCode(e: { code?: string; key: string }): string {
  if (e.code === 'NumpadEnter') return 'Enter'; // one Enter binding covers both keys, as before the keymap
  if (e.code) return e.code;
  if (e.key === ' ') return 'Space';
  if (e.key in SHIFTED) return SHIFTED[e.key];
  if (/^[a-z]$/i.test(e.key)) return `Key${e.key.toUpperCase()}`;
  if (/^[0-9]$/.test(e.key)) return `Digit${e.key}`;
  return e.key;
}

const same = (x: Binding, y: Binding) => x.code === y.code && x.ctrl === y.ctrl && x.shift === y.shift && x.alt === y.alt;
const MODIFIER_OR_LOCK = /^(?:(?:Shift|Control|Alt|Meta|OS)(?:Left|Right)?|CapsLock|NumLock|ScrollLock|AltGraph|Fn|FnLock|ContextMenu)$/;

/**
 * isModifierOrLockCode is every key that is never a binding on its own: the
 * modifiers (either side, OS included), the lock keys and AltGraph/Fn/
 * ContextMenu. Capture keeps waiting past them; validation drops them.
 */
export function isModifierOrLockCode(code: string): boolean {
  return MODIFIER_OR_LOCK.test(code);
}

function validBinding(v: unknown): v is Binding {
  if (typeof v !== 'object' || v === null) return false;
  const o = v as Record<string, unknown>;
  if (typeof o.code !== 'string' || !/^[A-Za-z0-9]{1,24}$/.test(o.code) || isModifierOrLockCode(o.code)) return false;
  if (typeof o.ctrl !== 'boolean' || typeof o.shift !== 'boolean' || typeof o.alt !== 'boolean') return false;
  return !(o.ctrl && !o.shift);
}

/** bindingFromEvent turns a captured keydown into a binding, or null for a reserved chord. */
export function bindingFromEvent(e: { code?: string; key: string; ctrlKey: boolean; shiftKey: boolean; altKey?: boolean; metaKey: boolean }): Binding | null {
  if (e.metaKey) return null;
  const cand = { code: eventCode(e), ctrl: e.ctrlKey, shift: e.shiftKey, alt: e.altKey ?? false };
  return validBinding(cand) ? cand : null;
}

const CODE_LABEL: Record<string, string> = { BracketRight: ']', BracketLeft: '[', Slash: '/', Space: 'Space', Enter: 'Enter', Escape: 'Esc', Backquote: '`', Minus: '-', Equal: '=', Comma: ',', Period: '.', Semicolon: ';', Quote: "'", Backslash: '\\' };

export function bindingLabel(x: Binding): string {
  const key = CODE_LABEL[x.code] ?? x.code.replace(/^Key/, '').replace(/^Digit/, '').replace(/^Numpad/, 'Num ');
  return [x.ctrl && 'Ctrl', x.alt && 'Alt', x.shift && 'Shift', key].filter(Boolean).join('+');
}

export function matchKeymap(k: Keymap, e: HotkeyEvent): KeyAction | null {
  const chord: Binding = { code: eventCode(e), ctrl: e.ctrlKey, shift: e.shiftKey, alt: e.altKey ?? false };
  for (const a of KEY_ACTIONS) if (k[a].some((x) => same(x, chord))) return a;
  return null;
}

export function conflictsFor(k: Keymap, action: KeyAction): KeyAction[] {
  return KEY_ACTIONS.filter((a) => a !== action && k[a].some((x) => k[action].some((y) => same(x, y))));
}

/** Escape is the panic key: it may be bound only to cancel-run. */
const escapeMisuse = (action: string, x: Binding) => x.code === 'Escape' && action !== 'cancel-run';

export function withBinding(k: Keymap, action: KeyAction, x: Binding): Keymap {
  if (!validBinding(x) || escapeMisuse(action, x) || k[action].some((y) => same(x, y))) return k;
  const list = [...k[action], x].slice(-MAX_BINDINGS);
  return { ...k, [action]: list };
}

export function withoutBinding(k: Keymap, action: KeyAction, i: number): Keymap {
  return { ...k, [action]: k[action].filter((_, j) => j !== i) };
}

/**
 * readKeymap reads a stored {version:1, overrides} blob onto the defaults;
 * unknown actions and bad bindings are dropped, never fatal, and `dropped`
 * counts the override entries that did not survive (a partly-bad entry
 * keeps the action's default and counts once). A chord repeated within one
 * action is kept once.
 */
export function readKeymap(v: unknown): { keymap: Keymap; dropped: number } | null {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (o.version !== 1 || typeof o.overrides !== 'object' || o.overrides === null) return null;
  const k = defaultKeymap();
  let dropped = 0;
  for (const [a, list] of Object.entries(o.overrides as Record<string, unknown>)) {
    if (!(KEY_ACTIONS as readonly string[]).includes(a) || !Array.isArray(list)) {
      dropped++;
      continue;
    }
    const good = list.filter((x): x is Binding => validBinding(x) && !escapeMisuse(a, x)).slice(0, MAX_BINDINGS);
    if (good.length !== list.length) {
      dropped++; // a partly-bad entry keeps the default
      continue;
    }
    const kept: Binding[] = [];
    for (const x of good) if (!kept.some((y) => same(x, y))) kept.push({ code: x.code, ctrl: x.ctrl, shift: x.shift, alt: x.alt });
    k[a as KeyAction] = kept;
  }
  return { keymap: k, dropped };
}

/** validateKeymap is readKeymap's keymap alone: the stored blob's validator. */
export function validateKeymap(v: unknown): Keymap | null {
  return readKeymap(v)?.keymap ?? null;
}

/** overridesOf is the difference from defaults: only changed actions are stored or exported. */
export function overridesOf(k: Keymap): Partial<Keymap> {
  const d = defaultKeymap();
  const out: Partial<Keymap> = {};
  for (const a of KEY_ACTIONS) {
    const same2 = k[a].length === d[a].length && k[a].every((x, i) => same(x, d[a][i]));
    if (!same2) out[a] = k[a];
  }
  return out;
}

export function loadKeymap(storage: Storage | null): Keymap {
  try {
    const raw = storage?.getItem(KEYMAP_KEY) ?? null;
    if (raw === null) return defaultKeymap();
    return validateKeymap(JSON.parse(raw)) ?? defaultKeymap();
  } catch {
    return defaultKeymap();
  }
}

export function saveKeymap(storage: Storage | null, k: Keymap): void {
  try {
    storage?.setItem(KEYMAP_KEY, JSON.stringify({ version: 1, overrides: overridesOf(k) }));
  } catch {
    /* private mode or quota: keep the in-memory copy */
  }
}
