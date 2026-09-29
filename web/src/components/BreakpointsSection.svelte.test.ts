import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import { SeatPanelState } from '../lib/seatpanel.svelte';
import { noBreakpoints } from '../lib/playsettings';
import BreakpointsSection from './BreakpointsSection.svelte';

const ctx = { seat: 0, token: 'tok' };
const html = (s: SeatPanelState) => render(BreakpointsSection, { props: { state: s } }).html;

describe('BreakpointsSection', () => {
  it('renders all off by default: no checks, depth Off, empty watchlist message', () => {
    const h = html(new SeatPanelState('t', 1, ctx, null));
    expect(h).toContain('Pause when');
    expect(h).toMatch(/<input[^>]*data-bp="targets-me"(?![^>]*checked)[^>]*>/);
    expect(h).toMatch(/<input[^>]*data-bp="attacked"(?![^>]*checked)[^>]*>/);
    expect(h).toMatch(/<option[^>]*value="0"[^>]*selected[^>]*>Off<\/option>/);
    expect(h).toContain('No cards on your watchlist.');
  });

  it('reflects set breakpoints and lists watchlist names with a remove button each', () => {
    const s = new SeatPanelState('t', 1, ctx, null);
    s.settings = { ...s.settings, breakpoints: { ...noBreakpoints(), targetsMe: true, stackDepth: 3, watchlist: ["Thassa's Oracle", 'Cyclonic Rift'] } };
    const h = html(s);
    expect(h).toMatch(/<input[^>]*data-bp="targets-me"[^>]*checked[^>]*>/);
    expect(h).toMatch(/<option[^>]*value="3"[^>]*selected[^>]*>3 or more<\/option>/);
    expect((h.match(/data-watch-item/g) ?? []).length).toBe(2);
    expect(h).toContain('aria-label="Remove Cyclonic Rift from watchlist"');
  });

  it('the watchlist input has an accessible name', () => {
    expect(html(new SeatPanelState('t', 1, ctx, null))).toMatch(/<input[^>]*data-watch-input[^>]*aria-label="Card name to watch"|<input[^>]*aria-label="Card name to watch"[^>]*data-watch-input/);
  });
});
