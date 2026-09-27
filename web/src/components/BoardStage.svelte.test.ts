import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

type Rect = { top: number; bottom: number; height: number; left: number; right: number; width: number };
type Geometry = {
  viewport: string;
  seats: number;
  stage: Rect;
  band: Rect;
  clock: Rect;
  strip: Rect;
  above: Rect;
  below: Rect;
  clearanceAbove: number;
  clearanceBelow: number;
  minimum: number;
};

async function geometry(width: number, height: number, seats: number): Promise<Geometry> {
  const page = await browser.newPage({ viewport: { width, height } });
  await page.goto(`${url}src/components/PhaseLane.geometry.html?seats=${seats}`);
  // goto resolves on the document load event, not on the Svelte mount: under
  // box load Vite's cold transform can take tens of seconds, and measuring
  // before the fixture mounts reads a null [data-board-stage]. Wait for every
  // element the measurement dereferences.
  for (const sel of ['[data-board-stage]', '[data-centre-instrument]', '[data-phase-track]', '[data-hot-strip]']) {
    await page.waitForSelector(sel, { state: 'attached', timeout: 60_000 });
  }
  const measured = await page.evaluate(({ width, height, seats }) => {
    const compact = (r: DOMRect) => ({
      top: r.top, bottom: r.bottom, height: r.height,
      left: r.left, right: r.right, width: r.width,
    });
    const stage = document.querySelector<HTMLElement>('[data-board-stage]')!.getBoundingClientRect();
    const band = document.querySelector<HTMLElement>('[data-centre-instrument]')!.getBoundingClientRect();
    const clock = document.querySelector<HTMLElement>('[data-phase-track]')!.getBoundingClientRect();
    const strip = document.querySelector<HTMLElement>('[data-hot-strip]')!.getBoundingClientRect();
    const rows = [...document.querySelectorAll<HTMLElement>('.quadrant .row.creatures')]
      .map((row) => row.getBoundingClientRect());
    const middle = band.top + band.height / 2;
    const above = rows.filter((r) => r.top + r.height / 2 < middle)
      .sort((a, b) => b.bottom - a.bottom)[0];
    const below = rows.filter((r) => r.top + r.height / 2 > middle)
      .sort((a, b) => a.top - b.top)[0];

    // Resolve the design-system minimum independently of the reservation.
    // Reading --phase-card-clearance here would mutate the oracle when that
    // implementation token is accidentally set to zero.
    const probe = document.createElement('i');
    probe.style.cssText = 'position:fixed;width:var(--sp-1);height:0';
    document.body.append(probe);
    const minimum = probe.getBoundingClientRect().width;
    probe.remove();

    return {
      viewport: `${width}x${height}`,
      seats,
      stage: compact(stage),
      band: compact(band),
      clock: compact(clock),
      strip: compact(strip),
      above: compact(above),
      below: compact(below),
      clearanceAbove: band.top - above.bottom,
      clearanceBelow: below.top - band.bottom,
      minimum,
      commanders: document.querySelectorAll('[data-cmd-state="command"]').length,
    };
  }, { width, height, seats });
  await page.close();

  expect(measured.commanders).toBe(seats);
  const result: Omit<typeof measured, 'commanders'> = { ...measured };
  delete (result as { commanders?: number }).commanders;
  return result;
}

