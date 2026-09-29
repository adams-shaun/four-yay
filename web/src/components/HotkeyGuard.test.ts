import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * HotkeyGuard.test.ts is the mounted half of the modal-picker guard (prio3
 * review r2 through sol2): the pure grammar test passes a synthetic picker
 * predicate, so it cannot catch a modal surface that markup failed to expose.
 * Here every transient picker kind found by the structural audit is mounted
 * over the real HotButtonStrip/SeatPanel wiring: OptionPicker's radial and
 * long-list branches, HandFan's separate menu, and PileModal's dialog. The
 * feedback dialog is covered too. Keyboard presses target the live document,
 * with focus deliberately moved off native controls where that matters.
 */

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

type Posts = { seq: number; player: number; choices: number[] }[];
type UndoPosts = { url: string; authorization: string }[];

const posts = (page: import('playwright').Page): Promise<Posts> =>
  page.evaluate(() => (window as unknown as { __posts: Posts }).__posts);

const playMode = (page: import('playwright').Page): Promise<string> =>
  page.evaluate(() => document.querySelector('[data-play-mode]')?.getAttribute('data-play-mode') ?? '');

const undoPosts = (page: import('playwright').Page): Promise<UndoPosts> =>
  page.evaluate(() => (window as unknown as { __undos: UndoPosts }).__undos);

