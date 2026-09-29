import { describe, expect, it } from 'vitest';
import { closesOptionsPanel, hotkeyAction, type HotkeyEvent } from './hotkeys';
import { defaultKeymap, withBinding } from './keymap';

/**
 * The narrowed focus guard (final-review C2). A fake target answers
 * closest() the way Element.closest does for the element it stands for, and
 * matches() for the element itself, so the guard's two selectors are
 * exercised exactly as the DOM would answer them.
 */
function focusedOn(tag: 'button' | 'a' | 'input' | 'textarea' | 'select' | 'contenteditable'): EventTarget {
  const is = (sel: string): boolean => {
    const parts = sel.split(',').map((s) => s.trim());
    if (tag === 'button') return parts.includes('button');
    if (tag === 'a') return parts.some((p) => p.startsWith('a'));
    if (tag === 'contenteditable') return parts.some((p) => p.startsWith('[contenteditable'));
    return parts.includes(tag);
  };
  const el = { matches: is };
  return { closest: (sel: string) => (is(sel) ? el : null), matches: is } as unknown as EventTarget;
}

const ev = (over: Partial<HotkeyEvent>): HotkeyEvent => ({
  key: ' ', ctrlKey: false, shiftKey: false, metaKey: false, target: null, ...over,
});

describe('hotkeys — the focus guard after the Options panel returns focus to its button', () => {
  it('a digit, ? and a Ctrl+Shift chord still fire with focus on a button or link', () => {
    for (const t of [focusedOn('button'), focusedOn('a')]) {
      expect(hotkeyAction(ev({ key: '1', code: 'Digit1', target: t }))).toBe('pick-1');
      expect(hotkeyAction(ev({ key: '?', code: 'Slash', shiftKey: true, target: t }))).toBe('show-keys');
      expect(hotkeyAction(ev({ key: 'O', code: 'KeyO', ctrlKey: true, shiftKey: true, target: t }))).toBe('toggle-options');
    }
  });

  it('Space and Enter (NumpadEnter too) stay with a focused button or link: its own activation owns them', () => {
    for (const t of [focusedOn('button'), focusedOn('a')]) {
      expect(hotkeyAction(ev({ key: ' ', code: 'Space', target: t }))).toBeNull();
      expect(hotkeyAction(ev({ key: ' ', code: 'Space', shiftKey: true, target: t }))).toBeNull();
      expect(hotkeyAction(ev({ key: 'Enter', code: 'Enter', target: t }))).toBeNull();
      expect(hotkeyAction(ev({ key: 'Enter', code: 'NumpadEnter', target: t }))).toBeNull();
      expect(hotkeyAction(ev({ key: 'Enter', code: 'Enter', shiftKey: true, target: t }))).toBeNull();
    }
  });

  it('every key but Escape is ignored while typing in an input, textarea, select or contenteditable', () => {
    for (const t of [focusedOn('input'), focusedOn('textarea'), focusedOn('select'), focusedOn('contenteditable')]) {
      expect(hotkeyAction(ev({ key: '1', code: 'Digit1', target: t }))).toBeNull();
      expect(hotkeyAction(ev({ key: '?', code: 'Slash', shiftKey: true, target: t }))).toBeNull();
      expect(hotkeyAction(ev({ key: 'f', code: 'KeyF', ctrlKey: true, shiftKey: true, target: t }))).toBeNull();
      expect(hotkeyAction(ev({ key: ' ', code: 'Space', target: t }))).toBeNull();
      expect(hotkeyAction(ev({ key: 'Escape', code: 'Escape', target: t }))).toBe('cancel-run');
    }
  });
});

describe('closesOptionsPanel — the open Game Options panel closes on its own key', () => {
  const felt = { closest: () => null } as unknown as EventTarget;
  const k = defaultKeymap();
  const chord = (over: Partial<HotkeyEvent & { defaultPrevented: boolean }> = {}) =>
    ({ ...ev({ key: 'O', code: 'KeyO', ctrlKey: true, shiftKey: true, target: felt }), defaultPrevented: false, ...over });

  it('Escape and the toggle-options chord close it — the chord as rebound, too', () => {
    expect(closesOptionsPanel(ev({ key: 'Escape', code: 'Escape', target: felt }), k, false)).toBe(true);
    expect(closesOptionsPanel(chord(), k, false)).toBe(true);
    expect(closesOptionsPanel(chord({ target: focusedOn('button') }), k, false)).toBe(true);
    const rebound = withBinding(k, 'toggle-options', { code: 'KeyG', ctrl: false, shift: false, alt: false });
    expect(closesOptionsPanel(ev({ key: 'g', code: 'KeyG', target: felt }), rebound, false)).toBe(true);
  });

  it('never while typing, capturing a chord, with Meta held, or after a hotkey already acted', () => {
    expect(closesOptionsPanel(chord({ target: focusedOn('input') }), k, false)).toBe(false);
    expect(closesOptionsPanel(chord(), k, true)).toBe(false);
    expect(closesOptionsPanel(chord({ metaKey: true }), k, false)).toBe(false);
    // the press that OPENED the panel was consumed by the strip's hotkey
    expect(closesOptionsPanel(chord({ defaultPrevented: true }), k, false)).toBe(false);
    expect(closesOptionsPanel(ev({ key: ' ', code: 'Space', target: felt }), k, false)).toBe(false);
  });
});
