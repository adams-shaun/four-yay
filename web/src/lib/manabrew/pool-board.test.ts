import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import SeatBox from '../../components/SeatBox.svelte';
import { allRecords, stateOf } from './testdata/fixture';
import { projectView } from './project';

describe('ManaBrew floating pool on the live board surface', () => {
  it('projects the wire pool into a labelled own-seat readout', () => {
    const captured = stateOf(allRecords[0]);
    const wirePool = { C: 1, W: 2 };
    expect(Object.values(wirePool).reduce((sum, n) => sum + n, 0)).toBe(3);
    const gameView = {
      ...captured,
      players: captured.players.map((p, i) => i === 0 ? { ...p, manaPool: wirePool } : p),
    };

    const view = projectView(gameView, { viewer: 0 });
    const own = view.players.find((p) => p.seat === view.viewer)!;
    expect(own.pool).toEqual(wirePool);

    const { html } = render(SeatBox, { props: { player: own, name: own.name, colour: '#e5484d' } });
    expect(html).toContain('data-mana-pool');
    expect(html).toContain('data-mana-tag="pool"');
    expect(html).toContain('>pool</span>');
    expect(html).toContain('data-mana="W"');
    expect(html).toContain('data-mana="C"');
    expect(html).toContain('>2</span>');
    expect(html).toContain('>1</span>');
  });
});
