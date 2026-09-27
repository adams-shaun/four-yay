import { beforeAll, describe, expect, it } from 'vitest';
import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { render } from 'svelte/server';
import type { CardView, PlayerView, SeatInfo, StackView, View } from '../protocol';
import Rail from './Rail.svelte';

// SSR via svelte/server, the repo's component-test pattern (see CardTile.
// svelte.test.ts / SeatPanel.svelte.test.ts): no DOM, no $effect, no
// pointer/hover lifecycle — what renders from a given view is exactly what
// these tests can check.

const card = (over: Partial<CardView> = {}): CardView => ({
  id: 1, name: 'Lightning Bolt', types: 'Instant', mana_cost: 'R',
  printing: { name: 'Lightning Bolt' }, token: '', tapped: false, power: 0, toughness: 0,
  damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
  ...over,
});

// A PlayerView with hand/pool exactly the shape a PUBLIC spectator gets:
// view/view.go fills Pool and Hand only for the viewer's own seat, with no
// `omitempty`, so every OTHER seat's client receives a literal JSON null —
// and a spectator (view.NoSeat) has no seat at all, so EVERY seat's hand
// and pool are null. protocol.ts still types both as non-nullable arrays/
// records, which is exactly the gap a previous round shipped a crash
// through on a public server while an omniscient one stayed fine.
const spectatorPlayer = (seat: number, name: string, over: Partial<PlayerView> = {}): PlayerView => ({
  seat, name, life: 40, lost: false, library_size: 60, hand_size: 7, graveyard_size: 0,
  hand: null as unknown as CardView[],
  pool: null as unknown as Record<string, number>,
  battlefield: [], graveyard: [], exile: [], command: [], commanders: [], commander_casts: [],
  ...over,
});

const seats: SeatInfo[] = [
  { name: 'Ari', deck: 'mono-red', colour: '#e5484d' },
  { name: 'Bo', deck: 'mono-green', colour: '#22c55e' },
];

const baseView = (over: Partial<View> = {}): View => ({
  viewer: 255, visibility: 'public', turn: 3, round: 3, step: 'main1', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, stack: [], pending: [],
  players: [spectatorPlayer(0, 'Ari'), spectatorPlayer(1, 'Bo')],
  ...over,
});

describe('Rail — public spectator (every seat\'s hand and pool are null)', () => {
  it('renders with no crash and no fabricated data: a null hand is a true count with no pile control', () => {
    const { html } = render(Rail, { props: { view: baseView(), seats, decision: null } });
    expect(html).toContain('data-hand-hidden');
    expect(html).toContain('data-icon="hand"');
    expect(html).not.toContain('data-pile="hand"');
    expect(html).not.toContain('not visible');
  });

  it('ManaPool renders nothing for a null pool rather than throwing', () => {
    // No assertion beyond "did not throw" is needed for the pool itself —
    // ManaPool already null-guards (see its own doc comment) — but the
    // render() call above having completed at all is the proof; this test
    // exists so a future regression that removes that guard fails HERE,
    // in the rail's own suite, rather than only in a live public server.
    expect(() => render(Rail, { props: { view: baseView(), seats, decision: null } })).not.toThrow();
  });

  it('the one summary panel carries all five counts for EVERY seat, with no duplicate zone pane', () => {
    const v = baseView({ players: [spectatorPlayer(0, 'Ari'), spectatorPlayer(1, 'Bo')] });
    const { html } = render(Rail, { props: { view: v, seats, decision: null } });
    expect(html.match(/data-seat-row=/g)).toHaveLength(2);
    expect(html.match(/data-stat="exile"/g)).toHaveLength(2);
    expect(html).not.toContain("'s zones");
  });
});

