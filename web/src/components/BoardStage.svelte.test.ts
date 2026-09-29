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

async function table(width: number, height: number, query: string) {
  const page = await browser.newPage({ viewport: { width, height } });
  await page.goto(`${url}src/components/PhaseLane.geometry.html?${query}`);
  // goto resolves before the Svelte mount; under load Vite's cold transform
  // can take tens of seconds. Wait for everything the measurement reads.
  for (const sel of ['[data-board-stage]', '[data-centre-strip]', '[data-phase-track]', '[data-own-board] .quadrant']) {
    await page.waitForSelector(sel, { state: 'attached', timeout: 60_000 });
  }
  return page;
}

async function measure(width: number, height: number, query: string) {
  const page = await table(width, height, query);
  const m = await page.evaluate(() => {
    const r = (el: Element): Rect => {
      const b = el.getBoundingClientRect();
      return { top: b.top, bottom: b.bottom, height: b.height, left: b.left, right: b.right, width: b.width };
    };
    const stage = r(document.querySelector('[data-board-stage]')!);
    const strip = r(document.querySelector('[data-centre-strip]')!);
    const opps = [...document.querySelectorAll('[data-opponents] .quadrant')].map(r);
    const own = r(document.querySelector('[data-own-board] .quadrant')!);
    // every card tile drawn on the table (not the piles column)
    const tiles = [...document.querySelectorAll('.quadrant .card-tile, .quadrant [data-cmd-state]')].map(r);
    return {
      stage, strip, opps, own, tiles,
      headers: document.querySelectorAll('[data-seat-header]').length,
      commanders: document.querySelectorAll('[data-cmd-state="command"]').length,
      cells: [...document.querySelectorAll('[data-cell-seat]')].map((e) => Number(e.getAttribute('data-cell-seat'))),
    };
  });
  await page.close();
  return m;
}

describe('BoardStage — the table is arranged, and the centre strip is a real lane', () => {
  it('every seat is placed at 2, 4, 6 and 8 seats, and no card crosses the centre strip', { timeout: 120_000 }, async () => {
    // [width, height, seats, query, commander tiles drawn]: a focus side
    // strip draws no command zone (its header stands in), so 8-seat focus
    // shows the focused opponent's and the viewer's only.
    const cases: [number, number, number, string, number][] = [
      [1440, 900, 2, 'seats=2', 2],
      [1000, 900, 4, 'seats=4&preset=cmd4', 4],
      [1440, 900, 6, 'seats=6&preset=grid6', 6],
      [1440, 900, 8, 'seats=8&preset=focus8', 2],
      [650, 700, 4, 'seats=4', 4],
    ];
    for (const [w, h, seats, q, cmds] of cases) {
      const m = await measure(w, h, q);
      const at = `${w}x${h}, ${q}`;
      expect.soft(m.cells.length + 1, `${at}: every seat placed`).toBe(seats);
      expect.soft(m.commanders, `${at}: commander tiles`).toBe(cmds);
      expect.soft(m.headers, `${at}: every opponent named`).toBe(seats - 1);
      expect.soft(m.strip.left, `${at}: strip spans the felt`).toBe(m.stage.left);
      expect.soft(m.strip.right, `${at}: strip spans the felt`).toBe(m.stage.right);
      for (const o of m.opps) expect.soft(o.bottom, `${at}: opponents end above the strip`).toBeLessThanOrEqual(m.strip.top + 0.5);
      expect.soft(m.own.top, `${at}: your board starts below the strip`).toBeGreaterThanOrEqual(m.strip.bottom - 0.5);
      for (const t of m.tiles) {
        const crosses = t.top < m.strip.bottom && t.bottom > m.strip.top;
        expect.soft(crosses, `${at}: a card crosses the centre strip`).toBe(false);
      }
    }
  });

  it('the ACTIONS tab stays keyboard reachable in the strip, opens on focus and closes on Escape', async () => {
    const page = await table(1000, 900, 'seats=2');
    // The strip keeps ACTIONS and DONE; Pass, End turn and Undo live in the gilt action cluster.
    await expect.poll(() => page.locator('[data-hot-tab]').count()).toBe(2);
    await expect.poll(() => page.locator('[data-action-cluster]').count()).toBe(1);
    const actions = page.locator('[data-hot-tab="actions"]');
    const panel = page.locator('[data-hot-panel="actions"]');
    await actions.focus();
    await expect.poll(() => panel.evaluate((e) => getComputedStyle(e).visibility)).toBe('visible');
    await page.keyboard.press('Escape');
    await expect.poll(() => panel.evaluate((e) => getComputedStyle(e).visibility)).toBe('hidden');
    await page.close();
  });

  it('dragging the centre bar moves the split and saves it; a double-click resets it', async () => {
    const page = await table(1200, 900, 'seats=2');
    const before = await page.locator('[data-opponents]').boundingBox();
    const strip = await page.locator('[data-centre-strip] [data-turn-label]').boundingBox();
    // grab the strip's empty felt just right of the turn label
    const x = strip!.x + strip!.width + 4;
    const y = strip!.y + strip!.height / 2;
    await page.mouse.move(x, y);
    await page.mouse.down();
    await page.mouse.move(x, y + 120, { steps: 6 });
    await page.mouse.up();
    await expect.poll(async () => (await page.locator('[data-opponents]').boundingBox())!.height).toBeGreaterThan(before!.height + 60);
    const saved = await page.evaluate(() => JSON.parse(localStorage.getItem('gorge.layouts.v1') ?? '{}').current?.table?.split);
    expect(saved).toBeGreaterThan(0.45);
    await page.mouse.dblclick(x, y + 120);
    await expect.poll(async () => (await page.locator('[data-opponents]').boundingBox())!.height).toBeCloseTo(before!.height, 0);
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

  it('a player offered by a target decision glows verdigris: the opponent\'s header bar and your seat box', async () => {
    const page = await table(1200, 900, 'decision=target');
    await expect.poll(() => page.locator('[data-seat-header="1"]').getAttribute('class')).toMatch(/\btarget\b/);
    await expect.poll(() => page.locator('[data-seat-box="0"]').getAttribute('class')).toMatch(/\btarget\b/);
    const glow = await page.locator('[data-seat-header="1"]').evaluate((e) => getComputedStyle(e).boxShadow);
    expect(glow).toContain('rgb(111, 183, 174)'); // --verdigris
    await page.close();
  });

  it('the gilt Pass is Waiting without a pass option and posts a non-positional pass by its wire index', async () => {
    const unavailable = await table(1000, 900, 'decision=choose');
    // No pass on the wire: the button names the wait and posts nothing (R-E4-2).
    await expect.poll(() => unavailable.locator('[data-pass-action]').getAttribute('disabled')).not.toBeNull();
    expect(await unavailable.locator('[data-pass-action]').textContent()).toContain('Waiting');
    await unavailable.close();

    const page = await browser.newPage({ viewport: { width: 1000, height: 900 } });
    let intent: { choices?: number[] } | null = null;
    await page.route('**/api/tables/fixture/matches/1/intent', async (route) => {
      intent = route.request().postDataJSON() as { choices?: number[] };
      await route.fulfill({ status: 204, body: '' });
    });
    await page.goto(`${url}src/components/PhaseLane.geometry.html`);
    const pass = page.locator('[data-pass-action]');
    await expect.poll(() => pass.textContent()).toContain('Move to Beginning of combat');
    await pass.click();
    await expect.poll(() => intent?.choices).toEqual([42]);
    await page.close();
  });
});
