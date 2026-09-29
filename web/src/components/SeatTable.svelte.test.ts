import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import { beforeAll, describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { CardView, PlayerView, SeatInfo, View } from '../protocol';
import SeatTable from './SeatTable.svelte';

const card = (id: number, name = `Card ${id}`): CardView => ({
  id, name, types: 'Instant', printing: { name }, token: `#${id}`, tapped: false,
  power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0,
  summon_sick: false,
});

const player = (over: Partial<PlayerView> = {}): PlayerView => ({
  seat: 0, name: 'Ari', life: 40, lost: false, library_size: 52, hand_size: 2, graveyard_size: 1,
  completed_dungeons: 0,
  hand: [card(1), card(2)], battlefield: [], graveyard: [card(3)], exile: [card(4)],
  pool: {}, command: [], commanders: [], commander_casts: [], ...over,
});

const summaryView = (p: PlayerView): View => ({
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1',
  active: 0, priority: 0, over: false, draw: false, winner: null, stack: [], pending: [], players: [p],
});

const summarySeats: SeatInfo[] = [{ name: 'Ari', deck: 'red', colour: '#e5484d' }];

let browser: Browser;
let url = '';

beforeAll(async () => {
  url = browserURL;
  browser = await sharedBrowser();
});

describe('SeatTable — compact seat summary', () => {
  it('renders the five icon/count cells in order and gives only disclosable piles a caret control', () => {
    const { html } = render(SeatTable, { props: { view: summaryView(player()), seats: summarySeats, onFocus: () => {} } });
    const kinds = ['life', 'hand', 'library', 'graveyard', 'exile'];
    let at = -1;
    for (const kind of kinds) {
      const next = html.indexOf(`data-stat="${kind}"`);
      expect(next).toBeGreaterThan(at);
      at = next;
    }
    expect(html).toContain('data-icon="heart"');
    expect(html).toContain('data-icon="book"');
    expect(html).toContain('data-pile="hand"');
    expect(html).toContain('data-pile="graveyard"');
    expect(html).toContain('data-pile="exile"');
    expect(html).not.toContain('data-pile="library"');
  });

  it('puts all four zone counts on ONE line beside the life pill (the old two-high zone stack is gone)', () => {
    const { html } = render(SeatTable, { props: { view: summaryView(player()), seats: summarySeats, onFocus: () => {} } });
    expect(html.match(/zone-line/g)?.length).toBe(1);
    const line = html.slice(html.indexOf('zone-line'));
    // life stays in its own cell before the counts, outside the zone line
    expect(line.includes('data-stat="life"')).toBe(false);
    expect(line.indexOf('data-stat="hand"')).toBeGreaterThan(-1);
    expect(line.indexOf('data-stat="library"')).toBeGreaterThan(line.indexOf('data-stat="hand"'));
    expect(line.indexOf('data-stat="graveyard"')).toBeGreaterThan(line.indexOf('data-stat="library"'));
    expect(line.indexOf('data-stat="exile"')).toBeGreaterThan(line.indexOf('data-stat="graveyard"'));
  });

  it('shows true counts but no caret for empty or redacted lists', () => {
    const hidden = player({ hand: null as unknown as CardView[], graveyard: null as unknown as CardView[], graveyard_size: 4, exile: [] });
    const { html } = render(SeatTable, { props: { view: summaryView(hidden), seats: summarySeats, onFocus: () => {} } });
    expect(html).toContain('data-hand-hidden');
    expect(html).not.toContain('data-pile="hand"');
    expect(html).not.toContain('data-pile="graveyard"');
    expect(html).not.toContain('data-pile="exile"');
  });
});

describe('SeatTable — priority geometry', () => {
  it('keeps the seat row and name box at identical pixels while priority changes', async () => {
    const page = await browser.newPage();
    await page.goto(`${url}src/components/SeatTable.geometry.html`);
    // goto resolves before the Svelte fixture mounts; under load a cold Vite
    // transform leaves the row null (same race BoardStage's geometry hit).
    await page.waitForSelector('#seat-idle [data-seat-row="0"] .name', { state: 'attached', timeout: 60_000 });
    const geometry = await page.evaluate(() => {
      const rect = (selector: string) => {
        const { x, y, width, height } = document.querySelector<HTMLElement>(selector)!.getBoundingClientRect();
        return { x, y, width, height };
      };
      return {
        idle: {
          row: rect('#seat-idle [data-seat-row="0"]'),
          pick: rect('#seat-idle [data-seat-row="0"] .pick'),
          name: rect('#seat-idle [data-seat-row="0"] .name'),
          section: rect('#seat-idle .seats'),
          identity: rect('#identity-idle .seat-header'),
          identityName: rect('#identity-idle .name'),
        },
        priority: {
          row: rect('#seat-priority [data-seat-row="0"]'),
          pick: rect('#seat-priority [data-seat-row="0"] .pick'),
          name: rect('#seat-priority [data-seat-row="0"] .name'),
          section: rect('#seat-priority .seats'),
          identity: rect('#identity-priority .seat-header'),
          identityName: rect('#identity-priority .name'),
        },
      };
    });
    await page.close();

    expect(geometry.priority).toEqual(geometry.idle);
  });
});
describe('SeatTable — the rail floor (one-line counts let the rail shrink)', () => {
  // The grid floor in Table.svelte is 11rem = 176px. The fixture mounts
  // the whole rail (seat table, mana pool, decision line, stack tile, pending
  // tray) at exactly that width, with a 27-character seat name that was
  // ALREADY ellipsized at the old 17rem floor (needs 173px, had 64px there),
  // so the assertions below are measured facts about the reduced rail, not
  // about the name change. Since fb-20260917T232028Z the seat summary is one
  // text-line tall: its register (life pill + four one-line counts + row
  // chrome) measures ~141px — just under the stack tile's 144px floor, which
  // the 11rem floor cleared before this change too. The name takes the
  // remainder and ellipsizes.
  const FLOOR_PX = 176;

  async function atFloor(px = FLOOR_PX, concede: 'none' | 'idle' | 'confirm' = 'none', lost = false) {
    const params = new URLSearchParams();
    if (concede !== 'none') params.set('concede', concede);
    if (lost) params.set('lost', '1');
    const q = params.toString();
    const page = await browser.newPage();
    await page.goto(`${url}src/components/SeatTable.geometry.html${q ? `?${q}` : ''}`);
    await page.waitForSelector('#rail .rail-inner');
    await page.evaluate((w) => document.documentElement.style.setProperty('--rail-w', `${w}px`), px);
    return page;
  }

  it('fits every rail section horizontally at the 11rem floor', async () => {
    const page = await atFloor();
    const overflows = await page.evaluate(() => {
      const bad: { label: string; sw: number; cw: number }[] = [];
      const check = (el: HTMLElement, label: string) => {
        if (el.scrollWidth > el.clientWidth + 1) bad.push({ label, sw: el.scrollWidth, cw: el.clientWidth });
      };
      const rail = document.querySelector<HTMLElement>('#rail .rail-inner')!;
      check(rail, 'rail-inner');
      for (const child of Array.from(rail.children)) check(child as HTMLElement, child.className || child.tagName);
      return bad;
    });
    await page.close();
    expect(overflows).toEqual([]);
  });

  it('wraps the lost seat\'s long loss cause inside the rail instead of overflowing past it', async () => {
    // fb-20260917T232028Z round-2 finding: the eliminated line inherited the
    // row's nowrap inside a min-width: 0 flex item, so the longest real loss
    // cause (lib/seattable.ts lossCauses doc: "commander damage (21 or more
    // from one commander)") painted past the counts and past the rail box at
    // the 11rem floor (measured 349px scroll vs 176px client). The fix wraps
    // the cause inside the name box, like the pre-one-line table did; this
    // pins the class at the floor with the widest lost seat the rail holds
    // (hand 9 / library 41 / graveyard 12 / exile 2). A lost seat may be
    // TALLER than a live one (the wrapped cause) — only live seats owe their
    // one-line height.
    const page = await atFloor(FLOOR_PX, 'none', true);
    const measured = await page.evaluate(() => {
      const bad: { label: string; sw: number; cw: number }[] = [];
      const check = (el: HTMLElement, label: string) => {
        if (el.scrollWidth > el.clientWidth + 1) bad.push({ label, sw: el.scrollWidth, cw: el.clientWidth });
      };
      const rail = document.querySelector<HTMLElement>('#rail .rail-inner')!;
      check(rail, 'rail-inner');
      for (const child of Array.from(rail.children)) check(child as HTMLElement, child.className || child.tagName);
      const rows = Array.from(document.querySelectorAll<HTMLElement>('#rail [data-seat-row]'));
      for (const row of rows) check(row, `seat-row ${row.getAttribute('data-seat-row')}`);
      const lost = document.querySelector<HTMLElement>('#rail [data-eliminated]');
      const lh = lost ? parseFloat(getComputedStyle(lost).lineHeight) : 0;
      return {
        bad,
        eliminated: lost !== null,
        tag: lost?.querySelector('.eliminated__tag')?.textContent ?? null,
        cause: lost?.querySelector('.eliminated__cause')?.textContent ?? null,
        causeHeight: lost?.getBoundingClientRect().height ?? 0,
        lineHeight: lh,
        heights: rows.map((r) => ({ seat: r.getAttribute('data-seat-row'), h: r.getBoundingClientRect().height })),
      };
    });
    await page.close();
    // the fixture must have wired the lost seat, or the run proves nothing
    expect(measured.eliminated, 'the lost-seat fixture variant did not render an eliminated line').toBe(true);
    expect(measured.tag).toBe('Eliminated');
    expect(measured.cause).toContain('commander damage');
    expect(measured.bad, JSON.stringify(measured.bad)).toEqual([]);
    // AND it wraps rather than being clipped to one line: overflow: hidden on
    // .who contains the paint, so containment alone cannot tell a wrapped
    // cause from a clipped one — the cause box must hold at least two lines.
    expect(measured.causeHeight, `the cause did not wrap (${measured.causeHeight}px, one line is ${measured.lineHeight}px)`)
      .toBeGreaterThanOrEqual(measured.lineHeight * 2 - 1);
    const lostHeight = measured.heights.find((r) => r.seat === '1')!.h;
    const liveHeight = measured.heights.find((r) => r.seat === '2')!.h;
    expect(lostHeight, 'the long cause did not wrap (lost row is not taller than a live row)').toBeGreaterThan(liveHeight);
  });

  it('keeps the long seat name inside its row with the ellipsis doing the work', async () => {
    const page = await atFloor();
    const name = await page.evaluate(() => {
      const el = document.querySelector<HTMLElement>('#rail [data-seat-row="0"] .name')!;
      const row = document.querySelector<HTMLElement>('#rail [data-seat-row="0"]')!;
      const nr = el.getBoundingClientRect();
      const rr = row.getBoundingClientRect();
      return { right: nr.right, rowRight: rr.right, scroll: el.scrollWidth, client: el.clientWidth };
    });
    await page.close();
    expect(name.right).toBeLessThanOrEqual(name.rowRight + 1);
    // the ellipsis is active, so the name is truncated by dots, not clipped mid-glyph
    expect(name.scroll).toBeGreaterThan(name.client);
  });

  it('leaves room for the REAL "Concede — confirm" control Table renders in the rail\'s logbar', async () => {
    // The fixture renders the real ConcedeControl (the armed/widest state)
    // through Rail's real logbar snippet — the same seam Table.svelte uses —
    // so this measures the actual control, not a probe replica of it.
    const page = await atFloor(FLOOR_PX, 'confirm');
    const fits = await page.evaluate((floor) => {
      const control = document.querySelector<HTMLElement>('#rail [data-confirm-concede]')!;
      const logbar = document.querySelector<HTMLElement>('#rail .logbar')!;
      return {
        button: control.getBoundingClientRect().width,
        rail: logbar.clientWidth,
        floor,
        // the row must never overflow horizontally with the control in it
        overflow: logbar.scrollWidth - logbar.clientWidth,
      };
    }, FLOOR_PX);
    await page.close();
    expect(fits.button).toBeGreaterThan(0);
    expect(fits.overflow, 'the logbar row overflows horizontally with the armed control').toBeLessThanOrEqual(1);
    expect(fits.button).toBeLessThanOrEqual(fits.rail);
  });

  // fb-53bd45b9: the concede control used to be absolutely positioned at
  // top: 3rem inside the rail — an offset calibrated to clear the logbar
  // that instead landed ON seat row 0 (49.2px) once the zone counts stacked
  // two-high, painting over its life and pile counts with z-index: 9 and
  // eating the pile buttons' clicks. The control now renders inside the
  // REAL logbar row via Table's real seam (Rail's `logbar` snippet), in
  // flex flow at the row's right edge. This test renders the REAL
  // ConcedeControl through that seam in the fixture (RailFixture) and pins
  // the geometry the defect class needs for BOTH of its states — the
  // unarmed first-paint button and the armed "Concede — confirm" (the
  // wider label): inside the logbar's own band (anchored, not floating),
  // clear of the LOGS toggle, of every seat row and every [data-stat]
  // cell, at the 11rem floor AND at a wide rail (15% of 1920 = 288px).
  it('the REAL concede control (both states) shares the logbar row and intersects no seat row, stat cell or the toggle', async () => {
    for (const width of [FLOOR_PX, 288]) {
      for (const state of ['idle', 'confirm'] as const) {
        const page = await atFloor(width, state);
        const selector = state === 'confirm' ? '[data-confirm-concede]' : '[data-concede-control]';
        const measured = await page.evaluate((sel: string) => {
          const band = (el: Element) => {
            const { top, bottom, left, right } = el.getBoundingClientRect();
            return { top, bottom, left, right };
          };
          const intersects = (
            a: { top: number; bottom: number; left: number; right: number },
            b: { top: number; bottom: number; left: number; right: number },
          ) => a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
          // The REAL control, rendered by ConcedeControl through Rail's
          // logbar snippet — no injected probe. If it is absent the harness
          // must fail loudly, not measure a null.
          const control = document.querySelector<HTMLElement>(`#rail .concede-control ${sel}`);
          const logbar = document.querySelector<HTMLElement>('#rail .logbar')!;
          if (control === null || logbar === null) {
            return { state: sel, found: false } as Record<string, unknown>;
          }
          const p = band(control);
          const row = band(logbar);
          const toggle = band(document.querySelector<HTMLElement>('#rail [data-log-toggle]')!);
          const seatRows = Array.from(document.querySelectorAll<HTMLElement>('#rail [data-seat-row]')).map(band);
          const stats = Array.from(document.querySelectorAll<HTMLElement>('#rail [data-stat]')).map(band);
          return {
            state: sel,
            found: true,
            insideRail: control.closest('#rail') !== null,
            insideLogbar: control.closest('.logbar') === logbar,
            inLogbarBand: p.top >= row.top - 1 && p.bottom <= row.bottom + 1,
            // no rect overlap with the toggle; at a narrow rail the armed
            // label wraps to the row's own second line, so horizontal
            // separation is NOT required — only non-intersection is.
            // Right-alignment against the row (the anchor) is pinned
            // separately.
            clearOfToggle: !intersects(p, toggle),
            clearOfSeatRows: seatRows.every((r) => !intersects(p, r)),
            clearOfStats: stats.every((s) => !intersects(p, s)),
            seatRowCount: seatRows.length,
            statCount: stats.length,
            rightAligned: p.right >= row.right - 13,
            box: p,
          };
        }, selector);
        await page.close();
        const where = `rail ${width}px, state ${state}`;
        expect(measured.found, `${where}: the real concede control did not render`).toBe(true);
        expect(measured.insideRail, `${where}: control not inside #rail`).toBe(true);
        expect(measured.insideLogbar, `${where}: control not inside the logbar row`).toBe(true);
        expect(measured.seatRowCount).toBeGreaterThan(0);
        expect(measured.statCount).toBeGreaterThan(0);
        expect(measured.inLogbarBand, `${where}: control left the logbar band`).toBe(true);
        expect(measured.clearOfToggle, `${where}: control hits the LOGS toggle`).toBe(true);
        expect(measured.clearOfSeatRows, `${where}: control hits a seat row`).toBe(true);
        expect(measured.clearOfStats, `${where}: control hits a stat cell`).toBe(true);
        expect(measured.rightAligned, `${where}: control not anchored to the row's right edge`).toBe(true);
      }
    }
  });
});
