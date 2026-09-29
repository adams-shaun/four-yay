import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, Option, View } from '../protocol';
import { SeatPanelState, autoNoteText } from './seatpanel.svelte';
import { noBreakpoints } from './playsettings';

const { postIntentMock, fetchPendingMock } = vi.hoisted(() => ({ postIntentMock: vi.fn(), fetchPendingMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
  fetchPending: fetchPendingMock,
}));

const ctx = { seat: 0, token: 'tok' };
const pass = (i: number): Option => ({ index: i, kind: 'pass', label: 'Pass priority', player: 0 });
const concede = (i: number): Option => ({ index: i, kind: 'concede', label: 'Concede', player: 0 });
const quiet = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [pass(0), concede(1)] });

/** a view with two opponent objects on the stack; the stack rule is set to 'never' so only the breakpoint can stop */
const deep = (): View =>
  ({
    active: 1, step: 'main1', turn: 4, players: [],
    stack: [
      { id: 1, controller: 1, kind: 'spell', name: 'A', text: '', optional: false, targets: [] },
      { id: 2, controller: 1, kind: 'spell', name: 'B', text: '', optional: false, targets: [] },
    ],
  }) as unknown as View;

async function settle(predicate: () => boolean, maxTicks = 200): Promise<void> {
  for (let i = 0; i < maxTicks; i++) {
    if (predicate()) return;
    await Promise.resolve();
  }
  throw new Error('settle: condition still false');
}

function seat(): SeatPanelState {
  const p = new SeatPanelState('t1', 1, ctx, null);
  p.stops = { yours: new Set(), opponents: new Set() };
  p.setAuto(true);
  p.setActPass(false);
  p.settings = {
    ...p.settings,
    opponentSpell: 'never',
    pacing: { stepMs: 0, resolveMs: 0 },
    breakpoints: { ...noBreakpoints(), stackDepth: 2 },
  };
  return p;
}

describe('SeatPanelState breakpoints', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('stops the window, and says why in plain words', () => {
    const p = seat();
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(autoNoteText(p.note)).toBe('Auto paused here: 2 objects are on the stack.');
  });

  it('re-deriving the same window keeps it stopped', () => {
    const p = seat();
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    p.considerAuto(deep());
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
  });

  it('a later window for the same stack passes: the pause fired once', async () => {
    const p = seat();
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    p.adoptView(quiet(2));
    p.considerAuto(deep());
    await settle(() => p.postedSeq === 2);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
  });

  it('begin() forgets fired pauses (a new match)', () => {
    const p = seat();
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    p.begin();
    p.adoptView(quiet(2));
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
  });
});
