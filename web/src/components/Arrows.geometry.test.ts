import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';
import type { PlayerView, SeatInfo, StackView, View } from '../protocol';

/**
 * Arrows — the overlay's geometry pin, driven against the REAL production
 * route (routes/Table.svelte), not a fixture replica. Findings r2 rejected
 * the first cut, whose fixture mounted Arrows itself: removing Table.svelte's
 * production mount then left the live table with no arrows while the pin
 * still passed, because the fixture was quietly supplying its own overlay.
 * This pin mounts the route (Arrows.geometry.ts does nothing else) and
 * delivers its state the way the live page gets it — a snapshot frame on the
 * SSE stream — which the test intercepts and fulfils below. The only
 * `.arrows` element the page can ever contain is the one Table.svelte
 * mounts, so the assertions observe the production mount directly: removing
 * it leaves no overlay (waitForSelector times out), moving it back inside
 * the clipped felt section trips the containment assertion.
 *
 * fb-20260914T121642Z: a stack-to-stack target arrow — a counterspell
 * targeting the spell beneath it — was computed but invisible, because the
 * overlay lived inside Board.svelte, which the route mounts inside the felt
 * section whose `overflow: hidden` clips anything reaching into the rail.
 * The pin requires exactly one overlay, hosted by `main.table` (whose box
 * contains both the felt and the rail), and also pins the rail-scroll
 * alignment: section.stack is a real scroller, and scroll does not bubble,
 * so a line whose endpoints are rail tiles goes stale the moment the stack
 * scrolls unless the overlay listens for it.
 *
 * All assertions run against ONE page load: this file shares the machine
 * with the other browser suites in the same vitest run, and every page load
 * and dev server it starts is contention another file's timing-sensitive
 * assertions have to survive (measured: the suite flapped until this file
 * was cut to a single page).
 */

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

type Rect = { left: number; top: number; right: number; bottom: number; width: number; height: number };
type Overlay = { inTableRoot: boolean; inClippedFelt: boolean; rect: Rect };
type Line = { x1: number; y1: number; x2: number; y2: number };
type Measured = { ovs: Overlay[]; counterspell: Rect; bolt: Rect; line: Line; scrollable: boolean };
type Endpoint = { bolt: Rect; endpointX: number; endpointY: number };

// --- the fixture state the route renders ---
// view.stack lists bottom of the stack first; the rail renders it reversed,
// so the counterspell (900) is the UPPER rail tile and the bolt (890) the
// lower one — the pair the arrow runs between. Enough filler entries that
// section.stack actually scrolls at the test viewport.
const filler = (id: number): StackView => ({
  id, kind: 'spell', name: `Ritual ${id}`, text: '', controller: 0, targets: [], card: null, optional: false,
});
const stack: StackView[] = [
  ...Array.from({ length: 40 }, (_, i) => filler(100 + i)),
  { id: 890, kind: 'spell', name: 'Lightning Bolt', text: 'Lightning Bolt deals 3 damage to any target.', controller: 1, targets: [], card: null, optional: false },
  { id: 900, kind: 'spell', name: 'Counterspell', text: 'Counter target spell.', controller: 0, targets: [{ obj: 890, player: 1, is_player: false, label: 'spell' }], card: null, optional: false },
];
const players: PlayerView[] = [0, 1].map((seat) => ({
  seat, name: `Player ${seat + 1}`, life: 20, lost: false, library_size: 60, hand_size: 7,
  graveyard_size: 0, hand: [], battlefield: [], graveyard: [], exile: [], pool: {},
  completed_dungeons: 0,
  command: [], commanders: [], commander_casts: [],
}));
const seats: SeatInfo[] = players.map((p) => ({ name: p.name, deck: 'fixture', colour: p.seat === 0 ? '#e5484d' : '#22c55e' }));
const view: View = {
  viewer: 255, visibility: 'omniscient', turn: 2, round: 2, step: 'main1', phase: 'main1',
  active: 0, priority: 0, over: false, draw: false, winner: null, stack, pending: [], players,
};

// The route's state arrives over the one SSE stream session.svelte opens at
// module load. The stream is fulfilled in two phases: the first connection
// carries no frames, only a short retry hint, so the browser reconnects a
// moment later — by then the route is mounted and its frame handler is
// registered (MatchState reads the snapshot only through the handler it
// registers in onMount), and the snapshot frame finds a listener.
const sse = (frames: [string, unknown, Record<string, unknown>?][], retryMs: number): string =>
  frames.map(([t, body, extra]) =>
    `event: ${t}\ndata: ${JSON.stringify({ v: 1, t, seq: 0, ...extra, body })}\n\n`).join('')
  + `retry: ${retryMs}\n\n`;
