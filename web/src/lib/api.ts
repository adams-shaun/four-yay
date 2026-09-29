import type { Decision, ErrorBody, EventBody, Intent, MatchInfo, TableInfo, View } from '../protocol';
import type { SeatCtx } from './seat';
import { withBase } from './basepath';

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public head?: number) {
    super(message);
  }
}

const enc = encodeURIComponent;
// Every path below is built with withBase (./basepath), the one base-path
// value the client reads once at startup. With an empty base (the default)
// each one is byte-identical to the pre-base client.
export const tablesURL = () => withBase('/api/tables');
export const matchesURL = (t: string) => withBase(`/api/tables/${enc(t)}/matches`);
export const pendingURL = (t: string, k: number) => withBase(`/api/tables/${enc(t)}/matches/${k}/pending`);
export const intentURL = (t: string, k: number) => withBase(`/api/tables/${enc(t)}/matches/${k}/intent`);
export const undoURL = (t: string, k: number) => withBase(`/api/tables/${enc(t)}/matches/${k}/undo`);
export const gamesURL = () => withBase('/api/games');
export const decksURL = () => withBase('/api/decks');

// seatQuery is the seat/token query threading on the seat-scoped GETs
// (M2e-3's FL-99: ?seat=N&token=…). The token is a bearer credential for
// the local fixture — it rides in the URL exactly as the join URL printed
// it, and never in a rendered string.
export const seatQuery = (ctx: SeatCtx) => `?seat=${ctx.seat}&token=${encodeURIComponent(ctx.token)}`;

// viewURL appends the optional ?seq= and, for a seated request, the
// seat/token pair. With neither, it is byte-identical to the pre-seat URL
// (R-E4-4: no seat in the URL, no behaviour change).
export const viewURL = (t: string, k: number, seq?: number, ctx?: SeatCtx) => {
  const q = new URLSearchParams();
  if (seq !== undefined) q.set('seq', String(seq));
  if (ctx) {
    q.set('seat', String(ctx.seat));
    q.set('token', ctx.token);
  }
  const s = q.toString();
  return withBase(`/api/tables/${enc(t)}/matches/${k}/view${s === '' ? '' : `?${s}`}`);
};
export const eventsURL = (t: string, k: number, since: number, ctx?: SeatCtx) => {
  const q = new URLSearchParams({ since: String(since) });
  if (ctx) {
    q.set('seat', String(ctx.seat));
    q.set('token', ctx.token);
  }
  return withBase(`/api/tables/${enc(t)}/matches/${k}/events?${q.toString()}`);
};

// STATE_TIMEOUT bounds every state-channel request: view, events, pending,
// intent, undo, subscribe. A request that never settles is not merely a slow
// one. Both of the client's single-flight latches are released in a `finally`
// — MatchState.refreshLive's `inflight` (web/src/lib/match.svelte.ts) and the
// seat panel's `busy` (web/src/lib/seatpanel.svelte.ts) — so a promise that
// never settles pins them for the life of the page: the board stops
// repainting on every later decision frame, and the 1s /pending poll, gated
// on `!busy`, stops firing. The panel then sits on its last painted view,
// reading "<step> — waiting for <player>", while the match goroutine has long
// since moved on; only a reload clears it. A timeout converts that permanent
// wedge into one failed request the next frame or poll retries.
//
// It is reachable because the browser allows ~6 HTTP/1.1 connections per
// host and this channel competes for them: web/src/lib/images.ts documents
// art lookups starving these very requests, and bounds itself at the same
// 10s for the same reason.
const STATE_TIMEOUT = 10_000;

/** TIMEOUT_CODE is ApiError.code for a request this client abandoned. The status is 0: no server answered, so no HTTP status describes it. */
export const TIMEOUT_CODE = 'timeout';

/**
 * fetchBounded is fetch with a deadline. On expiry it aborts the request —
 * freeing the socket for the rest of the state channel — and rejects with an
 * ApiError the ordinary catch paths already handle.
 */
async function fetchBounded(path: string, init: RequestInit = {}, ms = STATE_TIMEOUT): Promise<Response> {
  const control = new AbortController();
  const timer = setTimeout(() => control.abort(), ms);
  try {
    return await fetch(path, { ...init, signal: control.signal });
  } catch (e) {
    if (control.signal.aborted) throw new ApiError(0, TIMEOUT_CODE, `request timed out after ${ms}ms`);
    throw e;
  } finally {
    clearTimeout(timer);
  }
}

