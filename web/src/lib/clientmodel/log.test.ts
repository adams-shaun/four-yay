import { describe, expect, it } from 'vitest';
import { transitionLine } from './log';

const name = (s: number) => (s === 0 ? 'You' : 'Mira');

describe('transitionLine', () => {
  it('renders each kind worth a line', () => {
    expect(transitionLine({ kind: 'move', obj: null, from: { seat: 1, zone: 'library' }, to: { seat: 1, zone: 'hand' } }, name)).toBe('Mira draws a card');
    expect(transitionLine({ kind: 'move', obj: 3, name: 'Bolt', from: { seat: 0, zone: 'hand' }, to: { seat: 0, zone: 'stack' } }, name)).toBe('Bolt hand → stack');
    expect(transitionLine({ kind: 'life', seat: 1, from: 20, to: 17 }, name)).toBe('Mira loses 3 life (20 → 17)');
    expect(transitionLine({ kind: 'damage', to: { seat: 0 }, amount: 2 }, name)).toBe('You is dealt 2 damage');
    expect(transitionLine({ kind: 'counter', on: { obj: 5 }, counter: '+1/+1', delta: 1 }, name)).toBe('#5 gets 1 +1/+1 counter');
    expect(transitionLine({ kind: 'stack', op: 'push', obj: 9, name: 'Soul Warden' }, name)).toBe('Soul Warden goes on the stack');
    expect(transitionLine({ kind: 'appear', obj: 2, name: 'Soldier', to: { seat: 0, zone: 'battlefield' } }, name)).toBe('Soldier enters the battlefield');
  });

  it('skips taps and moves into nowhere', () => {
    expect(transitionLine({ kind: 'tap', obj: 1, tapped: true }, name)).toBeNull();
    expect(transitionLine({ kind: 'move', obj: 1, from: { seat: 0, zone: 'battlefield' }, to: { seat: null, zone: 'other' } }, name)).toBeNull();
  });
});
