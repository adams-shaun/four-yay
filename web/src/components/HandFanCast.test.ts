import { type Browser, type Page } from 'playwright';
import { beforeAll, describe, expect, it } from 'vitest';
import { browserURL, sharedBrowser } from '../test/browser';

/**
 * HandFanCast.test.ts is the mounted regression proof for the ManaBrew
 * plan-less cast on the hand fan (fb-20260929T075330Z + its Auto-pay-ON
 * follow-up). In BOTH Auto-pay modes the fan must render the CAST button for
 * a plan-less payment action and the click must reach onCastPayment: castable
 * Actions offers the action in both (castAction falls back to announce-then-
 * pay for it), so a missing button would hide the only route to the cast.
 */

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

type FixtureWindow = { __casts: () => unknown[] };
const casts = (page: Page) => page.evaluate(() => (window as unknown as FixtureWindow).__casts());

async function open(surface: 'autopay-off' | 'autopay-on'): Promise<Page> {
  const page = await browser.newPage({ viewport: { width: 1100, height: 700 } });
  await page.goto(`${url}src/components/HandFanCast.fixture.html?case=${surface}`);
  return page;
}

describe('HandFan plan-less ManaBrew cast', () => {
  it('with Auto-pay OFF renders the CAST shortcut and the click reaches onCastPayment', async () => {
    const page = await open('autopay-off');
    const button = page.locator('[data-payment-card="pay-4ad785b7"]');
    await expect.poll(() => button.count()).toBe(1);
    expect(await casts(page)).toEqual([]);
    await button.click();
    await expect.poll(() => casts(page)).toEqual(['pay-4ad785b7']);
    await page.close();
  });

  it('with Auto-pay ON renders the CAST shortcut for the plan-less action and the click reaches onCastPayment', async () => {
    const page = await open('autopay-on');
    const button = page.locator('[data-payment-card="pay-4ad785b7"]');
    await expect.poll(() => button.count()).toBe(1);
    expect(await casts(page)).toEqual([]);
    await button.click();
    await expect.poll(() => casts(page)).toEqual(['pay-4ad785b7']);
    await page.close();
  });
});
