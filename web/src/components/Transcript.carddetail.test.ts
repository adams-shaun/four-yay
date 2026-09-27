import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { DvrState } from '../lib/dvr';
import { buildCardOwnerColour } from '../lib/logrender';
import { cardById, everyVisibleCard } from '../lib/board';
import type { CardView, PlayerView } from '../protocol';
import { CardHover } from '../lib/carddetail.svelte';
import Transcript from './Transcript.svelte';

/**
 * The transcript's card-name hover preview (fb-20260927T154603Z), SSR half.
 *
 * The pointer interaction itself is proved in real Chromium by
 * Transcript.hover.test.ts; this harness has no DOM, no pointer events and no
 * $effect, so it asserts the RENDER-TIME contract:
 *   - the id -> CardView lookup (cardById, lib/board.ts) resolves a visible
 *     object and degrades to null for one that is absent or redacted;
 *   - a card span whose id resolves is a panel trigger (carries tabindex),
 *     and one whose id does not keeps today's title-only span unchanged;
 *   - with no `cards` prop at all the render is byte-for-byte the old one
 *     (back-compat);
 *   - when the one shared CardHover is open the panel renders at the
 *     transcript CONTAINER level, never inside a row <button>.
 */

const card = (over: Partial<CardView> & { id: number }): CardView =>
  ({
    name: 'Island',
    types: 'Land',
    printing: { name: 'Island' },
    token: '',
    tapped: false,
    power: 0,
    toughness: 0,
    damage: 0,
    attacking: false,
    controller: 0,
    owner: 0,
    summon_sick: false,
    ...over,
  }) as CardView;

const PALETTE = ['#e5484d', '#3b82f6'] as const;
const seatColourOf = (seat: number): string => PALETTE[seat] ?? '#777777';
const identities = [
  { name: 'Ann', colour: '#e5484d' },
  { name: 'Bob', colour: '#3b82f6' },
];

const ev = (seq: number, kind: string, line: string) => ({ event: { seq, kind, player: -1 }, line });
const dvr = (events: ReturnType<typeof ev>[]): DvrState => ({
  match: 'm1', head: events.length, cursor: events.length, live: true, events, turnStarts: [], gap: false,
});

const JACE = card({ id: 4, name: 'Jace, the Mind Sculptor', types: 'Legendary Planeswalker Jace', owner: 0 });
const BOLT = card({ id: 30, name: 'Lightning Bolt', types: 'Instant', owner: 1 });
const CARDS = [JACE, BOLT];
const cardColour = buildCardOwnerColour(CARDS, seatColourOf);

const renderLog = (d: DvrState, cards?: CardView[]) =>
  render(Transcript, {
    props: { dvr: d, onSeek: () => {}, identities, cardColour, ...(cards ? { cards } : {}) },
  }).html;

describe('cardById — the transcript id -> CardView lookup', () => {
  it('resolves a visible object and degrades to null for an absent id', () => {
    expect(cardById(CARDS, 4)?.name).toBe('Jace, the Mind Sculptor');
    expect(cardById(CARDS, '30')?.name).toBe('Lightning Bolt');
    // precondition: the two values compared really differ (one resolves, one does not)
    expect(cardById(CARDS, 4)).not.toBeNull();
    expect(cardById(CARDS, 999)).toBeNull();
  });

  it('refuses the redacted id 0 and non-ids rather than inventing a card', () => {
    // describe.go renders id 0 as "a card"; it names no single object.
    expect(cardById(CARDS, 0)).toBeNull();
    expect(cardById(CARDS, -1)).toBeNull();
    expect(cardById(CARDS, 'abc')).toBeNull();
    // and it defends the public-spectator null list
    expect(cardById(null, 4)).toBeNull();
  });
});

