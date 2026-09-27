import { type Browser, type Page } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

/**
 * The prompt-surface browser gate (brief Jobs 1, 2 and 4): a real browser,
 * because the asks are about what a player can SEE and do — a required
 * prompt visible with the ACTIONS dropdown never opened, and the arrange
 * popup ordering cards by click and by drag and posting the picked order.
 * The fixture mounts the strip and the board prompt surface the way Table
 * mounts them, sharing one SeatPanelState; the arrange case captures the
 * posted intent at window.__posted so the wire shape is asserted without a
 * server.
 */
describe('PromptSurface', () => {
  it('attaches a required prompt below ACTIONS and answers it there', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html`);

    const tab = page.locator('[data-hot-tab="actions"]');
    const panel = page.locator('#hot-panel-actions [data-answer-surface]');
    const prompt = panel.locator('[data-prompt]');
    expect(await tab.getAttribute('aria-expanded')).toBe('true');
    expect(await panel.count()).toBe(1);
    expect(await prompt.isVisible()).toBe(true);
    expect(await prompt.textContent()).toContain('Choose a target');
    const tabBox = await tab.boundingBox();
    const panelBox = await panel.boundingBox();
    expect(tabBox).not.toBeNull();
    expect(panelBox).not.toBeNull();
    expect(panelBox!.y).toBeGreaterThanOrEqual(tabBox!.y + tabBox!.height - 1);

    const option = panel.locator('[data-option="0"]');
    expect(await option.isVisible()).toBe(true);
    await option.click();
    await page.waitForFunction(() => document.querySelectorAll('#hot-panel-actions [data-option]').length === 0);
    await page.close();
  });

  it('keeps an initiative decision open until it is answered', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html`);

    const tab = page.locator('[data-hot-tab="actions"]');
    const panel = page.locator('#hot-panel-actions [data-answer-surface]');
    expect(await page.locator('[data-strip-pointer]').count()).toBe(0);
    expect(await panel.locator('[data-option]').count()).toBe(1);
    await tab.hover();
    await page.mouse.move(10, 500);
    await page.waitForTimeout(250);
    expect(await tab.getAttribute('aria-expanded')).toBe('true');
    await page.close();
  });
});


