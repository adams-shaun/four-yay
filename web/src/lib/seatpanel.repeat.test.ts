import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, View } from '../protocol';
import { AUTO_PASS_CAP, SeatPanelState } from './seatpanel.svelte';
import { REPEAT_BEAT_MS, REPEAT_PASSES_PER_ITERATION } from './repeat';
import { repeatCard, repeatDecision as decision, repeatManaDecision as manaDecision, repeatStack as stack, repeatView as view } from './repeat.fixture';
import { ruleFromAnswer, saveSticky } from './sticky';
import { hotkeyAction } from './hotkeys';

const { postIntent, fetchPending } = vi.hoisted(() => ({ postIntent: vi.fn(), fetchPending: vi.fn() }));
vi.mock('./api', async (original) => ({ ...(await original<typeof import('./api')>()), postIntent, fetchPending }));
let id = 0;
const beat = () => vi.advanceTimersByTimeAsync(REPEAT_BEAT_MS);
const deliver = (p: SeatPanelState, d: Decision | null, v: View = view(d)) => { p.adoptView(d); p.considerAuto(v); };
const ask = (seq: number): Decision => ({ seq, player: 0, source: 20, kind: 'choose', prompt: 'Sacrifice', min: 1, max: 1,
  options: [{ index: 5, kind: 'sacrifice', label: 'Miner', obj: 21, player: 0 }] });
function sticky(p: SeatPanelState, d: Decision) {
  const r = ruleFromAnswer(d, view(), [5])!;
  saveSticky(p.table, p.match, new Map([[r.key, r]]), null);
}
async function manual(): Promise<SeatPanelState> {
  const p = new SeatPanelState(`repeat-${++id}`, 1, { seat: 0, token: 'test' }, null, null);
  p.settings = { ...p.settings, autoPass: false, passAfterAct: false, pacing: { stepMs: 999, resolveMs: 999 }, logAutoPasses: true };
  p.skipEmpty = false;
  deliver(p, decision());
  p.click(9);
  await Promise.resolve();
  expect(p.repeatCandidate).toMatchObject({ sourceName: 'Altar', abilityText: decision().options[0].label });
  return p;
}

beforeEach(() => { vi.useFakeTimers(); postIntent.mockReset(); postIntent.mockResolvedValue(undefined); fetchPending.mockReset(); fetchPending.mockResolvedValue(null); });
afterEach(() => vi.useRealTimers());

