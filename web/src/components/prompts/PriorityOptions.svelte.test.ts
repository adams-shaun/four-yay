import { type Browser, type Page } from 'playwright';
import { beforeAll, describe, expect, it } from 'vitest';
import { browserURL, sharedBrowser } from '../../test/browser';

/**
 * PriorityOptions.svelte.test.ts is the mounted proof for the ManaBrew
 * plan-less cast affordance (fb-20260929T075330Z): the wire offers a
 * `pay-<id>` cast with no payment plan, and PriorityOptions must render a
 * cast button for it and POST the announce when that rendered button is
 * CLICKED — not merely when submitAnnounce is called directly. The Auto-pay
 * ON case is pinned too: there is no route for a plan-less action, so no CAST
 * affordance may render (a dead button would be worse than none).
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

  it('with Auto-pay ON, offers no cast affordance for the plan-less action', async () => {
    const page = await open('manabrew-autopay');
    // The plan-less payment action is present on the decision but has no route
    // under Auto-pay ON, so neither the announce button nor a plan button may
    // appear — only the "unavailable" hint.
    expect(await page.locator('[data-announce]').count()).toBe(0);
    expect(await page.locator('[data-payment-plan]').count()).toBe(0);
    await expect.poll(() => page.locator('[data-payment-actions]').count()).toBe(1);
    await expect.poll(() => page.getByText('Suggested payment is unavailable').count()).toBe(1);
    expect(await posts(page)).toEqual([]);
    await page.close();
  });
});
