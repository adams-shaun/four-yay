import { describe, expect, it } from 'vitest';
import { compatPass, COMPAT_KEY, loadCompat, nothingToDo, saveCompat } from './compat';
import type { AgentPrompt, AvailableAction } from './wire';

const ask = (actions: AvailableAction[]): AgentPrompt => ({ promptId: 7, decidingPlayerId: 'p1', input: { type: 'chooseAction', actions } }) as AgentPrompt;
const mana: AvailableAction = { id: 'a1', type: 'activateAbility', cardId: 'c1', isManaAbility: true, producedMana: [{ color: 'G', amount: 1 }] };

describe('ManaBrew compat auto-pass', () => {
  it('passes an empty action list and a mana-only one', () => {
    expect(nothingToDo(ask([]))).toBe(true);
    expect(nothingToDo(ask([mana, { ...mana, id: 'a2' }]))).toBe(true);
    expect(nothingToDo(ask([mana, { id: 'u', type: 'undoMana' }]))).toBe(true);
  });

  it('shows the prompt when there is a cast or a non-mana ability', () => {
    expect(nothingToDo(ask([mana, { id: 'pay-5', type: 'cast', cardId: 'c5' }]))).toBe(false);
    expect(nothingToDo(ask([{ id: 'a3', type: 'activateAbility', cardId: 'c3', isManaAbility: false }]))).toBe(false);
  });

  it('never touches another prompt type', () => {
    expect(nothingToDo({ promptId: 1, decidingPlayerId: 'p1', input: { type: 'chooseNumber', min: 0, max: 3 } } as AgentPrompt)).toBe(false);
  });

  it('answers with a plain pass for that prompt id', () => {
    expect(compatPass(ask([]))).toEqual({ kind: 'response', promptId: 7, action: { type: 'chooseAction', output: { type: 'pass', exhaustStack: false } } });
  });

  it('is on by default and remembers off', () => {
    const m = new Map<string, string>();
    const s = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) } as unknown as Storage;
    expect(loadCompat(s)).toBe(true);
    saveCompat(false, s);
    expect(m.get(COMPAT_KEY)).toBe('off');
    expect(loadCompat(s)).toBe(false);
    saveCompat(true, s);
    expect(loadCompat(s)).toBe(true);
    expect(loadCompat(null)).toBe(true);
  });
});
