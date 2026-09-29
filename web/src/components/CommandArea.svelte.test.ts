import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { CardView, PlayerView, SeatInfo, View } from '../protocol';
import { HoverCard, type AnchorRect } from '../lib/carddetail.svelte';
import { commandZoneOf } from '../lib/commander';
import CommandArea from './CommandArea.svelte';
import CommanderTile from './CommanderTile.svelte';
import Board from './Board.svelte';
import Rail from './Rail.svelte';

/**
 * The board's command area replaces the rail's Commanders section (CZ1). Every
 * assertion the removed CommandZone.svelte.test.ts made is made here against
 * the new component — the derived CR 903.8 tax and never the raw count, the
 * zone each commander resolves to, a stack cast not counting as "in the
 * command zone", a constructed table drawing nothing, and one tile per
 * commander seat in seat order — and the rest are new: the drawing
 * states, inspectability in every one of them, art-free legibility, the
 * rail no longer carrying any of it, and the ui10 bug-1 rule — a commander
 * on the battlefield is the one permanent in the creature row, not a
 * command-zone tile as well, so the board never draws the same commander
 * twice.
 *
 * CZ2 moved the tiles OUT of a private, rim-pinned area and INTO the seat's
 * creatures row, at creature scale. fb-20260917T232202Z then gave the command
 * zone its OWN layout settings, which the creatures row cannot carry for it
 * (the row resolves --card-w on itself; the tiles are siblings of the
 * CardStacks in one flex row, where an independent justify-content has
 * nothing to act on): CommandArea now renders ONE wrapper — the command
 * pack, first among the creatures row's children — that is itself a
 * flex-wrap row carrying the command zone's own scale (--cmd-scale →
 * --card-w) and align (data-align). The tests that used to assert NO
 * wrapper were re-pointed at the pack: the old private-area assertions
 * (no data-command-area, no side-start/side-end recess) still hold — the
 * pack is inside the creatures row, not a private area elsewhere on the rim.
 */

const card = (id: number, name: string, manaCost?: string): CardView => ({
  id, name, types: 'Legendary Creature', mana_cost: manaCost,
  tapped: false, power: 3, toughness: 3, damage: 0, attacking: false,
  controller: 0, owner: 0, summon_sick: false, printing: { name }, token: `#${id}`,
});

const player = (over: Partial<PlayerView> = {}): PlayerView => ({
  seat: 0, name: 'P0', life: 40, lost: false, library_size: 90, hand_size: 7, graveyard_size: 0,
  completed_dungeons: 0,
  hand: [], battlefield: [], graveyard: [], exile: [], pool: {},
  command: [], commanders: [], commander_casts: [], ...over,
});

/** every tile in the rendered html, with the attributes the reader's states are carried by */
function tiles(html: string): { index: string; state: string; zone: string; tax: string | null }[] {
  return [...html.matchAll(/data-commander="(\d+)"[^>]*?data-cmd-state="([a-z]+)" data-cmd-zone="([a-z]+)"/g)].map((m) => ({
    index: m[1],
    state: m[2],
    zone: m[3],
    tax: null,
  }));
}

/** elem returns one element with its content, matched by a data attribute ('' when absent). Same helper PlaySettingsPanel.svelte.test.ts uses. */
function elem(html: string, attr: string): string {
  const m = new RegExp(`<([a-z]+)[^>]*${attr.replace(/"/g, '\\"')}[^>]*>([\\s\\S]*?)</\\1>`).exec(html);
  return m === null ? '' : m[0];
}

