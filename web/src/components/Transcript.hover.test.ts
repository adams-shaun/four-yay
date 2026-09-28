import { type Browser, type Page } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * The pointer half of the transcript's card-name hover preview
 * (fb-20260927T154603Z: "the card names in the logs should show the card
 * detail like the battlefield does"). The SSR harness
 * (Transcript.carddetail.test.ts) can prove the rendered trigger and the
 * portalled panel's placement in the tree, but it has no DOM and no pointer
 * events, so only a real browser can prove the dwell timer, the leave/blur
 * close, and that the panel opens on the card name rather than the row.
 *
 * The dwell is a real 250 ms timer here (CardHover's production default), so
 * each open waits for the panel to appear rather than advancing a clock. The
 * panel portals to `body > .card-detail#card-detail-<id>`.
 */

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

const detail = (page: Page, id: number) => `body > .card-detail#card-detail-${id}`;

const open = async (page: Page): Promise<void> => {
  await page.goto(`${url}src/components/Transcript.fixture.html`);
  // precondition: the resolvable card name renders as a trigger (id 4 is in
  // the fixture's view); the degraded line's name is present too.
  await expect.poll(() => page.locator('[data-seq="1"] .obj.card[tabindex]').count()).toBe(1);
};

describe('the transcript card-name hover preview (fb-20260927T154603Z)', () => {
  it('does not render an auto-pass snapshot until opened, then exposes JSON for export', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/Transcript.fixture.html`);
    const details = page.locator('.auto-detail');
    await expect.poll(() => details.count()).toBe(1);
    // Closed rows must not serialize/render the snapshot or retain an export URL.
    expect(await page.locator('.auto-detail pre').count()).toBe(0);
    expect(await page.locator('.auto-detail a[download="autopass-view.json"]').count()).toBe(0);

    await details.locator(':scope > summary').click();
    await details.locator('details > summary').click();
    const snapshot = details.locator('pre');
    await expect.poll(() => snapshot.textContent()).toContain('seat-redacted-view');
    const exportLink = details.locator('a[download="autopass-view.json"]');
    await expect.poll(() => exportLink.getAttribute('href')).toContain('data:application/json');
    expect(decodeURIComponent((await exportLink.getAttribute('href'))!.split(',')[1])).toContain('seat-redacted-view');
    await page.close();
  });

  it('a 250 ms pointer dwell over a log card name opens the detail panel; pointer leave closes it', async () => {
    const page = await browser.newPage();
    await open(page);

    await page.locator('[data-seq="1"] .obj.card').hover();
    const panel = await page.waitForSelector(detail(page, 4));

    // the panel describes the object the log line named, not the row and not
    // some other card
    await expect.poll(() => panel.textContent()).toContain('Jace, the Mind Sculptor');
    await expect.poll(() => panel.textContent()).toContain('#4');
    // exactly one panel for the whole transcript
    expect(await page.locator('body > .card-detail').count()).toBe(1);

    // the panel is NOT inside a row <button>: its parent is <body> (the portal)
    expect(await page.evaluate((el) => el.parentElement?.tagName, panel)).toBe('BODY');
    expect(await page.evaluate((el) => !!el.closest('button'), panel)).toBe(false);

    // pointer leave closes it
    await page.mouse.move(0, 0);
    await page.waitForSelector(detail(page, 4), { state: 'detached' });
    await expect.poll(() => page.locator('body > .card-detail').count()).toBe(0);
    await page.close();
  });

  it('keyboard focus opens immediately and Escape closes it', async () => {
    const page = await browser.newPage();
    await open(page);

    await page.locator('[data-seq="1"] .obj.card').focus();
    await page.waitForSelector(detail(page, 4));
    expect(await page.locator('body > .card-detail').count()).toBe(1);

    await page.keyboard.press('Escape');
    await page.waitForSelector(detail(page, 4), { state: 'detached' });
    await expect.poll(() => page.locator('body > .card-detail').count()).toBe(0);
    await page.close();
  });

  it('a card name whose object id is absent from the view keeps the title tooltip and opens no panel', async () => {
    const page = await browser.newPage();
    await open(page);

    // id 30 is named in the line and its NAME is a view card name (that is why
    // the parser still renders it as a card span), but object 30 is not in the
    // visible set — the honest degrade: no invented panel.
    const span = page.locator('[data-seq="2"] .obj.card');
    await expect.poll(() => span.getAttribute('title')).toBe('Lightning Bolt #30');
    // precondition: this span is the DEGRADED one — it carries no tabindex
    expect(await span.getAttribute('tabindex')).toBeNull();

    await span.hover();
    await page.waitForTimeout(500); // longer than one dwell: nothing may open
    expect(await page.locator('body > .card-detail').count()).toBe(0);
    await page.close();
  });
});
