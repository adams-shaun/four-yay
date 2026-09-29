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
    await pass.click();
    await page.waitForFunction(() => (window as unknown as { __posts: unknown[] }).__posts.length === 1);
    expect(await posts(page)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.evaluate(() => (window as unknown as { __newDecision: () => void }).__newDecision());
    await expect.poll(() => pass.isEnabled()).toBe(true);
    await page.evaluate(() => {
      const w = window as unknown as { __state: { passClick: () => void }; __restorePass: () => void };
      const original = w.__state.passClick.bind(w.__state);
      w.__state.passClick = () => {}; // preserve this fresh decision for the Space hotkey
      w.__restorePass = () => { w.__state.passClick = original; };
    });
    await pass.evaluate((el) => {
      el.addEventListener('click', () => {
        (window as unknown as { __focusedAtClick: boolean }).__focusedAtClick = document.activeElement === el;
      }, { capture: true, once: true });
      // A focused button also gets native Space activation. Suppress it so
      // only the global hotkey can produce the post under test.
      el.addEventListener('click', (e) => {
        if ((e as MouseEvent).detail === 0) {
          e.preventDefault();
          e.stopImmediatePropagation();
        }
      }, true);
      const original = HTMLElement.prototype.blur;
      HTMLElement.prototype.blur = function () {
        if (this === el) (window as unknown as { __passBlurCalled: boolean }).__passBlurCalled = true;
        original.call(this);
      };
    });
    await pass.click();
    expect(await page.evaluate(() => (window as unknown as { __focusedAtClick: boolean }).__focusedAtClick)).toBe(true);
    expect(await pass.evaluate((el) => document.activeElement === el)).toBe(false);
    expect(await posts(page)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.evaluate(() => (window as unknown as { __restorePass: () => void }).__restorePass());
    await page.keyboard.press('Space');
    await page.waitForFunction(() => (window as unknown as { __posts: unknown[] }).__posts.length === 2, null, { timeout: 2000 });
    const after = await posts(page);
    expect(after.length).toBeGreaterThan(1);
    expect(after).toEqual([
      { seq: 7, player: 0, choices: [9] },
      { seq: 8, player: 0, choices: [9] },
    ]);
    expect(await page.evaluate(() => (window as unknown as { __passBlurCalled: boolean }).__passBlurCalled)).toBe(true);
    await page.close();
  });

  it('keyboard Enter on secondary Undo keeps native activation and focus', async () => {
    const browser = await sharedBrowser();
    const page = await browser.newPage();
    await page.goto(`${browserURL}src/components/HotkeyGuard.fixture.html`);
    const undo = page.locator('#cluster [data-cluster-undo]');
    expect(await undo.isEnabled()).toBe(true);
    await undo.focus();
    expect(await undo.evaluate((el) => document.activeElement === el)).toBe(true);
    await undo.evaluate((el) => el.addEventListener('click', (e) => {
      (window as unknown as { __keyboardClickDetail: number }).__keyboardClickDetail = (e as MouseEvent).detail;
    }, { once: true }));
    await page.keyboard.press('Enter');
    expect(await page.evaluate(() => (window as unknown as { __keyboardClickDetail: number }).__keyboardClickDetail)).toBe(0);
    expect(await page.evaluate(() => (window as unknown as { __undos: unknown[] }).__undos.length)).toBe(1);
    expect(await undo.evaluate((el) => document.activeElement === el)).toBe(true);
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
