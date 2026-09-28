import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { PlayerView, View } from '../protocol';
import Board from './Board.svelte';
import { layoutStore } from '../lib/layoutsettings.svelte';

/**
 * Board threads the layout settings' `own` flag (fb-20260916T182801Z): the
 * on-board resize stepper mounts only on the VIEWER'S quadrant — computed
 * from view.viewer here, the one place that knows it — so four copies of one
 * control never fight over the one shared setting. A spectator (viewer −1)
 * gets no stepper at all.
 */

const player = (seat: number): PlayerView => ({
  seat, name: `P${seat}`, life: 40, lost: false, library_size: 60, hand_size: 7,
  graveyard_size: 0, hand: [], battlefield: [], graveyard: [], exile: [], pool: {},
  completed_dungeons: 0,
  command: [], commanders: [], commander_casts: [],
});

const view = (viewer: number, seats: number): View => ({
  viewer,
  visibility: 'public',
  turn: 1,
  round: 1,
  step: 'main1',
  phase: 'Main',
  active: 0,
  priority: 0,
  over: false,
  draw: false,
  winner: null,
  players: Array.from({ length: seats }, (_, i) => player(i)),
  stack: [],
  pending: [],
});

describe('Board — the resize stepper mounts on the viewer\'s own quadrant only', () => {
  it('a 1v1 viewer sees one stepper set (their own quadrant), the opponent\'s has none', () => {
    // fb-20260917T004304Z: the shipped default is HIDDEN, so the shared
    // store opts in for the mount assertions (the exact call the panel's
    // toggle onclick makes).
    layoutStore.setSteppersOnBoard(true);
    try {
      const { html } = render(Board, { props: { view: view(0, 2), seats: [] } });
      const own = html.indexOf('data-seat="0"');
      const theirs = html.indexOf('data-seat="1"');
      expect(own).toBeGreaterThanOrEqual(0);
      expect(theirs).toBeGreaterThan(own);
      // the stepper markup appears only inside the own quadrant's span
      expect(html.slice(own, theirs)).toContain('data-zone-stepper');
      expect(html.slice(theirs)).not.toContain('data-zone-stepper');
    } finally {
      layoutStore.reset();
      layoutStore.dispose();
    }
  });

  it('a spectator (viewer -1) sees no stepper anywhere', () => {
    const { html } = render(Board, { props: { view: view(-1, 4), seats: [] } });
    expect(html).not.toContain('data-zone-stepper');
  });
});
