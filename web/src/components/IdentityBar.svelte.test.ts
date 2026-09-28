import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { CardView, Decision, Option, PlayerView, SeatInfo } from '../protocol';
import { optionsByObj, optionsByPlayer, type CardOptions } from '../lib/cardoptions';
import IdentityBar from './IdentityBar.svelte';

const player = (over: Partial<PlayerView> = {}): PlayerView => ({
  seat: 0, name: 'Ari', life: 38, lost: false, library_size: 30, hand_size: 7, graveyard_size: 0,
  completed_dungeons: 0,
  hand: [], battlefield: [], graveyard: [], exile: [], pool: {}, available: {},
  command: [], commanders: [], commander_casts: [], ...over,
});

const bar = (p: PlayerView, seat?: SeatInfo) =>
  render(IdentityBar, { props: { player: p, seat, players: [p], colour: '#e5484d', active: false, priority: false, corner: 'bl' } }).html;

describe('IdentityBar — the three-line box (B1 / I-10)', () => {
  it('renders the name and life on one line, the zone counts on one row, and the mana pool on the third', () => {
    const html = bar(player({ life: 38, pool: { U: 2, B: 1 }, available: { W: 1 } }));
    // line 1: name then life bubble
    expect(html).toContain('>Ari<');
    expect(html).toContain('>38<');
    // line 2: library / hand / graveyard counts
    expect(html).toContain('library 30');
    expect(html).toContain('hand 7');
    expect(html).toContain('graveyard 0');
    // line 3: the mana readout — available (hollow) bubbles plus the
    // floating pool (solid), both present and both named
    expect(html).toContain('data-mana-available');
    expect(html).toContain('data-avail="W"');
    expect(html).toContain('data-mana="U"');
    expect(html).toContain('data-mana="B"');
    // the dropped features are gone: no commander-damage clock, no deck line
    expect(html).not.toContain('data-commander-damage');
    expect(html).not.toContain('Commander');
  });

  it('a public spectator (null pool) still draws the public available mana, and the two never conflate', () => {
    // The whole point of the feature: a spectator has a null pool for every
    // seat, but available mana derives from the public battlefield and so
    // still populates line 3. The floating pool stays hollow-absent while
    // the available group appears.
    const html = bar(player({ pool: null as unknown as Record<string, number>, available: { W: 3 } }));
    expect(html).toContain('data-mana-row');
    expect(html).toContain('data-mana-available');
    expect(html).toContain('data-avail="W"');
    expect(html).not.toContain('data-mana-pool');
    expect(html).toContain('">3<');
  });

  it('available and floating mana are rendered as distinct, clearly named groups on one line', () => {
    const html = bar(player({ pool: { R: 1 }, available: { G: 2, W: 1 } }));
    expect(html).toContain('data-mana-available');
    expect(html).toContain('data-mana-pool');
    expect(html).toMatch(/Available by tapping: /);
    expect(html).toMatch(/Mana pool: /);
    // The divider between the two groups prevents a reader from reading them
    // as one combined pile.
    expect(html).toContain('data-mana-sep');
  });

  it('full name never truncated when within the 10-char display limit', () => {
    const html = bar(player({ name: 'Ari' }));
    expect(html).toContain('>Ari<');
    expect(html).not.toContain('…');
  });

  it('a long name truncates to 10 characters in display but keeps the full name in the title', () => {
    const html = bar(player({ name: 'Avery Longplayer Name' }));
    expect(html).toContain('data-player-name="Avery Longplayer Name"');
    expect(html).toContain('title="Avery Longplayer Name"');
    // the visible text is clipped
    expect(html).toContain('Avery Long…');
    expect(html).not.toContain('>Avery Longplayer Name<');
  });

  it('exactly 10 characters is the last untruncated name; 11 clips', () => {
    expect(bar(player({ name: 'ABCDEFGHIJ' }))).toContain('>ABCDEFGHIJ<');
    expect(bar(player({ name: 'ABCDEFGHIJ' }))).not.toContain('…');
    const over = bar(player({ name: 'ABCDEFGHIJK' }));
    expect(over).toContain('>ABCDEFGHIJ…<');
    expect(over).toContain('title="ABCDEFGHIJK"');
  });
});

