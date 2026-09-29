import { describe, expect, it } from 'vitest';
import type { Decision, Option } from '../../protocol';
import { AnswerError, bindPrompt, undoMessage } from './prompt';
import { projectView } from './project';
import { isSearchPick } from '../search';
import { allRecords, promptOf, stateOf, type CaptureRecord } from './testdata/fixture';
import type { AgentPrompt } from './wire';

const withPrompt = allRecords.filter((r) => promptOf(r) !== null && r.native.decision !== null);

function bind(r: CaptureRecord) {
  const p = promptOf(r)!;
  return bindPrompt(p, { view: projectView(stateOf(r), { viewer: 0 }) });
}

/** identity is what makes a native option THE option: the facts the server maps answers by. */
const identity = (o: Option) => ({ kind: o.kind, obj: o.obj ?? null, player: o.kind === 'activate' || o.kind === 'play_land' || o.kind === 'cast' || o.kind === 'pass' || o.kind === 'concede' ? null : o.player, attacker: o.attacker ?? null, battle: o.battle ?? null, label: o.label });

/** expected is the ManaBrew answer the server's translator maps back to native option `o` of decision `d`. */
function expected(d: Decision, o: Option, r: CaptureRecord): unknown {
  const p = promptOf(r)!;
  switch (d.kind) {
    case 'priority':
      if (o.kind === 'pass') return { kind: 'response', promptId: d.seq, action: { type: 'chooseAction', output: { type: 'pass', exhaustStack: false } } };
      if (o.kind === 'concede') return { kind: 'directive', directive: { type: 'concede' } };
      return { kind: 'response', promptId: d.seq, action: { type: 'chooseAction', output: { type: 'act', actionId: `opt-${o.index}` } } };
    case 'target': {
      const ref = o.kind === 'player' ? { kind: 'player', id: `player-${o.player}` } : { kind: 'card', id: `o${o.obj}` };
      return { type: 'boardTargets', chosen: [expect.objectContaining(ref)] };
    }
    case 'attackers':
      return { type: 'declareAttackers', assignments: [{ attackerId: `o${o.obj}`, targetId: `player-${o.player}` }] };
    case 'blockers':
      return { type: 'declareBlockers', assignments: [{ blockerId: `o${o.obj}`, attackerId: `o${o.attacker}` }] };
    case 'mulligan':
      return { type: 'mulliganDecision', keep: o.kind === 'keep' };
    case 'starting_player':
      return { type: 'selectionDecision', chosenIndices: [o.index] };
    case 'choose':
      if (p.input.type === 'chooseCards') return { type: 'chooseCardsDecision', chosenCardIds: [`o${o.obj}`] };
      return undefined;
    default:
      return undefined;
  }
}