describe('BoardStage — the phase band is a reserved lane', () => {
  // The visible row-to-bar gap is the clearance token plus Quadrant's own
  // content padding (--sp-3, 12px) and the 1px cell padding. It measured
  // 37px per side at every acceptance viewport and seat count before
  // fb-20260916T200757Z; the player asked for half of that, so the band may
  // not regrow past half the measured baseline plus a 3px tolerance
  // (18.5 + 3 = 21.5px). The lower bound stays the design-system minimum:
  // the bar must never overlap a card or its clickable area.
  const HALF_GAP_MAX = 21.5;

  // This one measures four (viewport, seat count) pairs, each a fresh page
  // navigation through the shared browser and Vite's transform; under full
  // suite load that is far past the 5000ms default `it` budget the test
  // silently inherited (measured: transform alone 140s+ across 100 files, and
  // geometry.js waits up to 60s per selector for the fixture to mount). The
  // explicit budget matches the `waitForSelector` timeout below, as the
  // sibling geometry tests do (HandFan 20s, PromptSurface 30s, Arrows 120s).
  it('keeps the named clearance from command-card rows in two- and four-seat layouts at every acceptance viewport', { timeout: 60_000 }, async () => {
    const results: Geometry[] = [];
    for (const [width, height] of [[1440, 900], [1000, 900], [650, 700]]) {
      for (const seats of [2, 4]) results.push(await geometry(width, height, seats));
    }

    for (const result of results) {
      expect.soft(result.band.left, `${result.viewport}, ${result.seats} seats: felt left edge`).toBe(result.stage.left);
      expect.soft(result.band.right, `${result.viewport}, ${result.seats} seats: felt right edge`).toBe(result.stage.right);
      expect.soft(result.band.top + result.band.height / 2, `${result.viewport}, ${result.seats} seats: centred`).toBe(result.stage.top + result.stage.height / 2);
      expect.soft(result.clock.bottom, `${result.viewport}, ${result.seats} seats: tabs attach to clock`).toBe(result.strip.top);
      expect.soft(result.band.height, `${result.viewport}, ${result.seats} seats: reservation includes clock and tabs`).toBe(result.clock.height + result.strip.height);
      expect.soft(result.clearanceAbove, `${result.viewport}, ${result.seats} seats: above, at least the spacing floor`).toBeGreaterThanOrEqual(result.minimum);
      expect.soft(result.clearanceBelow, `${result.viewport}, ${result.seats} seats: below, at least the spacing floor`).toBeGreaterThanOrEqual(result.minimum);
      expect.soft(result.clearanceAbove, `${result.viewport}, ${result.seats} seats: above, at most half the measured 37px gap`).toBeLessThanOrEqual(HALF_GAP_MAX);
      expect.soft(result.clearanceBelow, `${result.viewport}, ${result.seats} seats: below, at most half the measured 37px gap`).toBeLessThanOrEqual(HALF_GAP_MAX);
    }
  });

  it('keeps every tab keyboard reachable, opens on focus, closes on Escape, and has a contiguous pointer path', async () => {
    const page = await browser.newPage({ viewport: { width: 1000, height: 900 } });
    await page.goto(`${url}src/components/PhaseLane.geometry.html?seats=2`);
    const tabs = page.locator('[data-hot-tab]');
    // Six fixed tabs: ACTIONS, PASS, END TURN, RESOLVE ALL, UNDO and DONE.
    // RESOLVE ALL owns its slot unconditionally since 7022042e6 (disabled
    // outside a live-stack priority window), so it is counted here even
    // though the fixture carries an empty stack.
    await expect.poll(() => tabs.count()).toBe(6);
    const count = await tabs.count();
    for (let i = 0; i < count; i++) await expect.soft(tabs.nth(i).getAttribute('aria-label')).resolves.toBeTruthy();
    await expect.soft(page.locator('[data-undo]').getAttribute('aria-label')).resolves.toBe('Undo my last action');

    const actions = page.locator('[data-hot-tab="actions"]');
    const panel = page.locator('[data-hot-panel="actions"]');
    await actions.focus();
    await expect.poll(() => panel.evaluate((e) => getComputedStyle(e).visibility)).toBe('visible');
    await page.keyboard.press('Escape');
    await expect.poll(() => panel.evaluate((e) => getComputedStyle(e).visibility)).toBe('hidden');

    await actions.hover();
    const [tabRect, panelRect] = await Promise.all([actions.boundingBox(), panel.boundingBox()]);
    expect(tabRect).not.toBeNull();
    expect(panelRect).not.toBeNull();
    expect.soft(panelRect!.y).toBe(tabRect!.y + tabRect!.height);
    await page.mouse.move(panelRect!.x + panelRect!.width / 2, panelRect!.y + 2);
    await new Promise((resolve) => setTimeout(resolve, 220));
    await expect.poll(() => panel.evaluate((e) => getComputedStyle(e).visibility)).toBe('visible');
    await page.close();
  });

  it('keeps a seven-card opening hand on one non-scrolling row at every acceptance viewport', async () => {
    for (const [width, height] of [[1440, 900], [1000, 900], [650, 700]]) {
      const page = await browser.newPage({ viewport: { width, height } });
      await page.goto(`${url}src/components/OpeningHand.geometry.html`);
      const measured = await page.evaluate(() => {
        const compact = (r: DOMRect) => ({ top: r.top, bottom: r.bottom, width: r.width, height: r.height });
        const row = document.querySelector<HTMLElement>('[data-opening-hand]')!;
        const cards = [...row.querySelectorAll<HTMLElement>('[data-obj]')].map((el) => compact(el.getBoundingClientRect()));
        return {
          row: compact(row.getBoundingClientRect()), cards,
          clientWidth: row.clientWidth, scrollWidth: row.scrollWidth,
        };
      });
      expect.soft(measured.cards, `${width}x${height}: seven cards`).toHaveLength(7);
      expect.soft(new Set(measured.cards.map((r) => r.top)).size, `${width}x${height}: one row`).toBe(1);
      expect.soft(measured.scrollWidth, `${width}x${height}: no sideways scroll`).toBeLessThanOrEqual(measured.clientWidth);
      expect.soft(Math.min(...measured.cards.map((r) => r.width)), `${width}x${height}: positive thumbnails`).toBeGreaterThan(0);
      await page.close();
    }

    // Seven is the tight case measured above; every smaller London hand uses
    // the same non-wrapping contract rather than falling back to the old CSS.
    for (let count = 0; count < 7; count++) {
      const page = await browser.newPage({ viewport: { width: 650, height: 700 } });
      await page.goto(`${url}src/components/OpeningHand.geometry.html?cards=${count}`);
      const measured = await page.evaluate(() => {
        const row = document.querySelector<HTMLElement>('[data-opening-hand]')!;
        const tops = [...row.querySelectorAll<HTMLElement>('[data-obj]')]
          .map((el) => el.getBoundingClientRect().top);
        return { count: tops.length, rows: new Set(tops).size, clientWidth: row.clientWidth, scrollWidth: row.scrollWidth };
      });
      expect.soft(measured.count, `${count} cards: all rendered`).toBe(count);
      expect.soft(measured.rows, `${count} cards: at most one row`).toBeLessThanOrEqual(1);
      expect.soft(measured.scrollWidth, `${count} cards: no sideways scroll`).toBeLessThanOrEqual(measured.clientWidth);
      await page.close();
    }
  });

  // The one-row rule alone did not catch the defect that shipped with it: the
  // cards fitted in one row and still looked wrong, because the SLOT resized
  // and the CARD did not. At 1440 a 89px card floated in a 143px slot; at 650
  // the same 89px card overflowed a 39px one, and CardTile's keyword chips and
  // P/T badge -- which are positioned from --card-w -- detached from the card
  // they belong to. This leaf measures the card against its own slot.
  it('each opening-hand card fills its own slot, at every viewport', async () => {
    for (const [width, height] of [[1440, 900], [1000, 900], [650, 700]] as const) {
      const page = await browser.newPage({ viewport: { width, height } });
      await page.goto(`${url}src/components/OpeningHand.geometry.html?cards=7`);
      const measured = await page.evaluate(() => {
        const row = document.querySelector<HTMLElement>('[data-opening-hand]')!;
        const slot = row.children[0] as HTMLElement;
        const card = slot.querySelector<HTMLElement>('.card-image') ?? slot;
        const s = slot.getBoundingClientRect();
        const c = card.getBoundingClientRect();
        // anything drawn outside the slot has come adrift from its card
        const escaping = [...slot.querySelectorAll<HTMLElement>('*')].filter((el) => {
          const r = el.getBoundingClientRect();
          return r.width > 0 && (r.x < s.x - 1 || r.right > s.right + 1 || r.bottom > s.bottom + 1);
        }).length;
        return { slotW: Math.round(s.width), cardW: Math.round(c.width), escaping };
      });
      expect.soft(measured.cardW, `${width}x${height}: card fills its slot`).toBe(measured.slotW);
      expect.soft(measured.escaping, `${width}x${height}: nothing drawn outside the slot`).toBe(0);
      await page.close();
    }
  });

  it('caps opening-hand cards at the shared gameplay scale', async () => {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    await page.goto(`${url}src/components/OpeningHand.geometry.html?cards=3`);
    const widths = await page.locator('[data-opening-hand] .card-image').evaluateAll((els) =>
      els.map((el) => Math.round(el.getBoundingClientRect().width)),
    );
    // --play-card-w is 104px. A spacious opening hand must use that same
    // gameplay scale instead of the old fixed 220px modal-card scale.
    expect(widths).toHaveLength(3);
    expect(Math.max(...widths)).toBeLessThanOrEqual(104);
    await page.close();
  });

  it('PASS is unavailable without a pass option and posts a non-positional pass by its wire index', async () => {
    const unavailable = await browser.newPage({ viewport: { width: 1000, height: 900 } });
    await unavailable.goto(`${url}src/components/PhaseLane.geometry.html?decision=choose`);
    // Pass is a transport control now, not a menu: the button is always
    // there, and "the wire did not offer a pass" shows as DISABLED rather
    // than as an absent panel. R-E4-2 is unchanged -- a disabled button
    // posts nothing.
    await expect.poll(() => unavailable.locator('[data-hot-tab="pass"]').getAttribute('aria-disabled')).toBe('true');
    expect(await unavailable.locator('[data-pass-action]:not([disabled])').count()).toBe(0);
    await unavailable.close();

    const page = await browser.newPage({ viewport: { width: 1000, height: 900 } });
    let intent: { choices?: number[] } | null = null;
    await page.route('**/api/tables/fixture/matches/1/intent', async (route) => {
      intent = route.request().postDataJSON() as { choices?: number[] };
      await route.fulfill({ status: 204, body: '' });
    });
    await page.goto(`${url}src/components/PhaseLane.geometry.html`);
    // One click, not two: the glyph IS the action.
    await page.locator('[data-hot-tab="pass"]').click();
    await expect.poll(() => intent?.choices).toEqual([42]);
    await page.close();
  });
});
