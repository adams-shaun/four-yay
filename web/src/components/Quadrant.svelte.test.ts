import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { CardView, PlayerView } from '../protocol';
import Quadrant from './Quadrant.svelte';
import { layoutStore } from '../lib/layouts.svelte';

/**
 * CZ2: a commander is a creature, so its tile belongs in the creatures row,
 * at the row's own --card-w (104px), not in a private rim-pinned area at
 * land scale. These tests assert the DOM structure that claim depends on —
 * the commander tile's markup falls inside the same span of html as the
 * creatures row's own CardStacks and before the others row begins — since
 * CommandArea.svelte.test.ts already covers the tile's own three states,
 * inspectability and tax.
 *
 * Task 3 (lost seats greyed on the board) is also covered here at the
 * Quadrant level, since Quadrant is what carries the `lost` class and the
 * greying scrim.
 */

const card = (id: number, name: string, types = 'Creature', manaCost?: string): CardView => ({
  id, name, types, mana_cost: manaCost,
  tapped: false, power: 2, toughness: 2, damage: 0, attacking: false,
  controller: 0, owner: 0, summon_sick: false, printing: { name }, token: `#${id}`,
});

const player = (over: Partial<PlayerView> = {}): PlayerView => ({
  seat: 0, name: 'P0', life: 40, lost: false, library_size: 90, hand_size: 7, graveyard_size: 0,
  completed_dungeons: 0,
  hand: [], battlefield: [], graveyard: [], exile: [], pool: {},
  command: [], commanders: [], commander_casts: [], ...over,
});

