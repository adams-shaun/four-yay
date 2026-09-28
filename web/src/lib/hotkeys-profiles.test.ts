import { describe, expect, it } from 'vitest';
import { hotkeyAction, type HotkeyEvent } from './hotkeys';

/** A non-interactive target: the felt, the panel text, the body. */
const felt = { closest: () => null } as unknown as EventTarget;
/** An interactive target: closest() matches inside a button/input/textarea. */
const inWidget = { closest: (sel: string) => (sel.includes('button') ? {} : null) } as unknown as Element;

const ev = (over: Partial<HotkeyEvent>): HotkeyEvent => ({
  key: ' ', ctrlKey: false, shiftKey: false, metaKey: false, target: felt, ...over,
});

describe('hotkeys — the profile and options keys (playsettings-profiles)', () => {
  it('Ctrl+Shift+] steps forward and Ctrl+Shift+[ steps back (US layout: key is } / {, code is the bracket)', () => {
    // PRECONDITION: on a standard US keyboard Shift turns ] into } and [ into
    // {. Matching `key` alone would never fire, so the binding reads the
    // physical `code`.
    expect(hotkeyAction(ev({ key: '}', code: 'BracketRight', ctrlKey: true, shiftKey: true }))).toBe('next-profile');
    expect(hotkeyAction(ev({ key: '{', code: 'BracketLeft', ctrlKey: true, shiftKey: true }))).toBe('prev-profile');
    // A synthetic event with no `code` still fires on the shifted `key` value.
    expect(hotkeyAction(ev({ key: '}', ctrlKey: true, shiftKey: true }))).toBe('next-profile');
    expect(hotkeyAction(ev({ key: '{', ctrlKey: true, shiftKey: true }))).toBe('prev-profile');
  });

  it('Ctrl+Shift+O toggles the Game Options panel; Ctrl+Shift+P is deliberately unbound (Firefox private window)', () => {
    expect(hotkeyAction(ev({ key: 'O', ctrlKey: true, shiftKey: true }))).toBe('toggle-options');
    expect(hotkeyAction(ev({ key: 'o', code: 'KeyO', ctrlKey: true, shiftKey: true }))).toBe('toggle-options');
    expect(hotkeyAction(ev({ key: 'P', ctrlKey: true, shiftKey: true }))).toBeNull();
  });

  it('the new keys are never bound under Meta (the OS/browser owns those)', () => {
    for (const over of [
      { key: '}', code: 'BracketRight' },
      { key: '{', code: 'BracketLeft' },
      { key: 'O' },
    ]) {
      expect(hotkeyAction(ev({ ...over, metaKey: true, ctrlKey: true, shiftKey: true }))).toBeNull();
    }
  });

  it('an open modal picker swallows every new key', () => {
    for (const over of [
      { key: '}', code: 'BracketRight' },
      { key: '{', code: 'BracketLeft' },
      { key: 'O' },
    ]) {
      expect(hotkeyAction(ev({ ...over, ctrlKey: true, shiftKey: true }), () => true)).toBeNull();
    }
  });

  it('focus in an interactive element suppresses every new key (Ctrl+Shift+O while typing is not ours)', () => {
    for (const over of [
      { key: '}', code: 'BracketRight' },
      { key: '{', code: 'BracketLeft' },
      { key: 'O' },
    ]) {
      expect(hotkeyAction(ev({ ...over, ctrlKey: true, shiftKey: true, target: inWidget }))).toBeNull();
    }
  });

  it('Ctrl alone is still the click modifier: the new keys need Shift too', () => {
    expect(hotkeyAction(ev({ key: 'O', ctrlKey: true }))).toBeNull();
    expect(hotkeyAction(ev({ key: '}', code: 'BracketRight', ctrlKey: true }))).toBeNull();
    expect(hotkeyAction(ev({ key: '{', code: 'BracketLeft', ctrlKey: true }))).toBeNull();
  });

  it('the existing toggle-full-control keeps Ctrl+Shift+F and the new keys do not collide with it', () => {
    expect(hotkeyAction(ev({ key: 'f', ctrlKey: true, shiftKey: true }))).toBe('toggle-full-control');
    // PRECONDITION: the new keys are distinct strings, so the checks above
    // cannot be accidentally passing on the same value.
    const actions = new Set([
      hotkeyAction(ev({ key: '}', code: 'BracketRight', ctrlKey: true, shiftKey: true })),
      hotkeyAction(ev({ key: '{', code: 'BracketLeft', ctrlKey: true, shiftKey: true })),
      hotkeyAction(ev({ key: 'O', ctrlKey: true, shiftKey: true })),
      hotkeyAction(ev({ key: 'f', ctrlKey: true, shiftKey: true })),
    ]);
    expect(actions.size).toBe(4);
  });
});