describe('bindPrompt (captured from a real gorged -manabrew)', () => {
  it.each(withPrompt.map((r) => [r.native.decision!.kind, r.native.decision!.seq, r] as const))('%s %i reconstructs the native decision', (_k, _s, r) => {
    const native = r.native.decision!;
    const d = bind(r).decision!;
    expect(d.seq).toBe(native.seq);
    expect(d.player).toBe(native.player);
    expect(d.kind).toBe(native.kind);
    if (native.kind !== 'priority' && native.kind !== 'attackers' && native.kind !== 'blockers') {
      expect([d.min, d.max]).toEqual([native.min, native.max]);
    }
    const mine = d.options.map(identity).sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)));
    const theirs = native.options.map(identity).sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)));
    expect(mine).toEqual(theirs);
    expect(d.options.map((o) => o.index)).toEqual(d.options.map((_, i) => i));
    expect(d.payment_actions?.map((a) => [a.id, a.cast.object, a.label]) ?? []).toEqual(native.payment_actions?.map((a) => [a.id, a.cast.object, a.label]) ?? []);
  });

  it.each(withPrompt.map((r) => [r.native.decision!.kind, r.native.decision!.seq, r] as const))('%s %i answers each option as the server maps it back', (_k, _s, r) => {
    const native = r.native.decision!;
    const b = bind(r);
    for (const o of native.options) {
      const want = expected(native, o, r);
      if (want === undefined) continue;
      const j = b.decision!.options.findIndex((x) => JSON.stringify(identity(x)) === JSON.stringify(identity(o)));
      const msg = b.respond({ seq: native.seq, player: native.player, choices: [j] });
      if (native.kind === 'priority') expect(msg).toEqual(want);
      else expect(msg).toEqual({ kind: 'response', promptId: native.seq, action: { type: promptOf(r)!.input.type, output: want } });
    }
  });

  it('an announce answers act pay-<id>; a planned payment is refused loudly', () => {
    const r = withPrompt.find((x) => (x.native.decision!.payment_actions?.length ?? 0) > 0)!;
    const b = bind(r);
    const a = b.decision!.payment_actions![0];
    expect(a.plans).toEqual([]);
    expect(b.respond({ seq: b.prompt.promptId, player: 0, choices: [], announce: { action_id: a.id } })).toEqual({
      kind: 'response', promptId: b.prompt.promptId, action: { type: 'chooseAction', output: { type: 'act', actionId: `pay-${a.id}` } },
    });
    expect(() => b.respond({ seq: b.prompt.promptId, player: 0, choices: [], payment: { action_id: a.id, plan: r.native.decision!.payment_actions![0].plans[0] } })).toThrow(AnswerError);
  });

  it('refuses a stale seq and an unoffered index', () => {
    const b = bind(withPrompt[0]);
    expect(() => b.respond({ seq: b.prompt.promptId + 1, player: 0, choices: [0] })).toThrow(AnswerError);
    expect(() => b.respond({ seq: b.prompt.promptId, player: 0, choices: [99] })).toThrow(AnswerError);
  });

  it('undo is restoreSnapshot at the open chooseAction prompt only', () => {
    const pr = withPrompt.map(promptOf).find((p) => p!.input.type === 'chooseAction')!;
    expect(undoMessage(pr)).toEqual({ kind: 'response', promptId: pr.promptId, action: { type: 'chooseAction', output: { type: 'restoreSnapshot', checkpointId: pr.promptId } } });
    const other = withPrompt.map(promptOf).find((p) => p!.input.type === 'mulligan')!;
    expect(() => undoMessage(other)).toThrow(AnswerError);
  });

  const synth = (input: AgentPrompt['input']): AgentPrompt => ({ promptId: 5, decidingPlayerId: 'player-0', input });
  it('uses CardDto identity names as search option labels', () => {
    const prompt: AgentPrompt = {
      promptId: 278,
      decidingPlayerId: 'player-0',
      input: {
        type: 'chooseCards',
        presentation: { title: 'Search a library: choose up to 1 card(s)' },
        cards: [
          { id: 'o5', identity: { name: 'Underground Sea' }, controllerId: 'player-0', ownerId: 'player-0' },
          { id: 'o6', identity: { name: 'Island' }, controllerId: 'player-0', ownerId: 'player-0' },
          { id: 'o1', identity: { name: 'Swamp' }, controllerId: 'player-0', ownerId: 'player-0' },
          { id: 'o4', identity: { name: 'Volcanic Island' }, controllerId: 'player-0', ownerId: 'player-0' },
        ],
        min: 0,
        max: 1,
      },
    };
    const d = bindPrompt(prompt, { view: null }).decision!;
    expect(d.options.map((o) => o.label)).toEqual(['Underground Sea', 'Island', 'Swamp', 'Volcanic Island']);
    expect(isSearchPick(d)).toBe(true);
  });

  it('maps the prompts gorge does not pose in the capture', () => {
    const bool = bindPrompt(synth({ type: 'chooseBoolean', confirmLabel: 'Pay', denyLabel: 'Decline' }), { view: null });
    expect(bool.decision!.options.map((o) => [o.kind, o.label])).toEqual([['yes', 'Pay'], ['no', 'Decline']]);
    expect(bool.respond({ seq: 5, player: 0, choices: [1] })).toMatchObject({ action: { output: { type: 'decision', value: false } } });

    const num = bindPrompt(synth({ type: 'chooseNumber', min: 1, max: 3 }), { view: null });
    expect(num.decision!.options.map((o) => o.amount)).toEqual([1, 2, 3]);
    expect(num.respond({ seq: 5, player: 0, choices: [2] })).toMatchObject({ action: { output: { type: 'numberDecision', chosenNumber: 3 } } });

    const col = bindPrompt(synth({ type: 'chooseColor', validColors: ['G', 'R'], amount: 2, repeatAllowed: true }), { view: null });
    expect(col.respond({ seq: 5, player: 0, choices: [1, 1] })).toMatchObject({ action: { output: { type: 'colorDecision', chosenColors: { R: 2 } } } });

    const ord = bindPrompt(synth({ type: 'reorder', items: [{ id: 'opt-0', oracle: 'A' }, { id: 'opt-1', oracle: 'B' }] }), { view: null });
    // The panel puts B on the stack first (B resolves last): ManaBrew wants A first.
    expect(ord.respond({ seq: 5, player: 0, choices: [1, 0] })).toMatchObject({ action: { output: { type: 'reorderDecision', orderedIds: ['opt-0', 'opt-1'] } } });

    const pay = bindPrompt(synth({ type: 'payManaCost', cardId: 'o9', cardName: 'Bolt', manaCost: 'R', canConfirmFromPool: true, actions: [{ id: 'opt-0', type: 'activateAbility', cardId: 'o3', description: 'Tap Mountain' }, { id: 'opt-3', type: 'autofill', description: 'Auto' }] }), { view: null });
    expect(pay.decision!.options.map((o) => o.kind)).toEqual(['mana', 'autofill', 'done', 'cancel_cast']);
    expect(pay.respond({ seq: 5, player: 0, choices: [0] })).toMatchObject({ action: { output: { type: 'act', actionId: 'opt-0' } } });
    expect(pay.respond({ seq: 5, player: 0, choices: [1] })).toMatchObject({ action: { output: { type: 'pay', auto: true } } });
    expect(pay.respond({ seq: 5, player: 0, choices: [3] })).toMatchObject({ action: { output: { type: 'cancel' } } });

    const scry = bindPrompt(synth({ type: 'scry', cards: [{ id: 'o1', identity: { name: 'A' }, controllerId: 'player-0', ownerId: 'player-0' }, { id: 'o2', identity: { name: 'B' }, controllerId: 'player-0', ownerId: 'player-0' }], zones: ['libraryTop', 'libraryBottom'] }), { view: null });
    expect(scry.decision!.kind).toBe('arrange');
    expect(scry.respond({ seq: 5, player: 0, choices: [1] })).toMatchObject({ action: { output: { type: 'scryDecision', zoneCardIds: [['o2'], ['o1']] } } });

    expect(bindPrompt(synth({ type: 'gameOver' }), { view: null }).decision).toBeNull();
  });
});
