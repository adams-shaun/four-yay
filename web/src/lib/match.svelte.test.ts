import { describe, expect, it, vi } from 'vitest';
import type { EventBody, MatchInfo, MatchStart, View } from '../protocol';
import { clientBreadcrumbs } from './breadcrumbs';

// match.svelte.ts calls fetchView (PL-16's post-decision/match_end refresh,
// and Task 21's cursor-driven fetch), fetchEvents (backfill, finished-match
// transcript) and fetchMatches (finished-match load); stub api so this stays
// a hermetic test of apply()/dispatch()/showCursor()/loadFinished() with
// hand-built frames, no network.
const { fetchViewMock, fetchEventsMock, fetchMatchesMock, ApiErrorMock } = vi.hoisted(() => {
  class ApiErrorMock extends Error {
    constructor(public status: number) { super(`HTTP ${status}`); }
  }
  return {
    fetchViewMock: vi.fn(),
    fetchEventsMock: vi.fn(),
    fetchMatchesMock: vi.fn(),
    ApiErrorMock,
  };
});
vi.mock('./api', () => ({ ApiError: ApiErrorMock, fetchView: fetchViewMock, fetchEvents: fetchEventsMock, fetchMatches: fetchMatchesMock }));

const { MatchState } = await import('./match.svelte');

const seats = [
  { name: 'Ari', deck: 'mono-red', colour: '#e5484d' },
  { name: 'Bo', deck: 'mono-green', colour: '#22c55e' },
];

const view = (turn = 1): View => ({
  viewer: 0, visibility: 'omniscient', turn, round: turn, step: 'main', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, players: [], stack: [], pending: [],
});

const matchStart = (): MatchStart => ({ seats, seed: 1, spectator: '', bot_policy: 'bot' });

const ev = (seq: number, kind = 'tap'): EventBody => ({ event: { seq, kind, player: 0 }, line: `${kind} ${seq}` });

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

// settle polls until predicate() is true instead of guessing how many
// microtask hops a promise chain needs (a ViewCache round trip alone is
// fetchViewMock's promise -> .then -> .finally -> showCursor's own await ->
// its .catch wrapper, and a backfill compounds that with a recursive
// dispatch/showCursor) — bounded so a real regression still fails fast
// rather than hanging.
async function settle(predicate: () => boolean, maxTicks = 200): Promise<void> {
  for (let i = 0; i < maxTicks; i++) {
    if (predicate()) return;
    await Promise.resolve();
  }
  throw new Error(`settle: condition still false after ${maxTicks} microtask ticks`);
}

// drain lets whatever is already queued run, for asserting that nothing
// further happens (no specific end-state to poll for).
async function drain(ticks = 20): Promise<void> {
  for (let i = 0; i < ticks; i++) await Promise.resolve();
}