const STREAM_BODY = (attempt: number): string =>
  attempt === 1
    ? sse([], 200)
    : sse([
        ['hello', { session: 'geometry-fixture', tables: [] }],
        ['snapshot', { view, turn_starts: [], head: 0, seats }, { table: 'arrows-fixture', match: 1 }],
      ], 60_000);

// Everything below runs inside page.evaluate, so it is self-contained: the
// browser context sees only the serialized callback, never this module's
// scope.
const IN_PAGE = `
  const compact = (r) => ({ left: r.left, top: r.top, right: r.right, bottom: r.bottom, width: r.width, height: r.height });
  const overlays = () => {
    const table = document.querySelector('main.table');
    const felt = document.querySelector('main.table > section.board');
    return [...document.querySelectorAll('.arrows')].map((el) => {
      const r = el.getBoundingClientRect();
      return {
        inTableRoot: el.parentElement === table,
        // "inside the clipped felt section": a descendant of the element the
        // route clips, so any line reaching into the rail is cut at the seam.
        inClippedFelt: felt !== null && felt.contains(el),
        rect: compact(r),
      };
    });
  };
  const lineAttrs = () => {
    const l = document.querySelector('.arrows line.line--target');
    return l ? { x1: Number(l.getAttribute('x1')), y1: Number(l.getAttribute('y1')), x2: Number(l.getAttribute('x2')), y2: Number(l.getAttribute('y2')) } : null;
  };
  const tile = (id) => compact(document.querySelector('[data-obj="' + id + '"]').getBoundingClientRect());
`;

const near = (a: number, b: number): boolean => Math.abs(a - b) <= 1;

