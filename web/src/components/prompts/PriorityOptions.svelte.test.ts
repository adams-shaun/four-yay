import { type Browser, type Page } from 'playwright';
import { beforeAll, describe, expect, it } from 'vitest';
import { browserURL, sharedBrowser } from '../../test/browser';

/**
 * PriorityOptions.svelte.test.ts is the mounted proof for the ManaBrew
 * plan-less cast affordance (fb-20260929T075330Z + its Auto-pay-ON follow-up):
 * the wire offers a `pay-<id>` cast with no payment plan, and PriorityOptions
 * must render a cast affordance for it and POST the announce when that
 * rendered control is CLICKED — not merely when submitAnnounce is called
 * directly — in BOTH Auto-pay modes. With Auto-pay ON the affordance is the
 * announce fallback inside the payment block (the action has no plan to
 * submit); with Auto-pay OFF it is the announce list's button.
 */

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

type FixtureWindow = { __posts: () => unknown[]; __state: () => { pending: number | null; postedSeq: number | null; paymentPlanCounts: number[] } };
const posts = (page: Page) => page.evaluate(() => (window as unknown as FixtureWindow).__posts());
const state = (page: Page) => page.evaluate(() => (window as unknown as FixtureWindow).__state());

async function open(surface: 'manabrew' | 'manabrew-autopay'): Promise<Page> {
  const page = await browser.newPage({ viewport: { width: 900, height: 700 } });
  await page.goto(`${url}src/components/prompts/PriorityOptions.fixture.html?case=${surface}`);
  // Preconditions: the panel adopted seq 109, and its sole offered payment
  // action is plan-less (the ManaBrew wire shape under test).
  await expect.poll(async () => (await state(page)).pending).toBe(109);
  await expect.poll(async () => (await state(page)).paymentPlanCounts).toEqual([0]);
  return page;
}

describe('PriorityOptions ManaBrew cast', () => {
  it('renders the plan-less cast and the click posts the announce with empty choices', async () => {
    const page = await open('manabrew');
    const button = page.locator('[data-announce="pay-4ad785b7"]');
    await expect.poll(() => button.count()).toBe(1);
    // Precondition: nothing has been posted before the click.
    expect(await posts(page)).toEqual([]);
    await button.click();
    await expect.poll(() => posts(page)).toHaveLength(1);
    expect((await posts(page))[0]).toEqual({ seq: 109, player: 0, choices: [], announce: { action_id: 'pay-4ad785b7' } });
    await page.close();
  });

  it('with Auto-pay ON, the plan-less action renders the announce fallback and the click posts it', async () => {
    const page = await open('manabrew-autopay');
    const button = page.locator('[data-announce="pay-4ad785b7"]');
    await expect.poll(() => button.count()).toBe(1);
    // Exactly one announce control: the payment block's fallback button. The
    // Auto-pay-ON announce list is empty (announceActions stays gated on
    // !autoPayMana), so nothing else may carry the same data-announce id.
    expect(await page.locator('[data-announce]').count()).toBe(1);
    expect(await page.locator('[data-payment-plan]').count()).toBe(0);
    // Precondition: nothing has been posted before the click.
    expect(await posts(page)).toEqual([]);
    await button.click();
    await expect.poll(() => posts(page)).toHaveLength(1);
    expect((await posts(page))[0]).toEqual({ seq: 109, player: 0, choices: [], announce: { action_id: 'pay-4ad785b7' } });
    await page.close();
  });
});