describe('Transcript — card-name hover trigger (fb-20260927T154603Z)', () => {
  it('a card whose id is in the view becomes a panel trigger; the id absent from the view keeps today\'s title-only span', () => {
    const d = dvr([
      ev(1, 'put_on_stack', 'Ann casts Jace, the Mind Sculptor #4'),
      ev(2, 'put_on_stack', 'Bob casts Lightning Bolt #999'),
    ]);
    const html = renderLog(d, CARDS);

    // Resolved (id 4): the span is a focusable trigger, so keyboard focus can
    // open the panel.
    expect(html).toContain('title="Jace, the Mind Sculptor #4"');
    expect(html).toMatch(/class="obj card[^"]*"[^>]*tabindex="0"/);
    expect(html).toContain('Jace, the Mind Sculptor');

    // Degraded (id 999 absent): the SAME visible text and title, but no
    // trigger — no panel, no invented card.
    expect(html).toContain('title="Lightning Bolt #999"');
    expect(html).not.toMatch(/class="obj card[^"]*"[^>]*title="Lightning Bolt #999"[^>]*tabindex/);
    expect(html).toContain('Lightning Bolt');
  });

  it('degrades the redacted "a card" reference (id 0) — no trigger', () => {
    const html = renderLog(dvr([ev(1, 'move_zone', 'Ann plays a card #0')]), CARDS);
    expect(html).toContain('a card');
    expect(html).not.toMatch(/tabindex="0"/);
  });

  it('with no cards prop the render is unchanged (back-compat): title-only spans, no trigger', () => {
    const d = dvr([ev(1, 'put_on_stack', 'Ann casts Jace, the Mind Sculptor #4')]);
    const html = renderLog(d);
    // the old affordance is intact and nothing became a trigger
    expect(html).toContain('title="Jace, the Mind Sculptor #4"');
    expect(html).toContain('Jace, the Mind Sculptor');
    expect(html).not.toMatch(/tabindex="0"/);
    expect(html).not.toContain('card-detail');
  });

  it('an open shared CardHover renders ONE panel at the container level, outside every row button', () => {
    // Drive the exact CardHover the component owns through an injected one,
    // the way CardTile's SSR test seeds its panel: this harness has no DOM, so
    // there are no pointer events to fire.
    const hover = new CardHover();
    hover.card = JACE;
    hover.anchor = { left: 0, top: 0, right: 10 };
    hover.hover.show = true;
    const html = render(Transcript, {
      props: {
        dvr: dvr([ev(1, 'put_on_stack', 'Ann casts Jace, the Mind Sculptor #4')]),
        onSeek: () => {},
        identities,
        cardColour,
        cards: CARDS,
        hover,
      },
    }).html;

    // precondition: the panel really rendered (not vacuously "outside" a button)
    expect(html).toContain('class="card-detail');
    expect(html).toContain('id="card-detail-4"');

    // exactly one panel, and it is NOT inside any row <button>: it follows the
    // last row's closing tag (the two toggles in the bar are earlier buttons).
    expect(html.match(/class="card-detail/g)?.length).toBe(1);
    const panelIdx = html.indexOf('class="card-detail');
    const lastRowClose = html.lastIndexOf('</button>');
    expect(lastRowClose).toBeGreaterThan(-1);
    expect(panelIdx).toBeGreaterThan(lastRowClose);
  });

  it('everyVisibleCard feeds the transcript the whole visible set (the Table wiring)', () => {
    // The wire shape Table flattens: a public-spectator null hand must not
    // drop the battlefield, and the resolved id opens the panel path.
    const players = [
      { seat: 0, hand: null, battlefield: [{ ...card({ id: 20, name: 'Grizzly Bears', types: 'Creature Bear', owner: 0 }) }], graveyard: [], exile: [], command: [], commanders: [] },
    ] as unknown as PlayerView[];
    const cards = everyVisibleCard(players);
    expect(cards.map((c) => c.id)).toEqual([20]);
    expect(cardById(cards, 20)?.name).toBe('Grizzly Bears');
    // the resolver and the lookup are built over the SAME flattened list, as
    // Table.svelte wires them
    const colours = buildCardOwnerColour(cards, seatColourOf);
    const html = render(Transcript, {
      props: {
        dvr: dvr([ev(1, 'land_played', 'Ann plays Grizzly Bears #20')]),
        onSeek: () => {},
        identities,
        cardColour: colours,
        cards,
      },
    }).html;
    expect(html).toMatch(/class="obj card[^"]*"[^>]*tabindex="0"/);
  });
});
