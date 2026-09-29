import { readFileSync } from 'node:fs';
import { gunzipSync } from 'node:zlib';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import type { View } from '../../protocol';
import type { ModelEvent } from '../clientmodel/types';
import { ManaBrewMatch } from './source.svelte';
import type { Transport } from './transport';
import { parseEngineMessage, type EngineMessage } from './wire';

/**
 * The server's own golden transcript (host/manabrewhttp/testdata/golden,
 * MB-11): a whole fixed-seed 2-seat game's engine→client stream for seat 0,
 * exactly as the transport writes it. Replaying it through the adapter
 * exercises every state and prompt a real game sends, in order.
 */
const golden = fileURLToPath(new URL('../../../../host/manabrewhttp/testdata/golden/2seat.ndjson.gz', import.meta.url));
const messages: EngineMessage[] = gunzipSync(readFileSync(golden)).toString('utf8').split('\n').filter((l) => l.trim() !== '').map((l) => parseEngineMessage(l)!).filter((m) => m !== null);

/** duplicates is every id that occurs twice in a list the board renders keyed by id. */
function duplicates(v: View): string[] {
  const out: string[] = [];
  const seen = new Set<number>();
  for (const p of v.players) {
    for (const c of [...(p.hand ?? []), ...p.battlefield, ...p.graveyard, ...p.exile, ...p.command]) {
      if (seen.has(c.id)) out.push(`card ${c.id}`);
      seen.add(c.id);
    }
  }
  const stack = new Set<number>();
  for (const s of v.stack) {
    if (stack.has(s.id)) out.push(`stack ${s.id}`);
    stack.add(s.id);
  }
  const d = v.decision;
  if (d) {
    const idx = new Set<number>();
    for (const o of d.options) {
      if (idx.has(o.index)) out.push(`option ${o.index}`);
      idx.add(o.index);
    }
    const pay = new Set<string>();
    for (const a of d.payment_actions ?? []) {
      if (pay.has(a.id)) out.push(`payment ${a.id}`);
      pay.add(a.id);
    }
  }
  return out;
}

describe('the server golden transcript through ManaBrewMatch', () => {
  it('replays every message: each prompt binds, the board steps, and no rendered list repeats a key', async () => {
    expect(messages.length).toBeGreaterThan(100);
    let push: ((m: EngineMessage) => void) | null = null;
    const transport: Transport = {
      fetchState: async () => [messages[0]],
      send: async () => {},
      openStream: (_u, on) => {
        push = on;
        return { close: () => {} };
      },
    };
    const m = new ManaBrewMatch('t1', { seat: 0, token: 'x' }, { transport, fetchMatches: async () => [{ table: 't1', match: 1, seed: 1, seats: [], state: 'live', winner: null, events: 1, turns: 1, bot_policy: '' }] });
    const events: ModelEvent[] = [];
    m.onModel((e) => events.push(e));
    expect(await m.start()).toEqual({ ok: true });
    const dups: string[] = [];
    let prompts = 0;
    for (const msg of messages.slice(1)) {
      push!(msg);
      if (msg.kind === 'prompt') {
        prompts++;
        expect(m.view?.decision?.seq).toBe(msg.promptId);
      }
      dups.push(...duplicates(m.view!));
    }
    expect(dups).toEqual([]);
    expect(prompts).toBeGreaterThan(50);
    expect(events.filter((e) => e.type === 'step').length).toBeGreaterThan(50);
    expect(m.dvr.events.length).toBeGreaterThan(20);
    expect(m.dvr.turnStarts.length).toBeGreaterThan(1);
    expect(m.notice).toBeNull();
  });
});
