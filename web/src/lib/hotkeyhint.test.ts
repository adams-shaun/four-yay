import { describe, expect, it } from 'vitest';
import { HotkeyHinter } from './hotkeyhint.svelte';

/**
 * HotkeyHinter is the transient suppression cue's state. These pin the two
 * properties the chip depends on: a later note SUPERSEDES an earlier one
 * (a fresh id, so the presenter restarts its expiry), and clear() drops the
 * hint outright (a successful hotkey must not leave a stale "why not").
 */
describe('HotkeyHinter', () => {
  it('starts empty, notes a reason with a fresh id, and a later note supersedes', () => {
    const h = new HotkeyHinter();
    // Precondition: nothing is claimed before the first note.
    expect(h.current).toBeNull();

    h.note('activatable-space-enter');
    const first = h.current;
    expect(first).toEqual({ id: 1, reason: 'activatable-space-enter' });

    h.note('activatable-space-enter');
    const second = h.current;
    // Precondition for the supersede assertion: the two ids actually differ.
    expect(second?.id).not.toBe(first?.id);
    expect(second).toEqual({ id: 2, reason: 'activatable-space-enter' });
  });

  it('clear drops the hint', () => {
    const h = new HotkeyHinter();
    h.note('activatable-space-enter');
    expect(h.current).not.toBeNull(); // precondition: there is something to clear
    h.clear();
    expect(h.current).toBeNull();
  });
});
