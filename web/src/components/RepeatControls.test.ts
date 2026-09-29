import { beforeAll, describe, expect, it } from 'vitest';
import type { Browser, Page } from 'playwright';
import type { Intent } from '../protocol';
import type { SeatPanelState } from '../lib/seatpanel.svelte';
import { browserURL, sharedBrowser } from '../test/browser';

let browser: Browser;
beforeAll(async () => { browser = await sharedBrowser(); });
type Harness = { __posts: Intent[]; __panel: SeatPanelState; __deliver: (seq: number, kind: 'priority' | 'choose' | 'wait', stack: boolean) => void };
const posts = (p: Page) => p.evaluate(() => (window as unknown as Harness).__posts);
const deliver = (p: Page, seq: number, kind: 'priority' | 'choose' | 'wait', stack = false) =>
  p.evaluate(([s, k, st]) => (window as unknown as Harness).__deliver(s, k, st), [seq, kind, stack] as const);
const log = (p: Page) => p.evaluate(() => (window as unknown as Harness).__panel.autoLog.map((n) => n.text));

async function open(): Promise<Page> {
  const p = await browser.newPage();
  await p.goto(`${browserURL}src/components/RepeatControls.fixture.html`);
  await p.locator('[data-hot-tab="actions"]').click();
  await p.locator('[data-option="9"]').click();
  await expect.poll(() => p.locator('#source [data-repeat-arm]').count()).toBe(1);
  return p;
}

// Browser workers share one Chromium process with the full mounted suite.
// Leave room for that contention while retaining bounded, real-click tests.
describe('repeat — mounted production tiles, strip and hotkeys', { timeout: 15_000 }, () => {
  it('offers on the accepted source and stack tile only; count defaults/clamps and typing 5 preserves stickies', async () => {
    const p = await open();
    try {
      await deliver(p, 2, 'wait', true);
      await p.waitForSelector('#stack-source [data-repeat-arm]');
      expect(await p.locator('#unrelated [data-repeat-arm]').count()).toBe(0);
      expect(await p.locator('#source [role="button"] [data-repeat-arm]').count()).toBe(0);
      expect(await p.locator('#stack-source .art [data-repeat-arm]').count()).toBe(0);
      const input = p.locator('#source [data-repeat-arm] input');
      expect(await input.inputValue()).toBe('10');
      await input.fill('');
      await input.press('5');
      expect(await input.inputValue()).toBe('5');
      expect(await p.evaluate(() => JSON.parse(sessionStorage.getItem('gorge.sticky.repeat-ui:1') ?? '[]').length)).toBe(1);
      await input.fill('1000');
      await input.press('Tab');
      expect(await input.inputValue()).toBe('999');
      await input.fill('0');
      await input.press('Tab');
      expect(await input.inputValue()).toBe('1');
      await p.locator('#source [data-repeat-arm] button').click();
      await expect.poll(() => p.locator('[data-repeat-progress]').textContent()).toContain('Repeating Altar 1/1');
      await p.keyboard.press('Escape');
      await expect.poll(() => log(p)).toEqual(['Repeat Altar ×1: done 1, halted: cancelled']);
    } finally { await p.close(); }
  });

  it('arms from the stack tile, visibly counts 1..3, uses a sticky continuation and reports done once', async () => {
    const p = await open();
    try {
      await deliver(p, 2, 'wait', true);
      await p.locator('#stack-source [data-repeat-arm] input').fill('3');
      await p.locator('#stack-source [data-repeat-arm] button').click();
      await expect.poll(() => p.locator('[data-repeat-progress]').textContent()).toContain('Repeating Altar 1/3');
      for (const [seq, count] of [[3, 2], [6, 3]]) {
        await deliver(p, seq, 'priority');
        await expect.poll(() => p.locator('[data-repeat-progress]').textContent()).toContain(`${count}/3`);
        await deliver(p, seq + 1, 'choose');
        await expect.poll(async () => (await posts(p)).at(-1)?.choices).toEqual([5]);
        await deliver(p, seq + 2, 'priority', true);
        await expect.poll(async () => (await posts(p)).at(-1)?.choices).toEqual([3]);
      }
      await deliver(p, 9, 'priority');
      await expect.poll(() => p.locator('[data-repeat-progress]').count()).toBe(0);
      expect(await log(p)).toEqual(['Repeat Altar ×3: done 3, halted: done']);
      expect(await p.locator('[data-hot-strip] > [data-repeat-halt]').textContent()).toContain('halted: done');
    } finally { await p.close(); }
  });

  it.each(['Stop', 'Escape', 'Undo'])('%s actually stops the live driver', async (control) => {
    const p = await open();
    try {
      await deliver(p, 2, 'wait');
      await p.locator('#source [data-repeat-arm] input').fill('20');
      await p.locator('#source [data-repeat-arm] button').click();
      await p.waitForSelector('[data-repeat-progress]');
      if (control === 'Escape') await p.keyboard.press('Escape');
      else if (control === 'Undo') await p.locator('[data-undo]').click();
      else await p.locator('[data-repeat-progress] button').click();
      await expect.poll(() => log(p)).toEqual([`Repeat Altar ×20: done 1, halted: ${control === 'Undo' ? 'undo' : 'cancelled'}`]);
      await deliver(p, 3, 'priority');
      await p.waitForTimeout(80);
      expect(await posts(p)).toHaveLength(1);
      expect(await p.locator('[data-repeat-progress]').count()).toBe(0);
    } finally { await p.close(); }
  });
});
