import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

// The stage is fixed 560x420 (see HandFan.geometry.html), smaller than every
// supported test viewport, so the geometry measured is the stage's clip, not
// the viewport's.
const VIEWPORTS = [
  { width: 1440, height: 900 },
  { width: 1000, height: 900 },
  { width: 650, height: 700 },
] as const;

interface Rect {
  top: number; bottom: number; left: number; right: number; width: number; height: number;
}

interface Measure {
  stage: Rect;
  card: Rect;
  /** the stage's scroll position: the board is overflow:hidden, which is
   *  programmatically scrollable — a focused half-clipped face would scroll
   *  it to "reveal" the clipped half (the felt jumps). Pinned at 0. */
  stageScrollTop: number;
}

// Tolerance: subpixel rounding across the transform chain (fan shift, card
// raise) plus the transition having settled. 2px is generous to rounding and
// still far below the half-card (~72px) the assertions discriminate on.
const TOL = 2;

function intersectH(a: Rect, b: Rect): number {
  return Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
}

// Wait until the card's raise TRANSITION has actually settled at the expected
// raised top (the .card transition is 0.12s, but under a loaded runner a
// fixed sleep races it — measured 32px of 73.6 mid-flight). A timeout throws
// with the live geometry, transform and focus state, so a genuine failure is
// diagnosable rather than a bare timeout.
async function settleRaised(
  page: Awaited<ReturnType<typeof browser.newPage>>,
  obj: string,
  raisedTop: number,
): Promise<void> {
  try {
    await page.waitForFunction(
      (a: { o: string; exp: number }) => {
        const c = document.querySelector(`[data-obj="${a.o}"]`);
        if (!c) return false;
        return Math.abs(c.getBoundingClientRect().top - a.exp) <= 1;
      },
      { o: obj, exp: raisedTop },
      { timeout: 10_000, polling: 50 },
    );
  } catch {
    const diag = await page.evaluate((o) => {
      const c = document.querySelector(`[data-obj="${o}"]`);
      const face = c?.querySelector('.face');
      return {
        top: c?.getBoundingClientRect().top ?? null,
        transform: c ? getComputedStyle(c).transform : null,
        activeObj: document.activeElement?.closest('[data-obj]')?.getAttribute('data-obj') ?? null,
        faceFocusVisible: face?.matches(':focus-visible') ?? null,
      };
    }, obj);
    throw new Error(`card ${obj} raise never settled at ${raisedTop}: ${JSON.stringify(diag)}`);
  }
}

// For a "STAYS raised" check the raise-wait above is the wrong shape: its
// first poll can read the STALE pre-drop top (the transform is still the old
// one on the frame the keypress resolves), so a card that is ABOUT to drop
// passes a settleRaised raisedTop wait without ever settling. The honest
// wait is for the card's CSS transitions to stop RUNNING, then measure: if
// the card stays raised there is no transition at all, if it drops the
// 0.12s transform transition finishes and the measurement reads the rest
// top — either way the subsequent assertion sees the truth.
async function settleTransitions(
  page: Awaited<ReturnType<typeof browser.newPage>>,
  obj: string,
): Promise<void> {
  await page.waitForFunction(
    (o) => {
      const c = document.querySelector(`[data-obj="${o}"]`);
      return !!c && c.getAnimations({ subtree: true }).every((a) => a.playState !== 'running');
    },
    obj,
    { timeout: 10_000, polling: 50 },
  );
}

// Land KEYBOARD focus on a given card's face before measuring the focus
// raise. Tab is retried only after an explicit blur, so a slow first Tab
// cannot be double-stepped past the target face; under a loaded runner the
// keypress can otherwise land while the raise settle is already waiting.
async function focusFaceByTab(
  page: Awaited<ReturnType<typeof browser.newPage>>,
  obj: string,
): Promise<void> {
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur?.());
    await page.keyboard.press('Tab');
    try {
      await page.waitForFunction(
        (o) => document.activeElement?.closest('[data-obj]')?.getAttribute('data-obj') === o,
        obj,
        { timeout: 2_000, polling: 25 },
      );
      return;
    } catch {
      /* retry from a clean slate */
    }
  }
  throw new Error(`keyboard focus never landed on card ${obj}`);
}