describe('MatchState', () => {
  it('returns a stale table-bound seat claim to the lobby instead of leaving its board stuck', () => {
    const assign = vi.fn();
    vi.stubGlobal('location', { assign });
    const m = new MatchState('g1', { seat: 0, token: 'old-token' });
    // leaveRejectedSeatClaim is private; the test drives it directly because
    // the 403 it reacts to arrives from the stream, not from a public call.
    // Cast to the one method being reached rather than to `any`, so a rename
    // or a signature change fails the build here instead of silently making
    // this test call nothing.
    (m as unknown as { leaveRejectedSeatClaim(e: unknown): void }).leaveRejectedSeatClaim(new ApiErrorMock(403));
    expect(assign).toHaveBeenCalledWith('/');
    vi.unstubAllGlobals();
  });

  // ui16 discrimination proof, entry path 1 — COLD LOAD. The table route is
  // entered directly (or refreshed): onMount seeds m.seats once from
  // tables.list, which is empty until the SSE hello (or tables.load) lands,
  // so a cold load read an empty list and never looked again (ui15). The
  // seat list must still end up populated — from the snapshot frame the
  // route receives, not from any tables.list seed in this test (none is
  // applied: the MatchState is constructed with nothing seeding seats).
  it('cold load: a snapshot frame after the route mounted populates seats with no tables.list seed', () => {
    const m = new MatchState('t1');
    // Nothing has seeded m.seats yet: tables.list was empty at onMount and
    // the hello that fills it has not landed (and here never will).
    expect(m.seats).toEqual([]);

    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 5, seats } });

    expect(m.seats).toEqual(seats);
  });

  // ui16 discrimination proof, entry path 2 — MID-GAME FOCUS SUBSCRIBE. A
  // subscriber who joins a table mid-match gets a snapshot frame but never a
  // match_start (host/session.go's Subscribe pushes only the snapshot); with
  // the seats only on match_start, this seat list stayed empty for the whole
  // match. It must be populated from the snapshot alone.
  it('mid-game subscribe: only a snapshot frame (no match_start) still populates seats', () => {
    const m = new MatchState('t1');

    // The one frame a mid-game focus subscribe delivers; deliberately no
    // match_start before or after it.
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 3, seq: 0, body: { view: view(1), turn_starts: [0], head: 5, seats } });

    expect(m.seats).toEqual(seats);

    // After the match rolls over on a perpetual table the subscriber gets a
    // fresh match_start + snapshot; the snapshot alone must carry the new
    // match's seats too, decoupled from any server-side cache of the old
    // match.
    const rotated = [
      { name: 'Bo', deck: 'mono-green', colour: '#22c55e' },
      { name: 'Ari', deck: 'mono-red', colour: '#e5484d' },
    ];
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 4, seq: 0, body: { view: view(1), turn_starts: [0], head: 0, seats: rotated } });
    expect(m.seats).toEqual(rotated);
  });

  it('routes match_start, snapshot, and a contiguous event; a decision then triggers exactly one fetch', async () => {
    fetchViewMock.mockReset();
    fetchViewMock.mockResolvedValue(view(2));
    const m = new MatchState('t1');

    m.apply({ v: 1, t: 'match_start', table: 't1', match: 1, seq: 0, body: matchStart() });
    expect(m.match).toBe(1);
    expect(m.seats).toEqual(seats);

    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 10 } });
    expect(m.view).toEqual(view(1));
    expect(m.dvr.head).toBe(10);

    m.apply({ v: 1, t: 'event', table: 't1', match: 1, seq: 11, body: { event: { seq: 11, kind: 'tap', player: 0 }, line: 'x taps' } });
    expect(m.dvr.head).toBe(11);
    expect(m.dvr.gap).toBe(false);

    m.apply({ v: 1, t: 'decision', table: 't1', match: 1, seq: 12, body: { player: 0, kind: 'priority', prompt: 'You have priority.' } });
    expect(m.decision).toEqual({ player: 0, kind: 'priority', prompt: 'You have priority.' });
    await Promise.resolve();
    await Promise.resolve();
    expect(fetchViewMock).toHaveBeenCalledTimes(1);
    expect(fetchViewMock).toHaveBeenCalledWith('t1', 1, 11);
    expect(m.view).toEqual(view(2));
  });

  it('coalesces a burst of decisions into one in-flight fetch plus at most one follow-up', async () => {
    fetchViewMock.mockReset();
    const d = deferred<View>();
    fetchViewMock.mockReturnValueOnce(d.promise).mockResolvedValue(view(9));
    const m = new MatchState('t2');
    m.apply({ v: 1, t: 'match_start', table: 't2', match: 1, seq: 0, body: matchStart() });
    m.apply({ v: 1, t: 'snapshot', table: 't2', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 5 } });

    m.apply({ v: 1, t: 'decision', table: 't2', match: 1, seq: 6, body: { player: 0, kind: 'priority', prompt: 'p1' } });
    m.apply({ v: 1, t: 'decision', table: 't2', match: 1, seq: 6, body: { player: 0, kind: 'priority', prompt: 'p2' } });
    m.apply({ v: 1, t: 'decision', table: 't2', match: 1, seq: 6, body: { player: 0, kind: 'priority', prompt: 'p3' } });
    expect(fetchViewMock).toHaveBeenCalledTimes(1); // second and third are coalesced, not new requests

    d.resolve(view(5));
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    expect(fetchViewMock).toHaveBeenCalledTimes(2); // exactly one coalesced follow-up, not three
    expect(m.view).toEqual(view(9));
  });

  it('ignores frames addressed to another table', () => {
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'match_start', table: 'other', match: 9, seq: 0, body: matchStart() });
    expect(m.match).toBeNull();
    expect(m.seats).toEqual([]);
  });

  it('an out-of-order event sets gap, and a fresh snapshot re-seeds and clears it', () => {
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 5 } });
    m.apply({ v: 1, t: 'event', table: 't1', match: 1, seq: 9, body: { event: { seq: 9, kind: 'tap', player: 0 }, line: 'x' } });
    expect(m.dvr.gap).toBe(true);

    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(2), turn_starts: [0, 6], head: 9 } });
    expect(m.dvr.gap).toBe(false);
    expect(m.dvr.head).toBe(9);
    expect(m.dvr.events).toEqual([]);
    expect(m.view).toEqual(view(2));
  });

  it('match_end clears the decision and refreshes the live view; table_halted records the reason', async () => {
    fetchViewMock.mockReset();
    fetchViewMock.mockResolvedValue(view(3));
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 4 } });
    m.apply({ v: 1, t: 'decision', table: 't1', match: 1, seq: 5, body: { player: 0, kind: 'priority', prompt: 'p' } });
    expect(m.decision).not.toBeNull();
    await Promise.resolve();
    await Promise.resolve();

    m.apply({ v: 1, t: 'match_end', table: 't1', match: 1, seq: 6, body: { result: 'win', winner: 0, head: 'h' } });
    expect(m.decision).toBeNull();
    await Promise.resolve();
    await Promise.resolve();
    expect(m.view).toEqual(view(3));

    m.apply({ v: 1, t: 'table_halted', table: 't1', seq: 7, body: { reason: 'panic' } });
    expect(m.halted).toBe('panic');
  });
});