export async function getJSON<T>(path: string): Promise<T> {
  const res = await fetchBounded(path, { headers: { Accept: 'application/json' } });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as Partial<ErrorBody>;
    throw new ApiError(res.status, body.code ?? 'http', body.message ?? res.statusText, body.head);
  }
  return (await res.json()) as T;
}

async function postJSON(path: string, body: unknown): Promise<void> {
  const res = await fetchBounded(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  if (!res.ok) {
    const e = (await res.json().catch(() => ({}))) as Partial<ErrorBody>;
    throw new ApiError(res.status, e.code ?? 'http', e.message ?? res.statusText);
  }
}

export const fetchTables = () => getJSON<TableInfo[]>(tablesURL());
export const fetchMatches = (t: string) => getJSON<MatchInfo[]>(matchesURL(t));

export interface DeckInfo {
  id: string;
  name: string;
  format: 'constructed' | 'commander';
  archetype: string;
  commander?: string;
}

export const fetchDecks = () => getJSON<DeckInfo[]>(decksURL());

/** A game a successful POST /api/games returns: the new table's identity, the human seat and its bearer token, and the join path that carries them (Task ui11). */
export interface CreateGame {
  table: string;
  match: number;
  seed: number;
  seat: number;
  token: string;
  join: string;
}

export interface CreateGameRequest {
  format: 'constructed' | 'commander';
  human_deck?: string;
  bot_deck?: string;
  /** Bot policy for every non-human seat; omitted keeps the server default. The restart control sends the table's own policy so a rematch keeps the same bot. */
  bot_policy?: string;
  /** London mulligan allowance for the created game; omitted keeps the server default (0 is a legal value, disabling the pre-game round). */
  mulligans?: number;
}

/** createGame asks the server to seat the player against a bot in the given format: a fresh single-shot table is created and started, and the join path it returns is the URL the player follows to sit in the seat. Omitted deck ids retain random assignment. A server that did not enable the play-vs-bot flow answers 404, surfaced as ApiError. */
export async function createGame(request: CreateGameRequest): Promise<CreateGame> {
  const res = await fetch(gamesURL(), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!res.ok) {
    const e = (await res.json().catch(() => ({}))) as Partial<ErrorBody>;
    throw new ApiError(res.status, e.code ?? 'http', e.message ?? res.statusText);
  }
  return (await res.json()) as CreateGame;
}
export const fetchView = (t: string, k: number, seq?: number, ctx?: SeatCtx) => getJSON<View>(viewURL(t, k, seq, ctx));
export const fetchEvents = (t: string, k: number, since: number, ctx?: SeatCtx) => getJSON<EventBody[]>(eventsURL(t, k, since, ctx));

/** fetchPending is the seat-scoped GET that names the decision currently asked of the seat — the full Decision, options included. A 409 conflict (nothing pending for this seat) rejects like any other server answer. */
export const fetchPending = (t: string, k: number, ctx: SeatCtx) => (ctx.transport ? ctx.transport.fetchPending(t, k) : getJSON<Decision>(pendingURL(t, k) + seatQuery(ctx)));

/** postIntent answers a decision. It takes no ?seat= (the claim is the fence, FL-99: Authorization: Bearer is the accepted second form), and the intent body's own seq/player/choices are validated server-side. */
export async function postIntent(t: string, k: number, intent: Intent, ctx: SeatCtx): Promise<void> {
  if (ctx.transport) return ctx.transport.postIntent(t, k, intent);
  const res = await fetchBounded(intentURL(t, k), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${ctx.token}` },
    body: JSON.stringify(intent),
  });
  if (!res.ok) {
    const e = (await res.json().catch(() => ({}))) as Partial<ErrorBody>;
    throw new ApiError(res.status, e.code ?? 'http', e.message ?? res.statusText);
  }
}

/** postUndo requests an in-place rewind. The seat claim is the same bearer fence as an intent; the rewind frame confirms when it lands. */
export async function postUndo(t: string, k: number, ctx: SeatCtx): Promise<void> {
  if (ctx.transport) return ctx.transport.postUndo(t, k);
  const res = await fetchBounded(undoURL(t, k), {
    method: 'POST',
    headers: { Authorization: `Bearer ${ctx.token}` },
  });
  if (!res.ok) {
    const e = (await res.json().catch(() => ({}))) as Partial<ErrorBody>;
    throw new ApiError(res.status, e.code ?? 'http', e.message ?? res.statusText);
  }
}
export const subscribe = (session: string, table: string, mode: 'overview' | 'focus') =>
  postJSON(withBase('/api/subscribe'), { session, table, mode });
export const unsubscribe = (session: string, table: string) => postJSON(withBase('/api/unsubscribe'), { session, table });
