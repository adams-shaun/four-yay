import { describe, expect, it } from 'vitest';
import {
  bindingFromEvent, bindingLabel, conflictsFor, defaultKeymap, eventCode, isModifierOrLockCode, KEY_ACTIONS, loadKeymap, matchKeymap,
  saveKeymap, validateKeymap, withBinding, withoutBinding, KEYMAP_KEY,
} from './keymap';
import { hotkeyAction } from './hotkeys';

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

  it('Numpad Enter is Enter: it ends the turn, and with Shift hard-skips', () => {
    const k = defaultKeymap();
    expect(eventCode(ev('Enter', { code: 'NumpadEnter' }))).toBe('Enter');
    expect(matchKeymap(k, ev('Enter', { code: 'NumpadEnter' }))).toBe('end-turn');
    expect(matchKeymap(k, ev('Enter', { code: 'NumpadEnter', shiftKey: true }))).toBe('hard-skip');
    expect(hotkeyAction(ev('Enter', { code: 'NumpadEnter' }))).toBe('end-turn');
  });

  it('Shift+Space passes, as it did before the keymap', () => {
    expect(defaultKeymap().pass.map(bindingLabel)).toEqual(['Space', 'Shift+Space']);
    expect(hotkeyAction(ev(' ', { shiftKey: true, code: 'Space' }))).toBe('pass');
  });

  it('Alt variants of the default chords are not hotkeys', () => {
    // Intentional: Alt+Space / Alt+Enter are OS and browser chords (window menu, fullscreen), never ours.
    expect(hotkeyAction(ev(' ', { altKey: true, code: 'Space' }))).toBeNull();
    expect(hotkeyAction(ev('Enter', { altKey: true, code: 'Enter' }))).toBeNull();
  });

  it('Escape may be bound only to cancel-run', () => {
    const esc = { code: 'Escape', ctrl: false, shift: false, alt: false };
    const k = defaultKeymap();
    expect(withBinding(k, 'undo', esc)).toBe(k);
    expect(withBinding(k, 'undo', { ...esc, shift: true })).toBe(k);
    const got = validateKeymap({ version: 1, overrides: { undo: [esc], 'cancel-run': [esc, { code: 'KeyQ', ctrl: false, shift: false, alt: false }] } });
    expect(got?.undo).toEqual(defaultKeymap().undo);
    expect(got?.['cancel-run'].map(bindingLabel)).toEqual(['Esc', 'Q']);
  });

  it('modifier and lock keys are never bindings: capture refuses them, validation drops them', () => {
    for (const code of ['ShiftLeft', 'ControlRight', 'AltLeft', 'MetaRight', 'OSLeft', 'OS', 'CapsLock', 'NumLock', 'ScrollLock', 'AltGraph', 'Fn', 'ContextMenu']) {
      expect(isModifierOrLockCode(code)).toBe(true);
      expect(bindingFromEvent({ key: code, code, ctrlKey: false, shiftKey: false, metaKey: false })).toBeNull();
      const k = validateKeymap({ version: 1, overrides: { 'resolve-all': [{ code, ctrl: false, shift: false, alt: false }] } });
      expect(k?.['resolve-all']).toEqual([]);
    }
    for (const code of ['KeyA', 'Digit1', 'Space', 'Enter', 'F1', 'Numpad1']) expect(isModifierOrLockCode(code)).toBe(false);
  });

  it('validateKeymap dedupes an action\'s repeated chord', () => {
    const x = { code: 'KeyR', ctrl: false, shift: false, alt: false };
    expect(validateKeymap({ version: 1, overrides: { 'resolve-all': [x, x] } })?.['resolve-all']).toEqual([x]);
  });
});


describe('the layout and view actions (UI rework §2/§3)', () => {
  it('have defaults that collide with nothing shipped, and layout 1-9 start unbound', async () => {
    const { defaultKeymap, conflictsFor, matchKeymap } = await import('./keymap');
    const k = defaultKeymap();
    for (const a of ['next-layout', 'prev-layout', 'toggle-log', 'toggle-stacking', 'zoom-card', 'open-grave', 'open-exile'] as const) {
      expect(k[a].length, a).toBe(1);
      expect(conflictsFor(k, a), a).toEqual([]);
    }
    for (let n = 1; n <= 9; n++) expect(k[`layout-${n}` as 'layout-1']).toEqual([]);
    expect(matchKeymap(k, { key: 'z', code: 'KeyZ', ctrlKey: false, shiftKey: false, metaKey: false })).toBe('zoom-card');
    expect(matchKeymap(k, { key: 'Z', code: 'KeyZ', ctrlKey: true, shiftKey: true, metaKey: false })).toBe('undo');
    expect(matchKeymap(k, { key: '>', code: 'Period', ctrlKey: true, shiftKey: true, metaKey: false })).toBe('next-layout');
  });
});
