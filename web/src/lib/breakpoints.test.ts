import { describe, expect, it } from 'vitest';
import { checkBreakpoints, targetsSeat } from './breakpoints';
import { noBreakpoints, type Breakpoints } from './playsettings';
import type { View } from '../protocol';

const card = (id: number, controller: number, name: string, extra: Record<string, unknown> = {}) => ({ id, controller, owner: controller, name, ...extra });
const top = (id: number, controller: number, name: string, targets: { obj?: number; player: number; is_player: boolean }[] = []) =>
  ({ id, controller, name, kind: 'spell', text: '', targets, optional: false });

const mkView = (over: Partial<Record<string, unknown>> = {}): View =>
  ({
    turn: 7,
    active: 1,
    step: 'main1',
    stack: [],
    players: [
      { seat: 0, battlefield: [card(10, 0, 'Dark Confidant')], hand: [], graveyard: [], exile: [] },
      { seat: 1, battlefield: [card(20, 1, 'Thragtusk')], hand: [], graveyard: [], exile: [] },
    ],
    ...over,
  }) as unknown as View;

const bp = (over: Partial<Breakpoints>): Breakpoints => ({ ...noBreakpoints(), ...over });
const run = (view: View, b: Breakpoints, fired = new Map<string, number>(), seq = 5, skipTop = false) =>
  checkBreakpoints({ view, seat: 0, bp: b, fired, seq, skipTop });

describe('targetsSeat', () => {
  it('matches a player target on the seat and an object the seat controls', () => {
    const v = mkView();
    expect(targetsSeat(v, 0, top(1, 1, 'Bolt', [{ player: 0, is_player: true }]))).toBe(true);
    expect(targetsSeat(v, 0, top(1, 1, 'Bolt', [{ obj: 10, player: 0, is_player: false }]))).toBe(true);
    expect(targetsSeat(v, 0, top(1, 1, 'Bolt', [{ obj: 20, player: 1, is_player: false }]))).toBe(false);
  });
});

describe('checkBreakpoints', () => {
  it('all off never fires', () => {
    const v = mkView({ stack: [top(1, 1, 'Bolt', [{ player: 0, is_player: true }])] });
    expect(run(v, noBreakpoints())).toBeNull();
  });

  it('targets-me fires for an opponent object targeting my permanent, naming it', () => {
    const v = mkView({ stack: [top(1, 1, 'Lightning Bolt', [{ obj: 10, player: 0, is_player: false }])] });
    expect(run(v, bp({ targetsMe: true }))).toEqual({ kind: 'targets-me', key: 'targets:1', detail: 'Lightning Bolt targets Dark Confidant' });
  });

  it('targets-me says "you" for a player target and ignores my own objects', () => {
    const mine = mkView({ stack: [top(1, 0, 'Bolt', [{ player: 0, is_player: true }])] });
    expect(run(mine, bp({ targetsMe: true }))).toBeNull();
    const theirs = mkView({ stack: [top(2, 1, 'Bolt', [{ player: 0, is_player: true }])] });
    expect(run(theirs, bp({ targetsMe: true }))?.detail).toBe('Bolt targets you');
  });

  it('watchlist matches the top object name case-insensitively, opponents only', () => {
    const v = mkView({ stack: [top(3, 1, "Thassa's Oracle")] });
    expect(run(v, bp({ watchlist: ["thassa's oracle"] }))).toEqual({ kind: 'watchlist', key: 'watch:3', detail: "Thassa's Oracle is on your watchlist" });
    const mine = mkView({ stack: [top(3, 0, "Thassa's Oracle")] });
    expect(run(mine, bp({ watchlist: ["Thassa's Oracle"] }))).toBeNull();
  });

  it('watchlist also matches the stack object’s card name (an ability named differently)', () => {
    const t = { ...top(4, 1, 'Triggered ability'), card: { id: 99, name: 'Rhystic Study' } };
    expect(run(mkView({ stack: [t] }), bp({ watchlist: ['Rhystic Study'] }))?.kind).toBe('watchlist');
  });

  it('attacked fires when an opponent creature attacks me, keyed per turn', () => {
    const players = [
      { seat: 0, battlefield: [], hand: [], graveyard: [], exile: [] },
      { seat: 1, battlefield: [card(20, 1, 'Thragtusk', { attacking: true, attacking_player: 0 }), card(21, 1, 'Elf', { attacking: true, attacking_player: 2 })], hand: [], graveyard: [], exile: [] },
    ];
    const v = mkView({ players, step: 'declare-attackers' });
    expect(run(v, bp({ attacked: true }))).toEqual({ kind: 'attacked', key: 'attacked:7', detail: '1 creature is attacking you' });
  });

  it('stack-depth fires at the threshold, keyed on the top object', () => {
    const v = mkView({ stack: [top(1, 1, 'A'), top(2, 0, 'B')] });
    expect(run(v, bp({ stackDepth: 3 }))).toBeNull();
    expect(run(v, bp({ stackDepth: 2 }))).toEqual({ kind: 'stack-depth', key: 'depth:2', detail: '2 objects are on the stack' });
  });

  it('a hit already fired at an earlier seq does not fire again; the same seq does', () => {
    const v = mkView({ stack: [top(1, 1, 'Bolt', [{ player: 0, is_player: true }])] });
    const b = bp({ targetsMe: true });
    expect(run(v, b, new Map([['targets:1', 5]]), 5)?.key).toBe('targets:1');
    expect(run(v, b, new Map([['targets:1', 4]]), 5)).toBeNull();
  });

  it('skipTop suppresses the top-object rules but not attacked', () => {
    const players = [
      { seat: 0, battlefield: [], hand: [], graveyard: [], exile: [] },
      { seat: 1, battlefield: [card(20, 1, 'Thragtusk', { attacking: true, attacking_player: 0 })], hand: [], graveyard: [], exile: [] },
    ];
    const v = mkView({ players, stack: [top(1, 1, 'Bolt', [{ player: 0, is_player: true }])] });
    expect(run(v, bp({ targetsMe: true, attacked: true }), new Map(), 5, true)?.kind).toBe('attacked');
    expect(run(v, bp({ targetsMe: true }), new Map(), 5, true)).toBeNull();
  });

  it('order: targets-me, then watchlist, then attacked, then stack depth', () => {
    const v = mkView({ stack: [top(1, 1, 'Bolt'), top(2, 1, 'Oracle', [{ player: 0, is_player: true }])] });
    expect(run(v, bp({ targetsMe: true, watchlist: ['Oracle'], stackDepth: 2 }))?.kind).toBe('targets-me');
  });
});
