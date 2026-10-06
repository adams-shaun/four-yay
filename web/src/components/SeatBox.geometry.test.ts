import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

// The seat-slot widths BoardStage's 10rem…15rem own-seat track produces.
const SLOT_WIDTHS = [240, 200, 190, 160] as const;

interface Probe {
  countsClientW: number;
  countsScrollW: number;
  countsLeft: number;
  countsRight: number;
  piles: Record<string, { left: number; right: number } | undefined>;
  poolPresent: boolean;
  pileCount: Record<string, number>;
  boxRight: number;
  poolRight: number;
}

async function probe(page: Awaited<ReturnType<typeof browser.newPage>>): Promise<Probe> {
  return page.evaluate(() => {
    const counts = document.querySelector('[data-seat-counts]') as HTMLElement;
    const c = counts.getBoundingClientRect();
    const piles: Probe['piles'] = {};
    for (const zone of ['graveyard', 'exile']) {
      const r = document.querySelector(`[data-pile="${zone}"]`)?.getBoundingClientRect();
      piles[zone] = r ? { left: r.left, right: r.right } : undefined;
    }
    const pool = document.querySelector('[data-mana-pool]');
    return {
      countsClientW: counts.clientWidth,
      countsScrollW: counts.scrollWidth,
      countsLeft: c.left,
      countsRight: c.right,
      piles,
      poolPresent: pool !== null && document.querySelector('[data-mana-restrictions]') !== null,
      pileCount: {
        graveyard: document.querySelectorAll('[data-pile="graveyard"]').length,
        exile: document.querySelectorAll('[data-pile="exile"]').length,
      },
      boxRight: document.querySelector('[data-seat-box]')!.getBoundingClientRect().right,
      poolRight: pool ? pool.getBoundingClientRect().right : 0,
    };
  });
}

describe('SeatBox — pile buttons are never clipped (fb-20261006T101047Z-cd69ff29)', () => {
  it('graveyard and exile buttons sit fully inside the counts box at every seat-slot width, with a restricted mana pool present', { timeout: 60_000 }, async () => {
    for (const w of SLOT_WIDTHS) {
      const page = await browser.newPage({ viewport: { width: 1267, height: 531 } });
      await page.goto(`${url}src/components/SeatBox.geometry.html?w=${w}`);
      await page.waitForSelector('[data-seat-counts]');
      const m = await probe(page);
      await page.close();
      console.log(`SEATBOX w${w} countsClientW ${m.countsClientW} countsScrollW ${m.countsScrollW} exile ${JSON.stringify(m.piles.exile)} counts.right ${m.countsRight}`);

      // Preconditions: both piles are real buttons, the restricted pool is
      // present, and the box is the width under test (so a vacuous fixture
      // fails loudly rather than passing).
      expect(m.pileCount).toEqual({ graveyard: 1, exile: 1 });
      expect(m.poolPresent).toBe(true);
      expect(m.boxRight).toBeLessThanOrEqual(w + 0.5);

      for (const zone of ['graveyard', 'exile']) {
        const r = m.piles[zone]!;
        expect(r, `${zone} button at w${w}`).toBeDefined();
        expect(r.left, `${zone} left at w${w}`).toBeGreaterThanOrEqual(m.countsLeft - 0.5);
        expect(r.right, `${zone} right at w${w}`).toBeLessThanOrEqual(m.countsRight + 0.5);
      }
      // Nothing in the counts line is cut: its content is not wider than its box.
      expect(m.countsScrollW, `counts scroll width at w${w}`).toBeLessThanOrEqual(m.countsClientW + 1);
      // The mana pool stays inside the seat box rather than spilling out of it.
      expect(m.poolRight, `pool right at w${w}`).toBeLessThanOrEqual(m.boxRight + 0.5);
    }
  });

  it('the visible exile button is wired: clicking it opens the shared PileModal on the viewer\'s own exile', { timeout: 60_000 }, async () => {
    const page = await browser.newPage({ viewport: { width: 1267, height: 531 } });
    await page.goto(`${url}src/components/SeatBox.geometry.html?w=190`);
    await page.waitForSelector('[data-pile="exile"]');
    const before = await page.getByRole('dialog').count();
    expect(before).toBe(0);
    await page.locator('[data-pile="exile"]').click();
    const dialog = page.getByRole('dialog', { name: 'Your exile' });
    expect(await dialog.isVisible()).toBe(true);
    expect(await dialog.locator('[data-obj]').count()).toBe(10);
    expect(await dialog.locator('[data-obj="101"]').textContent()).toContain('Exiled Card 101');
    await page.close();
  });
});
