import { withBase } from '../basepath';
import { parseEngineMessage, type ClientMessage, type EngineMessage, type ProtocolError } from './wire';

/**
 * transport.ts is gorge's ManaBrew wire transport, client side
 * (host/manabrewhttp, scope spec §5.2): an SSE stream of engine messages,
 * a POST per client message, and a one-shot state read. Every route is
 * seat-claim fenced; EventSource cannot set headers, so the token rides as
 * `?token=` on the GETs and as a Bearer header on the POST.
 */

const enc = encodeURIComponent;
const root = (t: string, k: number) => withBase(`/api/manabrew/v0/tables/${enc(t)}/matches/${k}`);
export const streamURL = (t: string, k: number, token: string) => `${root(t, k)}/stream?token=${enc(token)}`;
export const stateURL = (t: string, k: number, token: string) => `${root(t, k)}/state?token=${enc(token)}`;
export const sendURL = (t: string, k: number) => `${root(t, k)}/send`;

const TIMEOUT = 10_000;

/** MbError is a refused request: a ProtocolError from /send, or an HTTP-layer refusal (claim, route). */
export class MbError extends Error {
  constructor(public status: number, public code: string, message: string, public promptId?: number) {
    super(message);
  }
}

async function bounded(url: string, init: RequestInit = {}): Promise<Response> {
  const control = new AbortController();
  const timer = setTimeout(() => control.abort(), TIMEOUT);
  try {
    return await fetch(url, { ...init, signal: control.signal });
  } catch (e) {
    if (control.signal.aborted) throw new MbError(0, 'timeout', `request timed out after ${TIMEOUT}ms`);
    throw new MbError(0, 'network', e instanceof Error ? e.message : String(e));
  } finally {
    clearTimeout(timer);
  }
}

async function refusal(res: Response): Promise<MbError> {
  const body = (await res.json().catch(() => ({}))) as Partial<ProtocolError>;
  return new MbError(res.status, String(body.code ?? 'http'), body.message ?? res.statusText, typeof body.promptId === 'number' ? body.promptId : undefined);
}

/** fetchState is GET …/state: the current state and the open prompt, if any. */
export async function fetchState(t: string, k: number, token: string): Promise<EngineMessage[]> {
  const res = await bounded(stateURL(t, k, token), { headers: { Accept: 'application/json' } });
  if (!res.ok) throw await refusal(res);
  const body = (await res.json().catch(() => null)) as unknown;
  if (!Array.isArray(body)) throw new MbError(res.status, 'invalid', 'the ManaBrew state route did not answer a message list');
  return body.map((m) => parseEngineMessage(JSON.stringify(m))).filter((m): m is EngineMessage => m !== null);
}

/** send posts one client message; 204 is accepted, anything else rejects with the server's ProtocolError. */
export async function send(t: string, k: number, token: string, msg: ClientMessage): Promise<void> {
  const res = await bounded(sendURL(t, k), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify(msg),
  });
  if (!res.ok) throw await refusal(res);
}

/** The subset of EventSource the stream uses; tests pass a fake. */
export interface EventSourceLike {
  addEventListener(type: string, fn: (e: MessageEvent) => void): void;
  close(): void;
}
export type ESCtor = new (url: string) => EventSourceLike;

export interface MbStream {
  close(): void;
}

/**
 * openMbStream owns one EventSource on the ManaBrew stream. Messages carry
 * no SSE event name (`data:` only), so they arrive as `message`. The
 * browser reconnects by itself; the server answers every connect with the
 * full state and the open prompt, so a reconnect needs no bookkeeping.
 */
export function openMbStream(url: string, onMessage: (m: EngineMessage) => void, onError: () => void, es: ESCtor = EventSource as unknown as ESCtor): MbStream {
  const source = new es(url);
  source.addEventListener('message', (e: MessageEvent) => {
    const m = parseEngineMessage(String(e.data));
    if (m) onMessage(m);
  });
  source.addEventListener('error', () => onError());
  return { close: () => source.close() };
}

export interface Transport {
  fetchState: typeof fetchState;
  send: typeof send;
  openStream: (url: string, onMessage: (m: EngineMessage) => void, onError: () => void) => MbStream;
}

export const httpTransport: Transport = { fetchState, send, openStream: (u, m, e) => openMbStream(u, m, e) };
