import { type Browser, type Page } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

let browser: Browser;
beforeAll(async () => {
  browser = await sharedBrowser();
});

/**
 * "A prompt never intersects the Feedback button" (player report: prompts
 * default to the lower right rail, and must never cover — or be covered by —
 * the persistent Feedback button). The REAL PromptDock, PromptRailSlot (the
 * production lower-slot CSS) and FeedbackButton are mounted by
 * PromptDock.feedback.geometry.fixture.svelte; every case MEASURES both
 * rectangles (non-zero, so a vacuous mount fails) and asserts no
 * positive-area intersection, then performs REAL Playwright clicks on the
 * Feedback button and an answer control (the hit-target check fails if either
 * is covered).
 */

interface Rect { left: number; top: number; right: number; bottom: number; width: number; height: number }

async function rect(page: Page, sel: string): Promise<Rect> {
  const r = await page.evaluate((s) => {
    const el = document.querySelector(s);
    if (!el) return null;
    const b = el.getBoundingClientRect();
    return { left: b.left, top: b.top, right: b.right, bottom: b.bottom, width: b.width, height: b.height };
  }, sel);
  expect(r, `${sel} is mounted`).not.toBeNull();
  expect(r!.width, `${sel} has width`).toBeGreaterThan(0);
  expect(r!.height, `${sel} has height`).toBeGreaterThan(0);
  return r!;
}

/** The overlap area of two rects; 0 when they only touch or are apart. */
function overlap(a: Rect, b: Rect): number {
  const w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
  const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
  return w > 0 && h > 0 ? w * h : 0;
}

