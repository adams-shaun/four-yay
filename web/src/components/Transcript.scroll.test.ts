import { type Browser, type Page } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * The transcript's scroll behaviour (fb-20260928T043757Z, "it should
 * automatically scroll to the most recent action — follow the end of the
 * log"). The SSR harness has no DOM and no layout, so only a real browser
 * can prove a scroll position. The fixture
 * (Transcript.scroll.fixture.{html,svelte,ts}) mounts the production
 * Transcript inside a bounded overflow-y:auto wrapper that mimics
 * Table.svelte's `.log` — the wrapper, not Transcript's own `.transcript`,
 * is the scrolling element — and drives change through the real dvr
 * reducer in the seated client's shape (head dispatches, then a bulk
 * backfill). The defect the old cursor-keyed effect had is two-fold, both
 * measured here: at a decision boundary the newest seq is an engine-noise
 * kind, so the cursor names no [data-seq] row at all and the log never
 * follows (the effect's own old comment conceded this); and the seat's
 * client-local auto-pass notes render with no cursor relation whatsoever,
 * so they sat below the fold forever.
 *
 * Behaviour under test: while the DVR is live the log is an unconditional
 * tail follower (the DVR bar's ⏸ is the "I want to read history" contract),
 * and the tail target is the last RENDERED line, auto-pass notes included;
 * while paused the cursor row is what stays in view, so scrubbing works.
 */

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

const goto = async (page: Page): Promise<void> => {
  await page.goto(`${url}src/components/Transcript.scroll.fixture.html`);
};

/** The wrapper actually overflows: without this the scroll assertions prove nothing. */
const assertOverflows = async (page: Page): Promise<void> => {
  await expect
    .poll(() =>
      page.evaluate(() => {
        const log = document.querySelector('.log')!;
        return log.scrollHeight - log.clientHeight;
      }),
    )
    .toBeGreaterThan(0);
};

const geometry = (page: Page) =>
  page.evaluate(() => {
    const log = document.querySelector('.log')!;
    return {
      top: log.scrollTop,
      atEnd: log.scrollTop + log.clientHeight >= log.scrollHeight - 2,
      lastSeq: (() => {
        const rows = log.querySelectorAll('.line[data-seq]');
        return rows.length ? rows[rows.length - 1].getAttribute('data-seq') : null;
      })(),
    };
  });

/** A row (engine line or auto-pass note) sits fully inside the wrapper's viewport. */
const rowVisible = (page: Page, selector: string) =>
  page.evaluate((sel) => {
    const log = document.querySelector('.log')!;
    const row = log.querySelector(sel) as HTMLElement | null;
    if (!row) return false;
    const lr = log.getBoundingClientRect();
    const rr = row.getBoundingClientRect();
    return rr.top >= lr.top - 1 && rr.bottom <= lr.bottom + 1;
  }, selector);

describe('the transcript follows the end of the log while live (fb-20260928T043757Z)', () => {
  it('follows the rendered tail when the newest line is filtered out and the cursor names no row', async () => {
    const page = await browser.newPage();
    await goto(page);
    await assertOverflows(page);
    // Precondition: the initial mount already follows the tail (live DVR).
    let geo = await geometry(page);
    expect(geo.atEnd).toBe(true);
    expect(geo.lastSeq).toBe('40');

    // The player scrolls up to read history while the DVR stays live.
    await page.evaluate(() => ((document.querySelector('.log') as HTMLElement).scrollTop = 0));
    geo = await geometry(page);
    expect(geo.top).toBe(0);
    expect(geo.atEnd).toBe(false);

    // Seated shape, step 1: the stream's `head` chits announce seqs 41-50,
    // the LAST one an engine-noise kind ('decision_ask', hidden by default).
    // No rendered rows appear, so the log stays where the reader put it.
    await page.evaluate(() =>
      (window as unknown as { __headOnly: (n: number, hiddenTail?: boolean) => void }).__headOnly(10, true));
    geo = await geometry(page);
    expect(geo.top).toBe(0);
    expect(geo.lastSeq).toBe('40');

    // Step 2: the bulk backfill lands rows for 41-50 — but seq 50 is
    // engine noise, so the RENDERED tail is row 49 while the DVR cursor
    // sits on 50, which names no [data-seq] row. That is exactly the
    // seated path at a decision boundary: the old cursor-keyed effect
    // found no row for the cursor and gave up, leaving the log at the
    // top. The live log must follow the rendered tail instead.
    await page.evaluate(() => (window as unknown as { __backfillPending: () => void }).__backfillPending());
    geo = await geometry(page);
    expect(geo.lastSeq).toBe('49');
    expect(await rowVisible(page, '[data-seq="49"]')).toBe(true);
    expect(geo.atEnd).toBe(true);
    await page.close();
  });

  it('a live log also follows its own client-local auto-pass note at the tail', async () => {
    const page = await browser.newPage();
    await goto(page);
    await assertOverflows(page);
    // Precondition: no notes yet, and the log sits at its end.
    expect(await page.locator('[data-auto-log]').count()).toBe(0);
    expect((await geometry(page)).atEnd).toBe(true);

    await page.evaluate(() => (window as unknown as { __appendNote: () => void }).__appendNote());
    await expect.poll(() => page.locator('[data-auto-log]').count()).toBe(1);
    // The note renders under every engine line and is the tail the log follows.
    expect(await rowVisible(page, '[data-auto-log]')).toBe(true);
    expect((await geometry(page)).atEnd).toBe(true);
    await page.close();
  });

  it('a paused log keeps the cursor row in view instead of following appended lines', async () => {
    const page = await browser.newPage();
    await goto(page);
    await assertOverflows(page);

    // Pause with the cursor parked on an early line, then append ten more
    // (head chits do not move a paused cursor; the backfill never does).
    await page.evaluate(() => (window as unknown as { __pauseAt: (seq: number) => void }).__pauseAt(3));
    await page.evaluate(() => (window as unknown as { __appendLog: (n: number) => void }).__appendLog(10));
    await expect.poll(() => page.locator('[data-seq="50"]').count()).toBe(1);
    // Precondition: the appended rows exist and the cursor row is marked current.
    await expect.poll(() => page.locator('[data-seq="3"].current').count()).toBe(1);

    // The paused log did NOT follow the tail; the highlighted cursor row is
    // what was brought into view.
    const geo = await geometry(page);
    expect(geo.atEnd).toBe(false);
    expect(geo.lastSeq).toBe('50');
    expect(await rowVisible(page, '[data-seq="3"]')).toBe(true);
    await page.close();
  });
});
