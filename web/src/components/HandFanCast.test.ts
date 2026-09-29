import { type Browser, type Page } from 'playwright';
import { beforeAll, describe, expect, it } from 'vitest';
import { browserURL, sharedBrowser } from '../test/browser';

/**
 * HandFanCast.test.ts is the mounted regression proof for the ManaBrew
 * plan-less cast on the hand fan (fb-20260929T075330Z, review finding 1): a
 * plan-less payment action has no route under Auto-pay ON (castAction submits
 * the suggested plan and returns when there is none), so the fan must NOT
 * render a CAST button for it — a rendered-but-inert control is a regression.
 * With Auto-pay OFF it IS reachable (announce-then-pay), so the button must
 * exist and the click must reach onCastPayment.
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

  it('with Auto-pay ON renders no CAST shortcut for the plan-less action', async () => {
    const page = await open('autopay-on');
    // Precondition: the hand card is present, so a missing CAST button is the
    // gate and not a missing fan.
    await expect.poll(() => page.locator('[data-obj="54"]').count()).toBe(1);
    expect(await page.locator('[data-payment-card]').count()).toBe(0);
    expect(await casts(page)).toEqual([]);
    await page.close();
  });
});