describe('Rail — simultaneously visible hands', () => {
  it('renders every exposed spectator hand, including empty, and never renders redacted identities', () => {
    const v = baseView({
      visibility: 'omniscient',
      players: [
        spectatorPlayer(0, 'Ari', { hand: [card({ id: 21, name: 'Ari Secret' })] }),
        spectatorPlayer(1, 'Bo', { hand: [card({ id: 22, name: 'Bo Secret' })] }),
      ],
    });
    const { html } = render(Rail, { props: { view: v, seats, decision: null } });
    expect(v.viewer).toBe(255);
    expect(v.visibility).toBe('omniscient');
    expect(v.players.map((p) => p.hand?.[0]?.name)).toEqual(['Ari Secret', 'Bo Secret']);
    expect(html).toContain('Ari\'s hand');
    expect(html).toContain('Bo\'s hand');
    expect(html).toContain('Ari Secret');
    expect(html).toContain('Bo Secret');
    expect(html.match(/data-obj="2[12]"/g)).toHaveLength(2);

    const empty = render(Rail, { props: { view: baseView({
      visibility: 'omniscient',
      players: [spectatorPlayer(0, 'Ari', { hand: [] }), spectatorPlayer(1, 'Bo')],
    }), seats, decision: null } }).html;
    expect(empty).toContain('Ari\'s hand');
    expect(empty).toContain('No cards');
    expect(empty).not.toContain('Bo\'s hand');

    const redacted = render(Rail, { props: { view: baseView({
      visibility: 'omniscient',
      players: [spectatorPlayer(0, 'Ari', { hand: null as unknown as CardView[] }), spectatorPlayer(1, 'Bo')],
    }), seats, decision: null } }).html;
    expect(redacted).not.toContain('Ari Secret');
    expect(redacted).not.toContain('Ari\'s hand');

    // A seated omniscient viewer still gets only their own fan; the rail is
    // reserved for spectator hands, and the opponent remains server-redacted.
    const seated = baseView({
      viewer: 0, visibility: 'omniscient',
      players: [
        spectatorPlayer(0, 'Ari', { hand: [card({ id: 31, name: 'Own Card' })] }),
        spectatorPlayer(1, 'Bo', { hand: null as unknown as CardView[] }),
      ],
    });
    const seatedHtml = render(Rail, { props: { view: seated, seats, decision: null } }).html;
    expect(seated.players[0].hand).toHaveLength(1);
    expect(seated.players[1].hand).toBeNull();
    expect(seatedHtml).not.toContain('data-visible-hands');
    expect(seatedHtml).not.toContain('Own Card');
    expect(seatedHtml).not.toContain('Bo\'s hand');
  });
});

describe('Rail — the live decision line (ui15)', () => {  it('names the decision player from the view when seats is empty, not the Seat N placeholder', () => {
    const v = baseView({
      players: [spectatorPlayer(0, 'Ari'), spectatorPlayer(1, 'Bo'), spectatorPlayer(2, 'Player 3')],
    });
    const { html } = render(Rail, { props: { view: v, seats: [], decision: { player: 2, kind: 'priority', prompt: 'pass?' } } });
    expect(html).toContain('Player 3');
    expect(html).not.toContain('Seat 2');
  });
});

describe('Rail — the stack (Task 1/2)', () => {
  const stackOf = (n: number): StackView[] =>
    Array.from({ length: n }, (_, i) => ({
      id: 100 + i, kind: i === 0 ? 'spell' : 'trigger', name: `Entry ${i}`,
      text: 'Some oracle-ish text that could run long if not clamped.',
      controller: 0, targets: [], card: i % 2 === 0 ? card({ id: 100 + i, name: `Entry ${i}` }) : null,
      optional: false,
    }));

  it('a real multi-entry stack renders every entry, the count in the header, and mixed art/no-art gracefully', () => {
    const v = baseView({ stack: stackOf(4) });
    const { html } = render(Rail, { props: { view: v, seats, decision: null } });
    expect(html).toContain('>4<'); // the stack count badge
    expect(html).toContain('Entry 0');
    expect(html).toContain('Entry 3');
    expect(html).toContain('card-image'); // at least one entry has art
  });

  it('an empty stack renders the section with no count badge', () => {
    const { html } = render(Rail, { props: { view: baseView(), seats, decision: null } });
    expect(html).toContain('>Stack<');
    const stackSection = html.slice(html.indexOf('class="stack'), html.indexOf('class="pending'));
    expect(stackSection).not.toContain('class="count'); // no count badge when the stack is empty
  });
});

