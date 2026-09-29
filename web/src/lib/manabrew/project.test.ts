import { describe, expect, it } from 'vitest';
import type { CardView } from '../../protocol';
import { diffViews } from '../clientmodel/snapshotdiff';
import { gorgeStep, mbStep, projectView, rememberCards } from './project';
import { allRecords, carriedView, runs, stateOf } from './testdata/fixture';
import type { GameViewDto } from './wire';

const project = (gv: GameViewDto, viewer = 0, known?: Map<number, CardView>) => projectView(gv, { viewer, known });

describe('projectView (captured from a real gorged -manabrew)', () => {
  it.each(allRecords.map((r, i) => [i, r] as const))('record %i matches the native view on every carried field', (_i, r) => {
    expect(carriedView(project(stateOf(r)))).toEqual(carriedView(r.native.view));
  });

  it.each(runs.map((r) => [r.run, r] as const))('%s: diffs of projected snapshots equal diffs of native views', (_name, run) => {
    const known = new Map<number, CardView>();
    for (let i = 1; i < run.records.length; i++) {
      const a = run.records[i - 1];
      const b = run.records[i];
      const pa = project(stateOf(a), 0, known);
      rememberCards(pa, known);
      const pb = project(stateOf(b), 0, known);
      const strip = (ts: ReturnType<typeof diffViews>) => ts.map((t) => ('card' in t ? { ...t, card: undefined } : t));
      expect(strip(diffViews(pa, pb))).toEqual(strip(diffViews(a.native.view, b.native.view)));
    }
  });

  it('the cast run shows a hand → stack flight for the spell', () => {
    const run = runs.find((r) => r.run === 'cast')!;
    const known = new Map<number, CardView>();
    let moves: string[] = [];
    for (let i = 1; i < run.records.length; i++) {
      const pa = project(stateOf(run.records[i - 1]), 0, known);
      rememberCards(pa, known);
      const pb = project(stateOf(run.records[i]), 0, known);
      moves = moves.concat(diffViews(pa, pb).filter((t) => t.kind === 'move').map((t) => (t.kind === 'move' ? `${t.from.zone}>${t.to.zone}` : '')));
    }
    expect(moves).toContain('hand>stack');
  });

  it('a stack spell carries the face last seen in hand', () => {
    const run = runs.find((r) => r.run === 'cast')!;
    const known = new Map<number, CardView>();
    for (const r of run.records) {
      const v = project(stateOf(r), 0, known);
      for (const s of v.stack) if (s.kind === 'spell') expect(s.card?.types).not.toBe('');
      rememberCards(v, known);
    }
  });

  it('maps every step both ways', () => {
    for (const s of ['untap', 'upkeep', 'draw', 'main1', 'begin-combat', 'declare-attackers', 'declare-blockers', 'combat-damage', 'end-combat', 'main2', 'end', 'cleanup']) {
      expect(gorgeStep(mbStep(s))).toBe(s);
    }
    expect(gorgeStep('combatFirstStrikeDamage')).toBe('combat-damage');
    expect(gorgeStep('nope')).toBe('');
  });

  it('a hidden entry becomes a face-down card with a negative, stable id', () => {
    const gv = stateOf(allRecords[0]);
    const withHidden: GameViewDto = { ...gv, zones: gv.zones.map((z) => (z.zone === 'exile' && z.ownerId === 'player-1' ? { ...z, cards: [{ visibility: 'hidden', id: 'h-exile-1-0' }], count: 1 } : z)) };
    const a = project(withHidden).players[1].exile;
    const b = project(withHidden).players[1].exile;
    expect(a).toHaveLength(1);
    expect(a[0].face_down).toBe(true);
    expect(a[0].id).toBeLessThan(0);
    expect(a[0].id).toBe(b[0].id);
  });

  it('game over with no winner is a draw', () => {
    const gv = { ...stateOf(allRecords[0]), gameOver: true, winnerId: null };
    const v = project(gv);
    expect(v.over).toBe(true);
    expect(v.draw).toBe(true);
    const won = project({ ...gv, winnerId: 'player-1' });
    expect(won.winner).toBe(1);
    expect(won.draw).toBe(false);
  });
});
