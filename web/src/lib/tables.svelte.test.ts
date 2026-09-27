import { describe, expect, it, vi } from 'vitest';
import type { Frame, SeatInfo, TableInfo, Widget } from '../protocol';

// tables.svelte.ts wires its constructor to session.stream.onFrame, and the
// real session opens a browser EventSource — stub session so this stays a
// pure, hermetic test of apply()/load() with hand-built frames.
const { fakeSession } = vi.hoisted(() => {
  const fakeSession = { stream: { onFrame: (): (() => void) => () => {} } };
  return { fakeSession };
});
vi.mock('./session.svelte', () => ({ session: fakeSession }));

const fetchTablesMock = vi.fn();
vi.mock('./api', () => ({ fetchTables: fetchTablesMock }));

const { tables } = await import('./tables.svelte');

const info = (id: string, overrides: Partial<TableInfo> = {}): TableInfo =>
  ({ id, name: id, seats: 2, spectator: '', state: 'idle', match: 0, perpetual: false, format: 'constructed', bot_policy: 'bot', mulligans: 6, auto_mana: false, ...overrides });

describe('tables', () => {
  it('loads in host order and updates via apply(frame)', async () => {
    fetchTablesMock.mockResolvedValue([info('t2'), info('t1')]);
    await tables.load();
    expect(tables.list.map((t) => t.info.id)).toEqual(['t2', 't1']); // host order, not object-key order

    const helloFrame: Frame = { v: 1, t: 'hello', seq: 0, body: { session: 's1', tables: [info('t1'), info('t2')] } };
    tables.apply(helloFrame);
    expect(tables.list.map((t) => t.info.id)).toEqual(['t1', 't2']);

    const widget: Widget = { turn: 3, step: 'main', phase: 'main1', active: 0, priority: 0, life: [20, 18], lost: [false, false], stack_depth: 1, last: 'x drew a card', state: 'live' };
    tables.apply({ v: 1, t: 'widget', seq: 1, table: 't1', match: 5, body: widget });
    expect(tables.list[0].widget).toEqual(widget);
    expect(tables.list[0].match).toBe(5);

    // a reconnect hello must not lose the widget/seats already known for t1
    tables.apply(helloFrame);
    expect(tables.list[0].widget).toEqual(widget);

    const seats: SeatInfo[] = [{ name: 'Ari', deck: 'mono-red', colour: '#e5484d' }, { name: 'Bo', deck: 'mono-green', colour: '#22c55e' }];
    tables.apply({ v: 1, t: 'match_start', seq: 2, table: 't1', match: 6, body: { seats, seed: 1, spectator: '' } });
    expect(tables.list[0].seats).toEqual(seats);
    expect(tables.list[0].info.state).toBe('live');
    expect(tables.list[0].match).toBe(6);
    // the new match hasn't had a widget burst yet: the old match's
    // life/turn/phase/stack must not linger under the new LIVE badge
    expect(tables.list[0].widget).toBeNull();

    const widget2: Widget = { turn: 1, step: 'upkeep', phase: 'beginning', active: 0, priority: 0, life: [20, 20], lost: [false, false], stack_depth: 0, last: 'match 6 begins', state: 'live' };
    tables.apply({ v: 1, t: 'widget', seq: 3, table: 't1', match: 6, body: widget2 });
    expect(tables.list[0].widget).toEqual(widget2); // repopulated by the next widget burst

    tables.apply({ v: 1, t: 'table_halted', seq: 4, table: 't1', body: { reason: 'panic' } });
    expect(tables.list[0].info.state).toBe('halted');

    tables.apply({ v: 1, t: 'match_end', seq: 5, table: 't2', body: { result: 'win', winner: 0, head: 'h' } });
    expect(tables.list[1].info.state).toBe('idle'); // t2 is not perpetual

    // frames for unknown tables are ignored, not crashing
    tables.apply({ v: 1, t: 'widget', seq: 6, table: 'ghost', body: widget });
    expect(tables.list.find((t) => t.info.id === 'ghost')).toBeUndefined();
  });

  it('seeds seats from TableInfo.seat_names and lets a later match_start override', async () => {
    // A hello carrying deck names seeds the overview seats for a table whose
    // match_start already went out before this client connected — the common
    // mid-match spectator arrival.
    const withNames = info('n1', { seat_names: ["Ari's Deck", "Bo's Deck"] });
    tables.apply({ v: 1, t: 'hello', seq: 0, body: { session: 's1', tables: [withNames] } });
    expect(tables.list[0].seats.map((s) => s.name)).toEqual(["Ari's Deck", "Bo's Deck"]);

    // The richer MatchStart seat list (carries real deck+colour) replaces it.
    const ms: SeatInfo[] = [{ name: 'Ari', deck: 'mono-red', colour: '#e5484d' }, { name: 'Bo', deck: 'mono-green', colour: '#22c55e' }];
    tables.apply({ v: 1, t: 'match_start', seq: 1, table: 'n1', match: 7, body: { seats: ms, seed: 1, spectator: '' } });
    expect(tables.list[0].seats).toEqual(ms);

    // load() with seat_names but no prior match_start also seeds.
    fetchTablesMock.mockResolvedValue([info('n2', { seat_names: ['Dora', 'Erin'] })]);
    await tables.load();
    expect(tables.list[0].seats.map((s) => s.name)).toEqual(['Dora', 'Erin']);
  });

  it('a hello for a NEW match takes the wire names over the previous match\'s cache', () => {
    // Deck assignment rotates every match (host/table.go: Decks[(i+k)%len]),
    // so match 2's names are match 1's shifted by one seat — and a client
    // that was disconnected across the rollover never receives match 2's
    // match_start. The reconnect hello IS the fresh source of truth: cached
    // match-1 seats must give way to the wire's seat_names for match 2.
    tables.apply({ v: 1, t: 'hello', seq: 0, body: { session: 's1', tables: [info('r1', { match: 1 })] } });
    const match1: SeatInfo[] = [
      { name: 'A', deck: 'A', colour: '#e5484d' },
      { name: 'B', deck: 'B', colour: '#22c55e' },
      { name: 'C', deck: 'C', colour: '#46a758' },
      { name: 'D', deck: 'D', colour: '#e8a93c' },
    ];
    tables.apply({ v: 1, t: 'match_start', seq: 1, table: 'r1', match: 1, body: { seats: match1, seed: 1, spectator: '' } });
    expect(tables.list[0].seats.map((s) => s.name)).toEqual(['A', 'B', 'C', 'D']);

    // Reconnect during match 2: the hello's TableInfo says match 2, so it
    // must not keep serving match 1's names.
    tables.apply({ v: 1, t: 'hello', seq: 2, body: { session: 's1', tables: [info('r1', { match: 2, seat_names: ['B', 'C', 'D', 'A'] })] } });
    expect(tables.list[0].seats.map((s) => s.name)).toEqual(['B', 'C', 'D', 'A']);
  });

  it('renders nothing for a table that has never run a match', () => {
    // No seat_names (match never started): seats stays empty, so the cell's
    // wait-for-a-match path shows nothing rather than "Seat 0" or a blank row.
    tables.apply({ v: 1, t: 'hello', seq: 0, body: { session: 's1', tables: [info('fresh')] } });
    expect(tables.list[0].seats).toEqual([]);
  });
});