describe('ArrangeModal (via the seat panel)', () => {
  /** The HTML5 drag event sequence a real drag produces, dispatched on the keep row. */
  const dragKeepCard = (page: Page, from: number, onto: number) =>
    page.evaluate(([f, t]) => {
      const dt = new DataTransfer();
      const source = document.querySelector(`[data-arrange-keep-card="${f}"]`)!;
      const target = document.querySelector(`[data-arrange-keep-card="${t}"]`)!;
      source.dispatchEvent(new DragEvent('dragstart', { dataTransfer: dt, bubbles: true }));
      target.dispatchEvent(new DragEvent('dragover', { dataTransfer: dt, bubbles: true, cancelable: true }));
      target.dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true, cancelable: true }));
      source.dispatchEvent(new DragEvent('dragend', { dataTransfer: dt, bubbles: true }));
    }, [from, onto] as const);

  it('a pure reorder opens as a reorder surface — every card kept in offered order, drop guarded, drag reorder posted', async () => {
    const page = await browser.newPage();

    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    await page.locator('[data-arrange-open]').click();
    const keep = page.locator('[data-arrange-keep]');
    expect(await keep.isVisible()).toBe(true);
    // The MINOR fix's initial state, in a real browser: a fresh pure reorder
    // opens with EVERY card in the keep row, in offered order, and no pool
    // row — it is a reorder surface from the first paint, not a picking one.
    expect(await keep.locator('[data-arrange-keep-card]').count()).toBe(5);
    expect(await page.locator('[data-arrange-pool-card]').count()).toBe(0);
    expect(await page.locator('[data-arrange-submit]').isDisabled()).toBe(false);

    // The drop guard: on a pure reorder nothing can leave the keep pile (the
    // pool it would drop to does not exist), so clicking a kept card is a no-op.
    await keep.locator('[data-arrange-keep-card="2"] button.face').click();
    expect(await keep.locator('[data-arrange-keep-card]').count()).toBe(5);
    expect(await page.locator('[data-arrange-submit]').isDisabled()).toBe(false);

    // Drag the keep pile's card 3 onto card 0: the pile reads 3, 0, 1, 2, 4.
    await dragKeepCard(page, 3, 0);
    expect(await keep.locator('[data-arrange-keep-card="3"] button.face').getAttribute('aria-label')).toBe('1: Spell Pierce');
    expect(await keep.locator('[data-arrange-keep-card="0"] button.face').getAttribute('aria-label')).toBe('2: Brazen Borrower');

    await page.locator('[data-arrange-submit]').click();
    const posted = JSON.parse(await page.evaluate(() => (window as unknown as { __posted?: string }).__posted ?? 'null')) as { seq: number; player: number; choices: number[] };
    expect(posted.seq).toBe(7);
    expect(posted.player).toBe(0);
    expect(posted.choices).toEqual([3, 0, 1, 2, 4]);

    await page.close();
  });

  it('a scry keeps cards by click from the pool, orders them by drag, and posts the picked order', async () => {
    const page = await browser.newPage();

    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=scry`);

    await page.locator('[data-arrange-open]').click();
    const keep = page.locator('[data-arrange-keep]');
    const pool = page.locator('[data-arrange-pool]');
    expect(await keep.isVisible()).toBe(true);
    expect(await keep.locator('[data-arrange-keep-card]').count()).toBe(0);
    expect(await pool.locator('[data-arrange-pool-card]').count()).toBe(5);

    // Click three pool cards into the keep pile (click-to-place).
    await pool.locator('[data-arrange-pool-card="1"]').click();
    await pool.locator('[data-arrange-pool-card="0"]').click();
    await pool.locator('[data-arrange-pool-card="3"]').click();
    expect(await keep.locator('[data-arrange-keep-card]').count()).toBe(3);
    expect(await keep.locator('[data-arrange-keep-card="3"] button.face').getAttribute('aria-label')).toBe('3: Spell Pierce');
    expect(await keep.locator('[data-arrange-keep-card="0"] button.face').getAttribute('aria-label')).toBe('2: Brazen Borrower');

    // Drag the keep pile's last card (3) onto its first (1): the pile reads
    // 3, 1, 0.
    await dragKeepCard(page, 3, 1);
    expect(await keep.locator('[data-arrange-keep-card="3"] button.face').getAttribute('aria-label')).toBe('1: Spell Pierce');
    expect(await keep.locator('[data-arrange-keep-card="1"] button.face').getAttribute('aria-label')).toBe('2: Fabled Pass');

    // Keep the last two cards, then submit the keep pile's order.
    await pool.locator('[data-arrange-pool-card="2"]').click();
    await pool.locator('[data-arrange-pool-card="4"]').click();
    await page.locator('[data-arrange-submit]').click();

    const posted = JSON.parse(await page.evaluate(() => (window as unknown as { __posted?: string }).__posted ?? 'null')) as { seq: number; player: number; choices: number[] };
    expect(posted.seq).toBe(7);
    expect(posted.player).toBe(0);
    expect(posted.choices).toEqual([3, 1, 0, 2, 4]);

    await page.close();
  });

  it('the same final keep order posted through the old click-order picker is byte-identical', async () => {
    const page = await browser.newPage();

    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    // The old answer path, kept on the surface: clicking the arrange row's
    // cards in the final order appends each index, building exactly the
    // picked array the popup posts for the same ordering (lib/arrange.ts's
    // arrangeOrder). 3, 0, 1, 2, 4 — the same ordering the drag test posted.
    for (const i of [3, 0, 1, 2, 4]) {
      await page.locator(`[data-arrange-row] [data-option="${i}"]`).click();
    }
    await page.locator('[data-submit]').click();

    const posted = JSON.parse(await page.evaluate(() => (window as unknown as { __posted?: string }).__posted ?? 'null')) as { seq: number; player: number; choices: number[] };
    expect(posted.choices).toEqual([3, 0, 1, 2, 4]);

    await page.close();
  });

  it('a new decision while the modal is open closes it; the reopened modal is the NEW ask, never the old ask’s edits (stale-edits regression)', { timeout: 30_000 }, async () => {
    const page = await browser.newPage();

    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=seqswap`);

    await page.locator('[data-arrange-open]').click();
    const keep = page.locator('[data-arrange-keep]');
    expect(await keep.locator('[data-arrange-keep-card]').count()).toBe(5);
    // Edit the keep order so the modal's edit state is live for seq 7.
    await dragKeepCard(page, 3, 0);
    expect(await keep.locator('[data-arrange-keep-card="3"] button.face').getAttribute('aria-label')).toBe('1: Spell Pierce');

    // The ask changes underneath the open popup — the two-tab path: another
    // tab answered seq 7 and the seat's next arrange ask (seq 9) arrived.
    // The swap button sits under the modal's backdrop, so the swap is
    // dispatched programmatically, exactly as an SSE view replacement would
    // land.
    await page.evaluate(() => (document.querySelector('[data-swap-decision]') as HTMLButtonElement).click());

    // The popup belonged to the old ask: it is closed, and the board now
    // asks decision B. The settle wait covers the paced art lookups (100 ms
    // apart in lib/images) finishing their layout-affecting resolution.
    await page.waitForFunction(() => document.querySelectorAll('[data-arrange-modal]').length === 0, null, { timeout: 10_000 });
    await page.waitForTimeout(1500);
    expect(await page.locator('[data-answer-surface] [data-prompt]').textContent()).toContain('Rearrange the top 3 card(s)');

    // Reopening is the NEW ask, fresh: three decision-B cards in offered
    // order — none of decision A's edits — and the posted intent is B's.
    await page.locator('[data-arrange-open]').click({ timeout: 10_000 });
    expect(await page.locator('[data-arrange-keep-card]').count()).toBe(3);
    expect(await page.locator('[data-arrange-keep-card="0"] button.face').getAttribute('aria-label')).toBe('1: Absorb Vis');
    expect(await page.locator('[data-arrange-keep-card="2"] button.face').getAttribute('aria-label')).toBe('3: Counterspell');
    await page.locator('[data-arrange-submit]').click({ timeout: 10_000 });
    const posted = JSON.parse(await page.evaluate(() => (window as unknown as { __posted?: string }).__posted ?? 'null')) as { seq: number; choices: number[] };
    expect(posted.seq).toBe(9);
    expect(posted.choices).toEqual([0, 1, 2]);

    await page.close();
  });
});