describe('MatchState — DVR cursor fetching (Task 21)', () => {
  it('pause then step back fetches exactly one view for the new cursor', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    fetchViewMock.mockImplementation((_t: string, _k: number, seq: number) => Promise.resolve(view(seq)));
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 5 } });
    for (const seq of [6, 7, 8]) m.apply({ v: 1, t: 'event', table: 't1', match: 1, seq, body: ev(seq) });
    expect(m.dvr.head).toBe(8);

    m.dispatch({ type: 'pause' }); // cursor stays at head (8): one fetch for it
    await settle(() => m.view?.turn === 8);
    expect(fetchViewMock).toHaveBeenCalledTimes(1);
    expect(fetchViewMock).toHaveBeenLastCalledWith('t1', 1, 8);

    fetchViewMock.mockClear();
    m.dispatch({ type: 'step', by: -1 }); // cursor -> 7, cached events already cover it: no backfill
    await settle(() => m.view?.turn === 7);
    expect(fetchEventsMock).not.toHaveBeenCalled();
    expect(fetchViewMock).toHaveBeenCalledTimes(1);
    expect(fetchViewMock).toHaveBeenCalledWith('t1', 1, 7);
  });

  it('rapid scrubs are latest-wins: a stale response never overwrites a fresher one', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    const pending = new Map<number, ReturnType<typeof deferred<View>>>();
    fetchViewMock.mockImplementation((_t: string, _k: number, seq: number) => {
      const d = deferred<View>();
      pending.set(seq, d);
      return d.promise;
    });
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 5 } });
    for (const seq of [6, 7, 8, 9, 10]) m.apply({ v: 1, t: 'event', table: 't1', match: 1, seq, body: ev(seq) });
    m.dispatch({ type: 'pause' });
    await settle(() => pending.has(10));
    pending.get(10)?.resolve(view(10)); // the pause fetch (cursor is still at head=10): let it settle out of the way
    await settle(() => m.view?.turn === 10);

    m.dispatch({ type: 'scrub', seq: 7 });
    m.dispatch({ type: 'scrub', seq: 8 });
    m.dispatch({ type: 'scrub', seq: 9 }); // the cursor settles here
    expect(m.dvr.cursor).toBe(9);
    await settle(() => pending.has(7) && pending.has(8) && pending.has(9));

    // resolve out of order: the oldest request last, the newest first
    pending.get(9)?.resolve(view(9));
    await settle(() => m.view?.turn === 9);
    pending.get(7)?.resolve(view(7));
    pending.get(8)?.resolve(view(8));
    await drain();

    expect(m.view).toEqual(view(9)); // never clobbered by the later-resolving, earlier-issued fetches
  });

  it('feedback keeps the assigned view sequence while a scrub fetch is still pending', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    const delayed = deferred<View>();
    fetchViewMock.mockReturnValueOnce(delayed.promise);
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(10), turn_starts: [0], head: 10 } });
    m.apply({ v: 1, t: 'event', table: 't1', match: 1, seq: 11, body: ev(11) });

    // The DVR cursor immediately moves to 11, but the board is still the
    // snapshot rendered at 10 until showCursor's fetch resolves. Feedback
    // must capture the board sequence, not the requested cursor.
    m.dispatch({ type: 'pause' });
    await settle(() => fetchViewMock.mock.calls.length === 1);
    expect(m.dvr.cursor).toBe(11);
    expect(m.renderedSeq).toBe(10);
    expect(clientBreadcrumbs.snapshot(null).view).toEqual({ sequence: 10, intent_index: null });

    delayed.resolve(view(11));
    await settle(() => m.renderedSeq === 11);
    expect(clientBreadcrumbs.snapshot(null).view).toEqual({ sequence: 11, intent_index: null });
  });

  it('scrubbing before the cached events window triggers exactly one bounded backfill', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    fetchViewMock.mockImplementation((_t: string, _k: number, seq: number) => Promise.resolve(view(seq)));
    fetchEventsMock.mockResolvedValue([ev(0, 'turn'), ev(10, 'tap'), ev(40, 'turn')]);
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0, 40], head: 50 } });
    m.apply({ v: 1, t: 'event', table: 't1', match: 1, seq: 51, body: ev(51) });
    m.dispatch({ type: 'pause' }); // cursor 51, events window starts at 51: no backfill yet
    await settle(() => m.view?.turn === 51);
    expect(fetchEventsMock).not.toHaveBeenCalled();

    m.dispatch({ type: 'scrub', seq: 10 }); // well before the known window
    await settle(() => m.view?.turn === 10);

    expect(fetchEventsMock).toHaveBeenCalledTimes(1);
    expect(fetchEventsMock).toHaveBeenCalledWith('t1', 1, 0);
    expect(m.dvr.events.map((e) => e.event.seq)).toEqual([0, 10, 40, 51]);
    expect(m.view).toEqual(view(10));
  });

  it('loadFinished renders a finished match paused at its last seq, with no stream', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    fetchMatchesMock.mockReset();
    const info: MatchInfo = { table: 't1', match: 3, seed: 1, seats, state: 'finished', result: 'win', winner: 1, events: 6, turns: 2, bot_policy: 'bot' };
    fetchMatchesMock.mockResolvedValue([info]);
    const all = [ev(0, 'turn'), ev(1), ev(2), ev(3, 'turn'), ev(4), ev(5)];
    fetchEventsMock.mockResolvedValue(all);
    fetchViewMock.mockImplementation((_t: string, _k: number, seq: number) => Promise.resolve(view(seq)));

    const m = new MatchState('t1');
    await m.loadFinished(3);
    await settle(() => m.view !== null);

    expect(fetchMatchesMock).toHaveBeenCalledWith('t1');
    expect(fetchEventsMock).toHaveBeenCalledWith('t1', 3, 0);
    expect(m.match).toBe(3);
    expect(m.seats).toEqual(seats);
    expect(m.dvr.head).toBe(5);
    expect(m.dvr.cursor).toBe(5);
    expect(m.dvr.live).toBe(false);
    expect(m.dvr.turnStarts).toEqual([0, 3]);
    expect(m.dvr.events.map((e) => e.event.seq)).toEqual([0, 1, 2, 3, 4, 5]);
    expect(m.view).toEqual(view(5));
    expect(m.loadError).toBeNull();
  });

  it('loadFinished sets loadError (and never rejects) when the table has no such match', async () => {
    fetchMatchesMock.mockReset();
    fetchMatchesMock.mockResolvedValue([]);
    const m = new MatchState('t9');
    // A fire-and-forget `void m.loadFinished(k)` (as Table.svelte does for a
    // stale /t/{t}/m/{k} link) must never produce an unhandled rejection:
    // the promise resolves either way, and the failure is reported through
    // loadError for the caller to render instead.
    await expect(m.loadFinished(1)).resolves.toBeUndefined();
    expect(m.loadError).toBe('no match 1');
    expect(m.match).toBeNull();
  });

  it('loadFinished clears a previous loadError on a subsequent success', async () => {
    fetchMatchesMock.mockReset();
    fetchEventsMock.mockReset();
    fetchViewMock.mockReset();
    fetchMatchesMock.mockResolvedValueOnce([]).mockResolvedValue([
      { table: 't9', match: 2, seed: 1, seats, state: 'finished', winner: 0, events: 2, turns: 1, bot_policy: 'bot' } satisfies MatchInfo,
    ]);
    fetchEventsMock.mockResolvedValue([ev(0, 'turn'), ev(1)]);
    fetchViewMock.mockImplementation((_t: string, _k: number, seq: number) => Promise.resolve(view(seq)));
    const m = new MatchState('t9');

    await m.loadFinished(2); // no such match yet
    expect(m.loadError).toBe('no match 2');

    await m.loadFinished(2); // now it exists
    await settle(() => m.view !== null);
    expect(m.loadError).toBeNull();
    expect(m.match).toBe(2);
  });
});

