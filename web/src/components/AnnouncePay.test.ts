import { type Browser, type Page } from 'playwright';
import { beforeAll, describe, expect, it } from 'vitest';
import { browserURL, sharedBrowser } from '../test/browser';

/**
 * AnnouncePay.test.ts is the mounted proof of announce-then-pay's client flow
 * (docs/superpowers/specs/2026-09-27-announce-then-pay.md §8): with Auto-pay
 * OFF the hand card's CAST posts the announce selector; the announced window
 * renders as the select-mana panel; the dual land's two abilities are
 * separate buttons; the payable sources are marked and answerable on the
 * battlefield too; and the panel fits a phone-width viewport. Set
 * ANNOUNCE_PAY_SHOTS=<dir> to also write screenshots of both widths.
 */

let browser: Browser;
beforeAll(async () => {
  browser = await sharedBrowser();
});

type FixtureWindow = { __posts: () => unknown[]; __deliver: () => void; __state: () => { active: number | null; kind: string | null } };
const posts = (page: Page) => page.evaluate(() => (window as unknown as FixtureWindow).__posts());
const state = (page: Page) => page.evaluate(() => (window as unknown as FixtureWindow).__state());
const shots = process.env.ANNOUNCE_PAY_SHOTS;

async function open(width: number, height: number): Promise<Page> {
  const page = await browser.newPage({ viewport: { width, height } });
  await page.goto(`${browserURL}src/components/AnnouncePay.fixture.html`);
  await expect.poll(async () => (await state(page)).active).toBe(20);
  return page;
}

async function deliverWindow(page: Page): Promise<void> {
  await page.evaluate(() => (window as unknown as FixtureWindow).__deliver());
  await expect.poll(async () => (await state(page)).active).toBe(21);
  await page.locator('[data-mana-payment]').waitFor();
}

describe('announce then pay, mounted', () => {
  it('CAST with Auto-pay off posts announce; the window picks a dual land colour from the panel', async () => {
    const page = await open(1200, 900);
    const cast = page.locator('[data-payment-card="action-20"]');
    await expect.poll(() => cast.getAttribute('aria-label')).toBe('Cast Grixis Charm Test, then choose mana');
    await cast.click();
    await expect.poll(() => posts(page)).toEqual([{ seq: 20, player: 0, choices: [], announce: { action_id: 'action-20' } }]);
    await deliverWindow(page);
    if (shots) await page.screenshot({ path: `${shots}/announce-pay-desktop.png` });
    const sea = page.locator('[data-mana-payment] [data-mana-source="43"] button');
    expect(await sea.count()).toBe(2);
    await page.getByRole('button', { name: 'Tap Underground Sea for U' }).click();
    await expect.poll(async () => (await posts(page)).at(-1)).toEqual({ seq: 21, player: 0, choices: [2] });
    await page.close();
  });

  it('marks the payable sources on the battlefield, where a tile answers the window', async () => {
    const page = await open(1200, 900);
    await page.locator('[data-payment-card="action-20"]').click();
    await deliverWindow(page);
    // A single-ability Swamp tile answers with one click on its own badge.
    const swampTile = page.locator('[data-obj="41"] [data-single-action]').first();
    await swampTile.waitFor();
    await swampTile.click();
    await expect.poll(async () => (await posts(page)).at(-1)).toEqual({ seq: 21, player: 0, choices: [0] });
    await page.close();
  });

  it('fits a phone-width viewport without horizontal scroll', async () => {
    const page = await open(390, 844);
    await page.locator('[data-payment-card="action-20"]').click();
    await deliverWindow(page);
    if (shots) await page.screenshot({ path: `${shots}/announce-pay-phone.png` });
    const overflow = await page.locator('[data-mana-payment]').evaluate((el) => {
      const r = el.getBoundingClientRect();
      return { right: r.right, left: r.left, vw: window.innerWidth, scroll: el.scrollWidth - el.clientWidth };
    });
    expect(overflow.left).toBeGreaterThanOrEqual(0);
    expect(overflow.right).toBeLessThanOrEqual(overflow.vw);
    expect(overflow.scroll).toBeLessThanOrEqual(0);
    await page.close();
  });
});
