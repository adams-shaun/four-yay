/**
 * seat.ts is the page's seat identity, read ONCE from the join URL that
 * M2e-3's gorged prints on startup (FL-99): `?seat=N&token=…`. The seat is
 * "which player am I acting for" — a number and a bear credential, nothing
 * more. No rules knowledge lives here; the server's decision.Validate is
 * the whole contract.
 *
 * The token is a bearer credential for a local fixture. It is held here and
 * built into request URLs in api.ts (and, for the ManaBrew wire,
 * lib/manabrew/transport.ts), and nowhere else: never logged, never
 * put in the transcript, never rendered, never echoed in an error message.
 * The render path (routes/components) receives only SeatCtx via getSeat,
 * and no component string ever interpolates `token`.
 */

import type { Decision, Intent } from '../protocol';

/**
 * SeatTransport is an alternative game channel for the seat's answers (the
 * ManaBrew adapter, lib/manabrew). When a SeatCtx carries one, api.ts's
 * seat calls (postIntent, fetchPending, postUndo) go through it instead of
 * the native routes; the seat panel never knows which wire answered.
 */
export interface SeatTransport {
  postIntent(t: string, k: number, intent: Intent): Promise<void>;
  fetchPending(t: string, k: number): Promise<Decision>;
  postUndo(t: string, k: number): Promise<void>;
}

export interface SeatCtx {
  seat: number;
  token: string;
  transport?: SeatTransport;
}

const TOKEN_RE = /^[A-Za-z0-9._~+/-]+$/;

/** parseSeat reads ?seat= and ?token= out of a search string. Both must be present and well-formed, or there is no seat. */
export function parseSeat(search: string): SeatCtx | null {
  const q = new URLSearchParams(search);
  const seatRaw = q.get('seat');
  const token = q.get('token');
  if (seatRaw === null || token === null || token === '') return null;
  // a seat must be a non-negative integer; the token must be ordinary
  // bearer text (rejecting characters that could smuggle markup into a
  // render is the fixture's only hygiene — parse failures simply mean
  // "not a seat", and the page stays spectator-only)
  if (!/^\d+$/.test(seatRaw)) return null;
  if (!TOKEN_RE.test(token)) return null;
  return { seat: Number(seatRaw), token };
}

let current: SeatCtx | null = null;

/** initSeatContext is called once at startup (main.ts) with location.search. */
export function initSeatContext(search: string): SeatCtx | null {
  current = parseSeat(search);
  return current;
}

/** getSeat returns the page's seat, if the join URL had one. */
export function getSeat(): SeatCtx | null {
  return current;
}