describe('the hotkey guard against an open modal — mounted', () => {
  it('Space passes after a pointer click on the real hot-strip PASS button', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);
    const pass = page.locator('#fixture [data-hot-tab="pass"]');
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
    expect(await page.evaluate(() => (window as unknown as { __passBlurCalled: boolean }).__passBlurCalled)).toBe(true);
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1);
    const first = await posts(page);
    expect(first).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    expect(await pass.evaluate((el) => document.activeElement === el)).toBe(false);

    // The previous decision was answered; offer a fresh one so a second post
    // actually tests focus management rather than the seat's postedSeq latch.
    await page.evaluate(() => (window as unknown as { __newDecision: () => void }).__newDecision());
    await expect.poll(() => pass.isEnabled()).toBe(true);
    await page.keyboard.press('Space');
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 2);
    const after = await posts(page);
    expect(after.length).toBeGreaterThan(first.length);
    expect(after).toEqual([...first, { seq: 8, player: 0, choices: [9] }]);
    await page.close();
  });

  it('Space passes after a pointer click on the ACTIONS tab', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);
    const actions = page.locator('#fixture [data-hot-tab="actions"]');
    await actions.evaluate((el) => el.addEventListener('click', () => {
      (window as unknown as { __focusedAtClick: boolean }).__focusedAtClick = document.activeElement === el;
    }, { capture: true, once: true }));
    await actions.click();
    expect(await page.evaluate(() => (window as unknown as { __focusedAtClick: boolean }).__focusedAtClick)).toBe(true);
    const first = await posts(page);
    expect(first).toEqual([]);
    await page.keyboard.press('Space');
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1, null, { timeout: 2000 });
    const after = await posts(page);
    expect(after.length).toBeGreaterThan(first.length);
    expect(after).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });

  it('keyboard Enter still natively activates a focused strip button without blurring it', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);
    const actions = page.locator('#fixture [data-hot-tab="actions"]');
    await actions.focus();
    expect(await actions.evaluate((el) => document.activeElement === el)).toBe(true);
    await actions.evaluate((el) => el.addEventListener('click', (e) => {
      (window as unknown as { __keyboardClickDetail: number }).__keyboardClickDetail = (e as MouseEvent).detail;
    }, { once: true }));
    await page.keyboard.press('Enter');
    expect(await page.evaluate(() => (window as unknown as { __keyboardClickDetail: number }).__keyboardClickDetail)).toBe(0);
    expect(await actions.getAttribute('aria-expanded')).toBe('true');
    expect(await actions.evaluate((el) => document.activeElement === el)).toBe(true);
    await page.close();
  });

  it('the real UNDO button invokes postUndo, while the multi-human button cannot', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);

    const enabled = page.locator('#fixture [data-undo]');
    expect(await enabled.isEnabled()).toBe(true);
    await enabled.click();
    await page.waitForFunction(() => (window as unknown as { __undos: UndoPosts }).__undos.length === 1);
    expect(await undoPosts(page)).toEqual([{
      url: '/api/tables/fx/matches/1/undo',
      authorization: 'Bearer tok',
    }]);

    const disabled = page.locator('#undo-disabled [data-undo]');
    expect(await disabled.isDisabled()).toBe(true);
    // dispatchEvent deliberately bypasses the browser's disabled-button
    // suppression, proving the component handler's undoAllowed guard too.
    await disabled.dispatchEvent('click');
    expect(await undoPosts(page)).toHaveLength(1);
    await page.close();
  });

  it('the mounted live strip exposes the undo pause and its chip resumes the machine', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);

    await page.evaluate(() => (window as unknown as { __state: { rewind: () => void } }).__state.rewind());
    const chip = page.locator('#fixture [data-auto-status]');
    await expect.poll(() => chip.getAttribute('data-play-mode')).toBe('paused');
    expect(await chip.textContent()).toContain('AUTO');
    expect(await chip.getAttribute('aria-label')).toContain('Press the Auto switch (or apply a preset)');

    // This is a real click through HotButtonStrip's handler and the shared
    // pressAuto path, not a direct state call. The chip disappears only when
    // the session-scoped brake has actually lifted.
    await chip.click();
    await expect.poll(() => page.locator('#fixture [data-play-mode]').getAttribute('data-play-mode')).not.toBe('paused');
    expect(await page.evaluate(() => (window as unknown as { __state: { machinePaused: boolean } }).__state.machinePaused)).toBe(false);
    await page.close();
  });

  it('Space and Enter stay behind PileModal; Escape closes it and preserves End Turn', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);

    await page.evaluate(() => (window as unknown as { __openPile: () => void }).__openPile());
    await page.waitForSelector('[data-pile-modal]');
    // The review's exact scenario: focus rests on the non-interactive dialog.
    await page.waitForFunction(() => document.activeElement?.getAttribute('role') === 'dialog');

    await page.keyboard.press('Space');
    await page.keyboard.press('Enter');
    expect(await posts(page)).toEqual([]);
    expect(await playMode(page)).toBe('custom'); // no run armed under the modal

    // Arm without a pointer so the open surface stays present. Escape closes
    // PileModal's own layer while both capture listeners still see the dialog
    // and therefore leave the underlying run untouched.
    await page.evaluate(() => (window as unknown as { __armRun: () => void }).__armRun());
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1);
    expect(await playMode(page)).toBe('end-turn');
    await page.keyboard.press('Escape');
    await page.waitForSelector('[data-pile-modal]', { state: 'detached' });
    expect(await posts(page)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    expect(await playMode(page)).toBe('end-turn');
    await page.close();
  });

  it('Escape with the radial picker open closes the picker and the End Turn run survives; the next Escape cancels the run', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);

    // Open the radial FIRST (a pointer on it would cancel a live run — the
    // panel's own, correct pointerdown behaviour), then arm the run
    // programmatically: no pointer is involved in arming.
    await page.locator('#radial .badge').click();
    await page.waitForSelector('[data-option-picker][data-radial-picker]');
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Space');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(50);
    expect(await posts(page)).toEqual([]);
    expect(await playMode(page)).toBe('custom');

    await page.evaluate(() => (window as unknown as { __armRun: () => void }).__armRun());
    await page.waitForFunction(() => document.querySelector('[data-play-mode]')?.getAttribute('data-play-mode') === 'end-turn');
    const before = await posts(page);
    expect(before).toEqual([{ seq: 7, player: 0, choices: [9] }]);

    // The review's break: SeatPanel's own window listener used to cancel the
    // run here, past the grammar's guard. Now the modal closes and the run
    // survives — exactly one Escape per layer.
    await page.keyboard.press('Escape');
    await page.waitForSelector('[data-radial-picker]', { state: 'detached' });
    expect(await posts(page)).toEqual(before); // no further pass was posted
    expect(await playMode(page)).toBe('end-turn');

    // The picker is closed, so Escape is the panic key again.
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => document.querySelector('[data-play-mode]')?.getAttribute('data-play-mode') === 'custom');
    expect(await posts(page)).toEqual(before);
    await page.close();
  });

  it('Space, Enter and Escape stay behind the open seven-option list picker', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);

    await page.locator('#menu .badge').click();
    await page.waitForSelector('.menu-pop[data-option-picker]');
    // Deliberately remove button focus. Otherwise the generic interactive-
    // target guard would hide a missing modal marker — exactly the Safari /
    // programmatic-open failure this regression is meant to expose.
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());

    await page.keyboard.press('Space');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(50);
    expect(await posts(page)).toEqual([]);
    expect(await playMode(page)).toBe('custom');

    // Escape must close only the picker, not the End Turn run beneath it.
    // Arming is programmatic so no pointerdown cancels the run first.
    await page.evaluate(() => (window as unknown as { __armRun: () => void }).__armRun());
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1);
    expect(await playMode(page)).toBe('end-turn');
    await page.keyboard.press('Escape');
    await page.waitForSelector('.menu-pop[data-option-picker]', { state: 'detached' });
    expect(await posts(page)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    expect(await playMode(page)).toBe('end-turn');
    await page.close();
  });

  it('Space and Enter stay behind HandFan role=menu; Escape closes it and preserves End Turn', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);

    await page.locator('#hand .badge').click();
    await page.waitForSelector('#hand [role="menu"]');
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Space');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(50);
    expect(await posts(page)).toEqual([]);
    expect(await playMode(page)).toBe('custom');

    await page.evaluate(() => (window as unknown as { __armRun: () => void }).__armRun());
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1);
    await page.keyboard.press('Escape');
    await page.waitForSelector('#hand [role="menu"]', { state: 'detached' });
    expect(await posts(page)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    expect(await playMode(page)).toBe('end-turn');
    await page.close();
  });

  it('the feedback dialog owns Escape and preserves an underlying End Turn run', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);

    await page.locator('#feedback .feedback-badge').click();
    await page.waitForSelector('#feedback [role="dialog"]');
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Space');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(50);
    expect(await posts(page)).toEqual([]);

    await page.evaluate(() => (window as unknown as { __armRun: () => void }).__armRun());
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1);
    await page.keyboard.press('Escape');
    await page.waitForSelector('#feedback [role="dialog"]', { state: 'detached' });
    expect(await posts(page)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    expect(await playMode(page)).toBe('end-turn');
    await page.close();
  });

  it('modalPickerOpen structurally detects a bare role=menu with no data marker', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);

    const result = await page.evaluate(() => {
      const menu = document.createElement('div');
      menu.setAttribute('role', 'menu');
      document.body.append(menu);
      const open = (window as unknown as { __modalPickerOpen: () => boolean }).__modalPickerOpen();
      menu.remove();
      const closed = (window as unknown as { __modalPickerOpen: () => boolean }).__modalPickerOpen();
      return { open, closed };
    });
    expect(result).toEqual({ open: true, closed: false });
    await page.close();
  });

  it('a held key auto-repeats only pass: a repeated undo chord never posts, a repeated Space still passes', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);
    await page.locator('#fixture [data-undo]').waitFor();
    const press = (init: { key: string; code: string; ctrlKey?: boolean; shiftKey?: boolean; repeat?: boolean }) =>
      page.evaluate((i) => document.body.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...i })), init);

    await press({ key: 'Z', code: 'KeyZ', ctrlKey: true, shiftKey: true, repeat: true });
    await page.waitForTimeout(50);
    expect(await undoPosts(page)).toEqual([]);
    await press({ key: 'Z', code: 'KeyZ', ctrlKey: true, shiftKey: true });
    await page.waitForFunction(() => (window as unknown as { __undos: UndoPosts }).__undos.length === 1);

    const fresh = await browser.newPage();
    await fresh.goto(`${url}src/components/HotkeyGuard.fixture.html`);
    await fresh.locator('#fixture [data-undo]').waitFor();
    await fresh.evaluate(() => document.body.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', code: 'Space', repeat: true, bubbles: true, cancelable: true })));
    await fresh.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1);
    expect(await posts(fresh)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await fresh.close();
    await page.close();
  });

  it('? opens the shortcut sheet and stays open; ? again (focus elsewhere) and a backdrop click close it', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/HotkeyGuard.fixture.html`);
    await page.locator('#fixture [data-undo]').waitFor();
    const sheet = page.locator('[aria-labelledby="keys-title"]');
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Shift+Slash');
    await sheet.first().waitFor();
    await page.waitForTimeout(50);
    expect(await sheet.count()).toBeGreaterThan(0);
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Shift+Slash');
    await expect.poll(() => sheet.count()).toBe(0);

    await page.keyboard.press('Shift+Slash');
    await sheet.first().waitFor();
    await page.locator('[data-keys-backdrop]').first().click({ position: { x: 5, y: 5 } });
    await expect.poll(() => sheet.count()).toBe(0);
    await page.close();
  });
});

