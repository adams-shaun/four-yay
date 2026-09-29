import { describe, expect, it } from 'vitest';
import { AUTO_PASS_CAP } from './seatpanel.svelte';
import { repeatDecision as decision, repeatStack as stack, repeatView as view } from './repeat.fixture';
import { REPEAT_BEAT_MS, REPEAT_MAX, REPEAT_PASSES_PER_ITERATION, repeatPassAllowed, repeatStep, repeatTarget, type RepeatPlan } from './repeat';

const plan = (target = 3): RepeatPlan => ({ sourceName: 'Altar', abilityText: decision().options[0].label, target, done: 1, ownStackIds: new Set() });

describe('repeatStep — pure wire-order policy', () => {
  it('counts the manual activation as 1 and finishes only after the final stack resolves', () => {
    const p = plan(1);
    expect(repeatStep(p, decision(), view(null, [stack()]), 0)).toEqual({ act: 'pass', index: 3 });
    expect(repeatStep(p, decision(), view(), 0)).toEqual({ act: 'halt', reason: 'done' });
    p.target = 2;
    expect(repeatStep(p, decision(), view(), 0)).toEqual({ act: 'activate', index: 9 });
    expect(p.done).toBe(1); // caller counts issued activations, never the policy
    p.done++;
    expect(repeatStep(p, decision(), view(), 0)).toEqual({ act: 'halt', reason: 'done' });
  });

  it('requires BOTH the source name and the ability label; duplicate matches take first wire order', () => {
    const d = decision();
    d.options.unshift({ ...d.options[0], index: 90 });
    expect(repeatStep(plan(), d, view(), 0)).toEqual({ act: 'activate', index: 90 });
    for (const change of [{ sourceName: 'Other' }, { abilityText: 'Other' }]) {
      expect(repeatStep({ ...plan(), ...change }, d, view(), 0)).toEqual({ act: 'halt', reason: 'not_offered' });
    }
    expect(repeatStep(plan(), { ...d, options: [d.options[2]] }, view(), 0)).toEqual({ act: 'halt', reason: 'not_offered' });
    const mana = { ...d, options: [{ ...d.options[0], kind: 'activate' }] };
    expect(repeatStep(plan(), mana, view(), 0)).toEqual({ act: 'halt', reason: 'not_offered' });
  });

  it('passes own objects, stops on new opponent objects anywhere in the stack, remembers known own ids', () => {
    const p = plan();
    expect(repeatStep(p, decision(), view(null, [stack()]), 0)).toEqual({ act: 'pass', index: 3 });
    expect(p.ownStackIds.size).toBe(0); // pure: observation belongs to the driver
    expect(repeatStep(p, decision(), view(null, [stack(31, 1), stack()]), 0)).toEqual({ act: 'halt', reason: 'opponent_stack' });
    p.ownStackIds.add(31);
    expect(repeatStep(p, decision(), view(null, [stack(31, 1)]), 0)).toEqual({ act: 'pass', index: 3 });
  });

  it('unanswered decisions and malformed priority windows halt instead of inventing an answer', () => {
    expect(repeatStep(plan(), { ...decision(), kind: 'target' }, view(), 0)).toEqual({ act: 'halt', reason: 'unanswered_decision' });
    expect(repeatStep(plan(), { ...decision(), options: [] }, view(null, [stack()]), 0)).toEqual({ act: 'halt', reason: 'unanswered_decision' });
  });

  it('unsettled mana selection is payment, not a guessed mana or sticky answer', () => {
    const d = { ...decision(), kind: 'choose', options: [{ index: 8, kind: 'mana', label: 'Add B', player: 0 }] };
    expect(repeatStep(plan(), d, view(), 0)).toEqual({ act: 'halt', reason: 'payment' });
  });

  it('game over outranks every decision and stack condition', () => {
    expect(repeatStep(plan(), decision(), { ...view(null, [stack(31, 1)]), over: true }, 0)).toEqual({ act: 'halt', reason: 'game_over' });
  });

  it('bounds each iteration independently of the ordinary auto-pass cap', () => {
    expect(REPEAT_PASSES_PER_ITERATION).toBe(64);
    expect(repeatPassAllowed(AUTO_PASS_CAP)).toBe(true);
    expect(repeatPassAllowed(63)).toBe(true);
    expect(repeatPassAllowed(64)).toBe(false);
    expect(REPEAT_BEAT_MS).toBe(30);
  });

  it('clamps finite integer targets to 1..999 and defaults an empty/invalid input to 10', () => {
    expect(REPEAT_MAX).toBe(999);
    expect([-20, 0, 1, 20.8, 999, 1000, NaN].map(repeatTarget)).toEqual([1, 1, 1, 20, 999, 999, 10]);
  });
});
