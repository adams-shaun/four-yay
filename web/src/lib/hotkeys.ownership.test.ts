import { describe, expect, it } from 'vitest';
import { focusOwnership, hotkeyAction, type HotkeyEvent } from './hotkeys';

/**
 * focusOwnership is the classifier the suppression cue (lib/hotkeyhint) reads
 * to tell a Space the player expected to pass from a keystroke that correctly
 * went into a textbox. It is the SAME rule hotkeyAction applies, derived so
 * the two cannot drift. These tests build fake targets exactly as
 * hotkeys.focus.test.ts's focusedOn helper does — no jsdom in the pure suite.
 */
function focusedOn(tag: 'button' | 'a' | 'input' | 'textarea' | 'select' | 'contenteditable' | 'summary' | 'rolebutton'): EventTarget {
  const is = (sel: string): boolean => {
    const parts = sel.split(',').map((s) => s.trim());
    if (tag === 'button') return parts.includes('button');
    if (tag === 'a') return parts.some((p) => p.startsWith('a'));
    if (tag === 'summary') return parts.includes('summary');
    if (tag === 'rolebutton') return parts.includes('[role="button"]');
    if (tag === 'contenteditable') return parts.some((p) => p.startsWith('[contenteditable'));
    return parts.includes(tag);
  };
  const el = { matches: is };
  return { closest: (sel: string) => (is(sel) ? el : null), matches: is } as unknown as EventTarget;
}

/** A non-interactive target: the felt, the panel text, the body. */
const felt = { closest: () => null } as unknown as EventTarget;

const ev = (over: Partial<HotkeyEvent>): HotkeyEvent => ({
  key: ' ', ctrlKey: false, shiftKey: false, metaKey: false, target: null, ...over,
});

describe('focusOwnership — the reason the table did not get the key', () => {
  it('every key is text-entry while typing (input, textarea, select, contenteditable)', () => {
    for (const t of [focusedOn('input'), focusedOn('textarea'), focusedOn('select'), focusedOn('contenteditable')]) {
      expect(focusOwnership(ev({ key: ' ', code: 'Space', target: t }))).toBe('text-entry');
      expect(focusOwnership(ev({ key: '1', code: 'Digit1', target: t }))).toBe('text-entry');
      expect(focusOwnership(ev({ key: 'Escape', code: 'Escape', target: t }))).toBe('text-entry');
    }
  });

  it('Space and Enter (NumpadEnter too) are activatable-space-enter on a button, link, summary or role=button', () => {
    for (const t of [focusedOn('button'), focusedOn('a'), focusedOn('summary'), focusedOn('rolebutton')]) {
      expect(focusOwnership(ev({ key: ' ', code: 'Space', target: t }))).toBe('activatable-space-enter');
      expect(focusOwnership(ev({ key: 'Enter', code: 'Enter', target: t }))).toBe('activatable-space-enter');
      expect(focusOwnership(ev({ key: 'Enter', code: 'NumpadEnter', target: t }))).toBe('activatable-space-enter');
    }
  });

  it('every OTHER key on an activatable control is the table\u2019s (null), not a suppression', () => {
    for (const t of [focusedOn('button'), focusedOn('a')]) {
      expect(focusOwnership(ev({ key: '1', code: 'Digit1', target: t }))).toBeNull();
      expect(focusOwnership(ev({ key: '?', code: 'Slash', shiftKey: true, target: t }))).toBeNull();
    }
  });

  it('focus on the felt owns nothing: null', () => {
    expect(focusOwnership(ev({ key: ' ', code: 'Space', target: felt }))).toBeNull();
    expect(focusOwnership(ev({ key: ' ', code: 'Space', target: null }))).toBeNull();
  });

  it('derives hotkeyAction\u2019s guard exactly: a reason iff the action is suppressed', () => {
    // The precondition is that the two observations CAN differ — Space on a
    // button is suppressed, Space on the felt is a pass — so this is not a
    // test that passes for any input.
    expect(hotkeyAction(ev({ key: ' ', code: 'Space', target: focusedOn('button') }))).toBeNull();
    expect(hotkeyAction(ev({ key: ' ', code: 'Space', target: felt }))).toBe('pass');
    for (const target of [focusedOn('button'), focusedOn('input'), felt, null]) {
      for (const key of [' ', 'Enter', '1']) {
        const e = ev({ key, code: key === ' ' ? 'Space' : key === 'Enter' ? 'Enter' : 'Digit1', target });
        expect(focusOwnership(e) !== null).toBe(hotkeyAction(e) === null);
      }
    }
  });
});
