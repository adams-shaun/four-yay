import { describe, expect, it, vi } from 'vitest';
import type { Decision, Option } from '../protocol';
import { SeatPanelState } from './seatpanel.svelte';

// The seat panel posts through ./api; stub it so this file is hermetic and
// asserts only on the state machine's own gating.
const { postIntentMock, fetchPendingMock } = vi.hoisted(() => ({
  postIntentMock: vi.fn(),
  fetchPendingMock: vi.fn(),
}));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
  fetchPending: fetchPendingMock,
}));

const ctx = { seat: 0, token: 'tok' };
const opt = (index: number, name: string, obj: number): Option =>
  ({ index, kind: 'bottom', label: name, obj, player: 0 });

/** A KArrange reorder the way effRearrangeTopOfLibrary poses it: Min == Max == len(options). */
const reorder: Decision = {
  seq: 7, player: 0, kind: 'arrange', min: 5, max: 5,
  prompt: 'Rearrange the top 5 card(s); the first card you pick goes on top',
  source: 9,
  options: [
    opt(0, 'Brazen Borrower', 11),
    opt(1, 'Fabled Pass', 12),
    opt(2, 'Gitaxian Probe', 13),
    opt(3, 'Spell Pierce', 14),
    opt(4, 'Unholy Heat', 15),
  ],
};

describe('setPicked — the arrange popup’s write path', () => {
  it('replaces the picked set wholesale, in the given order, and submit posts exactly that order', async () => {
    const p = new SeatPanelState('t1', 1, ctx, null);
    p.adoptView(reorder);
    postIntentMock.mockClear();

    p.setPicked([3, 1, 0, 2, 4]);
    expect(p.picked).toEqual([3, 1, 0, 2, 4]);
    expect(p.canSubmit).toBe(true);

    p.submit();
    await vi.waitFor(() => expect(postIntentMock).toHaveBeenCalledTimes(1));
    const [, , intent] = postIntentMock.mock.calls[0];
    expect(intent).toEqual({ seq: 7, player: 0, choices: [3, 1, 0, 2, 4] });
  });

  it('refuses a set with an index the decision does not offer, a repeat, or a non-integer — nothing is picked', () => {
    const p = new SeatPanelState('t1', 1, ctx, null);
    p.adoptView(reorder);

    p.setPicked([0, 99]);
    expect(p.picked).toEqual([]);
    p.setPicked([0, 0]);
    expect(p.picked).toEqual([]);
    p.setPicked([0.5, 1]);
    expect(p.picked).toEqual([]);
    // A good set still lands after the refusals.
    p.setPicked([4]);
    expect(p.picked).toEqual([4]);
  });

  it('never posts by itself, and refuses once the decision is answered or a post is in flight', async () => {
    const p = new SeatPanelState('t1', 1, ctx, null);
    p.adoptView(reorder);

    p.submit(); // nothing picked: min 5 — refused
    await vi.waitFor(() => expect(postIntentMock).not.toHaveBeenCalled());

    p.setPicked([0, 1, 2, 3, 4]);
    p.busy = true; // a post in flight
    p.setPicked([4, 3, 2, 1, 0]);
    expect(p.picked).toEqual([0, 1, 2, 3, 4]);

    p.busy = false;
    p.submit();
    await vi.waitFor(() => expect(postIntentMock).toHaveBeenCalledTimes(1));
    p.setPicked([1, 0, 2, 3, 4]); // the decision is answered now
    expect(p.picked).toEqual([]);
  });

  it('arrangeOpen resets across the match boundary with the rest of the seat state', () => {
    const p = new SeatPanelState('t1', 1, ctx, null);
    p.adoptView(reorder);
    p.arrangeOpen = true;
    p.begin();
    expect(p.arrangeOpen).toBe(false);
    expect(p.picked).toEqual([]);
  });
});

describe('pickHotkey on an arrange ask', () => {
  // A one-card reorder is min==max==1, where click() would post at once; the
  // arrange row toggles instead, and so must the hotkey.
  it('toggles into the keep pile and never posts, even on a one-card ask', async () => {
    const p = new SeatPanelState('t1', 1, ctx, null);
    p.adoptView({ ...reorder, seq: 8, min: 1, max: 1, options: [reorder.options[0]] });
    postIntentMock.mockClear();
    expect(p.pickHotkey(1)).toBe(true);
    for (let i = 0; i < 20; i++) await Promise.resolve();
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(p.picked).toEqual([0]);
  });
});
