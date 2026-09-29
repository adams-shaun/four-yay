import { beforeAll, describe, expect, it } from 'vitest';
import type { Browser, Page } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import type { Decision, Intent } from '../protocol';

let browser: Browser;
beforeAll(async () => { browser = await sharedBrowser(); });
const posts = (page: Page) => page.evaluate(() => (window as unknown as { __posts: Intent[] }).__posts);
const deliver = (page: Page, overrides: Partial<Decision> = {}) => page.evaluate((d) =>
  (window as unknown as { __deliver: (d: Partial<Decision>) => void }).__deliver(d), overrides);
const stored = (page: Page) => page.evaluate(() => JSON.parse(sessionStorage.getItem('gorge.sticky.sticky-ui:1') ?? '[]'));

async function open(): Promise<Page> {
  const page = await browser.newPage();
  await page.goto(`${browserURL}src/components/StickyChoices.fixture.html`);
  await page.waitForSelector('[data-sticky-choice]');
  return page;
}
async function answerSticky(page: Page): Promise<void> {
  await page.locator('[data-sticky-choice] input').check();
  await page.locator('[data-option="7"]').click();
  await expect.poll(() => page.locator('[data-sticky-rule]').count()).toBeGreaterThan(0);
}

describe('sticky choices — mounted production controls', () => {
  it('checkbox persists before posting, next identical ask auto-answers by label, and both markers render reactively', async () => {
    const page = await open();
    try {
      expect(await page.locator('#source [data-sticky-marker]').count()).toBe(0);
      await answerSticky(page);
      expect((await stored(page))[0].answer.label).toBe('No');
      expect(await posts(page)).toEqual([{ seq: 10, player: 0, choices: [7] }]);
      await expect.poll(() => page.locator('#source [data-sticky-marker]').getAttribute('aria-label')).toBe('Sticky choices active for Miner');
      expect(await page.locator('#stack-source [data-sticky-marker]').getAttribute('aria-label')).toBe('Sticky choices active for Miner');
      await deliver(page, { options: [{ index: 12, kind: 'no', label: 'No', player: 0 }] });
      await expect.poll(async () => (await posts(page)).length).toBe(2);
      expect((await posts(page))[1]).toEqual({ seq: 11, player: 0, choices: [12] });
      await expect.poll(() => page.locator('[data-hot-strip] > [data-sticky-marker]').getAttribute('aria-label')).toContain('Answered by sticky choice: Miner:');
      expect(await page.locator('[data-sticky-answered] [data-sticky-marker]').getAttribute('aria-label')).toContain('Answered by sticky choice:');
    } finally { await page.close(); }
  });

  it('unticked answers store nothing, and the next ask remains manual', async () => {
    const page = await open();
    try {
      await page.locator('[data-option="7"]').click();
      await expect.poll(async () => (await posts(page)).length).toBe(1);
      expect(await stored(page)).toEqual([]);
      await deliver(page);
      await page.waitForSelector('[data-option="7"]');
      expect(await page.locator('[data-sticky-choice] input').isChecked()).toBe(false);
      expect(await posts(page)).toHaveLength(1);
    } finally { await page.close(); }
  });

  it('forget removes only one rule; clear button and guarded hotkey 5 clear persistence and source markers', async () => {
    const page = await open();
    try {
      await answerSticky(page);
      await deliver(page, { prompt: 'Another Miner choice?' });
      await page.waitForSelector('[data-option="7"]');
      expect(await page.locator('[data-sticky-choice] input').isChecked()).toBe(false);
      await answerSticky(page);
      await expect.poll(() => page.locator('[data-sticky-rule]').count()).toBe(2);
      await page.locator('[data-forget-sticky]').first().click();
      await expect.poll(() => page.locator('[data-sticky-rule]').count()).toBe(1);
      expect((await stored(page))[0].label).toContain('Another Miner choice?');
      expect(await page.locator('#source [data-sticky-marker]').count()).toBe(1);
      await page.locator('[data-clear-sticky]').click();
      await expect.poll(() => page.locator('[data-sticky-rule]').count()).toBe(0);
      expect(await stored(page)).toEqual([]);
      expect(await page.locator('#source [data-sticky-marker]').count()).toBe(0);
      expect(await page.locator('[data-clear-sticky]').isDisabled()).toBe(true);

      await deliver(page);
      await answerSticky(page);
      // Native button focus must protect 5 just like other non-panic keys.
      await page.locator('[data-clear-sticky]').focus();
      await page.keyboard.press('5');
      expect(await stored(page)).toHaveLength(1);
      await page.evaluate(() => (document.activeElement as HTMLElement)?.blur());
      await page.keyboard.press('5');
      await expect.poll(() => page.locator('[data-sticky-rule]').count()).toBe(0);
      expect(await stored(page)).toEqual([]);
      expect(await page.locator('#source [data-sticky-marker]').count()).toBe(0);
      await deliver(page);
      await page.waitForSelector('[data-option="7"]');
      expect(await posts(page)).toHaveLength(3);
    } finally { await page.close(); }
  });

  it.each(['target', 'choose', 'modes', 'trigger_order'])('stores and reuses a %s answer through the real submit surface', async (kind) => {
    const page = await open();
    try {
      const ordered = kind === 'trigger_order';
      await deliver(page, { kind, prompt: `Pick ${kind}`, min: ordered ? 2 : 1, max: ordered ? 2 : 1 });
      await expect.poll(() => page.locator('[data-prompt]').textContent()).toBe(`Pick ${kind}`);
      await page.locator('[data-sticky-choice] input').check();
      await page.locator('[data-option="7"]').click();
      if (ordered) {
        await page.locator('[data-option="3"]').click();
        await page.locator('[data-submit]').click();
      }
      await expect.poll(async () => (await posts(page)).length).toBe(1);
      expect(await stored(page)).toHaveLength(1);
      await deliver(page, { kind, prompt: `Pick ${kind}`, min: ordered ? 2 : 1, max: ordered ? 2 : 1 });
      await expect.poll(async () => (await posts(page)).length).toBe(2);
      expect((await posts(page))[1].choices).toEqual(ordered ? [7, 3] : [7]);
    } finally { await page.close(); }
  });

  it('renders a checkbox exactly for the stickyKey vocabulary', async () => {
    const page = await open();
    try {
      for (const kind of ['target', 'choose', 'modes', 'trigger_optional', 'trigger_order']) {
        await deliver(page, { kind });
        await expect.poll(() => page.locator('[data-prompt]').textContent()).toBe('Return Miner?');
        expect(await page.locator('[data-sticky-choice]').count()).toBe(1);
      }
      for (const d of [
        { kind: 'replacement' }, { kind: 'choose', max: 2 },
        { kind: 'choose', options: [{ index: 0, kind: 'x', label: 'X=1', player: 0 }] },
      ]) {
        await deliver(page, { ...d, prompt: 'Not stickable' });
        await expect.poll(() => page.locator('[data-prompt]').textContent()).toBe('Not stickable');
        expect(await page.locator('[data-sticky-choice]').count()).toBe(0);
      }
    } finally { await page.close(); }
  });
});