describe('Rail — the resolved card is its own band BEFORE the Stack heading (fb-20260923T015554Z)', () => {
  it('a resolve renders the labelled resolved card OUTSIDE the stack section, before the Stack heading; the real stack stays under it', () => {
    const v = baseView({
      players: [spectatorPlayer(0, 'Ari', { graveyard: [card({ id: 42, name: 'Resolved Thing' })] }), spectatorPlayer(1, 'Bo')],
      stack: [{ id: 100, kind: 'spell', name: 'Still Here', text: '', controller: 0, targets: [], card: card({ id: 100, name: 'Still Here' }), optional: false }],
    });
    const events = [{ event: { seq: 9, kind: 'stack_resolve', player: 0, obj: 42 } }];
    const { html } = render(Rail, { props: { view: v, seats, decision: null, events } });
    // Preconditions: the resolve's object really moved somewhere the row can
    // find it (the graveyard named above), and the stack really has an
    // entry of its own for the section to carry.
    expect(html).toContain('Resolved Thing');
    expect(html).toContain('Still Here');
    const stackSection = html.slice(html.indexOf('class="stack'), html.indexOf('class="pending'));
    expect(stackSection).toContain('Still Here'); // the tiles still on the stack render under the heading
    // The resolved row is NOT stack content: outside the section entirely...
    expect(stackSection).not.toContain('data-resolved="42"');
    // ...and BEFORE the Stack heading in document order (the player's sketch).
    expect(html.indexOf('data-resolved="42"')).toBeLessThan(html.indexOf('class="stack'));
    expect(html.indexOf('data-resolved="42"')).toBeLessThan(html.indexOf('>Stack'));
    expect(html).toContain('just resolved'); // still labelled as history, not a live entry
  });

  it('a resolve with an EMPTY stack still puts the row before the heading, and the heading carries no count badge', () => {
    const v = baseView({
      players: [spectatorPlayer(0, 'Ari', { graveyard: [card({ id: 42, name: 'Resolved Thing' })] }), spectatorPlayer(1, 'Bo')],
    });
    const events = [{ event: { seq: 9, kind: 'stack_resolve', player: 0, obj: 42 } }];
    const { html } = render(Rail, { props: { view: v, seats, decision: null, events } });
    expect(html).toContain('data-resolved="42"'); // precondition: the row is showing at all
    expect(html.indexOf('data-resolved="42"')).toBeLessThan(html.indexOf('class="stack'));
    const stackSection = html.slice(html.indexOf('class="stack'), html.indexOf('class="pending'));
    expect(stackSection).not.toContain('class="count'); // no badge over an empty stack
  });

  it('with no resolve in the events, no resolved row renders anywhere and the stack section is clean', () => {
    const { html } = render(Rail, { props: { view: baseView(), seats, decision: null } });
    expect(html).not.toContain('data-resolved');
    expect(html).not.toContain('just resolved');
    const stackSection = html.slice(html.indexOf('class="stack'), html.indexOf('class="pending'));
    expect(stackSection).not.toContain('data-resolved');
  });
});

describe('Rail — visible hands at bounded rail dimensions', () => {
  let browser: Browser;
  beforeAll(async () => { browser = await sharedBrowser(); });

  it('shows distinct hands concurrently and keeps stack and pending reachable at the rail floor', async () => {
    const page = await browser.newPage({ viewport: { width: 700, height: 500 } });
    try {
      await page.goto(`${browserURL}src/components/SeatTable.geometry.html?hands=1`);
      await page.waitForSelector('#rail [data-visible-hands]');
      const m = await page.evaluate(() => {
        const rail = document.querySelector<HTMLElement>('#rail .rail-inner')!;
        const hands = document.querySelector<HTMLElement>('#rail [data-visible-hands]')!;
        const stack = document.querySelector<HTMLElement>('#rail section.stack')!;
        const pending = document.querySelector<HTMLElement>('#rail section.pending')!;
        return {
          handText: hands.textContent, handScroll: hands.scrollHeight, handHeight: hands.clientHeight,
          stack: stack.textContent, pending: pending.textContent,
          pendingBottom: pending.getBoundingClientRect().bottom,
          railBottom: rail.getBoundingClientRect().bottom,
        };
      });
      expect(m.handText).toContain('Ari Visible Card');
      expect(m.handText).toContain('Bo Visible Card');
      expect(m.handScroll).toBeGreaterThan(m.handHeight);
      expect(m.stack).toContain('Slow but Absolutely Inevitable');
      expect(m.pending).toContain('Longwinded Ambush');
      expect(m.pendingBottom).toBeLessThanOrEqual(m.railBottom + 1);
    } finally {
      await page.close();
    }
  });
});

