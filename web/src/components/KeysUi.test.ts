import { type Browser, type Page } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * KeysUi.test.ts is the mounted half of the Keys editor and the `?` sheet
 * (final-review C1 and I6): a real click on "Add key" inside a root that runs
 * the table's outside-click test, a real key press captured into the store,
 * Escape cancelling, teardown clearing the capture flag, and the sheet
 * closing after focus has left it.
 */

let browser: Browser;

beforeAll(async () => {
  browser = await sharedBrowser();
});

type W = {
  __store: { capturing: boolean; current: Record<string, { code: string; ctrl: boolean; shift: boolean; alt: boolean }[]> };
  __outside: number;
  __unmountEditor: () => void;
  __sheetCloses: number;
  __openSheet: () => void;
};

async function open(): Promise<Page> {
  const page = await browser.newPage();
  await page.goto(`${browserURL}src/components/KeysUi.fixture.html`);
  await page.locator('[data-key-action="confirm"] .add').waitFor();
  return page;
}

async function openSheet(): Promise<Page> {
  const page = await open();
  await page.evaluate(() => (window as unknown as W).__openSheet());
  await page.locator('[role="dialog"]').waitFor();
  return page;
}

const read = <T>(page: Page, f: (w: W) => T): Promise<T> =>
  page.evaluate(`(${f.toString()})(window)`) as Promise<T>;

describe('the Keys editor — mounted', () => {
  it('Add key stays inside its popover, records the next chord and ends the capture', async () => {
    const page = await open();
    await page.locator('[data-key-action="confirm"] .add').click();
    // The clicked button is replaced by the "Press a key…" hint before the
    // click reaches the window; it is still an inside click.
    expect(await read(page, (w) => w.__outside)).toBe(0);
    expect(await read(page, (w) => w.__store.capturing)).toBe(true);
    await page.keyboard.press('k');
    expect(await read(page, (w) => w.__store.current['confirm'])).toEqual([{ code: 'KeyK', ctrl: false, shift: false, alt: false }]);
    expect(await read(page, (w) => w.__store.capturing)).toBe(false);
    await page.close();
  });

  it('Escape cancels a capture without binding anything', async () => {
    const page = await open();
    await page.locator('[data-key-action="confirm"] .add').click();
    await page.keyboard.press('Escape');
    expect(await read(page, (w) => w.__store.capturing)).toBe(false);
    expect(await read(page, (w) => w.__store.current['confirm'])).toEqual([]);
    expect(await page.locator('[data-key-action="confirm"] .add').count()).toBe(1);
    await page.close();
  });

  it('tearing the editor down mid-capture clears the capture flag', async () => {
    const page = await open();
    await page.locator('[data-key-action="confirm"] .add').click();
    expect(await read(page, (w) => w.__store.capturing)).toBe(true);
    await page.evaluate(() => (window as unknown as W).__unmountEditor());
    expect(await read(page, (w) => w.__store.capturing)).toBe(false);
    await page.close();
  });

  it('a file that cannot be read says so and clears the picker', async () => {
    const page = await open();
    await page.evaluate(() => {
      File.prototype.text = () => Promise.reject(new Error('unreadable'));
    });
    const input = page.locator('[data-keymap-editor] input[type="file"]');
    await input.setInputFiles({ name: 'keys.json', mimeType: 'application/json', buffer: Buffer.from('{}') });
    await expect.poll(() => page.locator('[data-keys-transfer]').textContent()).toBe('Could not read that file.');
    expect(await input.inputValue()).toBe('');
    await page.close();
  });

  it('a click outside the root still counts as outside', async () => {
    const page = await open();
    await page.locator('#board').dispatchEvent('click');
    expect(await read(page, (w) => w.__outside)).toBe(1);
    await page.close();
  });
});

describe('the ? sheet — mounted', () => {
  it('closes on Escape after focus has left it', async () => {
    const page = await openSheet();
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Escape');
    expect(await read(page, (w) => w.__sheetCloses)).toBe(1);
    await page.close();
  });

  it('closes on its own show-keys chord after focus has left it', async () => {
    const page = await openSheet();
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Shift+Slash');
    expect(await read(page, (w) => w.__sheetCloses)).toBe(1);
    await page.close();
  });

  it('closes on a click on its backdrop', async () => {
    const page = await openSheet();
    await page.locator('[data-keys-backdrop]').click({ position: { x: 5, y: 5 } });
    expect(await read(page, (w) => w.__sheetCloses)).toBe(1);
    expect(await page.locator('[role="dialog"]').count()).toBe(0);
    await page.close();
  });
});
