import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

// The panel is portalled to <body>, id'd by the card's object id, so the
// oracle-present card (id 1) and the plain card (id 2) are measured directly.
const VIEWPORTS = [
  { width: 1440, height: 900 },
  { width: 1000, height: 900 },
  { width: 650, height: 700 },
] as const;

interface Rect {
  x: number; y: number; width: number; height: number; top: number; bottom: number; left: number; right: number;
}

interface Rects {
  panel: Rect;
  plate: Rect | null;
  img: Rect | null;
  clientHeight: number;
  scrollHeight: number;
}

async function measure(page: Awaited<ReturnType<typeof browser.newPage>>, id: number): Promise<Rects> {
  return page.evaluate((cardId) => {
    const rect = (selector: string): Rect | null => {
      const el = document.querySelector(selector);
      if (!el) return null;
      const b = el.getBoundingClientRect();
      return { x: b.x, y: b.y, width: b.width, height: b.height, top: b.top, bottom: b.bottom, left: b.left, right: b.right };
    };
    const panel = document.querySelector(`#card-detail-${cardId}`)!;
    return {
      panel: rect(`#card-detail-${cardId}`)!,
      plate: rect(`#card-detail-${cardId} .plate`),
      img: rect(`#card-detail-${cardId} .plate img`),
      clientHeight: (panel as HTMLElement).clientHeight,
      scrollHeight: (panel as HTMLElement).scrollHeight,
    };
  }, id);
}

describe('CardDetail — plate non-occlusion (ui28)', () => {
  it('a card with a resolved oracle keeps its whole plate: the printed card is never cropped and its oracle block stays inside the panel', { timeout: 60_000 }, async () => {
    for (const { width, height } of VIEWPORTS) {
      const page = await browser.newPage({ viewport: { width, height } });
      await page.goto(`${url}src/components/CardDetail.geometry.html`);
      // The plate renders only once the image resolves; wait for it, then
      // for the oracle card's ledger to have painted.
      await page.waitForSelector('#card-detail-1 .plate img', { timeout: 5000 });
      await page.waitForTimeout(50);
      const m = await measure(page, 1);
      await page.close();

      // The plate and its image are guaranteed present (we waited for the
      // img): assert it, so the accesses below are provably not null.
      expect(m.plate).not.toBeNull();
      expect(m.img).not.toBeNull();
      const plate = m.plate!;
      const img = m.img!;

      // 1. The plate is its own uncut height, not a flex-shrunk sliver. The
      //    1px slack is the plate's top border offset relative to the img.
      expect(plate.height).toBeGreaterThanOrEqual(img.height - 0.5);
      // 2. The whole printed card — including the oracle text box that sits in
      //    the card's lower half — is inside the panel's live client rect, so
      //    the panel does not cut the printed oracle text off.
      expect(img.bottom).toBeLessThanOrEqual(m.panel.bottom + 0.5);
      // 3. The plate's oracle block (the printed text box, the lower 45%..95%
      //    of the card face) is fully contained by the panel rect: the panel's
      //    rect and the plate's oracle block do not intersect at the clip edge.
      const oracleTop = img.top + img.height * 0.45;
      const oracleBottom = img.top + img.height * 0.95;
      expect(oracleTop).toBeGreaterThanOrEqual(m.panel.top - 0.5);
      expect(oracleBottom).toBeLessThanOrEqual(m.panel.bottom + 0.5);
      expect(m.clientHeight).toBeGreaterThanOrEqual(img.height);
    }
  });

  it('a card with no resolved oracle (the normal case) is unchanged in kind and still fits', { timeout: 60_000 }, async () => {
    for (const { width, height } of VIEWPORTS) {
      const page = await browser.newPage({ viewport: { width, height } });
      await page.goto(`${url}src/components/CardDetail.geometry.html`);
      await page.waitForSelector('#card-detail-2', { timeout: 5000 });
      const m = await measure(page, 2);
      const html = await page.evaluate(() => document.querySelector('#card-detail-2')!.innerHTML);
      await page.close();

      // The plate stays absent (no image resolved): the panel is a plain
      // typeset record, not a frame for nothing.
      expect(html).not.toContain('card-image');
      // It still has its record rows and fits the viewport.
      expect(html).toContain('Squire');
      expect(html).toContain('Creature — Human Soldier');
      expect(html).toContain('3/2');
      expect(m.panel.top).toBeGreaterThanOrEqual(8);
      expect(m.panel.bottom).toBeLessThanOrEqual(height - 8);
      expect(m.panel.left).toBeGreaterThanOrEqual(8);
    }
  });

  it('a hand-position anchor still opens upward and the panel never leaves the viewport', { timeout: 60_000 }, async () => {
    for (const { width, height } of VIEWPORTS) {
      const page = await browser.newPage({ viewport: { width, height } });
      await page.goto(`${url}src/components/CardDetail.geometry.html`);
      await page.waitForSelector('#card-detail-1 .plate img', { timeout: 5000 });
      const m = await measure(page, 1);
      const anchorTop = Math.max(360, Math.round(height - 190));
      await page.close();

      // Opens upward: the panel's bottom edge sits above the card's top.
      expect(m.panel.bottom).toBeLessThanOrEqual(anchorTop - 8 + 0.5);
      // Fits on both axes.
      expect(m.panel.top).toBeGreaterThanOrEqual(8);
      expect(m.panel.bottom).toBeLessThanOrEqual(height - 8);
      expect(m.panel.left).toBeGreaterThanOrEqual(8);
      expect(m.panel.right).toBeLessThanOrEqual(width - 8);
    }
  });
});