describe('Arrows — the overlay escapes the felt clip and tracks the rail (real route)', () => {
  it('Table.svelte hosts one arrows overlay at the table root (never inside the clipped felt section), its bounds include the rail target tile, the line lands on both tiles, and the endpoint follows the rail scroll', async () => {
    const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });
    // The route's whole network surface, mocked at the page edge: the SSE
    // stream (the state carrier) and the subscribe/unsubscribe POSTs the
    // hello triggers (fulfilled so they reject nowhere).
    let streamAttempts = 0;
    await page.route('**/api/stream', (route) => {
      streamAttempts++;
      return route.fulfill({ status: 200, contentType: 'text/event-stream', body: STREAM_BODY(streamAttempts) });
    });
    for (const ep of ['**/api/subscribe', '**/api/unsubscribe']) {
      await page.route(ep, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: '{}' }));
    }
    await page.goto(`${url}src/components/Arrows.geometry.html`);
    // THE production-mount assertion: the fixture never renders Arrows
    // itself, so this selector can only match the overlay Table.svelte
    // mounts. If that mount is removed the page renders no overlay at all
    // and this wait is what fails.
    await page.waitForSelector('main.table .arrows', { state: 'attached', timeout: 60_000 });
    for (const sel of ['main.table section.stack [data-obj="900"]', 'main.table section.stack [data-obj="890"]']) {
      await page.waitForSelector(sel, { state: 'attached', timeout: 60_000 });
    }
    // UI rework §3: a stack item's target arrows are drawn only while that
    // item is hovered. The pointer is never moved (a real hover would be
    // re-hit-tested as the rail scrolls below), so the hover is the one
    // pointerover the table's delegated tracker listens for.
    await page.evaluate(() => {
      document.querySelector('main.table section.stack [data-obj="900"]')!
        .dispatchEvent(new PointerEvent('pointerover', { bubbles: true, composed: true }));
    });
    // The first measurement is rAF-deferred behind the mount's DOM update.
    await page.waitForFunction(() => {
      const l = document.querySelector('.arrows line.line--target');
      return l !== null && Number((l as SVGLineElement).getAttribute('x2')) !== 0;
    }, undefined, { timeout: 60_000 });

    const m = (await page.evaluate(`${IN_PAGE}
      (() => {
        const scroller = document.querySelector('.history-frame');
        return {
          ovs: overlays(),
          counterspell: tile(900),
          bolt: tile(890),
          line: lineAttrs(),
          scrollable: scroller.scrollHeight > scroller.clientHeight,
        };
      })()`)) as unknown as Measured;

    // --- the real route renders the brief's case for real ---
    expect(m.line, 'exactly one resolved target arrow: the counterspell\u2019s').not.toBeNull();
    // The counterspell and bolt tiles sit at DIFFERENT heights in the rail
    // (the counterspell is the upper entry), and the stack genuinely
    // overflows its scroller, so the scroll leaf below is not vacuous.
    expect(m.counterspell.top).toBeLessThan(m.bolt.top - 1);
    expect(m.scrollable, 'the history frame really overflows at the test viewport').toBe(true);

    // --- fb-20260914T121642Z: the overlay escapes the felt clip ---
    // Exactly ONE overlay, and it is the route's: mounted by Table.svelte as
    // a direct child of main.table, never a descendant of the clipped felt
    // section (whose overflow: hidden is what cut every stack-to-stack line
    // at the seam before the fix).
    expect(m.ovs, 'the route mounts exactly one arrows overlay').toHaveLength(1);
    for (const o of m.ovs) {
      expect(o.inTableRoot, 'the overlay is a direct child of main.table — Table.svelte\u2019s mount').toBe(true);
      expect(o.inClippedFelt, 'the overlay is not a descendant of the clipped felt section').toBe(false);
      // The bolt tile renders in the rail, right of the felt; the overlay
      // must span at least that far.
      expect(o.rect.left).toBeLessThanOrEqual(m.bolt.left + 1);
      expect(o.rect.right).toBeGreaterThanOrEqual(m.bolt.right - 1);
      expect(o.rect.top).toBeLessThanOrEqual(m.bolt.top + 1);
      expect(o.rect.bottom).toBeGreaterThanOrEqual(m.bolt.bottom - 1);
      // The resolved target arrow runs counterspell -> bolt. Line coordinates
      // are relative to the overlay's own box, so the viewport endpoint is
      // overlay origin + (x2, y2); it must be the bolt tile's centre, and the
      // tail the counterspell's.
      expect(near(o.rect.left + m.line.x2, m.bolt.left + m.bolt.width / 2), 'line x2 is the bolt tile centre').toBe(true);
      expect(near(o.rect.top + m.line.y2, m.bolt.top + m.bolt.height / 2), 'line y2 is the bolt tile centre').toBe(true);
      expect(near(o.rect.left + m.line.x1, m.counterspell.left + m.counterspell.width / 2), 'line x1 is the counterspell tile centre').toBe(true);
      expect(near(o.rect.top + m.line.y1, m.counterspell.top + m.counterspell.height / 2), 'line y1 is the counterspell tile centre').toBe(true);
    }

    // --- and it stays aligned when the rail's history/stack frame scrolls ---
    const before = (await page.evaluate(`${IN_PAGE}
      (() => {
        const o = overlays()[0].rect;
        const l = lineAttrs();
        return { bolt: tile(890), endpointX: o.left + l.x2, endpointY: o.top + l.y2 };
      })()`)) as unknown as Endpoint;
    // The history frame is the rail's one scroller and scroll does not bubble, so
    // the test scrolls it and dispatches the scroll event a real scroll
    // would fire; the overlay must recompute from that signal alone (no
    // polling, no timer).
    await page.evaluate(() => {
      const scroller = document.querySelector('.history-frame')!;
      scroller.scrollTop += 140;
      scroller.dispatchEvent(new Event('scroll'));
    });
    await page.waitForFunction(() => {
      const l = document.querySelector('.arrows line.line--target')!;
      const b = document.querySelector('[data-obj="890"]')!.getBoundingClientRect();
      const o = document.querySelector('.arrows')!.getBoundingClientRect();
      return Math.abs(o.left + Number(l.getAttribute('x2')) - (b.left + b.width / 2)) <= 1
        && Math.abs(o.top + Number(l.getAttribute('y2')) - (b.top + b.height / 2)) <= 1;
    }, undefined, { timeout: 60_000 });
    const after = (await page.evaluate(`${IN_PAGE}
      (() => {
        const o = overlays()[0].rect;
        const l = lineAttrs();
        return { bolt: tile(890), endpointX: o.left + l.x2, endpointY: o.top + l.y2 };
      })()`)) as unknown as Endpoint;

    // The scroll actually moved the tile (otherwise the assertion is vacuous).
    expect(Math.abs(after.bolt.top - before.bolt.top), 'the scroll moved the bolt tile').toBeGreaterThanOrEqual(1);
    // And the endpoint followed it, in the overlay's own coordinate space.
    expect(near(after.endpointX, after.bolt.left + after.bolt.width / 2), 'scrolled endpoint x lands on the tile').toBe(true);
    expect(near(after.endpointY, after.bolt.top + after.bolt.height / 2), 'scrolled endpoint y lands on the tile').toBe(true);
    expect(near(before.endpointX, before.bolt.left + before.bolt.width / 2), 'pre-scroll endpoint was on the tile too').toBe(true);
    await page.close();
  }, 120_000);
});
