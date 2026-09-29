import { describe, expect, it } from 'vitest';
import type { MatchInfo } from '../../protocol';
import { ApiError, fetchPending, postIntent, postUndo } from '../api';
import type { ModelEvent } from '../clientmodel/types';
import { ManaBrewMatch } from './source.svelte';
import { runs, stateOf, promptOf } from './testdata/fixture';
import { MbError, type Transport } from './transport';
import type { ClientMessage, EngineMessage } from './wire';

const seats = [{ name: 'You', deck: 'a', colour: '', human: true }, { name: 'Bot', deck: 'b', colour: '' }];
const info: MatchInfo = { table: 'g1', match: 1, seed: 1, seats, state: 'live', winner: null, events: 10, turns: 1, bot_policy: 'bot' };

function fake(first: EngineMessage[], opts: { stateError?: MbError; sendError?: MbError } = {}) {
  const sent: ClientMessage[] = [];
  const urls: string[] = [];
  let push: ((m: EngineMessage) => void) | null = null;
  const transport: Transport = {
    fetchState: async () => {
      if (opts.stateError) throw opts.stateError;
      return first;
    },
    send: async (_t, _k, _tok, msg) => {
      if (opts.sendError) throw opts.sendError;
      sent.push(msg);
    },
    openStream: (url, onMessage) => {
      urls.push(url);
      push = onMessage;
      return { close: () => { push = null; } };
    },
  };
  return { transport, sent, urls, emit: (m: EngineMessage) => push?.(m) };
}

const recordMsgs = (run: string, i: number) => runs.find((r) => r.run === run)!.records[i].manabrew;

async function started(run: string, opts: Parameters<typeof fake>[1] & { compat?: boolean } = {}) {
  const f = fake(recordMsgs(run, 0), opts);
  const m = new ManaBrewMatch('g1', { seat: 0, token: 'tok' }, { transport: f.transport, fetchMatches: async () => [info], compat: opts.compat ?? false });
  const events: ModelEvent[] = [];
  m.onModel((e) => events.push(e));
  const res = await m.start();
  return { m, f, events, res };
}

describe('ManaBrewMatch compat auto-pass', () => {
  const prompt = (promptId: number, actions: unknown[]): EngineMessage => ({ kind: 'prompt', promptId, decidingPlayerId: 'p0', input: { type: 'chooseAction', actions } }) as EngineMessage;
  const mana = { id: 'm1', type: 'activateAbility', cardId: 'c1', isManaAbility: true };

  it('passes a priority ask with nothing to do but make mana, without showing it', async () => {
    const { m, f } = await started('cast', { compat: true });
    f.emit(prompt(9000, []));
    f.emit(prompt(9001, [mana]));
    await Promise.resolve();
    expect(f.sent).toEqual([9000, 9001].map((promptId) => ({ kind: 'response', promptId, action: { type: 'chooseAction', output: { type: 'pass', exhaustStack: false } } })));
    expect(m.openPrompt).toBeNull();
    expect(m.view?.decision ?? null).toBeNull();
    expect(m.autoPassed).toBe(2);
  });

  it('shows an ask with a real play, and every ask when the setting is off', async () => {
    const on = await started('cast', { compat: true });
    on.f.emit(prompt(9000, [mana, { id: 'pay-5', type: 'cast', cardId: 'c5' }]));
    expect(on.f.sent).toEqual([]);
    expect(on.m.openPrompt?.promptId).toBe(9000);
    const off = await started('cast');
    off.f.emit(prompt(9000, []));
    expect(off.f.sent).toEqual([]);
    expect(off.m.openPrompt?.promptId).toBe(9000);
  });

  it('shows a rewound (undo) ask even when it has nothing to do', async () => {
    const { m, f } = await started('cast', { compat: true });
    f.emit(prompt(9005, [{ id: 'pay-5', type: 'cast', cardId: 'c5' }]));
    f.emit(prompt(9002, [mana]));
    expect(f.sent).toEqual([]);
    expect(m.openPrompt?.promptId).toBe(9002);
  });
});

