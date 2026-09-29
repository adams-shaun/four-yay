import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, Option } from '../protocol';
import { SeatPanelState, mulliganPhase, toneOf } from './seatpanel.svelte';

// Same hermetic stubs as seatpanel.svelte.test.ts: the state machine posts
// through ./api, and these tests care about what it does NOT post.
const { postIntentMock, fetchPendingMock } = vi.hoisted(() => ({ postIntentMock: vi.fn(), fetchPendingMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
  fetchPending: fetchPendingMock,
}));

const ctx = { seat: 0, token: 'tok' };

const opt = (index: number, kind: string, label: string, obj?: number): Option =>
  ({ index, kind, label, obj, player: 0 });

// The shapes rules/mulligan.go actually asks. askKeepMulligan: Min==Max==1
// over "keep" and, while a free mulligan remains, "mulligan". askBottoming:
// Min==Max==taken over one "bottom" option per hand card, each carrying Obj.
const keepAsk = (seq: number, withMulligan = true): Decision => ({
  seq, player: 0, kind: 'mulligan', min: 1, max: 1,
  prompt: 'London mulligan: alice keeps 7 and bottoms 1, or mulligans',
  options: withMulligan ? [opt(0, 'keep', 'keep'), opt(1, 'mulligan', 'mulligan')] : [opt(0, 'keep', 'keep')],
});
const bottomAsk = (seq: number, n: number, cards: number): Decision => ({
  seq, player: 0, kind: 'mulligan', min: n, max: n,
  prompt: `London mulligan: alice bottoms ${n} card(s)`,
  options: Array.from({ length: cards }, (_, j) => opt(j, 'bottom', `Card ${j}`, 100 + j)),
});
const priority = (seq: number): Decision => ({
  seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
  options: [opt(0, 'cast', 'Cast Bolt'), opt(1, 'pass', 'Pass priority'), opt(2, 'concede', 'Concede')],
});

describe('toneOf', () => {
  it('no decision is idle: the panel says nothing louder than its hairline', () => {
    expect(toneOf(null)).toBe('idle');
  });

  it('a decision offering pass is a window this seat may decline', () => {
    expect(toneOf(priority(1))).toBe('offered');
  });

  it('a decision with no pass is the initiative: the table is stopped until it is answered', () => {
    expect(toneOf(keepAsk(1))).toBe('initiative');
    expect(toneOf(bottomAsk(1, 2, 7))).toBe('initiative');
    expect(toneOf({ seq: 1, player: 0, kind: 'target', prompt: 'Choose a target', min: 1, max: 1,
      options: [opt(0, 'player', 'Seat 1'), opt(1, 'obj', 'Grizzly Bears')] })).toBe('initiative');
  });

  // FL-101: the tone is a KIND question. A label that reads like a pass, or a
  // pass option sitting anywhere but last-but-one, must not move it.
  it('reads kinds, never labels or positions', () => {
    const lying: Decision = { seq: 1, player: 0, kind: 'choose', prompt: 'Choose', min: 1, max: 1,
      options: [opt(0, 'exile', 'Pass priority'), opt(1, 'exile', 'Pass')] };
    expect(toneOf(lying)).toBe('initiative');

    const passFirst: Decision = { seq: 1, player: 0, kind: 'priority', prompt: 'p', min: 1, max: 1,
      options: [opt(0, 'pass', 'Do nothing'), opt(1, 'cast', 'Cast Bolt')] };
    expect(toneOf(passFirst)).toBe('offered');
  });
});

describe('mulliganPhase', () => {
  it('the keep/mulligan half is named by its option kinds', () => {
    expect(mulliganPhase(keepAsk(1))).toEqual({ phase: 'keep', choices: keepAsk(1).options });
  });

  it('a spent allowance offers keep alone and is still the keep half', () => {
    const d = keepAsk(1, false);
    expect(d.options).toHaveLength(1);
    expect(mulliganPhase(d)).toEqual({ phase: 'keep', choices: d.options });
  });

  it('the bottoming half is named by its option kinds, not by min/max or option count', () => {
    const d = bottomAsk(1, 1, 7);
    expect(d.min).toBe(1); // the same min/max shape as a keep ask
    expect(mulliganPhase(d)).toEqual({ phase: 'bottom', cards: d.options });
  });

  it('any other decision kind has no mulligan layout', () => {
    expect(mulliganPhase(priority(1))).toBeNull();
    expect(mulliganPhase(null)).toBeNull();
    // kind 'choose' carrying bottom options is a different decision entirely
    expect(mulliganPhase({ ...bottomAsk(1, 2, 3), kind: 'choose' })).toBeNull();
  });

  // The layout must never swallow an option it does not understand: if the
  // engine ever adds a kind to this decision, the generic list renders it and
  // the player can still answer.
  it('an unrecognised option kind falls back to the generic list rather than dropping the option', () => {
    const d: Decision = { ...keepAsk(1), options: [opt(0, 'keep', 'keep'), opt(1, 'mulligan', 'mulligan'), opt(2, 'concede', 'Concede')] };
    expect(mulliganPhase(d)).toBeNull();
    expect(mulliganPhase({ ...bottomAsk(1, 1, 2), options: [opt(0, 'bottom', 'A', 100), opt(1, 'scry', 'B', 101)] })).toBeNull();
  });

  it('an empty option list has no layout', () => {
    expect(mulliganPhase({ ...keepAsk(1), options: [] })).toBeNull();
  });
});