/**
 * The arrange surfaces' card detail (fb-20260914T063020Z): the strip's cards
 * get the same hover/focus CardDetail inspector every other card surface
 * has, and the modal's preview shows the printed description beside the art.
 * Browser tests, because these are asks about what a player can SEE and do
 * with a real pointer and keyboard — the SSR harness cannot hover or focus.
 */
describe('the arrange strip hover inspector (fb-20260914T063020Z Job 1)', () => {
  /** Give the portalled inspector the same catalog and resolved art plate as the demo. */
  async function injectCatalog(page: Page): Promise<void> {
    await page.route('**/PromptSurface.fixture.html*', async (route) => {
      const res = await route.fetch();
      await route.fulfill({ response: res, body: (await res.text()).replace('</head>', '<meta name="gorge-cards" content=""></head>') });
    });
    await page.route('**/cards/named*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ name: 'Brazen Borrower', oracle_text: 'Flying, ward 2.' }),
      }));
    await page.route('**/art/named*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ image_uris: { normal: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==' } }),
      }));
  }

  it('hovering a strip card opens the CardDetail inspector (portalled to <body>), describing that card', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    const pick = page.locator('[data-arrange-row] [data-option="0"]');
    await pick.hover();
    // The panel lives on <body>, not inside the seat panel — the panel is a
    // containing block for fixed descendants (its own style NOTE), which is
    // exactly why CardDetail portals.
    const detail = page.locator('body > .card-detail');
    await detail.waitFor({ state: 'visible', timeout: 5_000 }); // the ~250ms dwell
    expect(await detail.textContent()).toContain('Brazen Borrower');
    expect(await pick.getAttribute('aria-describedby')).toBe('card-detail-11');

    // pointer leave closes it again
    await page.mouse.move(5, 5);
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });

    await page.close();
  });

  it('clicking a strip card opens the inspector, then pointer leave closes it', async () => {
    const page = await browser.newPage();
    await injectCatalog(page);
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    await page.locator('[data-arrange-row] [data-option="0"]').click();
    const detail = page.locator('body > .card-detail');
    await detail.waitFor({ state: 'visible', timeout: 5_000 });
    expect(await detail.isVisible()).toBe(true);

    await page.mouse.move(5, 5);
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });
    await page.close();
  });

  it('clicking a keyboard-focused strip card transfers ownership to the pointer, so leave closes it', async () => {
    const page = await browser.newPage();
    await injectCatalog(page);
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    const pick = page.locator('[data-arrange-row] [data-option="0"]');
    const detail = page.locator('body > .card-detail');
    await pick.focus();
    await detail.waitFor({ state: 'visible', timeout: 5_000 });

    // An already-focused button gets pointer events on click but no second
    // focus event. pointerdown itself must therefore transfer ownership.
    await pick.click();
    await page.mouse.move(5, 5);
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });
    await page.close();
  });

  it('a second click on the still-focused strip card reopens the inspector immediately', async () => {
    const page = await browser.newPage();
    await injectCatalog(page);
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    const pick = page.locator('[data-arrange-row] [data-option="0"]');
    const detail = page.locator('body > .card-detail');
    await pick.click();
    await detail.waitFor({ state: 'visible', timeout: 5_000 });
    await page.mouse.move(5, 5);
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });
    expect(await pick.evaluate((el) => document.activeElement === el)).toBe(true);

    // No focus event follows this click. The panel must be mounted by the
    // pointerdown path, before the 250 ms hover dwell could fire.
    await pick.click();
    expect(await detail.count()).toBe(1);
    expect(await detail.isVisible()).toBe(true);
    await page.mouse.move(5, 5);
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });
    await page.close();
  });

  it('leaving a clicked strip card removes the portalled panel before it can cover Confirm', async () => {
    const page = await browser.newPage();
    await injectCatalog(page);
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    await page.locator('[data-arrange-row] [data-option="0"]').click();
    await page.locator('body > .card-detail--has-plate').waitFor({ state: 'visible', timeout: 5_000 });
    await page.mouse.move(5, 5);
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });

    // The detail is portalled and pointer-transparent, so elementFromPoint
    // alone cannot see visual occlusion. Check both its absence and rectangle
    // intersection against the board-mounted Confirm control.
    const coversConfirm = await page.locator('[data-answer-surface] [data-submit]').evaluate((confirm) => {
      const c = confirm.getBoundingClientRect();
      return [...document.querySelectorAll('body > .card-detail')].some((panel) => {
        const p = panel.getBoundingClientRect();
        return p.left < c.right && c.left < p.right && p.top < c.bottom && c.top < p.bottom;
      });
    });
    expect(coversConfirm).toBe(false);
    await page.close();
  });

  it('keyboard focus opens the inspector immediately, and Escape closes it', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    const pick = page.locator('[data-arrange-row] [data-option="0"]');
    await pick.focus();
    const detail = page.locator('body > .card-detail');
    await detail.waitFor({ state: 'visible', timeout: 5_000 });

    // No catalog in the fixture: the designed degradation is the wire-only
    // ledger — no oracle block, no art plate.
    expect(await detail.locator('.card-detail__oracle').count()).toBe(0);

    await page.keyboard.press('Escape');
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });

    await page.close();
  });

  it('a stale pointerleave from card A cannot close keyboard-focused card B', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    const cardA = page.locator('[data-arrange-row] [data-option="0"]');
    const cardB = page.locator('[data-arrange-row] [data-option="1"]');
    const detail = page.locator('body > .card-detail');

    // Open A by pointer dwell, then move keyboard ownership to B without
    // moving the pointer. Leaving A afterwards is a stale event with respect
    // to the shared panel and must not dismiss B's focus inspector.
    await cardA.hover();
    await detail.waitFor({ state: 'visible', timeout: 5_000 });
    await cardB.focus();
    expect(await detail.textContent()).toContain('Fabled Pass');
    await page.mouse.move(5, 5);
    expect(await detail.isVisible()).toBe(true);
    expect(await detail.textContent()).toContain('Fabled Pass');
    expect(await cardB.getAttribute('aria-describedby')).toBe('card-detail-12');

    // Once B actually loses focus, its inspector closes.
    await cardB.evaluate((el) => el.blur());
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });
    await page.close();
  });

  it('a card leaving the ask closes the panel with no pointer event (the lifecycle contract)', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=seqswap`);

    await page.locator('[data-arrange-row] [data-option="0"]').hover();
    await page.locator('body > .card-detail').waitFor({ state: 'visible', timeout: 5_000 });

    // The ask changes underneath the stationary pointer (the two-tab path);
    // the swap is dispatched programmatically exactly as an SSE view
    // replacement would land. Decision B's options do not carry the card
    // the panel describes, so the panel closes without pointerleave.
    await page.evaluate(() => (document.querySelector('[data-swap-decision]') as HTMLButtonElement).click());
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });

    await page.close();
  });
});

describe('the arrange modal preview (fb-20260914T063020Z Job 2)', () => {
  it('without a catalog, hovering a card shows the large art and its name — and no oracle block (the designed degradation)', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);

    await page.locator('[data-arrange-open]').click();
    await page.locator('[data-arrange-keep-card="0"] button.face').hover();
    const preview = page.locator('[data-arrange-preview]');
    await preview.locator('.name').waitFor({ state: 'visible', timeout: 5_000 });
    expect(await preview.locator('.name').textContent()).toBe('Brazen Borrower');
    expect(await preview.locator('[data-arrange-preview-oracle]').count()).toBe(0);

    await page.close();
  });

  it('with a catalog, the preview shows the printed description under the large art', async () => {
    const page = await browser.newPage();
    // A page WITH a catalog: the <meta name="gorge-cards"> tag opts in
    // (empty content = the same-origin catalog — exactly what cmd/gorged
    // injects into its served index), rewritten into the fixture response.
    await page.route('**/PromptSurface.fixture.html*', async (route) => {
      const res = await route.fetch();
      await route.fulfill({ response: res, body: (await res.text()).replace('</head>', '<meta name="gorge-cards" content=""></head>') });
    });
    await page.route('**/cards/named*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ name: 'Brazen Borrower', oracle_text: 'Flying, ward 2.' }),
      }));

    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);
    await page.locator('[data-arrange-open]').click();
    await page.locator('[data-arrange-keep-card="0"] button.face').hover();

    const oracle = page.locator('[data-arrange-preview-oracle]');
    await oracle.waitFor({ state: 'visible', timeout: 5_000 });
    expect(await oracle.textContent()).toContain('Flying, ward 2.');

    await page.close();
  });

  it('clears card A’s description while card B’s delayed lookup is pending, then renders only B’s result', async () => {
    const page = await browser.newPage();
    await page.route('**/PromptSurface.fixture.html*', async (route) => {
      const res = await route.fetch();
      await route.fulfill({ response: res, body: (await res.text()).replace('</head>', '<meta name="gorge-cards" content=""></head>') });
    });

    let notifyBStarted = () => {};
    const bStarted = new Promise<void>((resolve) => { notifyBStarted = resolve; });
    let releaseB = () => {};
    const bReleased = new Promise<void>((resolve) => { releaseB = resolve; });
    await page.route('**/cards/named*', async (route) => {
      const name = new URL(route.request().url()).searchParams.get('exact') ?? '';
      if (name === 'Fabled Pass') {
        notifyBStarted();
        await bReleased;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ name, oracle_text: `${name} rules text.` }),
      });
    });

    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=arrange`);
    await page.locator('[data-arrange-open]').click();
    const preview = page.locator('[data-arrange-preview]');
    const oracle = preview.locator('[data-arrange-preview-oracle]');

    // Resolve A first, then move keyboard focus directly to B. B's art/name
    // change immediately while its request remains deliberately unresolved.
    await page.locator('[data-arrange-keep-card="0"] button.face').focus();
    await oracle.waitFor({ state: 'visible', timeout: 5_000 });
    expect(await oracle.textContent()).toBe('Brazen Borrower rules text.');
    await page.locator('[data-arrange-keep-card="1"] button.face').focus();
    await bStarted;
    expect(await preview.locator('.name').textContent()).toBe('Fabled Pass');
    expect(await oracle.count()).toBe(0); // never B art/name with A text

    releaseB();
    await oracle.waitFor({ state: 'visible', timeout: 5_000 });
    expect(await oracle.textContent()).toBe('Fabled Pass rules text.');
    expect(await oracle.textContent()).not.toContain('Brazen Borrower');

    await page.close();
  });
});

