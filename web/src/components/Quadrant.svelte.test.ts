import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { CardView, PlayerView } from '../protocol';
import Quadrant from './Quadrant.svelte';
import { layoutStore } from '../lib/layoutsettings.svelte';

/** elem returns one element with its content, matched by a data attribute ('' when absent). Same helper PlaySettingsPanel.svelte.test.ts uses. */
function elem(html: string, attr: string): string {
  const m = new RegExp(`<([a-z]+)[^>]*${attr.replace(/"/g, '\\"')}[^>]*>([\\s\\S]*?)</\\1>`).exec(html);
  return m === null ? '' : m[0];
}

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

    const rowStart = html.indexOf('row creatures');
    const rowOthersStart = html.indexOf('row others');
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
    const rowStart = html.indexOf('row creatures');
    const rowOthersStart = html.indexOf('row others');
    const packIdx = html.indexOf('data-cmd-pack');
    const creatureIdx = html.indexOf('data-obj="2"');
    // the pack renders inside the creatures row's markup, before the creatures
    // row's own CardStacks — the commanders-draw-first order CZ2 established
    expect(packIdx).toBeGreaterThan(rowStart);
    expect(packIdx).toBeLessThan(rowOthersStart);
    expect(packIdx).toBeLessThan(creatureIdx);
  });

  it('the command pack and the creatures row carry SEPARATE scales and aligns (fb-20260917T232202Z)', () => {
    const store = layoutStore;
    const cmd = card(1, 'Isamaru', 'Legendary Creature');
    const creature = card(2, 'Grizzly Bears');
    const p = player({ commanders: [cmd], command: [cmd], battlefield: [creature] });
    store.bump('command', 0.3);
    try {
      const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
      const creaturesRow = html.slice(html.indexOf('row creatures'), html.indexOf('row others'));
      // the row keeps ITS OWN scale (1) while the pack carries the command scale (1.3)
      expect(creaturesRow).toMatch(/--row-scale:\s*1/);
      expect(creaturesRow).toMatch(/--cmd-scale:\s*1\.3/);
    } finally {
      store.reset();
      store.dispose();
    }
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
    expect(html).toContain('>x3<');
  });
});

describe('Quadrant — layout settings (fb-20260916T182801Z)', () => {
  it('each battlefield row publishes its scale, alignment and zone key', () => {
    const p = player({ battlefield: [card(2, 'Grizzly Bears')] });
    const { html } = render(Quadrant, { props: { player: p, colour: '#e5484d' } });
    for (const zone of ['creatures', 'others', 'lands'] as const) {
      const row = elem(html, `data-zone-row="${zone}"`);
      expect(row).not.toBe('');
      expect(row).toContain('--row-scale');
      expect(row).toContain('data-align="left"'); // the shipped default
    }
  });

  it('the on-board resize stepper is mounted only on the viewer\'s own quadrant', () => {
    const p = player({ battlefield: [card(2, 'Grizzly Bears')] });
    // fb-20260917T004304Z: the shipped default is now HIDDEN, so the test
    // opts the shared store in for the stepper-mount assertions.
    layoutStore.setSteppersOnBoard(true);
    try {
      const spectator = render(Quadrant, { props: { player: p, colour: '#e5484d' } }).html;
      expect(spectator).not.toContain('data-zone-stepper');
      const own = render(Quadrant, { props: { player: p, colour: '#e5484d', own: true } }).html;
      expect(own).toContain('data-zone-stepper="creatures"');
      expect(own).toContain('data-zone-stepper="others"');
      expect(own).toContain('data-zone-stepper="lands"');
      // ...and never the hand's stepper, which HandFan owns
      expect(own).not.toContain('data-zone-stepper="hand"');
    } finally {
      layoutStore.reset();
      layoutStore.dispose();
    }
  });

  it('the Game Options show/hide toggle hides EVERY on-board stepper, and showing it restores exactly the previous mounts (fb-20260916T200925Z)', () => {
    const p = player({ battlefield: [card(2, 'Grizzly Bears')] });
    const props = { props: { player: p, colour: '#e5484d', own: true } };
    try {
      // The toggle is the layout store's steppersOnBoard; this is the exact
      // call the panel's switch onclick makes.
      layoutStore.setSteppersOnBoard(false);
      const hidden = render(Quadrant, props).html;
      // conditional render, not display:none: no stepper markup anywhere
      expect(hidden).not.toContain('data-zone-stepper');
      // the rows themselves stay (scale/align/outline are untouched)
      for (const zone of ['creatures', 'others', 'lands'] as const) {
        const row = elem(hidden, `data-zone-row="${zone}"`);
        expect(row).not.toBe('');
        expect(row).toContain('--row-scale');
      }

      layoutStore.setSteppersOnBoard(true);
      const shown = render(Quadrant, props).html;
      expect(shown).toContain('data-zone-stepper="creatures"');
      expect(shown).toContain('data-zone-stepper="others"');
      expect(shown).toContain('data-zone-stepper="lands"');
    } finally {
      layoutStore.reset();
      layoutStore.dispose();
    }
  });

  it('the dotted outline is up on every quadrant\'s row while the store pulses that zone, and down once the flash clears', () => {
    const p = player({ battlefield: [card(2, 'Grizzly Bears')] });
    // The store is the module singleton components read; bumping from a
    // "panel" pulse must be visible here, which is the whole point of the
    // one shared store.
    layoutStore.bump('lands', 0.1);
    const flashed = render(Quadrant, { props: { player: p, colour: '#e5484d' } }).html;
    expect(elem(flashed, 'data-zone-row="lands"')).toContain('zone-outline');
    expect(elem(flashed, 'data-zone-row="creatures"')).not.toContain('zone-outline');
    expect(layoutStore.scale('lands')).toBe(1.1);
    layoutStore.reset();
    layoutStore.dispose();
    const calm = render(Quadrant, { props: { player: p, colour: '#e5484d' } }).html;
    expect(elem(calm, 'data-zone-row="lands"')).not.toContain('zone-outline');
  });
});