describe('SeatPanelState.toggle', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  // The point of toggle: bottoming ONE card is a min==max==1 decision, the
  // same shape as a priority option, where click() posts on the first click.
  // Bottoming is irreversible, so the card is picked and then committed.
  it('bottoming a single card never posts on the click, only on submit', async () => {
    const p = new SeatPanelState('t1', 1, ctx);
    const d = bottomAsk(4, 1, 7);
    p.adoptView(d);

    p.toggle(3);
    for (let i = 0; i < 20; i++) await Promise.resolve();
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(p.picked).toEqual([3]);
    expect(p.canSubmit).toBe(true);

    p.submit();
    for (let i = 0; i < 40 && p.postedSeq !== 4; i++) await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledWith('t1', 1, { seq: 4, player: 0, choices: [3] }, ctx);
  });

  it('toggle keeps click order and deselects, and ignores an index the decision does not have', async () => {
    const p = new SeatPanelState('t1', 1, ctx);
    p.adoptView(bottomAsk(5, 2, 7));
    p.toggle(6);
    p.toggle(1);
    expect(p.picked).toEqual([6, 1]); // the order IS the answer: bottom order
    p.toggle(6);
    expect(p.picked).toEqual([1]);
    p.toggle(99);
    expect(p.picked).toEqual([1]);
    for (let i = 0; i < 20; i++) await Promise.resolve();
    expect(postIntentMock).not.toHaveBeenCalled();
  });

  it('toggle does nothing once the decision has been answered', async () => {
    const p = new SeatPanelState('t1', 1, ctx);
    p.adoptView(bottomAsk(7, 1, 3));
    p.toggle(0);
    p.submit();
    for (let i = 0; i < 40 && p.postedSeq !== 7; i++) await Promise.resolve();
    p.adoptView(bottomAsk(7, 1, 3));
    p.toggle(2);
    expect(p.picked).toEqual([]);
  });

  // The keep/mulligan half keeps the ordinary click path: one click IS the
  // answer, and it must still post immediately.
  it('the keep/mulligan half still answers on a single click', async () => {
    const p = new SeatPanelState('t1', 1, ctx);
    p.adoptView(keepAsk(9));
    p.click(1);
    for (let i = 0; i < 40 && p.postedSeq !== 9; i++) await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledWith('t1', 1, { seq: 9, player: 0, choices: [1] }, ctx);
  });
});

describe('SeatPanelState.pickHotkey on the mulligan', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  // A stray digit during a bottom-1 must pick, never commit: the bottom half
  // answers by toggle-then-Submit, exactly as the panel's card row does.
  it('pick-1 on a single-card bottom toggles the card and posts nothing', async () => {
    const p = new SeatPanelState('t1', 1, ctx);
    p.adoptView(bottomAsk(4, 1, 7));
    expect(p.pickHotkey(1)).toBe(true);
    for (let i = 0; i < 20; i++) await Promise.resolve();
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(p.picked).toEqual([0]);
    expect(p.pickHotkey(1)).toBe(true); // a second press deselects, as a second click would
    expect(p.picked).toEqual([]);
  });

  it('pick-N on the keep/mulligan half still answers on the press, as its click does', async () => {
    const p = new SeatPanelState('t1', 1, ctx);
    p.adoptView(keepAsk(9));
    expect(p.pickHotkey(2)).toBe(true);
    for (let i = 0; i < 40 && p.postedSeq !== 9; i++) await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledWith('t1', 1, { seq: 9, player: 0, choices: [1] }, ctx);
  });
});
