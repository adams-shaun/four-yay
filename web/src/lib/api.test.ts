import { describe, expect, it, vi } from 'vitest';
import { afterEach } from 'vitest';
import { deckStatsURL, fetchDeckStats, decksURL, eventsURL, matchesURL, pendingURL, seatQuery, tablesURL, undoURL, viewURL, fetchView, fetchEvents, fetchPending, fetchDecks, postIntent, postUndo, subscribe, createGame } from './api';
import { setBasePathForTests, withBase } from './basepath';
import type { Intent } from '../protocol';

const ctx = { seat: 2, token: 'tok-abc' };

// fetch is a global in node 20+; stub it so the api module's GET/POST
// paths are testable hermetically.
const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);

describe('api urls', () => {
  it('builds the documented paths', () => {
    expect(tablesURL()).toBe('/api/tables');
    expect(decksURL()).toBe('/api/decks');
    expect(deckStatsURL()).toBe('/api/stats/decks');
    expect(matchesURL('t1')).toBe('/api/tables/t1/matches');
    expect(viewURL('t1', 3)).toBe('/api/tables/t1/matches/3/view');
    expect(viewURL('t1', 3, 0)).toBe('/api/tables/t1/matches/3/view?seq=0');
    expect(eventsURL('t 1', 3, 42)).toBe('/api/tables/t%201/matches/3/events?since=42');
    expect(pendingURL('t1', 3)).toBe('/api/tables/t1/matches/3/pending');
    expect(undoURL('t 1', 3)).toBe('/api/tables/t%201/matches/3/undo');
    expect(seatQuery(ctx)).toBe('?seat=2&token=tok-abc');
  });

  it('threads seat and token onto the seat-scoped GETs and leaves the spectator URLs byte-identical', () => {
    expect(viewURL('t1', 3, 0, undefined)).toBe('/api/tables/t1/matches/3/view?seq=0');
    expect(viewURL('t1', 3, 5, ctx)).toBe('/api/tables/t1/matches/3/view?seq=5&seat=2&token=tok-abc');
    expect(viewURL('t1', 3, undefined, ctx)).toBe('/api/tables/t1/matches/3/view?seat=2&token=tok-abc');
    expect(eventsURL('t1', 3, 7, undefined)).toBe('/api/tables/t1/matches/3/events?since=7');
    expect(eventsURL('t1', 3, 7, ctx)).toBe('/api/tables/t1/matches/3/events?since=7&seat=2&token=tok-abc');
  });

  it('fetchPending GETs the pending decision with the seat query, and postIntent POSTs the intent under Bearer with no ?seat=', async () => {
    fetchMock.mockReset();
    const pending = { seq: 4, player: 2, kind: 'priority', prompt: 'p', min: 1, max: 1, options: [] };
    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => pending });
    await expect(fetchPending('t1', 3, ctx)).resolves.toEqual(pending);
    expect(fetchMock).toHaveBeenCalledWith('/api/tables/t1/matches/3/pending?seat=2&token=tok-abc', expect.objectContaining({ headers: expect.objectContaining({ Accept: 'application/json' }) }));

    fetchMock.mockResolvedValueOnce({ ok: true, status: 204, json: async () => ({}) });
    const intent: Intent = { seq: 4, player: 2, choices: [1] };
    await postIntent('t1', 3, intent, ctx);
    expect(fetchMock).toHaveBeenLastCalledWith('/api/tables/t1/matches/3/intent', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'Content-Type': 'application/json', Authorization: 'Bearer tok-abc' }),
      body: JSON.stringify(intent),
    }));
  });

  it('postUndo POSTs the human seat claim without a body and surfaces rejection', async () => {
    fetchMock.mockReset();
    fetchMock.mockResolvedValueOnce({ ok: true, status: 204 });
    await postUndo('t1', 3, ctx);
    // objectContaining, not a literal: every state-channel request also
    // carries the abort signal that bounds it (STATE_TIMEOUT in ./api).
    expect(fetchMock).toHaveBeenLastCalledWith('/api/tables/t1/matches/3/undo', expect.objectContaining({
      method: 'POST', headers: { Authorization: 'Bearer tok-abc' },
    }));

    fetchMock.mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ code: 'conflict', message: 'nothing to undo' }) });
    await expect(postUndo('t1', 3, ctx)).rejects.toMatchObject({ status: 409, code: 'conflict', message: 'nothing to undo' });
  });

  it('postIntent surfaces a server rejection as ApiError, and fetchView/fetchEvents route seat-scoped GETs', async () => {
    fetchMock.mockReset();
    const d = { seq: 4, player: 2, kind: 'priority', prompt: 'p', min: 1, max: 1, options: [] };
    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => d });
    await expect(fetchView('t1', 3, 9, ctx)).resolves.toEqual(d);
    expect(fetchMock).toHaveBeenCalledWith('/api/tables/t1/matches/3/view?seq=9&seat=2&token=tok-abc', expect.anything());

    fetchMock.mockReset();
    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => [] });
    await expect(fetchEvents('t1', 3, 0, ctx)).resolves.toEqual([]);
    expect(fetchMock).toHaveBeenCalledWith('/api/tables/t1/matches/3/events?since=0&seat=2&token=tok-abc', expect.anything());

    fetchMock.mockReset();
    fetchMock.mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ code: 'conflict', message: 'intent seq 4, pending decision seq 6' }) });
    await expect(postIntent('t1', 3, { seq: 4, player: 2, choices: [1] }, ctx)).rejects.toMatchObject({ status: 409, code: 'conflict', message: 'intent seq 4, pending decision seq 6' });
  });

  it('fetchPending 409 (nothing pending) rejects rather than returning a stale empty', async () => {
    fetchMock.mockReset();
    fetchMock.mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ code: 'conflict', message: 'no decision pending for this seat' }) });
    await expect(fetchPending('t1', 3, ctx)).rejects.toMatchObject({ status: 409, code: 'conflict' });
  });
});