describe('IdentityBar — the pile affordances (fb-20260916T225802Z)', () => {
  const card = (id: number): CardView => ({
    id, name: `Card ${id}`, types: 'Instant', printing: { name: `Card ${id}` }, token: `#${id}`,
    tapped: false, power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0,
    summon_sick: false,
  });
  const opt = (index: number, obj: number | undefined, kind = 'cast', label = `option ${index}`): Option => ({
    index, kind, label, obj, player: 0,
  });
  const bundleFor = (options: Option[], tone: 'initiative' | 'offered'): CardOptions => {
    const d: Decision = { seq: 1, player: 0, kind: 'priority', prompt: 'pile', min: 0, max: 1, options };
    return { byObj: optionsByObj(d), byPlayer: optionsByPlayer(d), picked: [], tone, post: () => {} };
  };

  it('a full graveyard and exile render as pile buttons in the SeatTable label style; empty zones stay plain counts', () => {
    const html = bar(player({ graveyard: [card(1), card(2)], graveyard_size: 2, exile: [card(9)], library_size: 30 }));
    // both counts are buttons, with the rail's accessible-label style
    expect(html).toContain('data-pile="graveyard"');
    expect(html).toContain('data-pile="exile"');
    expect(html).toMatch(/View Ari(&#39;|')s graveyard \(2 cards\)/);
    expect(html).toMatch(/View Ari(&#39;|')s exile \(1 card\)/);
    // the counts row still shows all four zones, library and hand as text
    expect(html).toContain('library 30');
    expect(html).toContain('hand 7');
  });

  it('a redacted zone (null list) is a true count with no button and no glow (the zonesFor null-guard)', () => {
    // a redacted graveyard still publishes its _size (3), so the plain count
    // reads 3; a redacted exile has no size field and reads 0. Neither
    // renders a button (no card list to open) and neither glows — no crash.
    const html = bar(player({ graveyard: null as unknown as CardView[], graveyard_size: 3, exile: null as unknown as CardView[] }));
    expect(html).toContain('graveyard 3');
    expect(html).toContain('exile 0');
    expect(html).not.toContain('data-pile="graveyard"');
    expect(html).not.toContain('data-pile="exile"');
  });

  it('a pile the pending decision touches wears the tone ring; an untouched pile wears none', () => {
    // the byObj ∩ zone-card-ids rule: card 1 sits in this seat's graveyard
    // and the decision offers a cast on it → the graveyard icon glows with
    // the SAME register the tiles read (toneOf's initiative/offered); the
    // exile pile is untouched → no data-tone at all.
    const bundle = bundleFor([opt(7, 1, 'cast', 'Flashback Card 1'), opt(99, undefined, 'pass')], 'offered');
    const html = render(IdentityBar, {
      props: {
        player: player({ graveyard: [card(1)], graveyard_size: 1, exile: [card(9)] }),
        players: [player({ graveyard: [card(1)], graveyard_size: 1 })],
        colour: '#e5484d', active: false, priority: false, corner: 'bl', options: bundle,
      },
    }).html;
    const gyAt = html.indexOf('data-pile="graveyard"');
    const exAt = html.indexOf('data-pile="exile"');
    expect(gyAt).toBeGreaterThan(-1);
    expect(exAt).toBeGreaterThan(gyAt);
    expect(html.slice(gyAt, exAt)).toContain('data-tone="offered"');
    expect(html.slice(exAt)).not.toContain('data-tone="offered"');
    expect(html.slice(exAt)).not.toContain('data-tone="initiative"');
  });

  it('an initiative decision (no pass) glows the touched pile with the initiative ring', () => {
    const bundle = bundleFor([opt(21, 9, 'permanent', 'Target Card 9')], 'initiative');
    const html = render(IdentityBar, {
      props: {
        player: player({ graveyard: [card(1)], graveyard_size: 1, exile: [card(9)] }),
        players: [player()],
        colour: '#e5484d', active: false, priority: false, corner: 'bl', options: bundle,
      },
    }).html;
    // NOT gated by pile owner: the target here is on a card in this seat's
    // own exile, but the rule is the same for an opponent's pile (pinned
    // end-to-end in PileModal.svelte.test.ts's identity fixture).
    const exAt = html.indexOf('data-pile="exile"');
    expect(exAt).toBeGreaterThan(-1);
    expect(html.slice(exAt)).toContain('data-tone="initiative"');
    const gyAt = html.indexOf('data-pile="graveyard"');
    expect(html.slice(gyAt, exAt)).not.toContain('data-tone="initiative"');
  });

  it('no pending decision means no ring anywhere, but the buttons still read', () => {
    const html = bar(player({ graveyard: [card(1)], graveyard_size: 1, exile: [card(9)] }));
    expect(html).toContain('data-pile="graveyard"');
    expect(html).toContain('data-pile="exile"');
    expect(html).not.toContain('data-tone="initiative"');
    expect(html).not.toContain('data-tone="offered"');
  });
});

describe('IdentityBar — the active seat is a full perimeter in its OWN colour', () => {
  it('the active class carries the seat colour var the perimeter/glow rules key off', () => {
    const html = render(IdentityBar, {
      props: { player: player(), players: [player()], colour: '#3b82f6', active: true, priority: false, corner: 'bl' },
    }).html;
    expect(html).toMatch(/class="identity[^"]*\bactive\b/);
    expect(html).toContain('--seat:#3b82f6');
  });

  it('an inactive seat carries no active class, though the same --seat var is still set', () => {
    const html = render(IdentityBar, {
      props: { player: player(), players: [player()], colour: '#3b82f6', active: false, priority: false, corner: 'bl' },
    }).html;
    expect(html).not.toMatch(/class="identity[^"]*\bactive\b/);
    expect(html).toContain('--seat:#3b82f6');
  });
});

describe('IdentityBar — an eliminated seat (Task 3: "there are 2 dead players, but their health doesn\'t reflect 0")', () => {
  it('a lost seat reads ELIMINATED-looking: struck through, without touching the life bubble', () => {
    // life untouched is the point: commander damage, an empty library and a
    // concession all end a game with life wherever it happened to be
    const html = bar(player({ life: 39, lost: true }));
    expect(html).toContain('>39<'); // life is reported exactly as it is, not forced to 0
  });
});