describe('ManaBrewMatch', () => {
  it('starts from the probe, opens the stream with the seat token, and poses the open prompt', async () => {
    const { m, f, res } = await started('start');
    expect(res).toEqual({ ok: true });
    expect(m.match).toBe(1);
    expect(m.seats).toEqual(seats);
    expect(f.urls).toEqual(['/api/manabrew/v0/tables/g1/matches/1/stream?token=tok']);
    expect(m.view?.decision?.kind).toBe('starting_player');
    expect(m.clientDecision?.seq).toBe(promptOf(runs[0].records[0])!.promptId);
  });

  it('falls back with a clear reason when the server has no ManaBrew routes', async () => {
    const { res, f } = await started('start', { stateError: new MbError(404, 'not_found', 'no route') });
    expect(res.ok).toBe(false);
    expect(!res.ok && res.reason).toMatch(/not enabled/);
    expect(f.urls).toEqual([]);
  });

  it('diffs successive snapshots into animated steps, a log and a DVR ring', async () => {
    const { m, f, events } = await started('cast');
    const run = runs.find((r) => r.run === 'cast')!;
    for (let i = 1; i < run.records.length; i++) for (const msg of run.records[i].manabrew) f.emit(msg);
    const steps = events.filter((e): e is Extract<ModelEvent, { type: 'step' }> => e.type === 'step');
    expect(steps.length).toBe(run.records.length - 1);
    expect(steps.flatMap((s) => s.transitions).some((t) => t.kind === 'move' && t.from.zone === 'hand' && t.to.zone === 'stack')).toBe(true);
    expect(m.dvr.events.length).toBeGreaterThan(0);
    expect(m.dvr.events.map((e) => e.event.seq)).toEqual(m.dvr.events.map((_, i) => i + 1));
    expect(m.dvr.head).toBe(m.dvr.events.length);
    // Scrub to the start: the first snapshot, without a live decision.
    const liveView = m.view;
    m.dispatch({ type: 'scrub', seq: 0 });
    expect(m.view).not.toBe(liveView);
    expect(m.view?.decision).toBeNull();
    expect(m.view?.stack).toEqual([]);
    m.dispatch({ type: 'live' });
    expect(m.view?.decision?.seq).toBe(promptOf(run.records[run.records.length - 1])!.promptId);
  });

  it('an announce-able cast is a potential play of the seat, so smart stops see it', async () => {
    const { m, f } = await started('cast');
    const run = runs.find((r) => r.run === 'cast')!;
    const withPay = run.records.findIndex((r) => (r.native.decision?.payment_actions?.length ?? 0) > 0);
    expect(withPay).toBeGreaterThanOrEqual(0);
    for (const msg of run.records[withPay].manabrew) f.emit(msg);
    const me = m.view!.players.find((p) => p.seat === 0)!;
    expect(me.potential_actions?.map((a) => a.obj).sort()).toEqual(run.records[withPay].native.decision!.payment_actions!.map((a) => a.cast.object).sort());
    expect(m.view!.players.find((p) => p.seat === 1)!.potential_actions).toBeUndefined();
  });

  it('answers through the seat transport: api.ts routes the panel to /send, and the answered prompt is withdrawn', async () => {
    const { m, f } = await started('start');
    const d = await fetchPending('g1', 1, m.ctx);
    await postIntent('g1', 1, { seq: d.seq, player: 0, choices: [0] }, m.ctx);
    expect(f.sent).toEqual([{ kind: 'response', promptId: d.seq, action: { type: 'chooseFromSelection', output: { type: 'selectionDecision', chosenIndices: [0] } } }]);
    expect(m.view?.decision).toBeNull();
    await expect(fetchPending('g1', 1, m.ctx)).rejects.toMatchObject({ status: 409 });
  });

  it('surfaces a refused answer as an ApiError and a visible notice', async () => {
    const { m } = await started('start', { sendError: new MbError(409, 'stalePrompt', 'response names promptId 1', 2) });
    const d = m.clientDecision!;
    await expect(postIntent('g1', 1, { seq: d.seq, player: 0, choices: [0] }, m.ctx)).rejects.toBeInstanceOf(ApiError);
    expect(m.notice).toMatch(/stalePrompt/);
    expect(m.clientDecision?.seq).toBe(d.seq); // still open: the player may answer again
  });

  it('shows pushed protocol errors', async () => {
    const { m, f } = await started('start');
    f.emit({ kind: 'error', error: { code: 'invalidShape', message: 'prompt kind trigger_order has no ManaBrew translation yet' } });
    expect(m.notice).toMatch(/invalidShape/);
  });

  it('a lower promptId is a rewind: the panel is told and motion resets', async () => {
    const { m, f, events } = await started('cast');
    let rewound = 0;
    m.onRewind = () => rewound++;
    const later = recordMsgs('cast', 2);
    for (const msg of later) f.emit(msg);
    const before = events.length;
    for (const msg of recordMsgs('cast', 0)) f.emit(msg);
    expect(rewound).toBe(1);
    expect(events.slice(before).some((e) => e.type === 'reset')).toBe(true);
  });

  it('undo is restoreSnapshot at the open priority prompt', async () => {
    const { m, f } = await started('cast');
    await postUndo('g1', 1, m.ctx);
    const p = m.openPrompt!;
    expect(f.sent.at(-1)).toEqual({ kind: 'response', promptId: p.promptId, action: { type: 'chooseAction', output: { type: 'restoreSnapshot', checkpointId: p.promptId } } });
  });

  it('applies a stateDelta onto the last full state', async () => {
    const { m, f } = await started('start');
    f.emit({ kind: 'stateDelta', base: '', fingerprint: 'x', patch: { players: { $k: { 'player-1': { life: 13 } } } } });
    expect(m.view?.players[1].life).toBe(13);
    expect(stateOf(runs[0].records[0]).players[1].life).toBe(20);
  });
});