async function open(w: number, h: number, query: string): Promise<Page> {
  const page = await browser.newPage();
  await page.setViewportSize({ width: w, height: h });
  await page.goto(`${browserURL}src/components/PromptDock.feedback.geometry.html?${query}`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('[data-prompt-dock] [data-option="0"]', { timeout: 5000 });
  await page.waitForSelector('[data-feedback-button]', { timeout: 5000 });
  return page;
}

/** The part of the prompt a player sees: the slot (a scroll container) for a rail dock, else the dock itself. */
const visibleDock = (page: Page, rail: boolean) => rect(page, rail ? '[data-prompt-dock-slot]' : '[data-prompt-dock]');

/** Asserts the no-intersection contract and that Feedback and an answer control are really clickable. */
async function expectClear(page: Page, rail: boolean): Promise<{ dock: Rect; fb: Rect }> {
  const dock = await visibleDock(page, rail);
  const fb = await rect(page, '[data-feedback-button]');
  expect(overlap(dock, fb), `dock ${JSON.stringify(dock)} vs feedback ${JSON.stringify(fb)}`).toBe(0);
  return { dock, fb };
}

async function clickBoth(page: Page): Promise<void> {
  await page.locator('[data-prompt-dock] button[data-option="0"]').click({ timeout: 5000 });
  await page.waitForFunction(() => document.querySelector('[data-prompt-dock] button[data-option="0"]')?.classList.contains('picked') === true, undefined, { timeout: 5000 });
  await page.getByRole('button', { name: 'Feedback' }).click({ timeout: 5000 });
  await page.waitForSelector('[role="dialog"][aria-label="Send feedback"]', { timeout: 5000 });
}

describe('lower rail slot clears the Feedback button', () => {
  it('desktop: a short question in the right rail sits above Feedback', { timeout: 60_000 }, async () => {
    const page = await open(1280, 800, 'place=rail-bottom&ask=short');
    try {
      const { dock, fb } = await expectClear(page, true);
      // Precondition: the dock really is the lower rail slot, in the corner column Feedback lives in.
      expect(await page.locator('[data-prompt-dock]').getAttribute('data-placement')).toBe('rail-bottom');
      expect(dock.right).toBeGreaterThan(fb.left);
      expect(dock.top).toBeGreaterThan(400);
      await clickBoth(page);
    } finally {
      await page.close();
    }
  });

  it('compact viewport: a tall scrolling question keeps its last answer reachable and clear of Feedback', { timeout: 60_000 }, async () => {
    const page = await open(800, 500, 'place=rail-bottom&ask=huge');
    try {
      const { dock } = await expectClear(page, true);
      // Precondition: the question really overflows its slot, so it scrolls.
      const scrollable = await page.evaluate(() => {
        const s = document.querySelector('[data-prompt-dock-slot]') as HTMLElement;
        return s.scrollHeight - s.clientHeight;
      });
      expect(scrollable).toBeGreaterThan(50);
      expect(dock.height).toBeLessThanOrEqual(500 * 0.45 + 1);
      const last = page.locator('[data-prompt-dock] button[data-option="23"]');
      await last.scrollIntoViewIfNeeded();
      const fb = await rect(page, '[data-feedback-button]');
      expect(overlap(await rect(page, '[data-prompt-dock] button[data-option="23"]'), fb)).toBe(0);
      await last.click({ timeout: 5000 });
      await page.waitForFunction(() => document.querySelector('[data-prompt-dock] button[data-option="23"]')?.classList.contains('picked') === true, undefined, { timeout: 5000 });
      await page.getByRole('button', { name: 'Feedback' }).click({ timeout: 5000 });
      await page.waitForSelector('[role="dialog"][aria-label="Send feedback"]', { timeout: 5000 });
    } finally {
      await page.close();
    }
  });

  it('a left rail and a peeked hidden-rail drawer both stay clear of Feedback', { timeout: 60_000 }, async () => {
    for (const side of ['left', 'hidden']) {
      const page = await open(1280, 800, `place=rail-bottom&ask=long&side=${side}`);
      try {
        const { dock, fb } = await expectClear(page, true);
        // Precondition: the hidden drawer really reaches the Feedback column; the left rail really does not.
        if (side === 'hidden') expect(dock.right).toBeGreaterThan(fb.left);
        else expect(dock.right).toBeLessThan(fb.left);
        await clickBoth(page);
      } finally {
        await page.close();
      }
    }
  });
});

describe('floating and near-table prompts keep clear of Feedback', () => {
  /** A saved position whose top-left sits on the Feedback button. */
  async function aimedAtFeedback(w: number, h: number): Promise<Page> {
    // Feedback is ~ 90x28 at the bottom-right; x/y are the dock's top-left in px.
    return open(w, h, `place=floating&ask=short&fx=${w - 120}&fy=${h - 60}`);
  }

  it('restore: a saved spot over the button is drawn clear, at desktop and compact sizes', { timeout: 60_000 }, async () => {
    for (const [w, h] of [[1280, 800], [640, 420]] as const) {
      const page = await aimedAtFeedback(w, h);
      try {
        const { dock, fb } = await expectClear(page, false);
        // Precondition: the SAVED spot really overlaps the button's box if the dock were drawn there.
        const saved = { left: w - 120, top: h - 60, right: w - 120 + dock.width, bottom: h - 60 + dock.height, width: dock.width, height: dock.height };
        expect(overlap(saved, fb)).toBeGreaterThan(0);
        await clickBoth(page);
      } finally {
        await page.close();
      }
    }
  });

  it('keyboard nudges and a pointer drag toward the button never end on it; a resize re-clears it', { timeout: 60_000 }, async () => {
    const page = await open(1280, 800, 'place=floating&ask=short&fx=700&fy=500');
    try {
      await expectClear(page, false);
      await page.locator('[data-dock-grip]').focus();
      // Push right and down far past the button; every step must stay clear.
      for (let i = 0; i < 40; i++) {
        await page.keyboard.press(i % 2 === 0 ? 'Shift+ArrowRight' : 'Shift+ArrowDown');
        if (i % 8 === 7) await expectClear(page, false);
      }
      await expectClear(page, false);

      // Pointer: drag the grip onto the Feedback button's centre.
      const fb = await rect(page, '[data-feedback-button]');
      const grip = await rect(page, '[data-dock-grip]');
      await page.mouse.move(grip.left + grip.width / 2, grip.top + grip.height / 2);
      await page.mouse.down();
      await page.mouse.move(fb.left + fb.width / 2, fb.top + fb.height / 2, { steps: 8 });
      expect(overlap(await rect(page, '[data-prompt-dock]'), fb), 'clear while dragging').toBe(0);
      await page.mouse.up();
      await expectClear(page, false);

      // Resize into a compact window: the dock is re-placed clear of the (moved) button.
      await page.setViewportSize({ width: 640, height: 420 });
      await page.waitForFunction(() => {
        const d = document.querySelector('[data-prompt-dock]')!.getBoundingClientRect();
        // clampPosition keeps the grip reachable (it may overhang the edge), so wait for it to run.
        return d.left <= window.innerWidth - 100 && d.top <= window.innerHeight - 48;
      }, undefined, { timeout: 5000 });
      await expectClear(page, false);
      await clickBoth(page);
    } finally {
      await page.close();
    }
  });

  it('near-table with no action button (corner fallback) sits above Feedback', { timeout: 60_000 }, async () => {
    const page = await open(1000, 700, 'place=table&ask=short&side=left');
    try {
      await page.waitForSelector('[data-prompt-dock][data-placement="table"]', { timeout: 5000 });
      const { dock, fb } = await expectClear(page, false);
      // Precondition: the dock reaches the Feedback column, so the lift is what keeps it clear.
      expect(dock.right).toBeGreaterThan(fb.left);
      expect(fb.top - dock.bottom).toBeGreaterThanOrEqual(0);
      await clickBoth(page);
    } finally {
      await page.close();
    }
  });
});
