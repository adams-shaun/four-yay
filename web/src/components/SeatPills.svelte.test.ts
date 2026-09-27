import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { PlayerView, SeatInfo, View } from '../protocol';
import SeatPills from './SeatPills.svelte';

const player = (seat: number, name: string, more: Partial<PlayerView> = {}): PlayerView => ({
  seat, name, life: seat ? 17 : 20, lost: false, library_size: 52, hand_size: 7,
  graveyard_size: 2, hand: [], battlefield: [], graveyard: [], exile: [],
  pool: {}, available: {}, command: [], commanders: [], commander_casts: [], ...more,
});
const view = (players: PlayerView[]): View => ({
  viewer: 0, visibility: 'seat', active: 0, priority: 1, turn: 2, round: 1, step: 'main', phase: 'main1',
  over: false, draw: false, winner: null, players, stack: [], pending: [],
});
const seats: SeatInfo[] = [
  { name: 'Avery Longplayer Name', deck: 'a', colour: '#e5484d' },
  { name: 'Bot', deck: 'b', colour: '#3b82f6' },
];

describe('SeatPills', () => {
  it('renders every player as one compact, labelled pill beside the controls', () => {
    const html = render(SeatPills, { props: { view: view([player(0, seats[0].name), player(1, 'Bot')]), seats } }).html;
    expect(html).toContain('data-seat-pills');
    expect(html).toContain('data-player-pill="0"');
    expect(html).toContain('data-player-pill="1"');
    expect(html).toContain('data-player-name="Avery Longplayer Name"');
    expect(html).toContain('title="Avery Longplayer Name"');
    expect(html).toContain('Avery Longpl…');
    expect(html).toContain('>20<');
    expect(html).toContain('>17<');
    expect(html).toContain('L52');
    expect(html).toContain('H7');
    expect(html).toContain('G0');
    expect(html).toContain('E0');
  });

  it('can render one docked player without duplicating the other seat', () => {
    const html = render(SeatPills, {
      props: { view: view([player(0, seats[0].name), player(1, 'Bot')]), seats, seat: 1 },
    }).html;
    expect(html).not.toContain('data-player-pill="0"');
    expect(html).toContain('data-player-pill="1"');
    expect(html).toContain('>17<');
  });

  it('keeps the public mana pool in a compact second row', () => {
    const html = render(SeatPills, {
      props: { view: view([player(0, 'Ari', { pool: { U: 2, R: 1 } }), player(1, 'Bo', { pool: { G: 1 } })]), seats },
    }).html;
    expect(html).toContain('data-mana-pool');
    expect(html).toContain('Mana pool: 2 U, 1 R');
    expect(html).toContain('Mana pool: 1 G');
  });

  it('does not hide interactive pile controls from assistive technology', () => {
    const html = render(SeatPills, { props: { view: view([player(0, 'Ari', { graveyard: [{ id: 9 }] as never }), player(1, 'Bo')]), seats } }).html;
    expect(html).toContain('data-pile="graveyard"');
    expect(html).not.toContain('class="counts" aria-hidden="true"');
  });

  it('states priority and elimination in the accessible player summary', () => {
    const html = render(SeatPills, { props: { view: view([player(0, 'Ari'), player(1, 'Bo', { lost: true })]), seats } }).html;
    expect(html).toContain('has priority');
    expect(html).toContain('eliminated');
    expect(html).toMatch(/class="seat-pill[^"]*priority/);
  });
});

  it('keeps overflow anchored at the first player so earlier seats remain reachable', () => {
    const players = Array.from({ length: 8 }, (_, seat) => player(seat, `Seat ${seat}`));
    const html = render(SeatPills, { props: { view: view(players), seats } }).html;
    expect(html.indexOf('data-player-pill="0"')).toBeLessThan(html.indexOf('data-player-pill="7"'));
    // Overflow starts at the first child: the user can scroll forward for
    // later players without losing the viewer/first seat off the left edge.
    expect(readFileSync(fileURLToPath(new URL('./SeatPills.svelte', import.meta.url)), 'utf8')).toMatch(/justify-content:\s*flex-start/);
  });