/**
 * The discard-pick card-face row (fb-20260914T120705Z): the Thoughtseize /
 * Mind Rot discard ask — a `modes` decision whose options are "discard" card
 * picks — renders real card faces (synthesized from {obj, label}, exactly as
 * the arrange strip does) with the shared CardHover → CardDetail inspector,
 * instead of the plain text list it used to be. Browser tests, because these
 * are asks about what a player SEES (faces, hover inspector) and does (the
 * posting path must be the ordinary one, byte-identical to the text list).
 */
describe('the discard-pick card-face row (fb-20260914T120705Z)', () => {
  it('a Thoughtseize-shaped ask renders card faces with the hover inspector, and a click answers it', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=discard1`);

    const row = page.locator('[data-answer-surface] [data-discard]');
    await row.waitFor({ state: 'visible' });
    // Four card faces, and NO generic text option buttons.
    expect(await row.locator('[data-option]').count()).toBe(4);
    expect(await page.locator('[data-answer-surface] .option').count()).toBe(0);
    // The face is the card, named without the "Discard " prefix — the exact
    // string the art proxy and the oracle resolver look the card up by.
    expect(await row.locator('[data-option="0"] .blank__name').textContent()).toBe('Brazen Borrower');
    expect(await row.locator('[data-option="3"] .blank__name').textContent()).toBe('Spell Pierce');
    // Buttons stay buttons with the card name as their accessible label.
    expect(await row.locator('[data-option="0"]').getAttribute('aria-label')).toBe('Brazen Borrower');

    // Pointer dwell opens the portalled CardDetail inspector describing that card.
    await row.locator('[data-option="0"]').hover();
    const detail = page.locator('body > .card-detail');
    await detail.waitFor({ state: 'visible', timeout: 5_000 }); // the ~250ms dwell
    expect(await detail.textContent()).toContain('Brazen Borrower');
    expect(await row.locator('[data-option="0"]').getAttribute('aria-describedby')).toBe('card-detail-11');

    // Leave closes it; keyboard focus opens it again, on another card.
    await page.mouse.move(5, 5);
    await page.waitForFunction(() => document.querySelectorAll('body > .card-detail').length === 0, null, { timeout: 5_000 });
    await row.locator('[data-option="1"]').focus();
    await detail.waitFor({ state: 'visible', timeout: 5_000 });
    expect(await detail.textContent()).toContain('Fabled Pass');

    // The card row lives in the ACTIONS-anchored response surface. There is
    // no second board overlay for this initiative decision.
    expect(await page.locator('#hot-panel-actions [data-strip-pointer]').count()).toBe(0);
    expect(await page.locator('#hot-panel-actions [data-option]').count()).toBe(4);

    // The posting path is unchanged: Min == Max == 1, so the click IS the
    // answer — the same [index] intent the text list posted.
    await row.locator('[data-option="2"]').click();
    await page.waitForFunction(() => document.querySelectorAll('[data-answer-surface] [data-option]').length === 0);
    const posted = JSON.parse(await page.evaluate(() => (window as unknown as { __posted?: string }).__posted ?? 'null')) as { seq: number; player: number; choices: number[] };
    expect(posted.seq).toBe(11);
    expect(posted.player).toBe(0);
    expect(posted.choices).toEqual([2]);

    await page.close();
  });

  it('a Mind Rot multi-pick toggles faces and commits through the ordinary submit', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=discard2`);

    const row = page.locator('[data-answer-surface] [data-discard]');
    await row.waitFor({ state: 'visible' });
    expect(await row.locator('[data-option]').count()).toBe(5);

    // Too few picks: the commit is gated on the decision's own min.
    const submit = page.locator('[data-answer-surface] [data-submit]');
    expect(await submit.isDisabled()).toBe(true);

    // Toggle two faces in: picked state on the wire-vocabulary affordances
    // (aria-pressed) and the pick ordinal, as the mulligan-bottom row shows.
    await row.locator('[data-option="2"]').click();
    await row.locator('[data-option="0"]').click();
    expect(await row.locator('[data-option="2"]').getAttribute('aria-pressed')).toBe('true');
    expect(await row.locator('[data-option="0"]').getAttribute('aria-pressed')).toBe('true');
    expect(await row.locator('[data-option="2"] .order').textContent()).toBe('1');
    expect(await row.locator('[data-option="0"] .order').textContent()).toBe('2');

    // Submit posts the picked order — the same intent the text list posted.
    await submit.click();
    await page.waitForFunction(() => document.querySelectorAll('[data-answer-surface] [data-option]').length === 0);
    const posted = JSON.parse(await page.evaluate(() => (window as unknown as { __posted?: string }).__posted ?? 'null')) as { seq: number; choices: number[] };
    expect(posted.seq).toBe(12);
    expect(posted.choices).toEqual([2, 0]);

    await page.close();
  });

  it('a modal mode list (no Obj) still renders the generic text list', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/PromptSurface.fixture.html?case=charm`);

    const options = page.locator('[data-answer-surface] .option');
    await options.first().waitFor({ state: 'visible' });
    expect(await options.count()).toBe(2);
    expect(await options.first().textContent()).toContain('Deal 2 damage');
    // No card-face row: the mode list is not a discard-pick ask.
    expect(await page.locator('[data-answer-surface] [data-discard]').count()).toBe(0);

    await page.close();
  });
});

/**
 * The empty-answer safety net (the Squadron Hawk fail-to-find soft-lock's
 * client half): a pending decision for this seat that carries NO options is
 * one no picker can render, so the Pending tray must never answer it with
 * "Nothing waiting". The tray names the decision, and when the minimum legal
 * answer is the empty one (Min 0) it offers a Continue that posts it.
 *
 * These cases share this file's dev server and browser rather than starting
 * their own: another concurrent Vite server plus Chromium in the suite was
 * measured to make unrelated browser tests flake on a cold optimizer cache.
 * The seated route's wiring (Table renders the entry and Continue for the
 * seat, none for a spectator) is pinned in Table.svelte.test.ts, and
 * continueEmpty's gate in seatpanel.submit.test.ts.
 */
describe('PendingTray empty-answer safety net', () => {
  const tray = (which: string) => `${url}src/components/PendingTray.fixture.html${which ? `?case=${which}` : ''}`;

  it('an empty tray still reads Nothing waiting when nothing is stuck', async () => {
    const page = await browser.newPage();
    await page.goto(tray(''));
    await page.waitForSelector('.empty');
    expect(await page.locator('.empty').textContent()).toContain('Nothing waiting');
    expect(await page.locator('[data-stuck]').count()).toBe(0);
    await page.close();
  });

  it('a pending Min 0 / Max 0 choose with no options renders a visible, answerable Continue that posts the empty answer', async () => {
    const page = await browser.newPage();
    await page.goto(tray('stuck'));
    const stuck = page.locator('[data-stuck]');
    await stuck.waitFor();
    expect(await stuck.isVisible()).toBe(true);
    expect(await stuck.textContent()).toContain('Search a library: choose up to 0 card(s)');
    // "Nothing waiting" is gone: the seat is NOT idle, it is blocked on a
    // decision the tray now names.
    expect(await page.locator('.empty').count()).toBe(0);
    const cont = page.locator('[data-stuck] [data-continue]');
    expect(await cont.isVisible()).toBe(true);
    expect((await cont.textContent())?.trim()).toBe('Continue');
    // The click goes through the real SeatPanelState.continueEmpty and the
    // real postIntent: the wire body is the minimum legal (empty) answer.
    await cont.click();
    await page.waitForFunction(() => (window as unknown as { __posted?: string }).__posted !== undefined);
    const posted = JSON.parse(await page.evaluate(() => (window as unknown as { __posted?: string }).__posted ?? 'null')) as unknown;
    expect(posted).toEqual({ seq: 846, player: 0, choices: [] });
    await page.close();
  });

  it('a decision with no options and no legal answer is named but offers no Continue', async () => {
    const page = await browser.newPage();
    await page.goto(tray('stuck-unanswerable'));
    const stuck = page.locator('[data-stuck]');
    await stuck.waitFor();
    expect(await stuck.textContent()).toContain('Search a library: choose 1 card(s)');
    // There is no minimum legal answer to submit (Min 1 over nothing), so the
    // tray names the decision and says so instead of offering a control that
    // could never succeed.
    expect(await page.locator('[data-stuck] [data-continue]').count()).toBe(0);
    expect(await stuck.textContent()).toContain('This decision offers no choices');
    await page.close();
  });
});