describe('base path', () => {
  afterEach(() => setBasePathForTests(''));

  it('an empty base path builds exactly the URLs it builds today', async () => {
    // The vitest page has no <meta name="gorge-base">, so the base is the
    // default ""; every constructed URL must be byte-identical to the
    // pre-base client: a REST GET, the SSE stream, and a POST.
    expect(viewURL('t1', 3)).toBe('/api/tables/t1/matches/3/view');
    // withBase('/api/stream') is the path the session opens (./session.svelte).
    expect(withBase('/api/stream')).toBe('/api/stream');
    fetchMock.mockReset();
    fetchMock.mockResolvedValueOnce({ ok: true, status: 204, json: async () => ({}) });
    await subscribe('s1', 't1', 'focus');
    expect(fetchMock).toHaveBeenCalledWith('/api/subscribe', expect.anything());
  });

  it('a base path prefixes every constructed URL', async () => {
    setBasePathForTests('/gorge');
    expect(tablesURL()).toBe('/gorge/api/tables');
    expect(viewURL('t1', 3, 5)).toBe('/gorge/api/tables/t1/matches/3/view?seq=5');
    expect(withBase('/api/stream')).toBe('/gorge/api/stream');
    fetchMock.mockReset();
    fetchMock.mockResolvedValueOnce({ ok: true, status: 204, json: async () => ({}) });
    await subscribe('s1', 't1', 'focus');
    expect(fetchMock).toHaveBeenCalledWith('/gorge/api/subscribe', expect.anything());
  });

  it('a base path with a trailing slash and one without produce the same URL', () => {
    setBasePathForTests('/gorge/');
    const slashed = { tables: tablesURL(), stream: withBase('/api/stream'), view: viewURL('t1', 3), pending: pendingURL('t1', 3) };
    setBasePathForTests('/gorge');
    expect(tablesURL()).toBe(slashed.tables);
    expect(withBase('/api/stream')).toBe(slashed.stream);
    expect(viewURL('t1', 3)).toBe(slashed.view);
    expect(pendingURL('t1', 3)).toBe(slashed.pending);
    expect(slashed.tables).toBe('/gorge/api/tables');
    expect(slashed.view).toBe('/gorge/api/tables/t1/matches/3/view');
    expect(slashed.stream).toBe('/gorge/api/stream');
  });
});