describe('MatchState — stale paused-cursor fetches never clobber a fresher view (Task 21 fix round)', () => {
  it('returning to live discards a slower in-flight paused-cursor fetch', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    const slow = deferred<View>();
    fetchViewMock
      .mockImplementationOnce(() => slow.promise) // the paused cursor's fetch, at seq 10
      .mockImplementation((_t: string, _k: number, seq: number) => Promise.resolve(view(seq))); // refreshLive's fetch onward
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 5 } });
    for (const seq of [6, 7, 8, 9, 10]) m.apply({ v: 1, t: 'event', table: 't1', match: 1, seq, body: ev(seq) });

    m.dispatch({ type: 'pause' }); // fires the slow fetch for seq 10; it will not resolve yet
    await drain();
    expect(fetchViewMock).toHaveBeenCalledTimes(1);

    m.dispatch({ type: 'live' }); // returns to live; refreshLive fetches head (10) via a fresh, fast call
    await settle(() => m.view?.turn === 10);
    expect(fetchViewMock).toHaveBeenCalledTimes(2);

    slow.resolve(view(999)); // the stale paused-cursor fetch finally resolves
    await drain();
    expect(m.view).toEqual(view(10)); // unchanged: the live head view, not the stale paused one
  });

  it('a match transition discards a slower in-flight paused-cursor fetch from the old match', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    const slow = deferred<View>();
    fetchViewMock.mockImplementationOnce(() => slow.promise); // match 1's paused cursor fetch
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'match_start', table: 't1', match: 1, seq: 0, body: matchStart() });
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 5 } });
    for (const seq of [6, 7]) m.apply({ v: 1, t: 'event', table: 't1', match: 1, seq, body: ev(seq) });
    m.dispatch({ type: 'pause' }); // fires the slow fetch for seq 7; leaves it in flight
    await drain();
    expect(fetchViewMock).toHaveBeenCalledTimes(1);

    // match 1 ends, match 2 starts and snapshots in live — the new match's
    // view comes straight from the frame, no fetch involved.
    m.apply({ v: 1, t: 'match_start', table: 't1', match: 2, seq: 0, body: matchStart() });
    expect(m.view).toBeNull(); // match_start clears the previous match's board
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 2, seq: 0, body: { view: view(42), turn_starts: [0], head: 0 } });
    expect(m.view).toEqual(view(42));

    slow.resolve(view(999)); // match 1's stale fetch finally resolves
    await drain();
    expect(m.view).toEqual(view(42)); // still match 2's view, untouched by match 1's stale fetch
  });
});

