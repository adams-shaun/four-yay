import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

const posts = (page: import('playwright').Page): Promise<{ seq: number; player: number; choices: number[] }[]> =>
  page.evaluate(() => (window as unknown as { __posts: { seq: number; player: number; choices: number[] }[] }).__posts);

beforeAll(async () => { await sharedBrowser(); });

describe('ActionCluster pointer focus', () => {
  it('Space passes on a new decision after clicking the gilt PASS button', async () => {
    const browser = await sharedBrowser();
    const page = await browser.newPage();
    await page.goto(`${browserURL}src/components/HotkeyGuard.fixture.html`);
    const pass = page.locator('#cluster [data-pass-action]');
    expect(await pass.isEnabled()).toBe(true);
    await pass.evaluate((el) => {
      el.addEventListener('click', () => {
        (window as unknown as { __focusedAtClick: boolean }).__focusedAtClick = document.activeElement === el;
      }, { capture: true, once: true });
      const original = HTMLElement.prototype.blur;
      HTMLElement.prototype.blur = function () {
        if (this === el) (window as unknown as { __passBlurCalled: boolean }).__passBlurCalled = true;
        original.call(this);
      };
    });
    await pass.click();
    expect(await page.evaluate(() => (window as unknown as { __focusedAtClick: boolean }).__focusedAtClick)).toBe(true);
    await page.waitForFunction(() => (window as unknown as { __posts: unknown[] }).__posts.length === 1);
    const first = await posts(page);
    expect(first).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    // Behavioural assertion first: this is the pass that proves the hotkey
    // survived the click. Measured caveat: the busy post disables this button
    // and the browser blurs it to body before the next decision, so this
    // assertion also passes on the unfixed parent; the secondary-Undo test
    // below (focus retained) is the behavioural revert proof.
    await page.evaluate(() => (window as unknown as { __newDecision: () => void }).__newDecision());
    await expect.poll(() => pass.isEnabled()).toBe(true);
    await page.keyboard.press('Space');
    await page.waitForFunction(() => (window as unknown as { __posts: unknown[] }).__posts.length === 2, null, { timeout: 2000 });
    const after = await posts(page);
    expect(after.length).toBeGreaterThan(first.length);
    expect(after).toEqual([...first, { seq: 8, player: 0, choices: [9] }]);
    // Preconditions, now proven.
    expect(await page.evaluate(() => (window as unknown as { __passBlurCalled: boolean }).__passBlurCalled)).toBe(true);
    expect(await pass.evaluate((el) => document.activeElement === el)).toBe(false);
    await page.close();
  });

  it('a pointer click on secondary Undo does not trap Space', async () => {
    const browser = await sharedBrowser();
    const page = await browser.newPage();
    await page.goto(`${browserURL}src/components/HotkeyGuard.fixture.html`);
    const undo = page.locator('#cluster [data-cluster-undo]');
    expect(await undo.isEnabled()).toBe(true);
    await undo.evaluate((el) => el.addEventListener('click', () => {
      (window as unknown as { __focusedAtClick: boolean }).__focusedAtClick = document.activeElement === el;
    }, { capture: true, once: true }));
    await undo.click();
    expect(await page.evaluate(() => (window as unknown as { __focusedAtClick: boolean }).__focusedAtClick)).toBe(true);
    await page.waitForFunction(() => (window as unknown as { __undos: unknown[] }).__undos.length === 1);
    const first = await posts(page);
    expect(first).toEqual([]);
    await page.keyboard.press('Space');
    await page.waitForFunction(() => (window as unknown as { __posts: unknown[] }).__posts.length === 1, null, { timeout: 2000 });
    const after = await posts(page);
    expect(after.length).toBeGreaterThan(first.length);
    expect(after).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });
});
