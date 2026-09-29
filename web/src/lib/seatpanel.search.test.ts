import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, Option } from '../protocol';
import { SeatPanelState } from './seatpanel.svelte';
import { searchOptions } from './search';

// The library-search picker's state half (fb-20260916T181754Z): the
// display-only filter lives on the shared SeatPanelState, resets on every
// newly adopted decision, and never touches the answer machinery — picked,
// canSubmit and the posted intent are the generic ones. The DISPLAY list
// itself (predicate, filter, sort) is pinned in lib/search.test.ts; this
// file pins what the component's branch leans on at the state level.

const { postIntentMock, fetchPendingMock } = vi.hoisted(() => ({ postIntentMock: vi.fn(), fetchPendingMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
  fetchPending: fetchPendingMock,
}));

const ctx = { seat: 1, token: 'tok' };

const searchOpt = (index: number, label: string, obj: number): Option =>
  ({ index, kind: 'search', label, obj, player: 1 });

/** A fetchland-shaped search ask: Min 0 / Max 1 over the library in scan order. */
const searchAsk: Decision = {
  seq: 12,
  player: 1,
  kind: 'choose',
  prompt: 'Search your library for a card',
  min: 0,
  max: 1,
  options: [
    searchOpt(0, 'Wooded Foothills', 401),
    searchOpt(1, 'an Island', 402),
    searchOpt(2, 'Mistveil Plains', 403),
  ],
};

const otherSearchAsk: Decision = { ...searchAsk, seq: 13, options: [searchOpt(0, 'Delta', 404)] };

function state(): SeatPanelState {
  return new SeatPanelState('t1', 1, ctx, null, null);
}

beforeEach(() => {
  postIntentMock.mockReset();
  fetchPendingMock.mockReset();
  fetchPendingMock.mockRejectedValue(Object.assign(new Error('nothing pending'), { status: 409 }));
});

describe('SeatPanelState.searchFilter — the display-only filter text', () => {
  it('starts empty on every fresh state', () => {
    expect(state().searchFilter).toBe('');
  });

  it('resets on every newly adopted decision, like the other per-ask surface state', () => {
    const s = state();
    s.adoptView(searchAsk);
    s.searchFilter = 'foo';
    s.adoptView(otherSearchAsk);
    expect(s.searchFilter).toBe('');
  });

  it('resetting on adopt does not disturb picked or the submit gate', () => {
    const s = state();
    s.adoptView(searchAsk);
    s.picked = [1];
    expect(s.canSubmit).toBe(true);
    s.searchFilter = 'foo';
    s.adoptView(otherSearchAsk);
    expect(s.picked).toEqual([]);
  });

  it('a re-adopt of the SAME seq (the first-view race path) keeps the filter the player typed', () => {
    // adoptView early-returns on an identical seq (seatpanel.svelte.ts
    // adopt's second guard), which is exactly the path the panel's init
    // render takes when a test (or a remount) hands the same decision back.
    const s = state();
    s.adoptView(searchAsk);
    s.searchFilter = 'foo';
    s.adoptView(searchAsk);
    expect(s.searchFilter).toBe('foo');
  });

  it('the filter text changes only the display list: picked, canSubmit and the post path are untouched', async () => {
    const s = state();
    s.adoptView(searchAsk);
    s.toggle(2); // pick index 2 in click order
    expect(s.picked).toEqual([2]);
    const before = searchOptions(searchAsk, s.searchFilter);
    s.searchFilter = 'foo';
    // The display list narrows; the picked set and the pending decision do not.
    expect(searchOptions(searchAsk, s.searchFilter)).not.toEqual(before);
    expect(s.picked).toEqual([2]);
    expect(s.canSubmit).toBe(true);
    await s.submit();
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    const intent = postIntentMock.mock.calls[0][2];
    expect(intent.choices).toEqual([2]);
  });
});

describe('pickHotkey on a library search', () => {
  // The grid renders a filtered, A→Z-sorted list, and its faces are
  // numbered in that order: digit 1 is "an Island" (index 1), not the
  // library's first card on the wire.
  it('answers the Nth card in rendered (sorted, filtered) order', async () => {
    const s = state();
    s.adoptView({ ...searchAsk, seq: 14, min: 1, max: 1 });
    expect(s.pickHotkey(1)).toBe(true);
    for (let i = 0; i < 20; i++) await Promise.resolve();
    expect(postIntentMock.mock.calls.at(-1)?.[2].choices).toEqual([1]);
  });

  it('a Min 0 search toggles the numbered card and a filter re-numbers', () => {
    const s = state();
    s.adoptView({ ...searchAsk, seq: 15 });
    s.searchFilter = 'plains';
    expect(s.pickHotkey(2)).toBe(false);
    expect(s.pickHotkey(1)).toBe(true);
    expect(s.picked).toEqual([2]);
    expect(postIntentMock).not.toHaveBeenCalled();
  });
});