describe('Quadrant — commanders share the creatures row at creature scale (CZ2)', () => {
  it('the commander tile and the real creature permanent both fall inside the creatures row"s own markup', () => {
    const cmd = card(1, 'Isamaru', 'Legendary Creature');
    const creature = card(2, 'Grizzly Bears');
    const p = player({ commanders: [cmd], command: [cmd], battlefield: [creature] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });

    const rowStart = html.indexOf('data-zone-row="creatures"');
    const rowOthersStart = html.indexOf('data-zone-row="lands"');
    expect(rowStart).toBeGreaterThanOrEqual(0);
    expect(rowOthersStart).toBeGreaterThan(rowStart);

    const tileIdx = html.indexOf('data-cmd-state="command"');
    const creatureIdx = html.indexOf('data-obj="2"');
    expect(tileIdx).toBeGreaterThan(rowStart);
    expect(tileIdx).toBeLessThan(rowOthersStart);
    expect(creatureIdx).toBeGreaterThan(rowStart);
    expect(creatureIdx).toBeLessThan(rowOthersStart);
  });

  it('the old private rim-pinned command area is gone entirely', () => {
    const cmd = card(1, 'Isamaru');
    const p = player({ commanders: [cmd], command: [cmd] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    expect(html).not.toContain('command-area');
    expect(html).not.toContain('side-start');
    expect(html).not.toContain('side-end');
  });

  it('a seat with no commanders adds nothing to the creatures row', () => {
    const creature = card(2, 'Grizzly Bears');
    const p = player({ battlefield: [creature] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    expect(html).not.toContain('data-commander');
    expect(html).not.toContain('data-cmd-state');
  });

  it('the command pack sits FIRST among the creatures row\'s children, ahead of every CardStack (genesis order, CZ2 preserved)', () => {
    const cmd = card(1, 'Isamaru', 'Legendary Creature');
    const creature = card(2, 'Grizzly Bears');
    const p = player({ commanders: [cmd], command: [cmd], battlefield: [creature] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    const rowStart = html.indexOf('data-zone-row="creatures"');
    const rowOthersStart = html.indexOf('data-zone-row="lands"');
    const packIdx = html.indexOf('data-cmd-pack');
    const creatureIdx = html.indexOf('data-obj="2"');
    // the pack renders inside the creatures row's markup, before the creatures
    // row's own CardStacks — the commanders-draw-first order CZ2 established
    expect(packIdx).toBeGreaterThan(rowStart);
    expect(packIdx).toBeLessThan(rowOthersStart);
    expect(packIdx).toBeLessThan(creatureIdx);
  });

});

describe('Quadrant — an eliminated seat is greyed out on the board (Task 3)', () => {
  it('a lost seat carries the lost class and a data-lost flag Board/CSS key off', () => {
    const p = player({ lost: true });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    expect(html).toContain('data-lost="true"');
    expect(html).toMatch(/class="quadrant[^"]*\blost\b/);
  });

  it('a live seat carries neither', () => {
    const p = player({ lost: false });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    expect(html).toContain('data-lost="false"');
    expect(html).not.toMatch(/class="quadrant[^"]*\blost\b/);
  });
});

describe('Quadrant — lands always stack, creatures keep the tapped split (fb-20260916T201423Z)', () => {
  const land = (id: number, tapped: boolean): CardView =>
    ({ id, name: 'Swamp', types: 'Basic Land Swamp', tapped, power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false, printing: { name: 'Swamp', set: 'LEB', number: '1' }, token: `#${id}` });
  const bear = (id: number, tapped: boolean): CardView =>
    ({ id, name: 'Grizzly Bears', types: 'Creature Bear', tapped, power: 2, toughness: 2, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false, printing: { name: 'Grizzly Bears', set: 'LEB', number: '1' }, token: `#${id}` });

  it('the report snapshot shape — Swamp x2 tapped + x1 untapped — renders as ONE pile, while a tapped creature mix still splits', () => {
    const p = player({ battlefield: [land(1, true), land(2, true), land(3, false), bear(4, true), bear(5, false)] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    // Exactly ONE merged group on the whole board: the three Swamps, ids
    // id-sorted in the anchor. The bears split — a group of one renders a
    // bare CardTile with no group anchor — so their tiles stay individual.
    expect(html.match(/data-obj-group="[^"]*"/g)).toEqual(['data-obj-group="1,2,3"']);
    // and every creature is still individually addressable
    expect(html).toContain('data-obj="4"');
    expect(html).toContain('data-obj="5"');
  });

  it('a uniform tapped land pile keeps the shipped xN tab (no readiness plate)', () => {
    const p = player({ battlefield: [land(1, true), land(2, true)] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    expect(html).toContain('data-obj-group="1,2"');
    expect(html).not.toContain('data-stack-ready');
  });

  it('a MIXED land pile shows the readiness plate in the lands row', () => {
    const p = player({ battlefield: [land(1, true), land(2, true), land(3, false)] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    expect(html).toContain('data-stack-ready');
    expect(html).toContain('>1 ready<');
    expect(html).toContain('>×3<');
  });
});

describe('Quadrant — the board template and sizes (UI rework §2)', () => {
  const bear = (id: number): CardView => card(id, 'Grizzly Bears');
  const forest = (id: number): CardView => ({ ...card(id, 'Forest', 'Basic Land Forest'), printing: { name: 'Forest', set: 'X', number: '1' } });

  it('the Duel template: creatures on the first row, lands then others on the second', () => {
    const p = player({ battlefield: [bear(1), forest(2)] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    const rows = html.match(/data-board-row="(\d)"/g);
    expect(rows).toEqual(['data-board-row="0"', 'data-board-row="1"']);
    expect(html.indexOf('data-zone-row="creatures"')).toBeLessThan(html.indexOf('data-board-row="1"'));
    expect(html.indexOf('data-zone-row="lands"')).toBeLessThan(html.indexOf('data-zone-row="others"'));
  });

  it('the card size is the computed one, and a compact size renders art tiles', () => {
    const p = player({ battlefield: [bear(1)] });
    const size = { cardH: 60, cardW: 43, compact: true, pileW: 0, rowsW: 400 };
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d', size } });
    expect(html).toMatch(/--card-w: 43px/);
    expect(html).toMatch(/class="card-tile[^"]*\bart\b/);
    expect(html).toContain('art-name');
  });

  it('an opponent panel carries its header bar and the mirrored row order', () => {
    const p = player({ seat: 1, name: 'Mira', battlefield: [bear(1)] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#3b82f6', header: true, mirrored: true, active: true } });
    expect(html).toContain('data-seat-header="1"');
    expect(html).toContain('>Turn<');
    expect(html).toMatch(/class="quadrant[^"]*\bmirrored\b/);
  });

  it('stacking off gives every identical permanent its own tile', () => {
    const p = player({ battlefield: [forest(1), forest(2), forest(3)] });
    expect(render(Quadrant, { props: { player: p, colour: '#e5484d' } }).html).toContain('data-obj-group="1,2,3"');
    layoutStore.toggleStacking();
    try {
      const off = render(Quadrant, { props: { player: p, colour: '#e5484d' } }).html;
      expect(off).not.toContain('data-obj-group');
      for (const id of [1, 2, 3]) expect(off).toContain(`data-obj="${id}"`);
    } finally {
      layoutStore.toggleStacking();
    }
  });

  it('a focus side strip draws only the creatures', () => {
    const p = player({ battlefield: [bear(1), forest(2)] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d', strip: true } });
    expect(html).toContain('data-obj="1"');
    expect(html).not.toContain('data-zone-row="lands"');
  });
});
