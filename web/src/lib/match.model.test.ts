import { describe, expect, it, vi } from 'vitest';
import type { View } from '../protocol';
import type { ModelEvent } from './clientmodel/types';

const { fetchViewMock } = vi.hoisted(() => ({ fetchViewMock: vi.fn() }));
vi.mock('./api', () => ({ ApiError: class extends Error {}, fetchView: fetchViewMock, fetchEvents: vi.fn().mockResolvedValue([]), fetchMatches: vi.fn() }));

const { MatchState } = await import('./match.svelte');

const view = (life: number): View => ({
  viewer: 0, visibility: 'omniscient', turn: 1, round: 1, step: 'main', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, stack: [], pending: [],
  players: [{ seat: 0, name: 'Ari', life, lost: false, library_size: 40, hand_size: 0, graveyard_size: 0, hand: [], battlefield: [], graveyard: [], exile: [], command: [], commanders: [], pool: {}, completed_dungeons: 0, commander_casts: [] }],
});

async function settle(p: () => boolean) {
  for (let i = 0; i < 200 && !p(); i++) await Promise.resolve();
  if (!p()) throw new Error('settle timed out');
}

describe('MatchState as the gorge client-model adapter', () => {
  it('a live forward step emits its transitions before the view is assigned; a rewind resets', async () => {
    fetchViewMock.mockReset();
    fetchViewMock.mockResolvedValue(view(17));
    const m = new MatchState('t1');
    const seen: { e: ModelEvent; paintedLife: number | undefined }[] = [];
    m.onModel((e) => seen.push({ e, paintedLife: m.view?.players[0]?.life }));

    m.apply({ v: 1, t: 'snapshot', table: 't1', match: 1, seq: 0, body: { view: view(20), turn_starts: [0], head: 10, seats: [] } });
    m.apply({ v: 1, t: 'event', table: 't1', match: 1, seq: 11, body: { event: { seq: 11, kind: 'damage', player: 0, amount: 3 }, line: '' } });
    m.apply({ v: 1, t: 'decision', table: 't1', match: 1, seq: 12, body: { player: 0, kind: 'priority', prompt: '' } });
    await settle(() => m.view?.players[0].life === 17);

    expect(seen.map((s) => s.e.type)).toEqual(['reset', 'step']);
    const step = seen[1];
    expect(step.paintedLife).toBe(20); // emitted while the old board was still painted
    expect(step.e.type === 'step' && step.e.transitions).toEqual([
      { kind: 'damage', to: { seat: 0 }, amount: 3, seq: 11 },
      { kind: 'life', seat: 0, from: 20, to: 17 },
    ]);
    expect(m.clientDecision).toBeNull();

    m.apply({ v: 1, t: 'rewind', table: 't1', match: 1, seq: 0, body: { view: view(20), turn_starts: [0], head: 5, seats: [] } });
    expect(seen.at(-1)?.e.type).toBe('reset');
  });
});
