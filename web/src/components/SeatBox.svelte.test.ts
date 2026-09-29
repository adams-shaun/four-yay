import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { PlayerView } from '../protocol';
import SeatBox from './SeatBox.svelte';

const player = (pool: Record<string, number>): PlayerView => ({
  seat: 0, name: 'Alice', life: 20, lost: false, library_size: 53,
  hand_size: 7, graveyard_size: 0, hand: [], battlefield: [], graveyard: [], exile: [],
  pool, command: [], commanders: [], commander_casts: [], completed_dungeons: 0,
});

const props = (pool: Record<string, number>) => ({
  player: player(pool), name: 'Alice', colour: '#e5484d',
});

describe('SeatBox — floating mana pool', () => {
  it('shows a labelled pool and all floating units in the live seat box', () => {
    const floating = { C: 1, W: 2 };
    expect(Object.values(floating).reduce((sum, count) => sum + count, 0)).toBe(3);
    expect(floating).not.toEqual({});

    const { html } = render(SeatBox, { props: props(floating) });
    expect(html).toContain('data-mana-pool');
    expect(html).toContain('data-mana="W"');
    expect(html).toContain('data-mana="C"');
    expect(html).toContain('data-mana-tag="pool"');
    expect(html).toContain('>pool</span>');
    expect(html).toContain('>2</span>');
    expect(html).toContain('>1</span>');
    expect(html).toContain('aria-label="Mana pool: 2 white, 1 colourless"');
  });

  it('does not render a pool readout when the pool is empty', () => {
    const empty: Record<string, number> = {};
    expect(Object.keys(empty)).toHaveLength(0);

    const { html } = render(SeatBox, { props: props(empty) });
    expect(html).not.toContain('data-mana-pool');
  });
});