async function measure(page: Awaited<ReturnType<typeof browser.newPage>>, obj = '1'): Promise<Measure> {
  return page.evaluate((o) => {
    const rect = (el: Element): Rect => {
      const b = el.getBoundingClientRect();
      return { top: b.top, bottom: b.bottom, left: b.left, right: b.right, width: b.width, height: b.height };
    };
    const stage = document.querySelector('#stage')!;
    // The FIRST card: its right-hand band is covered by card 2 at rest (later
    // siblings paint above), so only the hover raise's z-index can put it
    // back on top there — the z-order assertion discriminates.
    const card = document.querySelector(`[data-obj="${o}"]`)!;
    return { stage: rect(stage), card: rect(card), stageScrollTop: stage.scrollTop };
  }, obj);
}

describe('HandFan — the hand-card peek (fb-20260916T024357Z-9005ad6a)', () => {
  it('at rest exactly the top half of a hand card is visible inside the clipped board; hover and keyboard focus each raise it fully inside, above its neighbours', { timeout: 20_000 }, async () => {
    for (const { width, height } of VIEWPORTS) {
      const page = await browser.newPage({ viewport: { width, height } });
      await page.goto(`${url}src/components/HandFan.geometry.html`);
      await page.waitForSelector('[data-obj="1"] .face', { timeout: 5000 });
      // Let the layout (ResizeObserver -> measured) and paints settle.
      await page.waitForTimeout(120);

      const atRest = await measure(page);
      const h = atRest.card.height;
      const stage = atRest.stage;

      // Sanity: the fixture really is a 63:88 card inside the stage.
      expect(h).toBeGreaterThan(50);

      // 1. AT REST — the peek. The card's lower half lies outside the board
      //    (the rect genuinely extends below the clip edge — getBoundingClientRect
      //    reports the layout box, the board's overflow: hidden is what cuts
      //    it), and the intersection of card and board is exactly half the
      //    card's height. On the unshifted fan the intersection is the whole
      //    card, so this fails both on the pre-fix code and with the shift
      //    removed.
      expect(atRest.card.bottom).toBeGreaterThan(stage.bottom - TOL);
      expect(atRest.card.bottom).toBeLessThanOrEqual(stage.bottom + h / 2 + TOL);
      expect(Math.abs(intersectH(atRest.card, stage) - h / 2)).toBeLessThanOrEqual(TOL);
      expect(atRest.card.top).toBeGreaterThanOrEqual(stage.top - TOL);
      expect(atRest.card.left).toBeGreaterThanOrEqual(stage.left - TOL);

      // 2. The rest-state z-order baseline: in the band card 1 shares with
      //    card 2, card 2 (later sibling) is the hit — card 1 is UNDER its
      //    neighbour at rest.
      const band = { x: atRest.card.left + atRest.card.width * 0.9, y: atRest.card.top + h * 0.25 };
      expect(band.x).toBeGreaterThan(atRest.card.left + atRest.card.width / 2);
      const hitAt = (x: number, y: number) =>
        page.evaluate(([px, py]) => {
          const el = document.elementFromPoint(px, py);
          return el?.closest('[data-obj]')?.getAttribute('data-obj') ?? null;
        }, [x, y]);
      expect(band.y).toBeLessThan(stage.bottom); // inside the visible half
      expect(await hitAt(band.x, band.y)).toBe('2');

      // 3. HOVER the VISIBLE face — a point only card 1 covers (its left
      //    region, clear of card 2's overlap), in the visible top half. The
      //    raise must be exactly the half the fan was lowered, the full rect
      //    must lie inside the board, and the card must come back above its
      //    neighbour in the shared band.
      await page.mouse.move(atRest.card.left + atRest.card.width * 0.35, atRest.card.top + h * 0.25);
      await settleRaised(page, '1', atRest.card.top - h / 2);
      const hovered = await measure(page);
      expect(Math.abs(hovered.card.top - (atRest.card.top - h / 2))).toBeLessThanOrEqual(TOL);
      expect(hovered.card.bottom).toBeLessThanOrEqual(stage.bottom + TOL);
      expect(hovered.card.top).toBeGreaterThanOrEqual(stage.top - TOL);
      expect(hovered.card.left).toBeGreaterThanOrEqual(stage.left - TOL);
      expect(hovered.card.right).toBeLessThanOrEqual(stage.right + TOL);
      // Above the neighbour again: the shared band now hits card 1.
      expect(await hitAt(band.x, band.y)).toBe('1');

      // 4. KEYBOARD FOCUS — same full-card geometry. Move the pointer well
      //    clear of the fan first (so :hover is off and ONLY the focus rule
      //    can raise the card), then Tab into the fan: its faces are the
      //    page's only focusables, so the first Tab lands on card 1's face.
      await page.mouse.move(5, 5);
      await focusFaceByTab(page, '1');
      await settleRaised(page, '1', atRest.card.top - h / 2);
      const focused = await measure(page);
      // The focus reveal must NOT have scrolled the clipped board to uncover
      // the resting face's lower half — the felt would jump by up to half a
      // card (measured 73-75px before the negative scroll-margin fix).
      expect(focused.stageScrollTop).toBe(0);
      const active = await page.evaluate(() => ({
        tag: document.activeElement?.tagName ?? null,
        obj: document.activeElement?.closest('[data-obj]')?.getAttribute('data-obj') ?? null,
      }));
      expect(active.obj).toBe('1');
      expect(active.tag).toBe('DIV');
      expect(Math.abs(focused.card.top - (atRest.card.top - h / 2))).toBeLessThanOrEqual(TOL);
      expect(focused.card.bottom).toBeLessThanOrEqual(stage.bottom + TOL);
      expect(focused.card.top).toBeGreaterThanOrEqual(stage.top - TOL);
      // The focused card is above its neighbour too (the same z-index rides
      // the :focus-visible rule).
      expect(await hitAt(band.x, band.y)).toBe('1');

      await page.close();
    }
  });

  it('at rest the fanned icon, badge and CAST shortcut hit their own buttons over the next face', async () => {
    for (const { width, height } of VIEWPORTS) {
      const page = await browser.newPage({ viewport: { width, height } });
      try {
        await page.goto(`${url}src/components/HandFan.geometry.html`);
        await page.waitForSelector('[data-obj="3"] .payment-shortcut', { timeout: 5000 });
        await page.waitForTimeout(120); // ResizeObserver layout has settled; no pointer/focus enters the fan.
        const resting = await page.evaluate(() => ({
          hovered: document.querySelector('.card:hover') !== null,
          focused: document.querySelector('.card:has(:focus-visible)') !== null,
        }));
        expect(resting).toEqual({ hovered: false, focused: false });
        const hitAt = (x: number, y: number) =>
          page.evaluate(([px, py]) => {
            const el = document.elementFromPoint(px, py);
            const button = el?.closest('button'); // glyph spans inside buttons can receive the hit
            return {
              obj: el?.closest('[data-obj]')?.getAttribute('data-obj') ?? null,
              button: !!button,
              icon: button?.hasAttribute('data-single-action') ?? false,
              badge: button?.matches('.badge') ?? false,
              payment: button?.matches('.payment-shortcut') ?? false,
            };
          }, [x, y]);
        for (const { obj, selector, kind } of [
          { obj: '1', selector: '.action-icon', kind: 'icon' },
          { obj: '2', selector: '.badge', kind: 'badge' },
          { obj: '3', selector: '.payment-shortcut', kind: 'payment' },
        ] as const) {
          const geometry = await page.evaluate(({ obj, selector }) => {
            const button = document.querySelector(`[data-obj="${obj}"] ${selector}`)!;
            const next = document.querySelector(`[data-obj="${Number(obj) + 1}"] .face`)!;
            const stage = document.querySelector('#stage')!;
            const b = button.getBoundingClientRect();
            const n = next.getBoundingClientRect();
            const s = stage.getBoundingClientRect();
            const x = b.left + b.width / 2;
            const y = b.top + b.height / 2;
            return { x, y, buttonWidth: b.width, overlap: n.left < x && x < n.right && n.top < y && y < n.bottom,
              visible: s.left < x && x < s.right && s.top < y && y < s.bottom, nextLeft: n.left };
          }, { obj, selector });
          // Without real overlap this test would pass even with the stacking bug.
          expect(geometry.buttonWidth).toBeGreaterThan(0);
          expect(geometry.visible).toBe(true);
          expect(geometry.overlap).toBe(true);
          expect(geometry.x).toBeGreaterThan(geometry.nextLeft);
          const hit = await hitAt(geometry.x, geometry.y);
          expect(hit.obj).toBe(obj);
          expect(hit.button, `${obj}: ${JSON.stringify(hit)}`).toBe(true);
          expect(hit[kind]).toBe(true);
        }
      } finally {
        await page.close();
      }
    }
  });

  it('at rest a card’s affordance leaves the next card’s exposed LEFT edge its own face', async () => {
    // The mirror of the occlusion case above, and the regression the first
    // escape-only fix introduced: once the affordances left their card’s
    // stacking context they painted over EVERY sibling face, including the
    // next card’s exposed left strip. That strip is the raise gesture
    // (`.card:hover`, and Playwright’s actionability hit-test) — covering it
    // sent the pointer to the neighbour’s control instead of raising the
    // card, the same two-step complaint on the other side. The affordance
    // must clear the neighbour’s left edge while its own centre still sits
    // over the neighbour’s face (asserted above).
    for (const { width, height } of VIEWPORTS) {
      const page = await browser.newPage({ viewport: { width, height } });
      try {
        await page.goto(`${url}src/components/HandFan.geometry.html`);
        await page.waitForSelector('[data-obj="3"] .payment-shortcut', { timeout: 5000 });
        await page.waitForTimeout(120); // ResizeObserver layout has settled; no pointer/focus enters the fan.
        for (const { obj, selector } of [
          { obj: '1', selector: '.action-icon' },
          { obj: '2', selector: '.badge' },
          { obj: '3', selector: '.payment-shortcut' },
        ] as const) {
          const probe = await page.evaluate(({ o, s }) => {
            const affordance = document.querySelector(`[data-obj="${o}"] ${s}`)!;
            const next = document.querySelector(`[data-obj="${Number(o) + 1}"] .face`)!;
            const a = affordance.getBoundingClientRect();
            const n = next.getBoundingClientRect();
            // The next card’s exposed left-edge point (its leftmost 2px, top
            // 8px — the point a left-edge raise hover uses).
            const x = n.left + 2;
            const y = n.top + 8;
            return {
              x, y, nextLeft: n.left, affordanceLeft: a.left,
              insideNext: n.left < x && x < n.right && n.top < y && y < n.bottom,
              // Precondition: the fixture’s cards really overlap.
              overlap: n.left < document.querySelector(`[data-obj="${o}"]`)!.getBoundingClientRect().right,
            };
          }, { o: obj, s: selector });
          expect(probe.insideNext, `${obj}: probe point ${probe.x},${probe.y} must lie in card ${Number(obj) + 1}`).toBe(true);
          expect(probe.overlap, `${obj}: the fixture must overlap`).toBe(true);
          // Proven non-vacuous: with the fix reverted the affordance covers the
          // point (see the report’s `Fails without the fix`); the shipped fix
          // moves its left edge clear of it.
          expect(probe.affordanceLeft, `${obj}: affordance left ${probe.affordanceLeft} must clear the neighbour left edge ${probe.nextLeft}`).toBeGreaterThan(probe.x);
          const hit = await page.evaluate(([px, py]) => {
            const el = document.elementFromPoint(px, py);
            return {
              obj: el?.closest('[data-obj]')?.getAttribute('data-obj') ?? null,
              button: !!el?.closest('button'),
            };
          }, [probe.x, probe.y]);
          expect(hit.obj, `${obj}: left edge ${probe.x},${probe.y} must hit card ${Number(obj) + 1}, got ${JSON.stringify(hit)}`).toBe(String(Number(obj) + 1));
          expect(hit.button, `${obj}: the left edge must not land on a button`).toBe(false);
        }
      } finally {
        await page.close();
      }
    }
  });

  it('keyboard focus stays fully raised when it moves from the face onto the card’s action controls (direct icon and open menu item)', { timeout: 30_000 }, async () => {
    // The r3 finding: the raise arm was `.card:has(.face:focus-visible)` —
    // qualified to the FACE. A hand card with an offered action has more
    // focusables inside it: the direct `.action-icon` (one option) and the
    // count badge plus its `.menu__item` controls (several). Tabbing from
    // the face onto any of them blurs the face, so the qualified :has()
    // stopped matching and the card dropped back to its half-clipped rest
    // while keyboard focus stayed on that card’s action — the affordance
    // was unusable with the keyboard. The selector is now unqualified
    // `.card:has(:focus-visible)`; this fixture Tab-steps face → direct
    // icon, and face → badge → Enter → menu item, and at EVERY step
    // measures that the focused card’s geometry is still the full raised
    // rect inside the board. Red with the face-qualified selector (the
    // card drops at the icon/menu-item step), red with the compensation
    // removed (never raises at all).
    for (const { width, height } of VIEWPORTS) {
      const page = await browser.newPage({ viewport: { width, height } });
      await page.goto(`${url}src/components/HandFan.geometry.html`);
      await page.waitForSelector('[data-obj="1"] .face', { timeout: 5000 });
      await page.waitForTimeout(120);

      const rest1 = await measure(page, '1');
      const rest2 = await measure(page, '2');
      const h = rest1.card.height;
      const stage = rest1.stage;
      // Both fans cards sit on the same row: same rest top, same height.
      expect(Math.abs(rest2.card.top - rest1.card.top)).toBeLessThanOrEqual(TOL);
      const raisedTop = rest1.card.top - h / 2;
      const fullInside = (m: Measure) => {
        expect(Math.abs(m.card.top - raisedTop)).toBeLessThanOrEqual(TOL);
        expect(m.card.bottom).toBeLessThanOrEqual(stage.bottom + TOL);
        expect(m.card.top).toBeGreaterThanOrEqual(stage.top - TOL);
        expect(m.card.left).toBeGreaterThanOrEqual(stage.left - TOL);
        expect(m.card.right).toBeLessThanOrEqual(stage.right + TOL);
      };

      // Clear the pointer and Tab into card 1’s face: the raise parity the
      // earlier fixture step already pins, restated here as the baseline.
      await page.mouse.move(5, 5);
      await focusFaceByTab(page, '1');
      await settleRaised(page, '1', raisedTop);
      fullInside(await measure(page, '1'));

      // Tab onto card 1’s DIRECT action icon (the fixture gave card 1 one
      // option, so the affordance is a single `.action-icon` button): the
      // face blurs, and the card must STAY raised under the icon’s focus.
      await page.keyboard.press('Tab');
      const onIcon = await page.evaluate(() => {
        const el = document.activeElement as HTMLElement | null;
        return {
          obj: el?.closest('[data-obj]')?.getAttribute('data-obj') ?? null,
          isDirectIcon: el?.hasAttribute('data-single-action') ?? false,
          iconFocusVisible: el?.matches(':focus-visible') ?? false,
        };
      });
      expect(onIcon).toEqual({ obj: '1', isDirectIcon: true, iconFocusVisible: true });
      await settleTransitions(page, '1');
      fullInside(await measure(page, '1'));

      // Tab onto card 2’s face (card 2 raises, card 1 drops back to rest),
      // then onto card 2’s MULTI-action count badge.
      await page.keyboard.press('Tab');
      await settleRaised(page, '2', raisedTop);
      await page.keyboard.press('Tab');
      const onBadge = await page.evaluate(() => {
        const el = document.activeElement as HTMLElement | null;
        return {
          obj: el?.closest('[data-obj]')?.getAttribute('data-obj') ?? null,
          expanded: el?.getAttribute('aria-expanded') ?? null,
        };
      });
      expect(onBadge).toEqual({ obj: '2', expanded: 'false' });
      await settleTransitions(page, '2');
      fullInside(await measure(page, '2'));

      // Enter opens the badge’s menu (Svelte binds aria-expanded to the same
      // state), Tab steps into the first `.menu__item`: the card must STILL
      // be fully raised while the menu item carries the focus.
      await page.keyboard.press('Enter');
      await page.waitForSelector('[data-obj="2"] .menu__item', { timeout: 5000 });
      await page.keyboard.press('Tab');
      const onItem = await page.evaluate(() => {
        const el = document.activeElement as HTMLElement | null;
        return {
          obj: el?.closest('[data-obj]')?.getAttribute('data-obj') ?? null,
          isMenuItem: el?.getAttribute('role') === 'menuitem',
          itemFocusVisible: el?.matches(':focus-visible') ?? false,
        };
      });
      expect(onItem).toEqual({ obj: '2', isMenuItem: true, itemFocusVisible: true });
      await settleTransitions(page, '2');
      const m2 = await measure(page, '2');
      fullInside(m2);
      // And the focus reveal still never scrolled the clipped board.
      expect(m2.stageScrollTop).toBe(0);

      await page.close();
    }
  });

  it('a half-hidden hand card still receives the pointer on its visible half and the clip leaves the neighbour faces alone', async () => {
    // The pointer-affordance half of the contract: a card's VISIBLE top half
    // is what the pointer hits (the clipped lower half is not hit-testable),
    // and the rest-state hit in each card's exclusive region is that card —
    // so entering the visible face, seeing it rise, and using its actions
    // all key off geometry that actually exists. Measured through
    // elementFromPoint, without moving the pointer.
    for (const { width, height } of VIEWPORTS) {
      const page = await browser.newPage({ viewport: { width, height } });
      await page.goto(`${url}src/components/HandFan.geometry.html`);
      await page.waitForSelector('[data-obj="1"] .face', { timeout: 5000 });
      await page.waitForTimeout(120);

      const m = await measure(page);
      const h = m.card.height;
      // A point in card 1's visible half that only card 1 covers.
      const probe = { x: m.card.left + m.card.width * 0.35, y: m.card.top + h * 0.25 };
      const hit = await page.evaluate(([px, py]) => {
        const el = document.elementFromPoint(px, py);
        return el?.closest('[data-obj]')?.getAttribute('data-obj') ?? null;
      }, [probe.x, probe.y]);
      expect(hit).toBe('1');
      // And a point clearly below the clip edge (the hidden half's zone) hits
      // nothing inside the fan — the stage's overflow clips it away.
      const hidden = await page.evaluate(([px, py]) => {
        const el = document.elementFromPoint(px, py);
        return el?.closest('[data-obj]')?.getAttribute('data-obj') ?? null;
      }, [probe.x, m.stage.bottom + 10]);
      expect(hidden).toBeNull();

      await page.close();
    }
  });
});
