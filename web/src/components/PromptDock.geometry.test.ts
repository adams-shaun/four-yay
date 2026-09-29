import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

/**
 * PromptDock near-table geometry (ui24): the default placement pins the dock
 * above the action button and grows it UP OVER the board, so its rect covers
 * board cards. The contract this pins: the dock's chrome is
 * pointer-transparent — a click aimed at a board target under it reaches the
 * board — while the dock's own controls (tools buttons, renderer controls)
 * keep working, and the near-table anchor follows a post-mount board resize.
 *
 * Every assertion that depends on overlap first MEASURES the overlap: the
 * test pins the fake pile badge against the dock's live rect, so it cannot
 * silently pass with no board under the dock.
 */

interface Rect {
  left: number; top: number; right: number; bottom: number; width: number; height: number;
}

const rects = `(function () {
  const grab = (sel) => {
    const el = document.querySelector(sel);
    if (!el) return null;
    const b = el.getBoundingClientRect();
    return { left: b.left, top: b.top, right: b.right, bottom: b.bottom, width: b.width, height: b.height };
  };
  return { dock: grab('[data-prompt-dock]'), badge: grab('[data-pile-badge]'), cluster: grab('[data-action-cluster]') };
})()`;

/** Loads the fixture and pins the badge INSIDE the dock's measured rect. */
async function openWithBadgeUnderDock(page: Awaited<ReturnType<Browser['newPage']>>): Promise<void> {
  await page.setViewportSize({ width: 1000, height: 700 });
  await page.goto(`${url}src/components/PromptDock.geometry.html`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('[data-prompt-dock][data-placement="table"]', { timeout: 5000 });
  await page.waitForSelector('[data-prompt-title]', { timeout: 5000 });
  // Pin the badge against the dock's live rect: near its lower-left corner,
  // in the body's padding area — the chrome a transparent dock must let
  // through. position:fixed takes viewport coordinates, which is what the
  // dock's own rect is measured in.
  await page.evaluate(() => {
    const dock = document.querySelector('[data-prompt-dock]')!.getBoundingClientRect();
    const badge = document.querySelector('[data-pile-badge]') as HTMLElement;
    badge.style.position = 'fixed';
    badge.style.left = `${Math.round(dock.left + 40)}px`;
    badge.style.top = `${Math.round(dock.bottom - 40)}px`;
  });
}

describe('PromptDock near-table placement (ui24)', () => {
  it('a board badge under the dock is clickable: the dock chrome does not intercept the pointer', { timeout: 60_000 }, async () => {
    const page = await browser.newPage();
    await openWithBadgeUnderDock(page);
    try {
      const m = await page.evaluate<{ dock: Rect | null; badge: Rect | null }>(rects);
      expect(m.dock).not.toBeNull();
      expect(m.badge).not.toBeNull();
      const dock = m.dock!;
      const badge = m.badge!;
      // Precondition: the badge lies fully inside the dock's rect — without
      // this the test would pass on a dock that covers nothing.
      expect(badge.left).toBeGreaterThanOrEqual(dock.left);
      expect(badge.right).toBeLessThanOrEqual(dock.right);
      expect(badge.top).toBeGreaterThanOrEqual(dock.top);
      expect(badge.bottom).toBeLessThanOrEqual(dock.bottom);

      // A REAL click (Playwright's hit-target check, not elementFromPoint):
      // with an intercepting dock this never becomes actionable and the
      // click times out.
      await page.click('[data-pile-badge]', { timeout: 5000 });
      await page.waitForFunction(() => document.body.getAttribute('data-badge-clicked') === '1', undefined, { timeout: 5000 });
    } finally {
      await page.close();
    }
  });

  it("the dock's own controls still work: a renderer chip toggles and the placement toggle moves the dock", { timeout: 60_000 }, async () => {
    const page = await browser.newPage();
    await openWithBadgeUnderDock(page);
    try {
      // The renderer's chip (a button in the transparent-rooted dock) is
      // clickable and toggles into the picked state (min 1 / max 3 ask, so a
      // click toggles and never posts).
      const chip = page.locator('[data-prompt-dock] button[data-option="1"]');
      await chip.click({ timeout: 5000 });
      await page.waitForFunction(
        () => document.querySelector('[data-prompt-dock] button[data-option="1"]')?.classList.contains('picked') === true,
        undefined,
        { timeout: 5000 },
      );

      // The tools button cycles the placement: table -> rail.
      await page.click('[data-dock-placement-toggle]', { timeout: 5000 });
      await page.waitForSelector('[data-prompt-dock][data-placement="rail"]', { timeout: 5000 });
    } finally {
      await page.close();
    }
  });

  it('keyboard access is unchanged: a focused chip answers from the keyboard', { timeout: 60_000 }, async () => {
    const page = await browser.newPage();
    await openWithBadgeUnderDock(page);
    try {
      // pointer-events never affects the keyboard path; pin it anyway: the
      // chip answers Enter while focused, without any pointer event.
      await page.locator('[data-prompt-dock] button[data-option="0"]').focus();
      await page.keyboard.press('Enter');
      await page.waitForFunction(
        () => document.querySelector('[data-prompt-dock] button[data-option="0"]')?.classList.contains('picked') === true,
        undefined,
        { timeout: 5000 },
      );
    } finally {
      await page.close();
    }
  });

  it('the near-table anchor follows a post-mount board resize (ResizeObserver and window resize)', { timeout: 60_000 }, async () => {
    const page = await browser.newPage();
    await openWithBadgeUnderDock(page);
    try {
      const before = await page.evaluate<{ dock: Rect | null; cluster: Rect | null }>(rects);
      expect(before.dock).not.toBeNull();
      expect(before.cluster).not.toBeNull();
      // The dock's contract from tableAnchor: pinned ABOVE the action
      // cluster with the anchor's 8px gap, right-aligned with it.
      const pinned = (d: Rect, c: Rect) => {
        expect(c.top - d.bottom).toBeGreaterThanOrEqual(3);
        expect(c.top - d.bottom).toBeLessThanOrEqual(13);
        expect(Math.abs(d.right - c.right)).toBeLessThanOrEqual(1.5);
      };
      pinned(before.dock!, before.cluster!);

      // 1. Resize the ACTION CLUSTER only (no window resize): the
      //    ResizeObserver on [data-action-cluster] must re-anchor the dock.
      await page.evaluate(() => {
        const c = document.querySelector('[data-action-cluster]') as HTMLElement;
        c.style.height = '120px';
      });
      await page.waitForFunction(
        (prevBottom) => {
          const d = document.querySelector('[data-prompt-dock]')!.getBoundingClientRect();
          return Math.abs(d.bottom - (prevBottom as number)) > 30;
        },
        before.dock!.bottom,
        { timeout: 5000 },
      );
      const afterRO = await page.evaluate<{ dock: Rect | null; cluster: Rect | null }>(rects);
      pinned(afterRO.dock!, afterRO.cluster!);

      // 2. Resize the WINDOW: the resize listener must re-anchor too.
      await page.setViewportSize({ width: 900, height: 820 });
      await page.waitForFunction(
        (prevRight) => {
          const d = document.querySelector('[data-prompt-dock]')!.getBoundingClientRect();
          return Math.abs(d.right - (prevRight as number)) > 30;
        },
        afterRO.dock!.right,
        { timeout: 5000 },
      );
      const afterResize = await page.evaluate<{ dock: Rect | null; cluster: Rect | null }>(rects);
      pinned(afterResize.dock!, afterResize.cluster!);
    } finally {
      await page.close();
    }
  });
});

/**
 * The auto-yield rule's coverage measurement, run in the page: the dock's own
 * interactive rects vs the board's option carriers. `anyCovered` is true when
 * a carrier is FULLY inside one control, the exact condition dockYields uses.
 */
const coverage = `(() => {
  const box = (r) => ({ left: r.left, right: r.right, top: r.top, bottom: r.bottom });
  const controls = [...document.querySelectorAll('[data-prompt-dock] .tool, [data-prompt-dock] .body button, [data-prompt-dock] .body input, [data-prompt-dock] .body select, [data-prompt-dock] .body textarea')].map((e) => e.getBoundingClientRect());
  const board = document.querySelector('.board');
  const carriers = board ? [...board.querySelectorAll('[data-options], [data-single-action], button.badge[aria-haspopup="menu"]')].map((e) => e.getBoundingClientRect()) : [];
  const inside = (inner, outer) => inner.left >= outer.left && inner.right <= outer.right && inner.top >= outer.top && inner.bottom <= outer.bottom;
  return {
    controls: controls.length,
    carriers: carriers.map((r) => ({ left: r.left, right: r.right, top: r.top, bottom: r.bottom })),
    anyCovered: carriers.some((c) => controls.some((o) => inside(c, o))),
  };
})()`;

describe('PromptDock near-table auto-yield (promptdock2)', () => {
  it('yields to the rail when its option rows would fully cover a board option carrier', { timeout: 60_000 }, async () => {
    const page = await browser.newPage();
    await page.setViewportSize({ width: 1000, height: 700 });
    await page.goto(`${url}src/components/PromptDock.geometry.html?ask=long`, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('[data-prompt-dock][data-placement="table"]', { timeout: 5000 });
    await page.waitForSelector('[data-prompt-dock] .row', { timeout: 5000 });
    try {
      // The repro needs a tall dock: assert the long ask actually rendered
      // before trusting anything below.
      const rows = await page.locator('[data-prompt-dock] .row').count();
      expect(rows).toBeGreaterThanOrEqual(7);

      // Pin the board option carrier (the pile badge) entirely inside one of
      // the dock's own option rows — the measured ui24 overlap.
      await page.evaluate(() => {
        const row = document.querySelectorAll('[data-prompt-dock] .row')[2] as HTMLElement;
        const r = row.getBoundingClientRect();
        const badge = document.querySelector('[data-pile-badge]') as HTMLElement;
        badge.style.position = 'fixed';
        badge.style.width = '28px';
        badge.style.height = '22px';
        badge.style.left = `${Math.round(r.left + r.width / 2 - 14)}px`;
        badge.style.top = `${Math.round(r.top + r.height / 2 - 11)}px`;
      });

      // Precondition, measured: the badge lies FULLY inside a dock control.
      // Without this the test could pass on a dock that covers nothing.
      const before = await page.evaluate<{ controls: number; anyCovered: boolean }>(coverage);
      expect(before.controls).toBeGreaterThan(0);
      expect(before.anyCovered).toBe(true);

      // The component re-measures on a window resize (its existing trigger);
      // the dock must now yield out of the board's way, unprompted.
      await page.evaluate(() => window.dispatchEvent(new Event('resize')));
      await page.waitForSelector('[data-prompt-dock][data-placement="rail"]', { timeout: 5000 });

      // After yielding, no board option carrier is fully covered, and a real
      // click (Playwright's hit-target check) reaches the badge.
      const after = await page.evaluate<{ anyCovered: boolean }>(coverage);
      expect(after.anyCovered).toBe(false);
      await page.click('[data-pile-badge]', { timeout: 5000 });
      await page.waitForFunction(() => document.body.getAttribute('data-badge-clicked') === '1', undefined, { timeout: 5000 });
    } finally {
      await page.close();
    }
  });
});