describe('MatchState rewind', () => {
  it('applies the shorter full snapshot, clears the pending decision, and rejects stale live fetches', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    const slow = deferred<View>();
    fetchViewMock.mockImplementationOnce(() => slow.promise);
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 20, body: { view: view(20), turn_starts: [0, 10], head: 20, seats } });
    m.apply({ v: 1, t: 'decision', table: 't1', match: 1, seq: 20, body: { player: 0, kind: 'priority', prompt: 'act' } });
    expect(m.decision).not.toBeNull();

    m.apply({ v: 1, t: 'rewind', table: 't1', match: 1, seq: 5, body: { view: view(5), turn_starts: [0], head: 5, seats } });
    expect(m.decision).toBeNull();
    expect(m.view).toEqual(view(5));
    expect(m.dvr).toMatchObject({ head: 5, cursor: 5, live: true, events: [], turnStarts: [0] });

    slow.resolve(view(999));
    await drain();
    expect(m.view).toEqual(view(5));
  });

  it('recognises a shorter reconnect snapshot as a rewind', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    const m = new MatchState('t1');
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 20, body: { view: view(20), turn_starts: [0, 10], head: 20, seats } });
    m.apply({ v: 1, t: 'decision', table: 't1', match: 1, seq: 20, body: { player: 0, kind: 'priority', prompt: 'act' } });

    // An SSE reconnect starts from a snapshot. If the rewind frame was lost,
    // this is the only evidence the browser has that its old seq space died.
    const rewound = m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 5, body: { view: view(5), turn_starts: [0], head: 5, seats } });
    expect(rewound).toBe(true);
    expect(m.decision).toBeNull();
    expect(m.view).toEqual(view(5));
    expect(m.dvr).toMatchObject({ head: 5, cursor: 5, live: true, events: [], turnStarts: [0] });
  });
});