describe('CommandArea — one seat"s commanders, on the board', () => {
  it('draws one tile per ROSTER commander in genesis order, not one per card in the zone', () => {
    const inZone = card(1, 'Isamaru', '1 W');
    const gone = card(2, 'Edgar', '2 W B');
    const p = player({ commanders: [inZone, gone], command: [inZone], graveyard: [gone] });
    const { html } = render(CommandArea, { props: { player: p } });
    expect(tiles(html).map((t) => t.index)).toEqual(['0', '1']);
    // the roster is never shrunk, so a commander that has LEFT the zone still
    // has a tile — that is the state the tile exists to show
    expect(html).toContain('data-cmd-zone="graveyard"');
  });

  it('draws command-zone and away tiles; the BATTLEFIELD commander is skipped so it renders once (ui10 bug 1)', () => {
    const inZone = card(1, 'Isamaru', 'W');
    const onBoard = card(2, 'Zur', '1 U U');
    const dead = card(3, 'Liliana', '2 B B');
    const p = player({
      commanders: [inZone, onBoard, dead],
      command: [inZone], battlefield: [onBoard], graveyard: [dead],
    });
    const { html } = render(CommandArea, { props: { player: p } });
    // the commander IN PLAY is a battlefield permanent — its real CardStack in
    // the creatures row is the one copy, so the command-zone tile is NOT drawn
    // for it (the duplication the demo showed).
    expect(html).not.toContain('data-cmd-state="battlefield"');
    expect(html).not.toContain('cmd-tile--battlefield');
    expect(html).not.toContain('>on the battlefield<');
    // the two states that DO get a tile: in the command zone, and anywhere else
    expect(tiles(html)).toEqual([
      { index: '0', state: 'command', zone: 'command', tax: null },
      { index: '2', state: 'away', zone: 'graveyard', tax: null },
    ]);
    expect(html).toContain('>command zone<');
    expect(html).toContain('>graveyard<');
    // …and the two states are drawn differently, not only labelled
    // differently: greyed art in the zone, and an empty box (no art) when away
    expect(html).toContain('cmd-tile--command');
    expect(html).toContain('cmd-tile--away');
  });

  it('BOTH rendered states carry the command-zone dotted box, and the away box is empty', () => {
    const inZone = card(1, 'Isamaru', 'W');
    const dead = card(3, 'Liliana', '2 B B');
    const p = player({
      commanders: [inZone, dead],
      command: [inZone], graveyard: [dead],
    });
    const { html } = render(CommandArea, { props: { player: p } });
    // one dotted command-zone box per rendered tile (command + away)
    expect(html.match(/cmd-zone-box/g)).toHaveLength(2);
    // away is an EMPTY box: no art element at all — not an <img>, not even
    // the blank's .card-image wrapper the command state still renders
    const awayHtml = html.slice(html.indexOf('cmd-tile--away'));
    expect(awayHtml).not.toContain('card-image');
  });

  it('away draws NO art — the card is not somewhere you can look — but still names and frames it', () => {
    const dead = card(3, 'Liliana', '2 B B');
    const p = player({ commanders: [dead], graveyard: [dead] });
    const { html } = render(CommandArea, { props: { player: p } });
    // no art element of any kind
    expect(html).not.toContain('<img');
    expect(html).not.toContain('card-image');
    // the box, the name and the state band all stay
    expect(html).toContain('cmd-zone-box');
    expect(html).toContain('>Liliana<');
    expect(html).toContain('>graveyard<');
  });

  it('with a roster the tiles render inside the command pack', () => {
    const c = card(1, 'Isamaru');
    const p = player({ commanders: [c], command: [c] });
    const { html } = render(CommandArea, { props: { player: p } });
    const pack = elem(html, 'data-cmd-pack');
    expect(pack).not.toBe('');
    expect(pack).toContain('data-commander="0"');
  });

  it('the empty away box is STILL inspectable and still names its card', () => {
    const dead = card(3, 'Liliana', '2 B B');
    const p = player({ commanders: [dead], graveyard: [dead] });
    const { html } = render(CommandArea, { props: { player: p } });
    // it is the NEW empty dotted box (the thing that replaced the greyed art)
    expect(html).toContain('cmd-zone-box');
    // focusable, describable, and names the commander even with no art
    expect(html).toContain('role="button"');
    expect(html).toContain('tabindex="0"');
    expect(html).toContain('aria-label=');
    expect(html).toContain('P0 — Liliana is in the graveyard');
  });

  it('exile, hand and an unseen zone are all the same "away" state — an unbrowsable zone is not an error', () => {
    const exiled = card(1, 'Isamaru');
    const held = card(2, 'Edgar');
    const hidden = card(3, 'Zur');
    const p = player({ commanders: [exiled, held, hidden], exile: [exiled], hand: [held] });
    const { html } = render(CommandArea, { props: { player: p } });
    expect(tiles(html).map((t) => `${t.state}:${t.zone}`)).toEqual(['away:exile', 'away:hand', 'away:library']);
  });

  it('a commander mid-cast is on the stack, not in the command zone, and prices no cast', () => {
    const cast = card(1, 'Isamaru', 'W');
    const p = player({ commanders: [cast], commander_casts: [1] });
    const { html } = render(CommandArea, {
      props: { player: p, stack: [{ id: 1, kind: 'spell', name: 'Isamaru', text: '', controller: 0, targets: [], card: cast, optional: false }] },
    });
    expect(tiles(html).map((t) => `${t.state}:${t.zone}`)).toEqual(['away:stack']);
    // the tax belongs to the NEXT cast from the command zone; there is no
    // command-zone cast to price while the spell is on the stack
    expect(html).not.toContain('data-tax=');
  });

  it('the tax is the derived CR 903.8 number — {2} × prior casts — and is drawn only in the command zone', () => {
    const three = card(1, 'Edgar', '2 W B');
    const none = card(2, 'Liliana', '2 B B');
    const p = player({ commanders: [three, none], command: [three, none], commander_casts: [3, 0] });
    const { html } = render(CommandArea, { props: { player: p } });
    expect(html).toContain('data-tax="6"');
    expect(html).toContain('data-casts="3"');
    // parenthesised — a derived value, marked as not the printed cost, in
    // the corner the printed cost occupies
    expect(html).toContain('>(+6)<');
    // three prior casts is a 6, never a 3
    expect(html).not.toContain('data-tax="3"');
    // …and a commander that has never been cast carries no chip at all
    expect(html.match(/data-tax=/g)).toHaveLength(1);
  });

  it('the tax renders in the mana-cost corner of the FACE (CZ3), not below it, and keeps its CR 903.8 tooltip', () => {
    const c = card(1, 'Edgar', '2 W B');
    const p = player({ commanders: [c], command: [c], commander_casts: [3] });
    const { html } = render(CommandArea, { props: { player: p } });
    // the tax span is a child of .face — it appears before .who in document
    // order, since .face is the FIRST thing the tile renders
    const faceIdx = html.indexOf('class="face');
    const taxIdx = html.indexOf('data-tax="6"');
    const whoIdx = html.indexOf('class="who');
    expect(faceIdx).toBeGreaterThanOrEqual(0);
    expect(taxIdx).toBeGreaterThan(faceIdx);
    expect(taxIdx).toBeLessThan(whoIdx);
    // marked as a derived, not printed, value: parenthesised
    expect(html).toContain('>(+6)<');
    expect(html).not.toContain('>+6<');
    // the specific CR 903.8 tooltip survives the move
    expect(html).toContain('title="Commander tax (CR 903.8): 6 generic on the next cast, for 3 prior casts"');
    // …and the tile's own accessible name still mentions it too
    expect(html).toContain('next cast pays 6 generic commander tax (CR 903.8)');
  });

  it('a commander on the battlefield is not a command-zone tile at all (ui10 bug 1)', () => {
    const c = card(1, 'Isamaru', 'W');
    const p = player({ commanders: [c], battlefield: [c], commander_casts: [2] });
    const { html } = render(CommandArea, { props: { player: p } });
    expect(html).not.toContain('data-cmd-state="battlefield"');
    expect(html).not.toContain('data-commander');
  });

  it('the next-cast cost keeps the printed cost and adds the tax as its own pip, never rewriting the card', () => {
    const c = card(1, 'Edgar', '2 W B');
    const p = player({ commanders: [c], command: [c], commander_casts: [2] });
    const { html } = render(CommandArea, { props: { player: p } });
    expect(html).toContain('data-next-cost="2 W B 4"');
  });

  it('EVERY rendered state is inspectable — command and away each open the same inspector', () => {
    const inZone = card(1, 'Isamaru', 'W');
    const onBoard = card(2, 'Zur', '1 U U');
    const hidden = card(3, 'Edgar', '2 W B');
    const p = player({ commanders: [inZone, onBoard, hidden], command: [inZone], battlefield: [onBoard] });
    const { html } = render(CommandArea, { props: { player: p } });
    // one focusable, describable trigger per rendered tile (command + away):
    // an opponent's commander must be readable at any time, wherever it is
    expect(html.match(/role="button"/g)).toHaveLength(2);
    expect(html.match(/tabindex="0"/g)).toHaveLength(2);
    expect(html.match(/aria-label="/g)).toHaveLength(2);
    // and each accessible name says whose it is and where it is
    expect(html).toContain('P0 — Isamaru is in the command zone');
    expect(html).toContain('P0 — Edgar is in the library');
    // the battlefield commander is not a tile here — its accessible name reads
    // from the creature row's permanent, not from a command-zone tile
    expect(html).not.toContain('P0 — Zur is on the battlefield');
    expect(html).not.toContain('data-cmd-state="battlefield"');
  });

  it('with no art resolved the tile is still the commander: name, cost and type line are on the face', () => {
    // CardImage's blank is the NORMAL state — cmd/gorged ships no catalog —
    // so a tile that only reads with Scryfall art would fail for most players
    const c = card(1, 'Isamaru', '1 W');
    const p = player({ commanders: [c], command: [c], commander_casts: [1] });
    const { html } = render(CommandArea, { props: { player: p } });
    expect(html).toContain('Isamaru');
    expect(html).toContain('Legendary Creature');
    // the name is also set OUTSIDE the face, at full ink, so it survives both
    // the blank's clipping and the two dimmed states
    expect(html).toContain('<div class="who');
    // the state band sits OUTSIDE the face, so it reads the same whichever
    // half CardImage drew; the tax overlays the face itself, in the corner a
    // printed cost would occupy, marked as computed with parentheses
    expect(html).toContain('command zone');
    expect(html).toContain('>(+2)<');
  });

  it('a constructed seat renders NOTHING — no tile, no slot, no frame, no heading', () => {
    const { html } = render(CommandArea, { props: { player: player() } });
    expect(html).not.toContain('data-command-area');
    expect(html).not.toContain('data-commander');
    expect(html.replace(/<!--[\s\S]*?-->/g, '').trim()).toBe('');
  });

  it('the private rim-pinned area is gone (CZ2): no private area element, no corner-dependent placement, no recess', () => {
    const c = card(1, 'Isamaru');
    const p = player({ commanders: [c], command: [c] });
    const { html } = render(CommandArea, { props: { player: p } });
    // CommandArea takes no `corner` prop any more — placement is entirely
    // the creatures row's, not this component's
    expect(html).not.toContain('data-command-area');
    expect(html).not.toContain('side-start');
    expect(html).not.toContain('side-end');
    expect(html).not.toContain('command-area');
  });
});

describe('the inspector opens on a commander tile in every state', () => {
  // SSR has no DOM, so no pointer event can open the panel and $effect never
  // runs: CommanderTile therefore accepts the HoverCard and anchor it would
  // otherwise build itself (the harness CardTile uses since CD1), and these
  // renders drive that shared instance into the open state the reader sees.
  const anchor: AnchorRect = { left: 120, top: 90, right: 200 };
  const statusFor = (over: Partial<PlayerView>, index = 0) =>
    commandZoneOf(player({ commanders: [card(7, 'Ghoulcaller Gisa', '3 B B')], commander_casts: [2], ...over }))[index];

  const cases: [string, Partial<PlayerView>][] = [
    ['command', { command: [card(7, 'Ghoulcaller Gisa', '3 B B')] }],
    ['battlefield', { battlefield: [card(7, 'Ghoulcaller Gisa', '3 B B')] }],
    ['away (graveyard)', { graveyard: [card(7, 'Ghoulcaller Gisa', '3 B B')] }],
    ['away (a zone this viewer cannot see into)', {}],
  ];

  for (const [name, over] of cases) {
    it(`${name}: the full card face and the engine's ledger open from the tile`, () => {
      const hover = new HoverCard();
      hover.open(7);
      const { html } = render(CommanderTile, { props: { status: statusFor(over), player: 'P0', seat: 0, hover, anchor } });
      expect(html).toContain('card-detail');
      expect(html).toContain('Ghoulcaller Gisa');
      expect(html).toContain('#7'); // the panel names the object it describes
      expect(html).toContain('id="card-detail-7"');
      expect(html).toContain('aria-describedby="card-detail-7"');
    });
  }

  it('a closed tile renders no panel — the inspector is opened, never permanently mounted', () => {
    const { html } = render(CommanderTile, {
      props: { status: statusFor({ graveyard: [card(7, 'Ghoulcaller Gisa', '3 B B')] }), player: 'P0', seat: 0, anchor },
    });
    expect(html).not.toContain('card-detail');
    expect(html).not.toContain('aria-describedby');
  });

  it("the panel's lifetime follows the object: a tile rendering a different commander closes it (CD1)", () => {
    const hover = new HoverCard();
    hover.open(7);
    expect(hover.superviseRendering(9)).toBe(true);
  });
});

describe('the board draws one commander tile per roster commander, inline in each seat"s creatures row', () => {
  const seats: SeatInfo[] = [0, 1, 2, 3].map((i) => ({ name: `S${i}`, deck: `deck-${i}`, colour: `#00000${i}` }));
  const view = (): View => ({
    viewer: 4, visibility: 'public', turn: 5, round: 5, step: 'main1', phase: 'main1', active: 0, priority: 0,
    over: false, draw: false, winner: null, stack: [], pending: [],
    players: [
      // seat 0: commander still in the zone, one prior cast
      player({ seat: 0, name: 'S0', commanders: [card(10, 'Isamaru', 'W')], commander_casts: [1], command: [card(10, 'Isamaru', 'W')] }),
      // seat 1: commander already on the battlefield — in play, so full art
      player({ seat: 1, name: 'S1', commanders: [card(11, 'Zur', '1 U U')], commander_casts: [2], battlefield: [card(11, 'Zur', '1 U U')] }),
      // seat 2: two commanders, one in the zone after three prior casts
      player({
        seat: 2, name: 'S2',
        commanders: [card(12, 'Edgar', '2 W B'), card(13, 'Liliana', '2 B B')],
        commander_casts: [3, 0],
        command: [card(12, 'Edgar', '2 W B')], graveyard: [card(13, 'Liliana', '2 B B')],
      }),
      // seat 3: no commanders at all — the format-less seat
      player({ seat: 3, name: 'S3' }),
    ],
  });

  it('draws a tile for every seat that has a roster AND a commander not in play; a battlefield commander is not a tile (ui10 bug 1)', () => {
    const { html } = render(Board, { props: { view: view(), seats } });
    // each cmd-tile carries the owning seat as its own data-seat attribute
    // now that there is no wrapping area to carry it instead (CZ2)
    const bySeat = [...html.matchAll(/data-cmd-state="[a-z]+" data-cmd-zone="[a-z]+" data-seat="(\d)"/g)].map((m) => m[1]);
    // seat 0: one tile (in the zone). seat 1: NONE — its commander is on the
    // battlefield, so the real permanent is the one copy. seat 2: two tiles
    // (its roster order, both not in play). seat 3: none.
    // (DOM order is the table's arrangement: the spectator's bottom seat, 0,
    // is drawn after the opponents' area.)
    expect([...bySeat].sort()).toEqual(['0', '2', '2']);
  });

  it('a constructed table draws no commander tiles anywhere on the board', () => {
    const v = view();
    v.players = v.players.map((p) => ({ ...p, commanders: [], command: [], commander_casts: [] }));
    const { html } = render(Board, { props: { view: v, seats } });
    expect(html).not.toContain('data-command-area');
    expect(html).not.toContain('data-commander');
    expect(html).not.toContain('data-cmd-state');
  });

  it('the tax on the board is the derived number for the seat that owns it', () => {
    const { html } = render(Board, { props: { view: view(), seats } });
    expect(html).toContain('data-tax="6"'); // seat 2's Edgar, three prior casts
    expect(html).toContain('data-tax="2"'); // seat 0's Isamaru, one prior cast
    expect(html).not.toContain('data-tax="3"');
    // seat 1's commander is on the battlefield and seat 2's Liliana has never
    // been cast: two tiles, no chips
    expect(html.match(/data-tax=/g)).toHaveLength(2);
  });

  it('the commander on the battlefield renders ONCE — as the permanent in the creature row, not again as a command-zone tile (ui10 bug 1)', () => {
    const { html } = render(Board, { props: { view: view(), seats } });
    // no command-zone tile for the seat-1 battlefield commander
    expect(html).not.toContain('data-cmd-state="battlefield"');
    expect(html).not.toContain('cmd-tile--battlefield');
    expect(html).not.toContain('S1 — Zur is on the battlefield');
    // the permanent itself is still drawn as a battlefield CardStack, exactly once
    expect(html).toMatch(/data-obj="11"/);
    expect(html.match(/data-obj="11"/g)).toHaveLength(1);
  });
});

describe('the rail no longer carries the command zone', () => {
  const seats: SeatInfo[] = [0, 1].map((i) => ({ name: `S${i}`, deck: `deck-${i}`, colour: `#00000${i}` }));
  const view = (): View => ({
    viewer: 4, visibility: 'public', turn: 5, round: 5, step: 'main1', phase: 'main1', active: 0, priority: 0,
    over: false, draw: false, winner: null, stack: [], pending: [],
    players: [
      player({ seat: 0, name: 'S0', commanders: [card(10, 'Isamaru', 'W')], commander_casts: [1], command: [card(10, 'Isamaru', 'W')] }),
      player({ seat: 1, name: 'S1', commanders: [card(11, 'Zur', '1 U U')], commander_casts: [2], battlefield: [card(11, 'Zur', '1 U U')] }),
    ],
  });

  it('a Commander table renders no Commanders section, no command rows and no tax in the rail', () => {
    const { html } = render(Rail, { props: { view: view(), seats, decision: null } });
    expect(html).not.toContain('>Commanders<');
    expect(html).not.toContain('data-command-zone');
    expect(html).not.toContain('data-commander');
    expect(html).not.toContain('data-tax=');
  });
});
