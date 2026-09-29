import { describe, expect, it } from 'vitest';
import {
  bindingFromEvent, bindingLabel, conflictsFor, defaultKeymap, eventCode, KEY_ACTIONS, loadKeymap, matchKeymap,
  saveKeymap, validateKeymap, withBinding, withoutBinding, KEYMAP_KEY,
} from './keymap';

const mem = (): Storage => {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k), clear: () => m.clear(), key: () => null, length: 0 } as unknown as Storage;
};
const ev = (key: string, mods: Partial<{ ctrlKey: boolean; shiftKey: boolean; altKey: boolean; metaKey: boolean; code: string }> = {}) =>
  ({ key, ctrlKey: false, shiftKey: false, altKey: false, metaKey: false, ...mods });

describe('keymap', () => {
  it('defaults reproduce today’s bindings', () => {
    const k = defaultKeymap();
    expect(matchKeymap(k, ev(' '))).toBe('pass');
    expect(matchKeymap(k, ev('Enter'))).toBe('end-turn');
    expect(matchKeymap(k, ev('Enter', { shiftKey: true }))).toBe('hard-skip');
    expect(matchKeymap(k, ev('F', { ctrlKey: true, shiftKey: true }))).toBe('toggle-full-control');
    expect(matchKeymap(k, ev('O', { ctrlKey: true, shiftKey: true }))).toBe('toggle-options');
    expect(matchKeymap(k, ev('}', { ctrlKey: true, shiftKey: true, code: 'BracketRight' }))).toBe('next-profile');
    expect(matchKeymap(k, ev('{', { ctrlKey: true, shiftKey: true }))).toBe('prev-profile');
    expect(matchKeymap(k, ev('3'))).toBe('pick-3');
    expect(matchKeymap(k, ev('!', { ctrlKey: true, shiftKey: true, code: 'Digit1' }))).toBe('profile-1');
    expect(matchKeymap(k, ev('?', { shiftKey: true, code: 'Slash' }))).toBe('show-keys');
  });

  it('eventCode derives a physical code for synthetic events without one', () => {
    expect(eventCode(ev(' '))).toBe('Space');
    expect(eventCode(ev('f'))).toBe('KeyF');
    expect(eventCode(ev('7'))).toBe('Digit7');
    expect(eventCode(ev('}'))).toBe('BracketRight');
    expect(eventCode(ev('?'))).toBe('Slash');
  });

  it('bindingFromEvent refuses reserved chords: bare modifiers, Meta, Ctrl without Shift', () => {
    expect(bindingFromEvent(ev('Shift', { shiftKey: true, code: 'ShiftLeft' }))).toBeNull();
    expect(bindingFromEvent(ev('k', { metaKey: true, code: 'KeyK' }))).toBeNull();
    expect(bindingFromEvent(ev('k', { ctrlKey: true, code: 'KeyK' }))).toBeNull();
    expect(bindingFromEvent(ev('k', { ctrlKey: true, shiftKey: true, code: 'KeyK' }))).toEqual({ code: 'KeyK', ctrl: true, shift: true, alt: false });
  });

  it('labels read like keys', () => {
    expect(bindingLabel({ code: 'KeyF', ctrl: true, shift: true, alt: false })).toBe('Ctrl+Shift+F');
    expect(bindingLabel({ code: 'Space', ctrl: false, shift: false, alt: false })).toBe('Space');
    expect(bindingLabel({ code: 'BracketRight', ctrl: true, shift: true, alt: false })).toBe('Ctrl+Shift+]');
    expect(bindingLabel({ code: 'Digit4', ctrl: false, shift: false, alt: false })).toBe('4');
  });

  it('withBinding caps at two per action; withoutBinding removes by position', () => {
    let k = defaultKeymap();
    k = withBinding(k, 'undo', { code: 'KeyU', ctrl: false, shift: false, alt: false });
    k = withBinding(k, 'undo', { code: 'KeyY', ctrl: false, shift: false, alt: false });
    expect(k.undo.length).toBe(2);
    // cap of two: the default Ctrl+Shift+Z dropped when Y arrived
    expect(k.undo.map(bindingLabel)).toEqual(['U', 'Y']);
    k = withoutBinding(k, 'undo', 0);
    expect(k.undo.map(bindingLabel)).toEqual(['Y']);
  });

  it('conflictsFor names other actions sharing a chord', () => {
    const k = withBinding(defaultKeymap(), 'undo', { code: 'Space', ctrl: false, shift: false, alt: false });
    expect(conflictsFor(k, 'undo')).toEqual(['pass']);
    expect(conflictsFor(defaultKeymap(), 'pass')).toEqual([]);
  });

  it('save stores only overrides; load merges them onto defaults; corrupt storage yields defaults', () => {
    const st = mem();
    const k = withBinding(defaultKeymap(), 'resolve-all', { code: 'KeyR', ctrl: false, shift: false, alt: false });
    saveKeymap(st, k);
    const raw = JSON.parse(st.getItem(KEYMAP_KEY)!);
    expect(Object.keys(raw.overrides)).toEqual(['resolve-all']);
    expect(loadKeymap(st)).toEqual(k);
    st.setItem(KEYMAP_KEY, '{bad');
    expect(loadKeymap(st)).toEqual(defaultKeymap());
  });

  it('validateKeymap drops unknown actions and invalid bindings, keeps the rest', () => {
    const got = validateKeymap({ version: 1, overrides: { undo: [{ code: 'KeyU', ctrl: false, shift: false, alt: false }], nope: [], pass: [{ code: 'KeyK', ctrl: true, shift: false, alt: false }] } });
    expect(got?.undo).toEqual([{ code: 'KeyU', ctrl: false, shift: false, alt: false }]);
    expect(got?.pass).toEqual(defaultKeymap().pass);
    expect(validateKeymap({ version: 2, overrides: {} })).toBeNull();
  });

  it('every action has a label and belongs to exactly one group', async () => {
    const { ACTION_LABELS, ACTION_GROUPS } = await import('./keymap');
    const grouped = ACTION_GROUPS.flatMap((g) => g.actions);
    expect([...grouped].sort()).toEqual([...KEY_ACTIONS].sort());
    for (const a of KEY_ACTIONS) expect(ACTION_LABELS[a].length).toBeGreaterThan(0);
  });
});
