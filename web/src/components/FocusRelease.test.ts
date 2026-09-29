import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * FocusRelease.test.ts is the mounted half of hotkey-focus2: a pointer click
 * on any focusable table control outside the modal pickers must not leave
 * that control owning the next Space/Enter. One representative per structural
 * family F1–F7 is mounted beside the REAL hot wiring (HotButtonStrip +
 * SeatPanelState), so each test can click with the pointer, then observe —
 * through the live document — that the next Space reaches the table hotkey
 * (a pass intent POST) instead of re-activating the control. The
 * keyboard-native contract is asserted too: Enter/Space on a FOCUSED control
 * still activates it (click detail 0) and keeps focus there.
 */

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

type Posts = { seq: number; player: number; choices: number[] }[];

const posts = (page: import('playwright').Page): Promise<Posts> =>
  page.evaluate(() => (window as unknown as { __posts: Posts }).__posts);

/** Waits for at least one pass POST to appear and returns it. */
async function nextPassPost(page: import('playwright').Page, before: number): Promise<Posts> {
  await page.waitForFunction(
    (n) => (window as unknown as { __posts: Posts }).__posts.length > n,
    before,
    { timeout: 2000 },
  );
  return posts(page);
}

const activeIs = (page: import('playwright').Page, sel: string): Promise<boolean> =>
  page.evaluate(
    (s) => document.activeElement?.matches(s) ?? false,
    sel,
  );