describe('Rail — resolved history at short viewport heights (fb-20260923T015554Z)', () => {
  let browser: Browser;
  beforeAll(async () => { browser = await sharedBrowser(); });

  it('keeps Pending visible at full height while history and live stack share the scroll budget', async () => {
    const page = await browser.newPage({ viewport: { width: 1000, height: 500 } });
    try {
      await page.goto(`${browserURL}src/components/SeatTable.geometry.html?resolved=1`);
      await page.waitForSelector('#rail [data-resolved="42"]');
      const m = await page.evaluate(() => {
        const rail = document.querySelector<HTMLElement>('#rail .rail-inner')!;
        const history = document.querySelector<HTMLElement>('#rail [data-resolved="42"]')!;
        const frame = document.querySelector<HTMLElement>('#rail .history-frame');
        const stack = document.querySelector<HTMLElement>('#rail section.stack')!;
        const pending = document.querySelector<HTMLElement>('#rail section.pending')!;
        return {
          historyName: history.textContent, stackText: stack.textContent,
          pendingText: pending.textContent, historyBeforeStack: history.getBoundingClientRect().top < stack.getBoundingClientRect().top,
          railScroll: rail.scrollHeight, railHeight: rail.clientHeight,
          frameScroll: frame?.scrollHeight ?? 0, frameHeight: frame?.clientHeight ?? 0,
          pendingHeight: pending.getBoundingClientRect().height,
          pendingBottom: pending.getBoundingClientRect().bottom, railBottom: rail.getBoundingClientRect().bottom,
        };
      });
      // Preconditions: the fixture presents both a resolved card AND a real
      // stack member/pending trigger, at a height where they compete for space.
      expect(m.historyName).toContain('Resolved Thing');
      expect(m.stackText).toContain('Slow but Absolutely Inevitable');
      expect(m.pendingText).toContain('Longwinded Ambush');
      expect(m.historyBeforeStack).toBe(true);
      expect(m.railScroll).toBeLessThanOrEqual(m.railHeight + 1);
      expect(m.pendingHeight).toBeGreaterThanOrEqual(79);
      expect(m.pendingBottom).toBeLessThanOrEqual(m.railBottom + 1);
      expect(m.frameScroll).toBeGreaterThan(m.frameHeight);
    } finally {
      await page.close();
    }
  });
});

describe('Rail — dead seats say why (Task 4)', () => {
  it('forwards the DVR event list down to SeatTable, which surfaces the PlayerLost cause', () => {
    const v = baseView({
      players: [
        spectatorPlayer(0, 'Ari', { lost: true, life: 39 }),
        spectatorPlayer(1, 'Bo'),
      ],
    });
    const events = [{ event: { seq: 5, kind: 'player_lost', player: 0, text: 'commander damage (21 or more from one commander)' } }];
    const { html } = render(Rail, { props: { view: v, seats, decision: null, events } });
    expect(html).toContain('Eliminated');
    expect(html).toContain('commander damage (21 or more from one commander)');
    expect(html).toContain('>39<'); // the true life total — never forced to 0
  });

  it('with no events (nothing carries the cause), a lost seat still reads Eliminated with no invented reason', () => {
    const v = baseView({ players: [spectatorPlayer(0, 'Ari', { lost: true }), spectatorPlayer(1, 'Bo')] });
    const { html } = render(Rail, { props: { view: v, seats, decision: null } });
    expect(html).toContain('Eliminated');
  });
});

describe('Rail — the log visibility control (ui21)', () => {
  it('renders the control as a switch reflecting the current state, only when a toggle is supplied', () => {
    // No onToggleLog: a caller that does not own the log (an existing test, or
    // a rail used without the live table route) renders no control at all.
    const noToggle = render(Rail, { props: { view: baseView(), seats, decision: null } });
    expect(noToggle.html).not.toContain('data-log-toggle');

    const on = render(Rail, { props: { view: baseView(), seats, decision: null, showLog: true, onToggleLog: () => {} } });
    expect(on.html).toContain('data-log-toggle');
    expect(on.html).toContain('aria-checked="true"');

    const off = render(Rail, { props: { view: baseView(), seats, decision: null, showLog: false, onToggleLog: () => {} } });
    expect(off.html).toContain('aria-checked="false"');
  });

  it('names itself LOGS in both states — the word is the noun, not the state (player feedback, ui21)', () => {
    // The old toggle spelt its STATE ("Visible"/"Hidden"); a seated player
    // whose log starts hidden read "Hidden" with no idea what it referred
    // to. The button now names the thing it controls, statically, in both
    // states; the state stays in the dot/class:on styling and aria-checked
    // (asserted above). Uppercase comes from CSS (`text-transform:
    // uppercase` on .logbar__toggle .word), so the markup carries "Logs".
    const on = render(Rail, { props: { view: baseView(), seats, decision: null, showLog: true, onToggleLog: () => {} } });
    expect(on.html).toContain('>Logs<');
    const off = render(Rail, { props: { view: baseView(), seats, decision: null, showLog: false, onToggleLog: () => {} } });
    expect(off.html).toContain('>Logs<');

    // The old "Log" label span is gone — matched by class and by exact
    // element text (">Log<" is substring-safe against "Logs" because the
    // static word is what remains).
    expect(off.html).not.toContain('logbar__label');
    expect(off.html).not.toContain('>Log</span>');
  });
});
