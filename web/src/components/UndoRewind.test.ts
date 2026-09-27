import { type Browser, type Page } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * UndoRewind.test.ts is the mounted regression for the 2026-09-26 demo
 * report "undo still doesn't work with new auto pay features" (table g3):
 * after UNDO the panel showed the Lord Windgrace discard ask, the chip read
 * Paused, and every answer was refused with `intent seq 1526, pending
 * decision seq 1519`.
 *
 * The server had rewound correctly (its log ends at the seq-1519 priority
 * window). The client did not: the rewind frame reset the panel's seq space
 * (begin() drops the seqHigh fence), and SeatPanel's adopt effect -- which
 * re-runs on the panel's own state writes -- re-adopted the decision still
 * embedded in the painted PRE-rewind seat view (1526) into the new space.
 * That set the fence to 1526, so the restored 1519 ask (from the fresh view
 * and from every /pending poll) was refused as "older" for good, and every
 * submission carried 1526. MatchState now withdraws the painted view's ask on
 * a seated rewind (withdrawPaintedDecision; match.seat.test.ts pins it), and
 * a /pending read from the discarded space is dropped
 * (seatpanel.rewind.test.ts).
 *
 * The fixture composes the real MatchState, SeatPanelState, Table's onFrame
 * glue and the real HotButtonStrip; only the server is simulated. The flow
 * runs with Auto Mana on (the report) and off: auto-pay is incidental, the
 * wedge is the same either way.
 */

let browser: Browser;
let url = '';

type State = { pending: number | null; kind: string | null; active: number | null; error: string | null; paused: boolean; busy: boolean };
type Post = { body: { seq: number; choices: number[]; payment?: { action_id: string } | null }; status: number; message?: string };
type FixtureWindow = {
  __state: () => State;
  __rendered: () => number | null;
  __rewoundPendingReads: () => number;
  __posts: () => Post[];
  __undos: () => string[];
  __deliverRewind: () => void;
  __click: (index: number) => void;
};

const state = (page: Page): Promise<State> => page.evaluate(() => (window as unknown as FixtureWindow).__state());
const posts = (page: Page): Promise<Post[]> => page.evaluate(() => (window as unknown as FixtureWindow).__posts());
const click = (page: Page, index: number): Promise<void> =>
  page.evaluate((i) => (window as unknown as FixtureWindow).__click(i), index);

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

async function undoToTheRestoredWindow(page: Page): Promise<void> {
  // PRECONDITION: the pre-undo ask is the discard at 1526, adopted from the
  // seat view the MatchState fetched at head.
  await page.waitForFunction(() => (window as unknown as FixtureWindow).__state().pending === 1526);
  expect(await state(page)).toMatchObject({ pending: 1526, kind: 'choose', paused: false });

  // The real UNDO button, then the server's rewind + decision frames.
  await page.locator('[data-undo]').click();
  await page.waitForFunction(() => (window as unknown as FixtureWindow).__undos().length === 1);
  await page.evaluate(() => (window as unknown as FixtureWindow).__deliverRewind());

  // Both recovery sources have had their chance: the seat view at the new
  // head is on the board, and the 1s /pending poll has read the rewound
  // server at least once.
  await page.waitForFunction(() => {
    const w = window as unknown as FixtureWindow;
    return w.__rendered() === 1519 && w.__rewoundPendingReads() >= 1;
  }, undefined, { timeout: 5000 });
}

describe('UNDO lands the seat on the restored decision, never the discarded one (demo g3, 2026-09-26)', () => {
  it('Auto Mana on: the restored window is adopted and the auto-pay cast posts its payment plan against seq 1519', { timeout: 15_000 }, async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/UndoRewind.fixture.html`);
    await undoToTheRestoredWindow(page);

    expect(await state(page)).toMatchObject({ pending: 1519, kind: 'priority', active: 1519, paused: true, error: null });

    // The next submission: a hand click on the Harrow cast, which Auto Mana
    // routes to the offered plan of THIS decision.
    await click(page, 0);
    await page.waitForFunction(() => (window as unknown as FixtureWindow).__posts().length === 1);
    const sent = await posts(page);
    expect(sent).toHaveLength(1);
    expect(sent[0]).toMatchObject({ status: 204, body: { seq: 1519, choices: [], payment: { action_id: 'pa-1519-13' } } });
    expect((await state(page)).error).toBeNull();
    await page.close();
  });

  it('Auto Mana off: the same undo adopts 1519 and a pass posts against it (auto-pay is not the cause)', { timeout: 15_000 }, async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/UndoRewind.fixture.html?autopay=0`);
    await undoToTheRestoredWindow(page);

    expect(await state(page)).toMatchObject({ pending: 1519, kind: 'priority', active: 1519, paused: true, error: null });

    await click(page, 1);
    await page.waitForFunction(() => (window as unknown as FixtureWindow).__posts().length === 1);
    expect(await posts(page)).toEqual([{ status: 204, body: { seq: 1519, player: 0, choices: [1] } }]);
    expect((await state(page)).error).toBeNull();
    await page.close();
  });
});