describe('createGame (Task ui11)', () => {
  it('fetches the deck catalogue and POSTs selected decks to /api/games', async () => {
    fetchMock.mockReset();
    const decks = [{ id: 'angels', name: 'Angels', format: 'commander', archetype: 'tribal', commander: 'Giada' }];
    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => decks });
    await expect(fetchDecks()).resolves.toEqual(decks);
    expect(fetchMock).toHaveBeenLastCalledWith('/api/decks', expect.objectContaining({ headers: expect.objectContaining({ Accept: 'application/json' }) }));

    const game = { table: 'g1', match: 1, seed: 42, seat: 0, token: 'tok', join: '/t/g1?seat=0&token=tok' };
    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => game });
    const request = { format: 'commander' as const, human_deck: 'angels', bot_deck: 'dragons' };
    await expect(createGame(request)).resolves.toEqual(game);
    expect(fetchMock).toHaveBeenLastCalledWith('/api/games', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(request),
    }));
  });

  it('surfaces a server rejection (a server that did not arm -vsbot answers 404) as ApiError', async () => {
    fetchMock.mockReset();
    fetchMock.mockResolvedValueOnce({ ok: false, status: 404, json: async () => ({ code: 'not_found', message: 'play-vs-bot games are not enabled on this server' }) });
    await expect(createGame({ format: 'constructed' })).rejects.toMatchObject({ status: 404, code: 'not_found' });
  });
});

describe('state-channel request deadline', () => {
  // A request that never settles used to wedge the client permanently:
  // refreshLive's `inflight` latch and the seat panel's `busy` flag are both
  // released in a `finally`, so an unsettled promise pins them for the life of
  // the page — the board stops repainting and the 1s /pending poll, gated on
  // !busy, stops firing. The panel then reads "<step> — waiting for <player>"
  // until the player reloads. Every state-channel call must abandon the socket
  // instead.
  afterEach(() => { vi.useRealTimers(); });

  const neverSettles = () => {
    fetchMock.mockReset();
    fetchMock.mockImplementationOnce((_url: string, init: RequestInit) => new Promise((_resolve, reject) => {
      init.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    }));
  };

  it('aborts a GET that never answers and rejects as a timeout', async () => {
    vi.useFakeTimers();
    neverSettles();
    const pending = fetchPending('t1', 3, ctx);
    const assertion = expect(pending).rejects.toMatchObject({ status: 0, code: 'timeout' });
    await vi.advanceTimersByTimeAsync(10_000);
    await assertion;
  });

  it('aborts an intent POST that never answers, so the seat panel releases its busy lock', async () => {
    vi.useFakeTimers();
    neverSettles();
    const intent: Intent = { seq: 4, player: 2, choices: [0] };
    const posted = postIntent('t1', 3, intent, ctx);
    const assertion = expect(posted).rejects.toMatchObject({ status: 0, code: 'timeout' });
    await vi.advanceTimersByTimeAsync(10_000);
    await assertion;
  });

  it('leaves a request that answers in time untouched', async () => {
    vi.useFakeTimers();
    fetchMock.mockReset();
    const pending = { seq: 4, player: 2, kind: 'priority', prompt: 'p', min: 1, max: 1, options: [] };
    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => pending });
    await expect(fetchPending('t1', 3, ctx)).resolves.toEqual(pending);
  });
});

describe('fetchDeckStats', () => {
  it('GETs the public deck leaderboard', async () => {
    fetchMock.mockReset();
    const tally = { decks: [{ deck: 'A', games: 10, wins: 6, losses: 4, draws: 0, win_rate: 0.6 }], min_games: 10, matches_counted: 5 };
    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => tally });
    await expect(fetchDeckStats()).resolves.toEqual(tally);
    expect(fetchMock).toHaveBeenLastCalledWith('/api/stats/decks', expect.objectContaining({ headers: expect.objectContaining({ Accept: 'application/json' }) }));
  });
});
