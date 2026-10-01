import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * FocusReleaseCardTile.test.ts pins the card-tile half of the
 * hotkey-focus-nonaction defect (the ticket is the verify-and-pin twin of
 * hotkey-focus2, whose F1–F7 suite lives in FocusRelease.test.ts): a pointer
 * click on a battlefield CardTile — role=button, tabindex=0, the exact
 * ACTIVATABLE control `lib/hotkeys.ts` `focusOwnsKey` defers to — used to
 * park focus there, after which Space never reached the table hotkey again
 * until the player clicked the felt. FocusRelease.fixture.ts mounts the tile
 * beside the REAL hot wiring (HotButtonStrip + SeatPanelState); these tests
 * click it with the pointer and observe, through the live document, that
 * Space passes priority (an intent POST through window.__posts).
 *
 * The keyboard contract the pointer fix preserves is asserted too: a
 * KEYBOARD-focused tile still owns Space (the guard defers to the focused
 * activatable control) — see hotkeys.focus.test.ts, which must stay true.
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

const TILE = '#tile [data-obj="21"]';

describe('a battlefield CardTile does not own the next Space after a pointer click (hotkey-focus-nonaction pin)', () => {
  it('a pointer click on the tile releases focus so Space reaches the pass hotkey', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const tile = page.locator(TILE);
    // Precondition: the tile is exactly the control the guard defers to —
    // an activatable, focusable element, focusable by pointer as well as Tab.
    expect(await tile.getAttribute('role')).toBe('button');
    expect(await tile.getAttribute('tabindex')).toBe('0');
    // Precondition: the click is a real POINTER click delivered to the tile
    // (detail > 0) — without it the Space assertion below would pass
    // vacuously, since an unclicked page never holds focus on the tile.
    await tile.evaluate((el) => el.addEventListener('click', (e) => {
      (window as unknown as { __tileClick: { detail: number; onTile: boolean } }).__tileClick =
        { detail: (e as MouseEvent).detail, onTile: el.contains(e.target as Node) };
    }, { once: true }));
    await tile.click();
    expect(await page.evaluate(() =>
      (window as unknown as { __tileClick?: { detail: number; onTile: boolean } }).__tileClick,
    )).toEqual({ detail: 1, onTile: true });
    // The fix: the tile released the focus the click parked on it...
    expect(await activeIs(page, TILE)).toBe(false);
    // ...so Space is a pass, not a swallowed key.
    await page.keyboard.press('Space');
    expect(await nextPassPost(page, 0)).toEqual([{ seq: 7, player: 0, choices: [9] }]);
    await page.close();
  });

  it('a KEYBOARD-focused tile still owns Space — the contract the pointer fix preserves', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/FocusRelease.fixture.html`);
    const tile = page.locator(TILE);
    // Precondition: focus really is on the tile (the keyboard path).
    await tile.focus();
    expect(await activeIs(page, TILE)).toBe(true);
    await page.keyboard.press('Space');
    // The guard gives the focused activatable control Space; the tile has no
    // click activation of its own, so the key goes nowhere — but it must NOT
    // pass priority.
    await page.waitForTimeout(300);
    expect(await posts(page)).toEqual([]);
    // And the keyboard focus is kept (the pointer-side blur is detail-gated).
    expect(await activeIs(page, TILE)).toBe(true);
    await page.close();
  });
});
