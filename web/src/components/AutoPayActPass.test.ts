import { type Browser, type Locator, type Page } from 'playwright';
import { beforeAll, describe, expect, it } from 'vitest';
import { browserURL, sharedBrowser } from '../test/browser';

/**
 * AutoPayActPass.test.ts is the mounted proof for aph-web-autopay-policy's
 * first half (spec §8: "Auto-pay changes which witness an explicit cast uses;
 * it does not otherwise change Auto/Manual policy"). A planned cast posts
 * choices=[] plus a payment selection; it must arm pass-after-acting exactly
 * as the same cast's legacy option does, and a Ctrl-held click (hold
 * priority) must skip the arming — from every UI entry point, clicked for
 * real: the seat-panel list's plan button, the same list inside the hot
 * strip's ACTIONS drop, and the hand card's CAST shortcut (wired as
 * Table.svelte wires it). The legacy-cast-click entry point and the rejected
 * post are pinned at the state level (seatpanel.payment.test.ts).
 */

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

type Surface = 'panel' | 'strip' | 'hand';
type FixtureState = { active: number | null; postedSeq: number | null; actPassed: number; busy: boolean };
type FixtureWindow = { __posts: () => unknown[]; __deliver: (seq: number) => void; __state: () => FixtureState };

const posts = (page: Page) => page.evaluate(() => (window as unknown as FixtureWindow).__posts());
const state = (page: Page) => page.evaluate(() => (window as unknown as FixtureWindow).__state());
const deliver = (page: Page, seq: number) => page.evaluate((s) => (window as unknown as FixtureWindow).__deliver(s), seq);

/** planButton finds the entry point's control for decision 17's plan, opening the ACTIONS drop first where the strip keeps it. */
async function planButton(page: Page, surface: Surface): Promise<Locator> {
  switch (surface) {
    case 'panel':
      return page.locator('[data-seat-panel] [data-payment-plan="plan-17-a"]');
    case 'strip':
      await page.locator('[data-hot-tab="actions"]').click();
      return page.locator('[data-hot-panel="actions"] [data-payment-plan="plan-17-a"]');
    case 'hand':
      return page.locator('[data-payment-card="action-17"]');
  }
}

async function open(surface: Surface): Promise<Page> {
  const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });
  await page.goto(`${url}src/components/AutoPayActPass.fixture.html?surface=${surface}`);
  await expect.poll(async () => (await state(page)).active).toBe(17);
  return page;
}

const plannedPost = { seq: 17, player: 0, choices: [], payment: { action_id: 'action-17', plan: { id: 'plan-17-a' } } };
const SURFACES = ['panel', 'strip', 'hand'] as const;

describe('a planned cast arms pass-after-acting from every UI entry point (spec §8)', () => {
  it.each(SURFACES)('%s: a click posts the plan, and the next priority window is machine-passed exactly once', async (surface) => {
    const page = await open(surface);
    await (await planButton(page, surface)).click();
    await expect.poll(() => posts(page)).toHaveLength(1);
    expect((await posts(page))[0]).toMatchObject(plannedPost);
    expect((await posts(page))[0]).not.toHaveProperty('rest');

    await deliver(page, 18);
    await expect.poll(() => posts(page)).toHaveLength(2);
    // The machine pass answers seq 18 with its PASS option's own wire index.
    expect((await posts(page))[1]).toEqual({ seq: 18, player: 0, choices: [8] });
    expect((await state(page)).actPassed).toBe(1);
    await page.close();
  });

  it.each(SURFACES)('%s: a Ctrl-held click posts the plan but holds priority — the next window is the player\'s', async (surface) => {
    const page = await open(surface);
    await (await planButton(page, surface)).click({ modifiers: ['Control'] });
    await expect.poll(() => posts(page)).toHaveLength(1);
    expect((await posts(page))[0]).toMatchObject(plannedPost);

    await deliver(page, 18);
    // Adoption and the autopilot pass run in the same flush, and the fetch
    // stub records a post synchronously: once 18 is the active ask, a machine
    // pass would already be on the list.
    await expect.poll(async () => (await state(page)).active).toBe(18);
    expect(await posts(page)).toHaveLength(1);
    expect((await state(page)).actPassed).toBe(0);
    await page.close();
  });
});
