import { beforeAll, describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import { type Browser } from 'playwright';
import { browserURL, sharedBrowser } from '../test/browser';
import type { CardView, PlayerView, View } from '../protocol';
import SeatTable from './SeatTable.svelte';

const card = (id: number): CardView => ({
  id, name: `Card ${id}`, types: 'Instant', printing: { name: `Card ${id}` }, token: `#${id}`,
  tapped: false, power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0,
  summon_sick: false,
});
const player = (lists: boolean): PlayerView => ({
  seat: 0, name: 'Ari', life: 20, lost: false, library_size: 49, hand_size: 7, graveyard_size: 2,
  completed_dungeons: 0,
  hand: lists ? [card(1)] : null as unknown as CardView[], battlefield: [],
  graveyard: lists ? [card(2)] : [], exile: lists ? [card(3)] : [],
  pool: {}, command: [], commanders: [], commander_casts: [],
});
const view = (p: PlayerView): View => ({
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1',
  active: 0, priority: 0, over: false, draw: false, winner: null, stack: [], pending: [], players: [p],
});

let browser: Browser;
beforeAll(async () => { browser = await sharedBrowser(); });

describe('SeatTable — count pills', () => {
  it.each([true, false])('wraps every icon and count in the same pill (lists available: %s)', (lists) => {
    const { html } = render(SeatTable, { props: { view: view(player(lists)), onFocus: () => {} } });
    const kinds = ['life', 'hand', 'library', 'graveyard', 'exile'];
    const icons = ['heart', 'hand', 'book', 'skull', 'exile'];
    expect((html.match(/data-stat=/g) ?? []).length).toBe(5);
    kinds.forEach((kind, i) => {
      const start = html.indexOf(`data-stat="${kind}"`);
      expect(start).toBeGreaterThan(-1);
      const cell = html.slice(start, html.indexOf('</span>', start) + '</span>'.length);
      expect(cell).toMatch(/class="(?:stat life|count|pile) pill(?:\s| ")/);
      expect(cell).toContain(`data-icon="${icons[i]}"`);
      expect(cell).toContain(i === 0 ? '>20</span>' : i === 1 ? '>7</span>' : i === 2 ? '>49</span>' : lists ? '>1</span>' : '>0</span>');
      if (['hand', 'graveyard', 'exile'].includes(kind)) {
        expect(cell.includes(`data-pile="${kind}"`)).toBe(lists);
      } else expect(cell).not.toContain('data-pile=');
    });
  });

  it('keeps the 12px outlined pills and live row inside the one-line 176px rail', async () => {
    const page = await browser.newPage();
    try {
      await page.goto(`${browserURL}src/components/SeatTable.geometry.html`);
      await page.waitForSelector('#rail [data-seat-row="0"] .pill');
      const measured = await page.evaluate(() => {
        document.documentElement.style.setProperty('--rail-w', '176px');
        const row = document.querySelector<HTMLElement>('#rail [data-seat-row="0"]')!;
        const pills = Array.from(row.querySelectorAll<HTMLElement>('.pill'));
        const sample = document.createElement('span');
        sample.style.color = 'var(--ink-dim)';
        row.append(sample);
        const ink = getComputedStyle(sample).color;
        sample.remove();
        return {
          count: pills.length,
          height: row.getBoundingClientRect().height,
          overflow: row.scrollWidth - row.clientWidth,
          pills: pills.map((el) => {
            const css = getComputedStyle(el);
            return {
              height: el.getBoundingClientRect().height, width: el.getBoundingClientRect().width,
              radius: css.borderTopLeftRadius, border: css.borderTopWidth,
              outline: css.borderTopColor, ink,
              icon: !!el.querySelector('svg[data-icon]'), number: !!el.querySelector('span'),
            };
          }),
        };
      });
      expect(measured.count).toBe(5);
      expect(measured.height).toBeLessThanOrEqual(24);
      expect(measured.overflow).toBeLessThanOrEqual(1);
      for (const pill of measured.pills) {
        expect(pill.height).toBeGreaterThanOrEqual(11);
        expect(pill.height).toBeLessThanOrEqual(13);
        expect(pill.width).toBeGreaterThanOrEqual(12);
        expect(pill.radius).toBe('999px');
        expect(pill.border).toBe('1px');
        expect(pill.outline).toBe(pill.ink);
        expect(pill.icon).toBe(true);
        expect(pill.number).toBe(true);
      }
    } finally { await page.close(); }
  });
});
