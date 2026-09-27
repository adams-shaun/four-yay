import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { Decision, EventBody, PlayerView, SeatInfo, StackView, View } from '../protocol';
import { initSeatContext } from '../lib/seat';

// Table.svelte's MatchState opens nothing at import, but session.svelte's
// module-level Session opens a browser EventSource and match.svelte wires to
// it — mock session, and MatchState with a fixture-carrying fake so the SSR
// render is deterministic (nothing mounts, so no onMount fetches run; the
// fake's view/seats/dvr are exactly what the template renders).
const { fakeMatch } = vi.hoisted(() => {
  const shared = {
    view: null as View | null,
    seats: [] as SeatInfo[],
    events: [] as EventBody[],
    ctorArgs: [] as [string, unknown][],
    lastSeat: undefined as unknown,
  };
  class FakeMatch {
    constructor(table: string, seat?: unknown) {
      shared.ctorArgs.push([table, seat]);
      shared.lastSeat = seat;
    }
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

// The component's default export is the Svelte component; vi.mock is
// hoisted above this static import, so the mocks are installed first.
import Table from './Table.svelte';
const player = (seat: number): PlayerView => ({
  seat, name: `P${seat}`, life: 20, lost: false, library_size: 30, hand_size: 7, graveyard_size: 0,
  hand: [], battlefield: [], graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
});
const seats: SeatInfo[] = [{ name: 'Ari', deck: 'mono-red', colour: '#e5484d' }, { name: 'Bo', deck: 'mono-green', colour: '#22c55e' }];
const view = (overrides: Partial<View> = {}): View => ({
  viewer: 0, visibility: 'seat', turn: 3, round: 3, step: 'main', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, players: [player(0), player(1)], stack: [], pending: [], ...overrides,
});

describe('Table.svelte seat gating (R-E4-4 / R-E4-5)', () => {
  it('test 5 — no seat in the URL renders no panel, and the spectator page renders as it always did', () => {
    initSeatContext('');
    fakeMatch.shared.view = view();
    fakeMatch.shared.seats = seats;
    fakeMatch.shared.ctorArgs = [];

    const { html } = render(Table, { props: { table: 't1' } });

    expect(html).not.toContain('data-seat-panel'); // no seat -> no panel
    expect(html).not.toContain('data-hot-strip'); // spectators get the display-only clock, never action tabs
    expect(html).toContain('data-log-toggle'); // fb-20260917T231628Z: the OPTIONS drop is not mounted for a spectator, so the rail keeps the LOGS toggle
    expect(html).toContain('data-cursor'); // the DVR bar still renders for the spectator
    // the seat identity never reached the MatchState: constructed with no
    // seat context, so no seat-scoped fetch can be built from it
    expect(fakeMatch.shared.ctorArgs).toEqual([['t1', undefined]]);
  });

  it('test 7 — seated, the panel renders, and the token never reaches the DOM or the transcript', () => {
    initSeatContext('?seat=0&token=TOPSECRETVALUEnEVERseen');
    fakeMatch.shared.view = view({
      decision: {
        seq: 9, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
        options: [
          { index: 0, kind: 'cast', label: 'Cast Lighting Bolt', obj: undefined, player: 0 },
          { index: 1, kind: 'pass', label: 'Pass priority', obj: undefined, player: 0 },
          { index: 2, kind: 'concede', label: 'Concede', obj: undefined, player: 0 },
        ],
      },
    });
    fakeMatch.shared.seats = seats;
    fakeMatch.shared.events = [{ event: { seq: 1, kind: 'draw', player: 0 }, line: 'Ari draws a card' }];

    const { html } = render(Table, { props: { table: 't1' } });

    expect(html).toContain('data-seat-panel'); // the seat surface is mounted
    expect(html).toContain('data-phase-track'); // the clock remains, now inside the board shard
    expect(html.indexOf('data-phase-track')).toBeGreaterThan(html.indexOf('<section class="board">'));
    expect(html.indexOf('data-phase-track')).toBeLessThan(html.indexOf('</section>'));
    expect(html).toContain('data-hot-strip');
    expect(html).toContain('data-hot-tab="actions"');
    expect(html).toContain('data-hot-tab="pass"');
    expect(html).toContain('data-hot-tab="end-turn"');
    expect(html).toContain('data-hot-tab="done"');
    expect(html).not.toContain('data-hot-tab="options"');
    // Options is a compact popover beside Feedback; the settings editor is
    // mounted only after opening it, so it cannot expand the whole rail.
    expect(html).toContain('data-rail-options');
    expect(html).toContain('aria-controls="play-options-popover"');
    expect(html).not.toContain('data-toggle="show-game-log"');
    expect(html).not.toContain('data-action-dock'); // ui26's rail dock moved here; it was not duplicated
    expect(html).toContain('data-concede-control');
    // Concede lives inside the rail's own box (position: relative), not
    // pinned to the viewport corner — a fixed-to-viewport control only
    // avoided the log toggle by luck, and the wider "confirm" label already
    // overran that luck and ate the toggle's clicks.
    expect(html.indexOf('data-concede-control')).toBeGreaterThan(html.indexOf('<aside class="rail">'));
    expect(html.indexOf('data-concede-control')).toBeLessThan(html.indexOf('</aside>'));
    // Concede has one page-level control and is not duplicated as a flyout
    // option label beside Pass.
    expect(html.match(/>Concede</g)).toHaveLength(1);
    // The transcript renders the line, AND (Task 3) Ari's own name in Ari's
    // own seat colour — a strictly stronger check than the line's raw text
    // alone, which the colour-coding change now splits across markup.
    expect(html).toContain('draws a card');
    expect(html).toMatch(/<span class="who[^"]*"[^>]*color:\s*#e5484d[^>]*>Ari<\/span>/);
    expect(html).not.toContain('TOPSECRETVALUEnEVERseen'); // R-E4-5: the token is never rendered
    expect(fakeMatch.shared.lastSeat).toEqual({ seat: 0, token: 'TOPSECRETVALUEnEVERseen' });

    // restore the no-seat baseline for any later test in this file
    initSeatContext('');
  });
});

describe('Table.svelte compact seat pills', () => {
  it('uses the two established docks for a two-seat game', () => {
    initSeatContext('');
    fakeMatch.shared.view = view();
    fakeMatch.shared.seats = seats;
    const { html } = render(Table, { props: { table: 't1' } });
    expect(html).toContain('data-seat-pill-dock="seat-0"');
    expect(html).toContain('data-seat-pill-dock="seat-1"');
    expect(html).not.toContain('data-seat-pill-dock="all"');
  });

  it('keeps every player reachable in supported multi-seat games', () => {
    initSeatContext('');
    fakeMatch.shared.view = view({ players: [player(0), player(1), player(2), player(3)] });
    fakeMatch.shared.seats = [...seats, { name: 'Cy', deck: 'c', colour: '#f59e0b' }, { name: 'Di', deck: 'd', colour: '#a855f7' }];
    const { html } = render(Table, { props: { table: 't1' } });
    expect(html).toContain('data-seat-pill-dock="all"');
    for (const seat of [0, 1, 2, 3]) expect(html).toContain(`data-player-pill="${seat}"`);
  });
});

describe('Table.svelte — the one arrows overlay mounts at the table root (fb-20260914T121642Z)', () => {
  // The counterspell-over-bolt stack: the stack-to-stack pair whose arrow was
  // computed but invisible while the overlay lived inside the felt section.
  const counterStack: StackView[] = [
    { id: 890, kind: 'spell', name: 'Lightning Bolt', text: '', controller: 1, targets: [], card: null, optional: false },
    { id: 900, kind: 'spell', name: 'Counterspell', text: '', controller: 0, targets: [{ obj: 890, player: 1, is_player: false, label: 'spell' }], card: null, optional: false },
  ];

  it('the route mounts exactly one overlay, after the rail and before the transcript — never inside the clipped felt section', () => {
    initSeatContext('');
    fakeMatch.shared.view = view({ stack: counterStack });
    fakeMatch.shared.seats = seats;

    const { html } = render(Table, { props: { table: 't1' } });

    // Exactly one overlay in the whole route render — Board mounts none.
    expect(html.match(/class="arrows[ "]/g)).toHaveLength(1);
    // DOM order in the route is section.board, aside.rail, footer.transcript,
    // then the overlay as main.table's own LAST child. `</aside>` closes the
    // rail, which is a later sibling of the board, so anything after it is
    // outside the clipped felt section; `</main>` closes the table root, so
    // anything before it is still inside the overlay's host. If the mount is
    // removed this index is -1; if it moves back into Board it lands before
    // <aside. (Arrows.geometry.test.ts pins the same mount at real layout.)
    const arrowsAt = html.indexOf('class="arrows');
    expect(arrowsAt, 'the overlay renders at all').toBeGreaterThan(-1);
    expect(arrowsAt, 'after the rail — outside the felt section Board.svelte never hosts it').toBeGreaterThan(html.indexOf('</aside>'));
    expect(arrowsAt, 'inside main.table — the table root is the overlay host').toBeLessThan(html.lastIndexOf('</main>'));
    initSeatContext('');
  });
});

describe('Table.svelte — opening hand owns the board', () => {
  it('never duplicates a mulligan in the ACTIONS surface', () => {
    initSeatContext('?seat=0&token=t');
    fakeMatch.shared.view = view({
      decision: {
        seq: 20, player: 0, kind: 'mulligan', prompt: 'Keep this hand?', min: 1, max: 1,
        options: [
          { index: 0, kind: 'keep', label: 'keep', player: 0 },
          { index: 1, kind: 'mulligan', label: 'mulligan', player: 0 },
        ],
      },
    });
    fakeMatch.shared.seats = seats;

    const { html } = render(Table, { props: { table: 't1' } });

    expect(html).not.toContain('data-hot-strip');
    expect(html.match(/data-seat-panel/g)).toHaveLength(1);
    expect(html).toMatch(/class="seat-panel[^"]*wide/);
    expect(html).toContain('data-seat-pill-dock="seat-0"');
    expect(html).toContain('data-seat-pill-dock="seat-1"');
    initSeatContext('');
  });
});

describe('Table.svelte — the ACTIONS-anchored prompt surface', () => {
  const initiative: Decision = {
    seq: 21, player: 0, kind: 'target', prompt: 'Choose a target', min: 1, max: 1, source: 9,
    options: [{ index: 0, kind: 'target', label: 'Target Bo', obj: 3, player: 0 }],
  };

  it('a required prompt has one ACTIONS-anchored surface', () => {
    initSeatContext('?seat=0&token=t');
    fakeMatch.shared.view = view({ decision: initiative });
    fakeMatch.shared.seats = seats;
    fakeMatch.shared.ctorArgs = [];

    const { html } = render(Table, { props: { table: 't1' } });

    expect(html.match(/data-seat-panel/g)).toHaveLength(1);
    expect(html).toContain('data-answer-surface');
    expect(html).toContain('data-prompt');
    initSeatContext('');
  });

  it('an offered window keeps its one ACTIONS surface', () => {
    initSeatContext('?seat=0&token=t');
    fakeMatch.shared.view = view({
      decision: {
        seq: 22, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
        options: [
          { index: 0, kind: 'cast', label: 'Cast Bolt', player: 0 },
          { index: 1, kind: 'pass', label: 'Pass priority', player: 0 },
          { index: 2, kind: 'concede', label: 'Concede', player: 0 },
        ],
      },
    });
    fakeMatch.shared.seats = seats;
    fakeMatch.shared.ctorArgs = [];

    const { html } = render(Table, { props: { table: 't1' } });

    expect(html.match(/data-seat-panel/g)).toHaveLength(1);
    expect(html).toContain('data-answer-surface');
    initSeatContext('');
  });
});

describe('Table.svelte — the empty-answer safety net reaches the seated Pending tray', () => {
  // The live soft-lock's wire decision (demo game g4, seat 0): Min 0 / Max 0
  // with NO options — nothing a picker can render.
  const emptyChoose: Decision = {
    seq: 846, player: 0, kind: 'choose', prompt: 'Search a library: choose up to 0 card(s)', min: 0, max: 0, options: [],
  };

  it('seated: the option-less Min-0 decision is named in the Pending tray with a Continue, not "Nothing waiting"', () => {
    initSeatContext('?seat=0&token=t');
    fakeMatch.shared.view = view({ decision: emptyChoose });
    fakeMatch.shared.seats = seats;

    const { html } = render(Table, { props: { table: 't1' } });

    const pendingAt = html.search(/<h3[^>]*>Pending<\/h3>/);
    expect(pendingAt, 'the rail renders its Pending section').toBeGreaterThan(-1);
    const tray = html.slice(pendingAt);
    expect(tray).toContain('data-stuck');
    expect(tray).toContain('Search a library: choose up to 0 card(s)');
    // The Continue renders only when Table wired an onContinue, which it
    // does only for a seated panel whose decision is answerable empty.
    expect(tray).toContain('data-continue');
    expect(tray.slice(0, tray.indexOf('</section>'))).not.toContain('Nothing waiting');
    initSeatContext('');
  });

  it('spectator: the same server-held decision is not theirs to answer — the tray stays "Nothing waiting"', () => {
    initSeatContext('');
    fakeMatch.shared.view = view({ decision: emptyChoose });
    fakeMatch.shared.seats = seats;

    const { html } = render(Table, { props: { table: 't1' } });

    const pendingAt = html.search(/<h3[^>]*>Pending<\/h3>/);
    expect(pendingAt, 'the rail renders its Pending section').toBeGreaterThan(-1);
    const tray = html.slice(pendingAt);
    expect(tray).not.toContain('data-stuck');
    expect(tray).not.toContain('data-continue');
    expect(tray).toContain('Nothing waiting');
  });
});