describe('overflow recovery', () => {
  // Session.push never blocks: a burst that outruns the SSE writer drops
  // frames, marks the session overflowed and closes it (host/session.go,
  // host/httpapi/sse.go). The dropped frames can include the decision frame,
  // which is this client's only repaint edge during play, so a client that
  // ignores the overflow frame -- as it used to -- holds its last painted
  // board until the player reloads.
  it('refetches the view and the seat transcript when the server reports dropped frames', async () => {
    fetchViewMock.mockReset();
    fetchEventsMock.mockReset();
    fetchViewMock.mockResolvedValue(view(7));
    fetchEventsMock.mockResolvedValue([] as EventBody[]);

    const m = new MatchState('t1', { seat: 0, token: 'tok' });
    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(1), turn_starts: [0], head: 5, seats } });
    await Promise.resolve();
    fetchViewMock.mockClear();
    fetchEventsMock.mockClear();

    // The overflow frame describes the SESSION, not a match, so it carries no
    // table and must be handled before the table guard drops it.
    m.apply({ v: 1, t: 'overflow', seq: 0, body: { dropped: 12 } } as never);
    await Promise.resolve();
    await Promise.resolve();

    expect(fetchViewMock).toHaveBeenCalled();
    expect(fetchEventsMock).toHaveBeenCalled();
  });

});