describe('a pointer click on every focusable table family releases focus (hotkey-focus2)', () => {
  it('F1: a pointer click on the CentreStrip layout pill opens the drawer and leaves Space to the pass hotkey', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const pill = page.locator('#centre [data-layout-pill]');
    await pill.click();
    // Precondition: the click did its job — the drawer is open (role=region,
    // NOT a modal-picker surface, so hotkeys still fire around it).
    expect(await pill.getAttribute('aria-expanded')).toBe('true');
    // The fix: focus is off the pill, so Space is a pass, not a re-toggle.
    expect(await activeIs(page, '[data-layout-pill]')).toBe(false);
    expect(await activeIs(page, '[data-layout-done]')).toBe(false);
    await page.keyboard.press('Space');
    const after = await nextPassPost(page, 0);
    expect(after).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });

  it('F1: the flow pill opens game options through onOpenFlow and releases focus', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const flow = page.locator('#centre [data-flow-pill]');
    // force: the pill can sit under overlapping chrome in this bare-bones
    // fixture page; the test only needs the click delivered.
    await flow.click({ force: true });
    expect(await page.evaluate(() => (window as unknown as { __flowOpened: boolean }).__flowOpened)).toBe(true);
    expect(await activeIs(page, '[data-flow-pill]')).toBe(false);
    await page.keyboard.press('Space');
    expect(await nextPassPost(page, 0)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });

  it('F1: keyboard Enter on the focused layout pill keeps focus and toggles the drawer (native activation preserved)', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const pill = page.locator('#centre [data-layout-pill]');
    await pill.focus();
    expect(await activeIs(page, '[data-layout-pill]')).toBe(true);
    await pill.evaluate((el) => el.addEventListener('click', (e) => {
      (window as unknown as { __kbDetail: number }).__kbDetail = (e as MouseEvent).detail;
    }, { once: true }));
    await page.keyboard.press('Enter');
    // Native activation: detail 0, the drawer opened, focus KEPT.
    expect(await page.evaluate(() => (window as unknown as { __kbDetail: number }).__kbDetail)).toBe(0);
    expect(await pill.getAttribute('aria-expanded')).toBe('true');
    expect(await activeIs(page, '[data-layout-pill]')).toBe(true);
    // And the hotkey did NOT also fire: Enter on a focused control is the
    // control's, so no pass intent was posted.
    expect(await posts(page)).toEqual([]);
    await page.close();
  });

  it('F2: a pointer click on the seat panel Skip-empty switch toggles it, keeps the strip open, and leaves Space to the hotkey', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    // Open the ACTIONS panel so the switch is reachable.
    await page.locator('#strip [data-hot-tab="actions"]').click();
    const skip = page.locator('#strip [data-skip-toggle]');
    expect(await skip.getAttribute('aria-checked')).toBe('false');
    await skip.click();
    // Precondition: the switch did its job.
    expect(await skip.getAttribute('aria-checked')).toBe('true');
    // The fix: focus released...
    expect(await activeIs(page, '[data-skip-toggle]')).toBe(false);
    // ...without arming the wrapper's close timer (a programmatic blur is a
    // null-relatedTarget focusout, not a departure — e681cd3c5's guard).
    await page.waitForTimeout(350);
    expect(await page.locator('#strip [data-hot-tab="actions"]').getAttribute('aria-expanded')).toBe('true');
    await page.keyboard.press('Space');
    expect(await nextPassPost(page, 0)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });

  it('F3: the single-post answer button posts its own option, and a persistent multi-pick chip releases focus', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    await page.locator('#strip [data-hot-tab="actions"]').click();
    const cast = page.locator('#strip [data-option="5"]');
    expect(await cast.count()).toBe(1);
    await cast.click();
    // Precondition: the click did its job — the option's own index posted.
    await page.waitForFunction(() => (window as unknown as { __posts: Posts }).__posts.length === 1, null, { timeout: 2000 });
    expect((await posts(page))[0].choices).toEqual([5]);
    // The persistent half: a modes chip toggles into picked and STAYS enabled,
    // so the focus contract is not swallowed by the busy-disable cycle.
    const chip = page.locator('#listmount [data-option="31"]');
    expect(await chip.getAttribute('aria-pressed')).toBe('false');
    await chip.click();
    expect(await chip.getAttribute('aria-pressed')).toBe('true');
    // The fix: focus is off the chip, so Space is a pass, not a re-toggle.
    expect(await activeIs(page, '#listmount [data-option="31"]')).toBe(false);
    // The cast answered window seq 7; adopt a fresh answerable window so the
    // Space hotkey below has a live pass to answer (the ListPrompt chip has
    // its own modes window on its own SeatPanelState and is unaffected).
    await page.evaluate(() => (window as unknown as { __newDecision: () => void }).__newDecision());
    await page.keyboard.press('Space');
    const after = await nextPassPost(page, 1);
    expect(after).toEqual([{ seq: 7, player: 0, choices: [5] }, { seq: 8, player: 0, choices: [9] }]);
    // Keyboard-native: Enter on a focused chip keeps focus and toggles again.
    await chip.focus();
    await page.keyboard.press('Enter');
    expect(await activeIs(page, '#listmount [data-option="31"]')).toBe(true);
    expect(await chip.getAttribute('aria-pressed')).toBe('false');
    await page.close();
  });

  it('F4: a pointer click on the mana window Auto-fill picks it and leaves Space to the hotkey', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const autofill = page.locator('#mp [data-mp-autofill]');
    await autofill.click();
    // Precondition: the pick went through the panel's own path.
    expect(await page.evaluate(() => (window as unknown as { __picks: number[] }).__picks)).toEqual([2]);
    expect(await activeIs(page, '[data-mp-autofill]')).toBe(false);
    await page.keyboard.press('Space');
    expect(await nextPassPost(page, 0)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    // A second control in the same family: the Pay button.
    const pay = page.locator('#mp [data-mp-pay]');
    await pay.click();
    expect(await page.evaluate(() => (window as unknown as { __picks: number[] }).__picks)).toEqual([2, 4]);
    expect(await activeIs(page, '[data-mp-pay]')).toBe(false);
    await page.close();
  });

  it('F5: a pointer-opened pile does not get focus back when the modal closes, so Space still reaches the hotkey', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const opener = page.locator('#zones [data-pile="graveyard"]');
    await opener.click();
    // Precondition: the modal opened with the pointer flag recorded.
    expect(await page.locator('[data-pile-modal]').count()).toBe(1);
    expect(await page.evaluate(() => (window as unknown as { __pile: { current: { byPointer: boolean } | null } }).__pile.current?.byPointer)).toBe(true);
    await page.keyboard.press('Escape');
    await expect.poll(() => page.locator('[data-pile-modal]').count()).toBe(0);
    // The fix: the pointer-opened opener does NOT receive focus back.
    expect(await activeIs(page, '[data-pile="graveyard"]')).toBe(false);
    await page.keyboard.press('Space');
    expect(await nextPassPost(page, 0)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });

  it('F5: a keyboard-opened pile (Enter on a focused opener) keeps the accessibility focus return on close', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const opener = page.locator('#zones [data-pile="graveyard"]');
    await opener.focus();
    await page.keyboard.press('Enter');
    await expect.poll(() => page.locator('[data-pile-modal]').count()).toBe(1);
    expect(await page.evaluate(() => (window as unknown as { __pile: { current: { byPointer: boolean } | null } }).__pile.current?.byPointer)).toBe(false);
    await page.keyboard.press('Escape');
    await expect.poll(() => page.locator('[data-pile-modal]').count()).toBe(0);
    // The contract worth keeping: a keyboard opener gets focus back.
    expect(await activeIs(page, '[data-pile="graveyard"]')).toBe(true);
    await page.close();
  });

  it('F6: a pointer click on the DVR pause button records the action and leaves Space to the hotkey', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const pause = page.locator('#dvr [aria-label="pause"]');
    await pause.click();
    // Precondition: the button did its job.
    expect(await page.evaluate(() => (window as unknown as { __dvrActions: unknown[] }).__dvrActions)).toEqual([{ type: 'pause' }]);
    expect(await activeIs(page, '[aria-label="pause"]')).toBe(false);
    await page.keyboard.press('Space');
    expect(await nextPassPost(page, 0)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });

  it('F6: a pointer click on the wire-notice dismiss records it and releases focus', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const dismiss = page.locator('#wire button');
    await dismiss.click();
    expect(await page.evaluate(() => (window as unknown as { __dismissed: boolean }).__dismissed)).toBe(true);
    expect(await activeIs(page, '#wire button')).toBe(false);
    await page.keyboard.press('Space');
    expect(await nextPassPost(page, 0)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });

  it('F7: a pointer click on a settings preset applies it and leaves Space to the hotkey', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const preset = page.locator('#settings [data-preset]').nth(1);
    const before = await page.evaluate(() => (window as unknown as { __state: { settings: { preset: string } } }).__state.settings.preset);
    await preset.click();
    // Precondition: the preset did its job — settings actually changed.
    const after = await page.evaluate(() => (window as unknown as { __state: { settings: { preset: string } } }).__state.settings.preset);
    expect(after).not.toBe(before);
    // The preset may change pacing; re-zero it so the Space assertion below
    // stays synchronous with the key.
    await page.evaluate(() => {
      const w = window as unknown as { __state: { settings: { pacing: { stepMs: number; resolveMs: number } } } };
      w.__state.settings = { ...w.__state.settings, pacing: { stepMs: 0, resolveMs: 0 } };
    });
    expect(await activeIs(page, '#settings [data-preset]')).toBe(false);
    await page.keyboard.press('Space');
    expect(await nextPassPost(page, 0)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });

  it('F7: keyboard Enter on a focused settings button keeps focus there (detail 0)', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const preset = page.locator('#settings [data-preset]').nth(1);
    await preset.focus();
    expect(await activeIs(page, '#settings [data-preset]')).toBe(true);
    await preset.evaluate((el) => el.addEventListener('click', (e) => {
      (window as unknown as { __kbDetail: number }).__kbDetail = (e as MouseEvent).detail;
    }, { once: true }));
    await page.keyboard.press('Enter');
    expect(await page.evaluate(() => (window as unknown as { __kbDetail: number }).__kbDetail)).toBe(0);
    expect(await activeIs(page, '#settings [data-preset]')).toBe(true);
    expect(await posts(page)).toEqual([]);
    await page.close();
  });

  // The battlefield CardTile is the one ACTIVATABLE control that is not a
  // <button>: tabindex=0 + role=button (CardTile.svelte L210–L212) put it
  // inside lib/hotkeys.ts's ACTIVATABLE set, so it is the first-named
  // instance of the defect this suite pins. Regression pin for
  // agent-20260929T101809Z-adba832d over dde071f91's wiring — no production
  // change of its own.
  it('F8: a pointer click on a battlefield CardTile releases focus so Space reaches the pass hotkey; keyboard focus still gives the tile Space', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const tile = page.locator('#board [data-obj="41"]');
    // The pointer click focuses the tile first (mousedown -> focus): record
    // that focus so the precondition "the click really put the keyboard on
    // the tile" is asserted, not assumed.
    await tile.evaluate((el) => {
      el.addEventListener('focus', () => {
        (window as unknown as { __tileFocused: boolean }).__tileFocused = true;
      }, { once: true });
    });
    // force: the bare-bones fixture page can overlap the tile with chrome;
    // the test only needs the click delivered (same reason F1's flow pill).
    await tile.click({ force: true });
    // Precondition: the click DID focus the role="button" tile...
    expect(await page.evaluate(() => (window as unknown as { __tileFocused: boolean }).__tileFocused)).toBe(true);
    // ...and the release removed it, so Space belongs to the table hotkey.
    expect(await activeIs(page, '[data-obj="41"]')).toBe(false);
    await page.keyboard.press('Space');
    const after = await nextPassPost(page, 0);
    expect(after).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    // The keyboard-native half of the contract: FOCUS on the tile still
    // gives it Space (hotkeys.focus.test.ts pins that for every activatable
    // control), and keyboard activation keeps it there — the release is
    // pointer-only (detail > 0), so no blur follows a keyboard press.
    await tile.focus();
    expect(await activeIs(page, '[data-obj="41"]')).toBe(true);
    await page.keyboard.press('Space');
    await page.waitForTimeout(150);
    expect(await posts(page)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    expect(await activeIs(page, '[data-obj="41"]')).toBe(true);
    await page.close();
  });
});