describe('repeat driver', () => {
  it('arm → N activations, sticky sacrifice asks, own stack passes → done, with ONE summary', async () => {
    const p = await manual();
    sticky(p, ask(2));
    deliver(p, decision(2), view(decision(2), [stack()]));
    expect(p.repeatCandidate?.stackId).toBe(30);
    p.armRepeat(3);
    expect(p.repeatPlan?.done).toBe(1);
    await vi.advanceTimersByTimeAsync(29);
    expect(postIntent).toHaveBeenCalledTimes(1); // own beat, not settings.pacing
    await vi.advanceTimersByTimeAsync(1);
    for (const [seq, done] of [[3, 2], [6, 3]]) {
      deliver(p, decision(seq));
      await beat();
      expect(p.repeatPlan?.done).toBe(done);
      deliver(p, ask(seq + 1));
      expect(postIntent.mock.calls.at(-1)![2].seq).toBe(seq); // sticky paced too
      await beat();
      expect(p.stickyAnswered?.seq).toBe(seq + 1);
      deliver(p, decision(seq + 2), view(decision(seq + 2), [stack(seq + 100)]));
      expect(p.repeatPlan?.ownStackIds.has(seq + 100)).toBe(true);
      await beat();
    }
    deliver(p, decision(9));
    expect(p.oneShot).toBe('none');
    expect(p.repeatHalt?.reason).toBe('done');
    expect(postIntent.mock.calls.map((c) => c[2].choices)).toEqual([[9], [3], [9], [5], [3], [9], [5], [3]]);
    expect(p.autoRun).toBe(0);
    expect(p.autoLog.map((n) => n.text)).toEqual(['Repeat Altar ×3: done 3, halted: done']);
    p.considerAuto(view(decision(9)));
    p.cancelRun();
    expect(p.autoLog).toHaveLength(1);
  });

  it.each(['cancel-run', 'Escape', 'undo'] as const)('%s stops mid-run and abandons its scheduled POST', async (how) => {
    const p = await manual();
    deliver(p, decision(2));
    p.armRepeat(20);
    if (how === 'undo') p.rewind();
    else if (how === 'Escape') {
      expect(hotkeyAction({ key: 'Escape', ctrlKey: false, shiftKey: false, metaKey: false })).toBe('cancel-run');
      p.onKeydown('Escape');
    } else p.cancelRun();
    await beat();
    expect(postIntent).toHaveBeenCalledTimes(1);
    expect(p.repeatHalt?.reason).toBe(how === 'undo' ? 'undo' : 'cancelled');
    expect(p.autoLog).toHaveLength(1);
    expect(p.oneShot).toBe('none');
    if (how === 'undo') {
      deliver(p, decision());
      p.armRepeat(20);
      await beat();
      expect(p.machinePaused).toBe(true);
      expect(postIntent).toHaveBeenCalledTimes(1);
    }
  });

  it('bounds passes per iteration, exempts them from AUTO_PASS_CAP, and resets allowance on activation', async () => {
    const p = await manual();
    p.autoRun = AUTO_PASS_CAP;
    p.armRepeat(3);
    let seq = 2;
    for (let iteration = 0; iteration < 2; iteration++) {
      for (let i = 0; i < REPEAT_PASSES_PER_ITERATION; i++) {
        const d = decision(seq++);
        deliver(p, d, view(d, [stack()]));
        await beat();
        expect(p.oneShot).toBe('repeat');
      }
      if (!iteration) { deliver(p, decision(seq++)); await beat(); }
    }
    expect(p.autoRun).toBe(AUTO_PASS_CAP);
    expect(p.runPassed).toBe(128);
    const d = decision(seq);
    deliver(p, d, view(d, [stack()]));
    expect(p.repeatHalt?.reason).toBe('unanswered_decision');
    expect(p.autoLog).toHaveLength(1);
  });

  it.each(['unanswered_decision', 'payment', 'opponent_stack', 'not_offered', 'game_over'] as const)('halts with %s before any automatic post', async (reason) => {
    const p = await manual();
    p.armRepeat(2);
    let d = reason === 'unanswered_decision' || reason === 'payment' ? ask(2) : decision(2);
    if (reason === 'payment') d = { ...d, min: 2, max: 2, options: [{ ...d.options[0], kind: 'mana' }] };
    if (reason === 'not_offered') d = { ...d, options: [] };
    const v = view(d, reason === 'opponent_stack' ? [stack(31, 1)] : []);
    if (reason === 'game_over') { v.over = true; v.decision = null; }
    deliver(p, v.decision ?? null, v);
    expect(p.repeatHalt?.reason).toBe(reason);
    await beat();
    expect(postIntent).toHaveBeenCalledTimes(1);
  });

  it('a remembered/default auto-order answer cannot carry a repeat past a missing sticky', async () => {
    const p = await manual();
    p.armRepeat(2);
    const d = { ...ask(2), kind: 'trigger_order', min: 2, max: 2, options: [
      { index: 3, kind: 'trigger', label: 'Same', player: 0 }, { index: 5, kind: 'trigger', label: 'Same', player: 0 },
    ] };
    deliver(p, d);
    await beat();
    expect(p.repeatHalt?.reason).toBe('unanswered_decision');
    expect(postIntent).toHaveBeenCalledTimes(1);
  });

  it('POST errors halt without retry, including a rejected sticky continuation', async () => {
    const p = await manual();
    sticky(p, ask(2));
    p.armRepeat(3);
    postIntent.mockRejectedValueOnce(new Error('refused'));
    deliver(p, ask(2));
    await beat();
    expect(p.repeatHalt?.reason).toBe('post_error');
    expect(p.error).toBe('refused');
    deliver(p, ask(2));
    await beat();
    expect(postIntent).toHaveBeenCalledTimes(2);
    expect(p.autoLog).toHaveLength(1);
  });

  it('accepted mana activations arm from the card only, never a same-source trigger stack tile', async () => {
    const p = new SeatPanelState('repeat-mana', 1, { seat: 0, token: 'test' }, null, null);
    const d = manaDecision();
    deliver(p, d);
    p.click(9);
    await Promise.resolve();
    expect(postIntent).toHaveBeenCalledTimes(1);
    expect(p.repeatCandidate).toMatchObject({ source: 20, usesStack: false, stackId: null });
    deliver(p, null, view(null, [stack()]));
    expect(p.repeatCandidate?.stackId).toBeNull();
    p.armRepeat(20);
    expect(p.oneShot).toBe('repeat');
    expect(p.repeatPlan).toMatchObject({ done: 1, target: 20 });
  });

  it('repeats the Miner mana line: sacrifice → B → Rakdos target → crime pay → passes, N=3 → done', async () => {
    const p = new SeatPanelState('repeat-miner', 1, { seat: 0, token: 'test' }, null, null);
    p.settings = { ...p.settings, autoPass: false, passAfterAct: false };
    p.skipEmpty = false;
    const minerView = (d: Decision | null, entries: View['stack'] = []) => {
      const v = view(d, entries);
      v.players[0].battlefield = [
        { ...repeatCard, name: 'Phyrexian Altar' },
        { ...repeatCard, id: 21, name: 'Forsaken Miner' },
        { ...repeatCard, id: 22, name: 'Rakdos, the Muscle' },
      ];
      v.players[0].pool = { B: 1 }; // leftover mana must not stop repetition
      return v;
    };
    const sacrifice = { ...ask(2), options: [{ ...ask(2).options[0], label: 'Forsaken Miner' }] };
    const colour: Decision = { ...ask(3), prompt: 'Choose mana colour', options: [
      { index: 6, kind: 'mana', label: 'R', mana_symbol: 'R', player: 0 },
      { index: 7, kind: 'mana', label: 'B', mana_symbol: 'B', player: 0 },
    ] };
    const target: Decision = { ...ask(4), source: 22, kind: 'target', prompt: 'Rakdos target', options: [
      { index: 8, kind: 'player', label: 'Opponent', player: 1 },
    ] };
    const pay: Decision = { ...ask(6), source: 21, prompt: 'Return Forsaken Miner?', options: [
      { index: 10, kind: 'trigger_cost_pay', label: 'Pay {B}', player: 0 },
      { index: 11, kind: 'trigger_cost_decline', label: 'Decline', player: 0 },
    ] };
    const rules = [sacrifice, colour, target, pay].map((d, i) => ruleFromAnswer(d, minerView(d), [[5], [7], [8], [10]][i])!);
    saveSticky(p.table, p.match, new Map(rules.map((r) => [r.key, r])), null);
    deliver(p, manaDecision(), minerView(manaDecision()));
    p.click(9);
    await Promise.resolve();
    expect(p.repeatCandidate).toMatchObject({ sourceName: 'Phyrexian Altar', stackId: null });
    p.armRepeat(3);
    let seq = 2;
    for (let iteration = 1; iteration <= 3; iteration++) {
      const rakdos = { ...stack(100 + iteration * 2), source: 22, name: 'Rakdos, the Muscle' };
      const crime = { ...stack(101 + iteration * 2), source: 21, name: 'Forsaken Miner' };
      for (const [template, entries, choice] of [
        [sacrifice, [], 5], [colour, [], 7], [target, [rakdos], 8],
        [manaDecision(), [crime, rakdos], 3], [pay, [crime, rakdos], 10],
        [manaDecision(), [rakdos], 3],
      ] as const) {
        const d = { ...template, seq: seq++ };
        deliver(p, d, minerView(d, [...entries]));
        await beat();
        expect(postIntent.mock.calls.at(-1)![2]).toMatchObject({ seq: d.seq, choices: [choice] });
        expect(p.repeatHalt).toBeNull();
        if (d.kind !== 'priority') expect(p.stickyAnswered?.seq).toBe(d.seq);
      }
      expect(p.repeatPlan?.done).toBe(iteration);
      const next = manaDecision(seq++);
      deliver(p, next, minerView(next));
      await beat();
    }
    expect(p.repeatHalt?.reason).toBe('done');
    expect(p.oneShot).toBe('none');
    expect(postIntent.mock.calls.map((c) => c[2].choices)).toEqual(
      Array.from({ length: 3 }, () => [[9], [5], [7], [8], [3], [10], [3]]).flat(),
    );
    expect(p.autoLog.map((n) => n.text)).toEqual(['Repeat Phyrexian Altar ×3: done 3, halted: done']);
  });

  it('an untaught one-pick mana colour halts unanswered, not payment', async () => {
    const p = await manual();
    p.armRepeat(3);
    deliver(p, { ...ask(2), options: [{ index: 7, kind: 'mana', label: 'B', mana_symbol: 'B', player: 0 }] });
    await beat();
    expect(p.repeatHalt?.reason).toBe('unanswered_decision');
    expect(postIntent).toHaveBeenCalledTimes(1);
  });

  it('a new explicit one-shot can take over the window held by a repeat halt', async () => {
    const p = await manual();
    deliver(p, decision(2));
    p.armRepeat(20);
    p.cancelRun();
    p.considerAuto(view(decision(2)));
    await beat();
    expect(postIntent).toHaveBeenCalledTimes(1);
    p.settings = { ...p.settings, pacing: { stepMs: 0, resolveMs: 0 } };
    p.startEndTurn(view());
    p.considerAuto(view(decision(2)));
    await Promise.resolve();
    expect(postIntent.mock.calls.at(-1)![2]).toMatchObject({ seq: 2, choices: [3] });
  });

  it('refused hand activations never offer arming', async () => {
    const p = new SeatPanelState('repeat-refused', 1, { seat: 0, token: 'test' }, null, null);
    deliver(p, decision());
    postIntent.mockRejectedValueOnce(new Error('refused'));
    p.click(9);
    await Promise.resolve();
    expect(p.repeatCandidate).toBeNull();
    p.armRepeat(3);
    expect(p.oneShot).toBe('none');
  });

  it('revalidates after a changed view and cancels when the component destroys its timer', async () => {
    const p = await manual();
    deliver(p, decision(2));
    p.armRepeat(20);
    p.considerAuto(view(decision(2), [stack(31, 1)]));
    await beat();
    expect(p.repeatHalt?.reason).toBe('opponent_stack');
    expect(postIntent).toHaveBeenCalledTimes(1);
    const q = await manual();
    deliver(q, decision(2));
    q.armRepeat(20);
    q.cancelPass();
    await beat();
    expect(postIntent).toHaveBeenCalledTimes(2);
  });

  it('continues a fresh SSE decision received during a POST, but undo invalidates old responses', async () => {
    const p = await manual();
    let accept!: () => void;
    postIntent.mockImplementationOnce(() => new Promise<void>((resolve) => { accept = resolve; }));
    deliver(p, decision(2));
    p.armRepeat(3);
    await beat();
    expect(p.busy).toBe(true);
    deliver(p, decision(3), view(decision(3), [stack()]));
    accept();
    await Promise.resolve();
    await beat();
    expect(postIntent.mock.calls.at(-1)![2]).toMatchObject({ seq: 3, choices: [3] });
    postIntent.mockImplementationOnce(() => new Promise<void>((resolve) => { accept = resolve; }));
    deliver(p, decision(4));
    await beat();
    p.rewind();
    accept();
    await Promise.resolve();
    expect(p.repeatHalt?.reason).toBe('undo');
    expect(p.postedSeq).toBeNull();
  });
});
