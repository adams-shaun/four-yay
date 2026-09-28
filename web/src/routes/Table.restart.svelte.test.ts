import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { EventBody, PlayerView, SeatInfo, View } from '../protocol';
import { initSeatContext } from '../lib/seat';

// Same shape as Table.svelte.test.ts: mock the session stream and MatchState
// so the SSR render is deterministic (nothing mounts, so no fetch runs). The
// fake's seats carry the wire flags the restart-eligibility predicate reads.
const { fakeMatch } = vi.hoisted(() => {
  const shared = {
    view: null as View | null,
    seats: [] as SeatInfo[],
    events: [] as EventBody[],
  };
  class FakeMatch {
    constructor(_table: string, _seat?: unknown) {}
    match: number | null = 1;
    view = shared.view;
    seats = shared.seats;
    dvr = { match: 't1/1', head: 0, cursor: 0, live: true, events: shared.events, turnStarts: [0], gap: false };
    decision = null;
    halted: string | null = null;
    loadError: string | null = null;
    dispatch() {}
    apply() {}
    loadFinished() {}
  }
  return { fakeMatch: { shared, MatchState: FakeMatch } };
});
vi.mock('../lib/match.svelte', () => ({ MatchState: fakeMatch.MatchState }));
vi.mock('../lib/session.svelte', () => ({
  session: { stream: { onFrame: () => () => {} }, focus: async () => {}, unfocus: async () => {} },
}));

import Table from './Table.svelte';

const player = (seat: number): PlayerView => ({
  seat, name: `P${seat}`, life: 20, lost: false, library_size: 30, hand_size: 7, graveyard_size: 0,
  completed_dungeons: 0,
  hand: [], battlefield: [], graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
});
const view = (overrides: Partial<View> = {}): View => ({
  viewer: 0, visibility: 'seat', turn: 3, round: 3, step: 'main', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, players: [player(0), player(1)], stack: [], pending: [], ...overrides,
});

// The play-vs-bot two-seat shape createGame builds: seat 0 is the human, seat
// 1 the bot, and each names the exact deck id it plays.
const vsBotSeats: SeatInfo[] = [
  { name: 'You', deck: 'Mono-Red', colour: '#e5484d', human: true, deck_id: 'mono-red' },
  { name: 'Bot', deck: 'Mono-Green', colour: '#22c55e', deck_id: 'mono-green' },
];

describe('Table.svelte — Restart control eligibility and placement (fb-20260922T202722Z)', () => {
  it('the seated human of a 2-seat vs-bot table gets the Restart control inside the rail', () => {
    initSeatContext('?seat=0&token=tok');
    fakeMatch.shared.view = view();
    fakeMatch.shared.seats = vsBotSeats;
    // Precondition: exactly the shape the predicate reads — 2 seats, exactly
    // one human, and it is the viewer's seat. A mistyped fixture (e.g. no
    // human flag) must fail here, not pass with an absent control.
    expect(fakeMatch.shared.seats).toHaveLength(2);
    expect(fakeMatch.shared.seats.filter((s) => s.human)).toHaveLength(1);
    expect(fakeMatch.shared.seats.findIndex((s) => s.human)).toBe(0);

    const { html } = render(Table, { props: { table: 't1' } });

    expect(html).toContain('data-restart-control');
    // The control is hosted by the rail's own logbar row, like the concede
    // control — inside the rail <aside>, not floating over the seat table.
    expect(html.indexOf('data-restart-control')).toBeGreaterThan(html.indexOf('<aside class="rail">'));
    expect(html.indexOf('data-restart-control')).toBeLessThan(html.indexOf('</aside>'));
    initSeatContext('');
  });

  it('a spectator (no seat in the URL) gets no Restart control even on the vs-bot seat shape', () => {
    initSeatContext('');
    fakeMatch.shared.view = view();
    fakeMatch.shared.seats = vsBotSeats;

    const { html } = render(Table, { props: { table: 't1' } });

    expect(html).not.toContain('data-restart-control');
  });

  it('a two-human table gets no Restart control', () => {
    initSeatContext('?seat=0&token=tok');
    fakeMatch.shared.view = view();
    fakeMatch.shared.seats = [
      { ...vsBotSeats[0] },
      { ...vsBotSeats[1], human: true },
    ];
    // Precondition: two humans, which is the case being excluded.
    expect(fakeMatch.shared.seats.filter((s) => s.human)).toHaveLength(2);

    const { html } = render(Table, { props: { table: 't1' } });

    expect(html).not.toContain('data-restart-control');
    initSeatContext('');
  });

  it('a 2-seat table whose single human is the OTHER seat gets no control (the viewer is the bot)', () => {
    initSeatContext('?seat=1&token=tok');
    fakeMatch.shared.view = view();
    fakeMatch.shared.seats = vsBotSeats;
    // Precondition: the one human is seat 0, the viewer is seat 1.
    expect(fakeMatch.shared.seats.findIndex((s) => s.human)).toBe(0);

    const { html } = render(Table, { props: { table: 't1' } });

    expect(html).not.toContain('data-restart-control');
    initSeatContext('');
  });
});
