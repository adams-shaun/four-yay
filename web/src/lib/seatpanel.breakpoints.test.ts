import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, Option, View } from '../protocol';
import { SeatPanelState, autoNoteText } from './seatpanel.svelte';
import { applyPreset, noBreakpoints } from './playsettings';

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

  it('arming End Turn on the window a breakpoint stopped passes that window', async () => {
    const p = seat();
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
    // As HotButtonStrip does: arm the run, then re-derive the same window.
    p.startEndTurn(deep());
    p.considerAuto(deep());
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
  });

  it('a one-shot run stopped by a breakpoint records the hit and says so in its own wording', async () => {
    const p = seat();
    p.setAuto(false);
    p.adoptView(quiet(1));
    p.startEndTurn(deep());
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(autoNoteText(p.note)).toBe('End Turn stopped: a pause you set fired.');
    // Recorded: the next window for the same stack passes under Auto.
    p.setAuto(true);
    p.adoptView(quiet(2));
    p.considerAuto(deep());
    await settle(() => p.postedSeq === 2);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
  });

  it('the Manual empty-window floor honours a breakpoint', () => {
    const p = seat();
    p.setAuto(false);
    p.setSkipEmpty(true);
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(autoNoteText(p.note)).toBe('Auto paused here: 2 objects are on the stack.');
  });

  it('with the breakpoint off, the Manual floor passes the same window (today)', async () => {
    const p = seat();
    p.setAuto(false);
    p.setSkipEmpty(true);
    p.settings = { ...p.settings, breakpoints: noBreakpoints() };
    p.adoptView(quiet(1));
    p.considerAuto(deep());
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
  });

  it('toggling full control keeps the player\'s breakpoints both ways, edits made in full control included', () => {
    const p = seat();
    p.settings = { ...p.settings, breakpoints: { ...noBreakpoints(), targetsMe: true, watchlist: ['Counterspell'] } };
    p.toggleFullControl();
    expect(p.settings.preset).toBe('full-control');
    expect(p.settings.breakpoints).toEqual({ ...noBreakpoints(), targetsMe: true, watchlist: ['Counterspell'] });
    p.settings = { ...p.settings, breakpoints: { ...p.settings.breakpoints, watchlist: ['Counterspell', 'Force of Will'] } };
    p.toggleFullControl();
    expect(p.settings.preset).not.toBe('full-control');
    expect(p.settings.breakpoints.watchlist).toEqual(['Counterspell', 'Force of Will']);
    expect(p.settings.breakpoints.targetsMe).toBe(true);
  });

  it('toggling out of full control with no backup (after a reload) keeps the watchlist', () => {
    const p = seat();
    p.settings = { ...applyPreset('full-control'), breakpoints: { ...noBreakpoints(), watchlist: ['Counterspell'] } };
    p.toggleFullControl();
    expect(p.settings.preset).toBe('casual');
    expect(p.settings.breakpoints.watchlist).toEqual(['Counterspell']);
  });

  it('arming End Turn on a window two pauses stopped passes it on the first press', async () => {
    const p = seat();
    p.settings = { ...p.settings, breakpoints: { ...noBreakpoints(), targetsMe: true, stackDepth: 2 } };
    const targeted = (): View => {
      const v = deep();
      v.stack[1] = { ...v.stack[1], targets: [{ player: 0, is_player: true }] } as View['stack'][number];
      return v;
    };
    p.adoptView(quiet(1));
    p.considerAuto(targeted());
    expect(autoNoteText(p.note)).toBe('Auto paused here: B targets you.');
    p.startEndTurn(targeted());
    p.considerAuto(targeted());
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
  });

  it('arming a run on a window no pause stopped does not acknowledge a pause that has yet to fire', () => {
    const p = seat();
    p.setAuto(false);
    p.adoptView(quiet(1));
    p.startEndTurn(deep());
    p.considerAuto(deep());
    expect(postIntentMock).not.toHaveBeenCalled();
  });
});
