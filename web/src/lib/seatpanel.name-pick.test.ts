import { describe, expect, it, vi } from 'vitest';
import type { Decision } from '../protocol';
import { SeatPanelState } from './seatpanel.svelte';
const { postIntentMock } = vi.hoisted(() => ({ postIntentMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({ ...(await importOriginal<typeof import('./api')>()), postIntent: postIntentMock }));
const ask: Decision = { seq: 41, player: 1, kind: 'choose', prompt: 'Name a card', min: 1, max: 1, options: [{ index: 0, kind: 'name', label: 'Zulu', player: 1 }, { index: 1, kind: 'name', label: 'Alpha', player: 1 }] };
describe('name picker state', () => {
  it('reuses and resets the per-ask filter without changing click wire index behavior', async () => {
    const s = new SeatPanelState('t1', 1, { seat: 1, token: 'tok' }, null, null);
    s.adoptView(ask); s.searchFilter = 'alp';
    expect(s.picked).toEqual([]);
    s.adoptView({ ...ask, seq: 42 });
    expect(s.searchFilter).toBe('');
    await s.click(1);
    expect(postIntentMock.mock.calls.at(-1)?.[2].choices).toEqual([1]);
  });
});

describe('pickHotkey on a name pick', () => {
  // The name list is filtered and sorted A→Z, so option 1 on the wire (Zulu)
  // is not the first name on screen (Alpha): digit 1 answers Alpha, the
  // first rendered row, and a filter re-numbers the rows.
  it('answers the Nth name in rendered (sorted) order', async () => {
    postIntentMock.mockClear();
    const s = new SeatPanelState('t1', 1, { seat: 1, token: 'tok' }, null, null);
    s.adoptView({ ...ask, seq: 42 });
    expect(s.pickHotkey(1)).toBe(true);
    for (let i = 0; i < 20; i++) await Promise.resolve();
    expect(postIntentMock.mock.calls.at(-1)?.[2].choices).toEqual([1]);
  });

  it('numbers only the rows the filter leaves', async () => {
    postIntentMock.mockClear();
    const s = new SeatPanelState('t1', 1, { seat: 1, token: 'tok' }, null, null);
    s.adoptView({ ...ask, seq: 43 });
    s.searchFilter = 'zu';
    expect(s.pickHotkey(2)).toBe(false);
    expect(s.pickHotkey(1)).toBe(true);
    for (let i = 0; i < 20; i++) await Promise.resolve();
    expect(postIntentMock.mock.calls.at(-1)?.[2].choices).toEqual([0]);
  });
});
