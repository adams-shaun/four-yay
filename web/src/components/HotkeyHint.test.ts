import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * HotkeyHint.test.ts is the mounted half of the suppression cue
 * (fb-20260929T080219Z): the pure suite (hotkeys.ownership.test.ts,
 * hotkeyhint.test.ts) proves the classifier and the store, but only a mounted
 * page proves the REAL HotButtonStrip wiring notes the suppression, renders
 * the chip, records the breadcrumb, and leaves a key the felt owns alone.
 */

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

type Posts = { seq: number; player: number; choices: number[] }[];
type Crumb = { type: string; detail?: Record<string, unknown> };

const posts = (page: import('playwright').Page): Promise<Posts> =>
  page.evaluate(() => (window as unknown as { __posts: Posts }).__posts);

const crumbs = (page: import('playwright').Page): Promise<Crumb[]> =>
  page.evaluate(() => ((window as unknown as { __breadcrumbs: () => { actions: Crumb[] } }).__breadcrumbs()).actions);

const chip = (page: import('playwright').Page) => page.locator('[data-hotkey-hint]');

describe('the hotkey suppression cue — mounted through the real strip', () => {
  it('Space on a keyboard-focused strip button shows the chip and records the breadcrumb, and posts nothing', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyHint.fixture.html`);

    // Keyboard focus, NOT a click: the pointer path releases focus (the
    // sibling ticket), so a click would not reproduce the reported confusion.
    // The ACTIONS tab is a real strip button whose native Space activation
    // opens its panel without posting an intent — so any POST here would be a
    // hotkey leak, and any chip is a genuine suppression (the PASS button's
    // native activation would itself post, which is exactly why the guard
    // must suppress the hotkey there).
    const actions = page.locator('#fixture [data-hot-tab="actions"]');
    await actions.focus();
    // Precondition: focus really is on that button, so the rule reads it.
    expect(await actions.evaluate((el) => document.activeElement === el)).toBe(true);

    await page.keyboard.press('Space');

    const hint = chip(page);
    await hint.waitFor();
    expect(await hint.textContent()).toContain('Space/Enter act on the focused control');
    // Precondition for the no-POST assertion: the pass option IS available
    // (otherwise no POST could happen for any reason and the assertion would
    // be vacuous).
    expect(await page.locator('#fixture [data-hot-tab="pass"]').isEnabled()).toBe(true);
    expect(await posts(page)).toEqual([]);

    const recorded = await crumbs(page);
    const suppressed = recorded.find((c) => c.type === 'hotkey-suppressed');
    expect(suppressed).toBeDefined();
    expect(suppressed?.detail).toEqual({ key: ' ', code: 'Space', reason: 'activatable-space-enter' });
    // The suppression is NOT a successful hotkey: it must not also be logged
    // as one.
    expect(recorded.some((c) => c.type === 'hotkey')).toBe(false);
    await page.close();
  });

  it('Space with focus on the felt passes and shows no chip', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyHint.fixture.html`);

    expect(await page.locator('#fixture [data-hot-tab="pass"]').isEnabled()).toBe(true); // precondition
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    expect(await chip(page).count()).toBe(0); // precondition: the chip is not already showing

    await page.keyboard.press('Space');
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1, null, { timeout: 2000 });
    expect(await posts(page)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    expect(await chip(page).count()).toBe(0);
    const recorded = await crumbs(page);
    expect(recorded.some((c) => c.type === 'hotkey-suppressed')).toBe(false);
    expect(recorded.some((c) => c.type === 'hotkey')).toBe(true);
    await page.close();
  });

  it('a keystroke into a focused text-entry control stays fully silent (no chip, no breadcrumb, no POST)', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyHint.fixture.html`);

    // A real text field the fixture did not ship: the player's typing must go
    // into it and nowhere else. Space in a textbox is text, not a pass.
    await page.evaluate(() => {
      const input = document.createElement('input');
      input.id = 'probe-input';
      document.body.append(input);
      input.focus();
    });
    expect(await page.evaluate(() => document.activeElement?.id)).toBe('probe-input'); // precondition

    await page.keyboard.type('a ');
    await page.waitForTimeout(50);
    expect(await chip(page).count()).toBe(0);
    expect(await posts(page)).toEqual([]);
    expect((await crumbs(page)).some((c) => c.type === 'hotkey-suppressed')).toBe(false);
    await page.close();
  });

  it('Escape on a focused control is silent: it is the panic key, never a suppression', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyHint.fixture.html`);

    const actions = page.locator('#fixture [data-hot-tab="actions"]');
    await actions.focus();
    expect(await actions.evaluate((el) => document.activeElement === el)).toBe(true); // precondition

    await page.keyboard.press('Escape');
    await page.waitForTimeout(50);
    expect(await chip(page).count()).toBe(0);
    expect((await crumbs(page)).some((c) => c.type === 'hotkey-suppressed')).toBe(false);
    await page.close();
  });

  it('a successful hotkey clears a chip that was showing', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyHint.fixture.html`);

    const actions = page.locator('#fixture [data-hot-tab="actions"]');
    await actions.focus();
    expect(await actions.evaluate((el) => document.activeElement === el)).toBe(true);
    await page.keyboard.press('Space');
    await chip(page).waitFor();
    expect(await chip(page).count()).toBe(1); // precondition: the chip is up before the clearing key

    // Blur to the felt and take a real pass: the successful hotkey clears the
    // stale "why didn't it work" cue at once.
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Space');
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1, null, { timeout: 2000 });
    expect(await chip(page).count()).toBe(0);
    await page.close();
  });

  it('the chip auto-hides after its expiry', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyHint.fixture.html`);

    const actions = page.locator('#fixture [data-hot-tab="actions"]');
    await actions.focus();
    await page.keyboard.press('Space');
    await chip(page).waitFor();
    expect(await chip(page).count()).toBe(1); // precondition: there is a chip to expire

    // HINT_MS is 4s; allow headroom for a loaded CI machine.
    await chip(page).waitFor({ state: 'detached', timeout: 7000 });
    expect(await chip(page).count()).toBe(0);
    await page.close();
  }, 12000);
});
