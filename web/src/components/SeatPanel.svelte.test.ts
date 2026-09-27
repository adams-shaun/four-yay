import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import starterFixture from '../fixtures/mulligan-starter.json';
import type { CardView, Decision, Option, PaymentPlan, PlayerView, SeatInfo, View } from '../protocol';
import { SeatPanelState } from '../lib/seatpanel.svelte';
import SeatPanel from './SeatPanel.svelte';

// SSR via svelte/server, the repo's component-test pattern: onMount and
// $effect never run, so the panel renders from the view it was handed and
// nothing reaches the network. ./api is stubbed anyway so an accidental
// fetch would fail loudly rather than hang.
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  postIntent: vi.fn(),
  fetchPending: vi.fn(),
}));
// images.url resolves the Scryfall art; in SSR the promise never settles
// before render, so CardImage draws its text face. Stubbed to keep the test
// off the network entirely.
vi.mock('../lib/images', () => ({ images: { url: () => new Promise<string | null>(() => {}), offline: () => false } }));

const ctx = { seat: 1, token: 'tok' };
const seats: SeatInfo[] = [
  { name: 'alice', deck: 'burn', colour: '#e5484d' },
  { name: 'bob', deck: 'stompy', colour: '#30a46c' },
];

const card = (id: number, name: string): CardView => ({
  id, name, types: 'Creature Bear', printing: { name }, token: `#${id}`,
  tapped: false, power: 2, toughness: 2, damage: 0, attacking: false,
  controller: 1, owner: 1, summon_sick: false,
});

const player = (seat: number, hand: CardView[], pool: Record<string, number> = {}): PlayerView => ({
  seat, name: seats[seat].name, life: 20, lost: false, library_size: 53,
  hand_size: hand.length, graveyard_size: 0, hand, battlefield: [], graveyard: [], exile: [], pool, command: [], commanders: [], commander_casts: [],
});

const opt = (index: number, kind: string, label: string, obj?: number): Option =>
  ({ index, kind, label, obj, player: 1 });

// Seat 1 is the viewer and deliberately sits SECOND in the players array, so
// a panel indexing by array position instead of by seat number reads alice's
// row and the tests catch it.
const view = (decision: Decision | null, hand: CardView[] = [], pool: Record<string, number> = {}): View => ({
  viewer: 1, visibility: 'seat', turn: 3, round: 3, step: 'upkeep', phase: 'beginning',
  active: 0, priority: 1, over: false, draw: false, winner: null,
  players: [player(0, []), player(1, hand, pool)],
  stack: [], pending: [], decision,
});

const props = (v: View) => ({ view: v, seats, ctx, table: 't1', match: 1 });

const priority: Decision = {
  seq: 5, player: 1, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
  options: [opt(0, 'cast', 'Cast Grizzly Bears'), opt(1, 'pass', 'Pass priority'), opt(2, 'concede', 'Concede')],
};
const keepAsk: Decision = {
  seq: 6, player: 1, kind: 'mulligan', min: 1, max: 1,
  prompt: 'London mulligan: bob keeps 7 and bottoms 1, or mulligans',
  options: [opt(0, 'keep', 'keep'), opt(1, 'mulligan', 'mulligan')],
};
const hand = [card(101, 'Grizzly Bears'), card(102, 'Forest'), card(103, 'Llanowar Elves')];
const bottomAsk: Decision = {
  seq: 7, player: 1, kind: 'mulligan', min: 2, max: 2,
  prompt: 'London mulligan: bob bottoms 2 card(s)',
  options: hand.map((c, j) => opt(j, 'bottom', c.name, c.id)),
};

describe('SeatPanel — the test contract', () => {
  it('a live decision carries every data hook the seat surface is tested through', () => {
    const { html } = render(SeatPanel, { props: props(view(priority)) });
    for (const hook of ['data-seat-panel', 'data-prompt', 'data-options', 'data-option="0"', 'data-primary', 'data-concede']) {
      expect(html).toContain(hook);
    }
  });

  // ui15: the live table route hands the panel a view whose players carry
  // names but a `seats` array that is EMPTY (the one-shot seed in
  // Table.svelte races the async tables.lookup). The wait-the-priority line
  // must fall back to the wire's player name, not the 0-based `Seat N`
  // placeholder.
  it('names the priority player from the view when seats is empty (ui15 discrimination)', () => {
    // seat 2 is deliberately absent from the module-scope `seats` fixtures
    // (only 0/1 exist), so it is built by hand rather than through the helper
    // that reads seats[seat].name.
    const third: PlayerView = { ...player(1, []), seat: 2, name: 'Player 3' };
    const v = {
      ...view(null),
      priority: 2,
      players: [player(0, []), player(1, []), third],
    };
    const { html } = render(SeatPanel, { props: { view: v, seats: [], ctx, table: 't1', match: 1 } });
    expect(html).toContain('waiting for Player 3');
    expect(html).not.toContain('Seat 2');
  });

  it('with nothing to answer the panel waits, and names who on', () => {
    const { html } = render(SeatPanel, { props: props(view(null)) });
    expect(html).toContain('data-waiting');
    expect(html).toContain('waiting for bob'); // view.priority is seat 1
    expect(html).not.toContain('data-prompt');
  });

  it('concede is metadata only here: it is filtered from the flyout option list for the page-level control', () => {
    const { html } = render(SeatPanel, { props: props(view(priority)) });
    expect(html).toContain('data-concede="2"');
    expect(html).not.toContain('>Concede<');
    expect(html).not.toContain('data-confirm-concede');
  });

  it('the primary is the pass option resolved by kind, never the last option', () => {
    const { html } = render(SeatPanel, { props: props(view(priority)) });
    const primaryLabel = /data-primary[^>]*>\s*([^<]+?)\s*</.exec(html)?.[1];
    expect(primaryLabel).toBe('Pass priority');
    expect(primaryLabel).not.toBe('Concede');
  });
});

describe('SeatPanel — payment plans', () => {
  const plan = (id: string): PaymentPlan => ({
    version: 1, id, cost: { generic: 0, mana: [0, 1, 0, 0, 0, 0] },
    activations: [{ source: 11, source_zone_seq: 3, ability: { kind: 'intrinsic', intrinsic: 'basic_land' }, produces: [0, 1, 0, 0, 0, 0] }],
    pool_spend: [0, 0, 0, 0, 0, 0], pool_after: [0, 0, 0, 0, 0, 0],
  });

  it('groups a legacy cast with its ordered plan list and leaves planned-only casts manual-aware', () => {
    const d: Decision = {
      ...priority,
      payment_actions: [
        { id: 'a', cast: { object: 101, face: 0, origin: 'hand' }, base_option_index: 0, label: 'Cast Grizzly Bears', plans: [plan('first'), plan('second')] },
        { id: 'b', cast: { object: 102, face: 0, origin: 'hand' }, label: 'Cast Future Spell', plans: [plan('third')] },
      ],
      payment_fallback: { plan_id: 'first', reason: 'source_changed' },
    };
    const state = new SeatPanelState('t1', 1, ctx, null, null);
    state.setAutoManaAvailable(true);
    state.setAutoPayMana(true);
    state.adoptView(d);
    const html = render(SeatPanel, { props: { ...props(view(d)), state } }).html;
    expect(html.match(/Cast Grizzly Bears/g)).toHaveLength(1);
    expect(html).toContain('data-payment-plan="first"');
    expect(html).toContain('data-payment-plan="second"');
    expect(html).not.toContain('data-payment-manual="a"');
    expect(html).toContain('data-payment-manual-needed');
    expect(html).toContain('data-payment-fallback');
  });

  it('removes suggested-payment actions when Auto Mana is off and restores the ordinary cast', () => {
    const d: Decision = {
      ...priority,
      payment_actions: [{ id: 'a', cast: { object: 101, face: 0, origin: 'hand' }, base_option_index: 0, label: 'Cast Grizzly Bears', plans: [plan('first')] }],
    };
    const html = render(SeatPanel, { props: props(view(d)) }).html;
    expect(html).not.toContain('data-payment-plan');
    expect(html).toContain('data-option="0"');
    expect(html).toContain('Cast Grizzly Bears');
  });
});

describe('SeatPanel — tone', () => {
  it('a priority window is cool: passing is a legal answer', () => {
    expect(render(SeatPanel, { props: props(view(priority)) }).html).toContain('data-tone="offered"');
  });

  it('a decision the game is blocked on is warm', () => {
    expect(render(SeatPanel, { props: props(view(keepAsk, hand)) }).html).toContain('data-tone="initiative"');
  });

  it('waiting on someone else carries no state colour at all', () => {
    expect(render(SeatPanel, { props: props(view(null)) }).html).toContain('data-tone="idle"');
  });
});

describe('SeatPanel — the mana pool', () => {
  it("reads THIS seat's pool by seat number, not by position in the players array", () => {
    const { html } = render(SeatPanel, { props: props(view(priority, [], { G: 2 })) });
    expect(html).toContain('data-mana="G"');
    expect(html).toContain('data-mana-pool');
  });

  it('an empty pool puts nothing on the panel', () => {
    expect(render(SeatPanel, { props: props(view(priority)) }).html).not.toContain('data-mana-pool');
  });

  it('a view with no row for this seat renders the panel without a pool rather than throwing', () => {
    const v = view(priority);
    v.players = [player(0, [])];
    const { html } = render(SeatPanel, { props: props(v) });
    expect(html).toContain('data-seat-panel');
    expect(html).not.toContain('data-mana-pool');
  });
});

describe('SeatPanel — the mulligan round', () => {
  it('the keep half draws the hand as cards and the two choices as buttons, keyed by option kind', () => {
    const { html } = render(SeatPanel, { props: props(view(keepAsk, hand)) });
    expect(html).toContain('data-prompt');
    expect(html).toContain('data-options');
    // the hand, as cards: CardTile's per-object anchor, one per card
    for (const c of hand) expect(html).toContain(`data-obj="${c.id}"`);
    // the choices, labelled by what pressing them does
    expect(html).toContain('data-option="0"');
    expect(html).toContain('Keep this hand');
    expect(html).toContain('data-option="1"');
    expect(html).toContain('Mulligan');
    // no generic scrolling option list, and no primary: a mulligan has no pass
    expect(html).not.toContain('data-primary');
  });

  it('a spent mulligan allowance leaves the single keep choice', () => {
    const d: Decision = { ...keepAsk, options: [opt(0, 'keep', 'keep')] };
    const { html } = render(SeatPanel, { props: props(view(d, hand)) });
    expect(html).toContain('Keep this hand');
    expect(html).not.toContain('data-option="1"');
  });

  it('the bottoming half makes every card a control, with the submit that commits them', () => {
    const { html } = render(SeatPanel, { props: props(view(bottomAsk, hand)) });
    expect([...html.matchAll(/data-option="\d+"/g)]).toHaveLength(hand.length);
    expect(html).toContain('aria-pressed="false"');
    expect(html).toContain('data-submit');
    expect(html).toContain('Bottom 2 cards');
    // each control names its card for a screen reader
    for (const c of hand) expect(html).toContain(`aria-label="${c.name}"`);
  });

  it('bottoming one card says card, not cards', () => {
    const d: Decision = { ...bottomAsk, min: 1, max: 1 };
    expect(render(SeatPanel, { props: props(view(d, hand)) }).html).toContain('Bottom 1 card<');
  });

  // A bottoming option whose obj is not in the hand still has to be
  // answerable: the option, not the hand, is the contract.
  it('an option with no matching card falls back to its label and stays clickable', () => {
    const d: Decision = { ...bottomAsk, options: [opt(0, 'bottom', 'Unknown Card', 999)] };
    const { html } = render(SeatPanel, { props: props(view(d, hand)) });
    expect(html).toContain('data-option="0"');
    expect(html).toContain('Unknown Card');
  });

  // FL-101: the layout is driven by kinds. An option kind this body does not
  // cover sends the whole decision to the generic list, where it is reachable.
  it('an unrecognised option kind falls back to the generic list rather than being dropped', () => {
    const d: Decision = { ...keepAsk, options: [opt(0, 'keep', 'keep'), opt(1, 'mulligan', 'mulligan'), opt(2, 'surprise', 'Something new')] };
    const { html } = render(SeatPanel, { props: props(view(d, hand)) });
    expect(html).toContain('data-option="2"');
    expect(html).toContain('Something new');
    expect(html).toContain('keep'); // the server's own labels, verbatim
    expect(html).not.toContain('Keep this hand');
  });
});

describe('SeatPanel — match over', () => {
  it('names the winner and adds no colour of its own', () => {
    const v = { ...view(null), over: true, winner: 1 };
    const { html } = render(SeatPanel, { props: props(v) });
    expect(html).toContain('bob wins');
    expect(html).toContain('Match over');
  });

  it('a draw says draw', () => {
    const v = { ...view(null), over: true, draw: true, winner: null };
    expect(render(SeatPanel, { props: props(v) }).html).toContain('Draw');
  });
});

describe('SeatPanel — the prompt surface (fb prompts: never passed over, never an action)', () => {
  const bolt = (d: Decision): View => {
    const v = view(d);
    v.stack = [{ id: 9, kind: 'spell', name: 'Lightning Bolt', text: '', controller: 0, targets: [], optional: false }];
    return v;
  };
  const target: Decision = {
    seq: 11, player: 1, kind: 'target', prompt: 'Choose a target', min: 1, max: 1, source: 9,
    options: [opt(0, 'target', 'Target alice', 3)],
  };
  const arrangeAsk: Decision = {
    seq: 12, player: 1, kind: 'arrange', min: 3, max: 3, source: 9,
    prompt: 'Rearrange the top 3 card(s); the first card you pick goes on top',
    options: [opt(0, 'bottom', 'Brazen Borrower', 11), opt(1, 'bottom', 'Fabled Pass', 12), opt(2, 'bottom', 'Spell Pierce', 14)],
  };
  // The engine's measured trigger_optional shape (rules/trigger_queue.go
  // askTriggerOptional): Min == Max == 1, "yes" first, prompt "<question>? — <label>".
  const optionalAsk: Decision = {
    seq: 13, player: 1, kind: 'trigger_optional', min: 1, max: 1, source: 9,
    prompt: 'Put this optional triggered ability on the stack? — Bloodghast: Whenever a land enters, Bloodghast may return from the graveyard',
    options: [opt(0, 'yes', 'Yes — Bloodghast: Whenever a land enters, Bloodghast may return from the graveyard', 9), opt(1, 'no', 'No', 9)],
  };

  it('the context line names the source and the shape of the answer (Job 3)', () => {
    const { html } = render(SeatPanel, { props: props(bolt(target)) });
    expect(html).toMatch(/data-prompt-ctx[^>]*>From Lightning Bolt \(a resolving spell\) · Pick 1 target</);
  });

  it('a priority window carries no context line: its shape is stated by the transport controls', () => {
    expect(render(SeatPanel, { props: props(view(priority)) }).html).not.toContain('data-prompt-ctx');
  });

  it('an arrange decision renders card faces in offered order and opens the popup, not a text index (Job 4)', () => {
    const { html } = render(SeatPanel, { props: props(bolt(arrangeAsk)) });
    expect(html).toContain('data-arrange');
    expect(html).toContain('data-arrange-row');
    expect(html).toContain('data-arrange-open');
    expect(html).toContain('Open the card view');
    expect(html).toContain('Confirm order');
    expect([...html.matchAll(/data-option="\d+"/g)]).toHaveLength(3);
    for (const name of ['Brazen Borrower', 'Fabled Pass', 'Spell Pierce']) expect(html).toContain(`aria-label="${name}"`);
    // no pass on the wire, so no primary button
    expect(html).not.toContain('data-primary');
  });

  it('renders every in-game decision inside the ACTIONS-anchored strip surface', () => {
    const strip = (v: View) => render(SeatPanel, { props: { ...props(v), placement: 'strip' as const } }).html;
    const initiative = strip(bolt(target));
    expect(initiative).toContain('data-answer-surface');
    expect(initiative).toContain('data-prompt');
    expect(initiative).toContain('data-option="0"');
    const offered = strip(view(priority));
    expect(offered).toContain('data-option="0"');
    expect(offered).not.toContain('data-auto-pay-toggle');
  });

  it('while the undo pause holds, the Auto/Manual toggle reads Paused and the note says how to resume (fb-20260914T063523Z)', () => {
    // The route hands the panel its shared SeatPanelState; rewind() (the
    // stream's rewind frame) pauses the machine on it.
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.rewind();
    const { html } = render(SeatPanel, { props: { ...props(view(priority)), state } });
    // The toggle beside the note reads the paused state, not Auto/Manual.
    expect(html).toContain('<span class="word">Paused</span>');
    // The note names the resume control the player actually has: the
    // pause-aware Auto switch (pressAuto), whose press STARTS the machine
    // instead of toggling the persisted preference off.
    expect(html).toContain('Undo paused automatic passing');
    expect(html).toContain('Press the Auto switch');
  });

  it('keeps an optional-trigger choice explicit in the ACTIONS-anchored surface', () => {
    const strip = (v: View) => render(SeatPanel, { props: { ...props(v), placement: 'strip' as const } }).html;
    const optional = strip(bolt(optionalAsk));
    expect(optional).toContain('data-answer-surface');
    expect(optional).toContain('data-remember-answer');
    expect(optional).toContain('data-option="0"');
    expect(optional).toContain('data-option="1"');
  });

  it('a trigger_optional prompt renders the remember checkbox above the options; other kinds do not (B4)', () => {
    const optionalHtml = render(SeatPanel, { props: props(bolt(optionalAsk)) }).html;
    expect(optionalHtml).toContain('data-remember-answer');
    expect(optionalHtml).toContain('Remember this answer for identical future prompts');
    expect(optionalHtml).toContain('data-option="0"');
    const generic = render(SeatPanel, { props: props(bolt(target)) }).html;
    expect(generic).not.toContain('data-remember-answer');
    expect(generic).not.toContain('Remember this answer for identical future prompts');
  });
});

describe('SeatPanel — name-a-card picker', () => {
  const names: Decision = { seq: 30, player: 1, kind: 'choose', prompt: 'Name a nonland card', min: 1, max: 1, options: Array.from({ length: 205 }, (_, i) => ({ index: i, kind: 'name', label: `Card ${String(i).padStart(3, '0')}`, player: 1 })) };
  it('filters and caps rendered name rows while preserving wire indexes', () => {
    const st = new SeatPanelState('t1', 1, ctx, null, null); st.adoptView(names); st.searchFilter = 'card 20';
    const html = render(SeatPanel, { props: { ...props(view(names)), state: st } }).html;
    expect(html).toContain('data-name-filter'); expect(html).toContain('data-name-count');
    expect([...html.matchAll(/data-option="(\d+)"/g)].map((m) => Number(m[1]))).toEqual([200, 201, 202, 203, 204]);
    expect(html).toContain('Showing first 5 of 5 matches');
  });
  it('caps the unfiltered list at 200 and leaves modes on the generic list', () => {
    const html = render(SeatPanel, { props: props(view(names)) }).html;
    expect([...html.matchAll(/data-option="(\d+)"/g)]).toHaveLength(200);
    expect(html).toContain('205 cards — type to filter');
    const modes = render(SeatPanel, { props: props(view({ ...names, kind: 'modes', options: [opt(0, 'mode', 'Mode A')] })) }).html;
    expect(modes).not.toContain('data-name-filter'); expect(modes).toContain('Mode A');
  });
});

describe('SeatPanel — the mulligan prompt names the starting player (rv2a)', () => {
  // The fixture is the ENGINE'S OWN wire bytes, not a hand-written prompt:
  // host's TestTheMulliganStarterFixtureIsTheEngineSOwnWireBytes drives a real
  // two-human table (duplicate decks "a"/"a", PlayerNames Alice/Bob, table
  // seed 1 — seat 1 wins the CR 103.1 toss and is asked first), captures the
  // seat-scoped ViewAtSeat-at-head JSON the seated client fetches for the
  // round's two asks (the first keep/mulligan, and the spent-allowance
  // keep-only re-ask), and byte-compares both with this committed file. A
  // revert of rules/mulligan.go's keepMulliganPrompt fails THAT test first;
  // these tests prove the engine-produced prompt survives the wire and is
  // rendered by SeatPanel without being truncated or hidden.
  const fixtureAsk = (ask: typeof starterFixture.first_ask) =>
    render(SeatPanel, {
      props: {
        view: ask as unknown as View,
        seats: starterFixture.seats as SeatInfo[],
        ctx: { seat: 1, token: 'tok' },
        table: 't1',
        match: 1,
      },
    });

  it('the first keep/mulligan ask renders the engine-produced display-name prompt and its own options', () => {
    const { html } = fixtureAsk(starterFixture.first_ask);
    const prompt = /data-prompt[^>]*>([^<]+)</.exec(html)?.[1];
    expect(prompt).toContain('Bob plays first.');
    expect(prompt).not.toContain('same-deck plays first.');
    // the first ask's own engine shape: both options, zero-card penalty
    expect(prompt).toContain('put 0 cards on the bottom of your library');
    expect(prompt).toContain('or take a mulligan?');
    expect(html).toContain('data-option="0"');
    expect(html).toContain('Keep this hand');
    expect(html).toContain('data-option="1"');
    expect(html).toContain('Mulligan');
    expect(html).toContain('data-tone="initiative"');
  });

  it('the spent-allowance re-ask still names the starter, with the engine\'s keep-only shape', () => {
    const { html } = fixtureAsk(starterFixture.spent_ask);
    const prompt = /data-prompt[^>]*>([^<]+)</.exec(html)?.[1];
    expect(prompt).toContain('Bob plays first.');
    // the spent ask is genuinely the OTHER engine shape, not the first ask
    // re-rendered: one card to bottom, and no mulligan option offered
    expect(prompt).toContain('put 1 card on the bottom of your library');
    expect(prompt).not.toContain('or take a mulligan?');
    expect(html).toContain('data-option="0"');
    expect(html).toContain('Keep this hand');
    expect(html).not.toContain('data-option="1"');
  });
});

describe('SeatPanel — the library-search picker (fb-20260916T181754Z)', () => {
  // The wire shape the engine poses (effects/zone.go effSearchLibrary): a
  // KChoose whose options are kind "search" with Obj set and the card's bare
  // name as Label, in LIBRARY scan order — deliberately unalphabetical so
  // the display sort has something to do. The predicate/sort/filter itself
  // is pinned in lib/search.test.ts; these tests hold the component branch:
  // the input renders, the display is sorted/filtered, and the data-option
  // indexes stay the engine's whatever the display does.
  const searchOpt = (index: number, label: string, obj: number): Option =>
    ({ index, kind: 'search', label, obj, player: 1 });
  const searchAsk: Decision = {
    seq: 21, player: 1, kind: 'choose', prompt: 'Search your library for a card', min: 0, max: 1,
    options: [
      searchOpt(0, 'Wooded Foothills', 401),
      searchOpt(1, 'an Island', 402),
      searchOpt(2, 'Mistveil Plains', 403),
      searchOpt(3, 'a card', 404),
    ],
  };
  const modesAsk: Decision = {
    seq: 22, player: 1, kind: 'modes', prompt: 'Choose a mode', min: 1, max: 1,
    options: [opt(0, 'mode', 'Mode A'), opt(1, 'mode', 'Mode B')],
  };

  const optionOrder = (html: string): number[] =>
    [...html.matchAll(/data-option="(\d+)"/g)].map((m) => Number(m[1]));

  it('name asks filter, sort, cap rendered rows, and keep modes in the generic branch', () => {
    const nameAsk: Decision = { seq: 31, player: 1, kind: 'choose', prompt: 'Name a card', min: 1, max: 1, options: [
      { index: 0, kind: 'name', label: 'Zulu', player: 1 },
      { index: 1, kind: 'name', label: 'Alpha', player: 1 },
      { index: 2, kind: 'name', label: 'Beta', player: 1 },
    ] };
    let html = render(SeatPanel, { props: props(view(nameAsk)) }).html;
    expect(html).toContain('data-name-filter');
    expect(html).toContain('3 cards — type to filter');
    expect(optionOrder(html)).toEqual([1, 2, 0]);
    const st = new SeatPanelState('t1', 1, ctx, null, null);
    st.adoptView(nameAsk);
    st.searchFilter = 'et';
    html = render(SeatPanel, { props: { ...props(view(nameAsk)), state: st } }).html;
    expect(optionOrder(html)).toEqual([2]);
    expect(html).toContain('Showing first 1 of 1 matches');
    const many = { ...nameAsk, seq: 32, options: Array.from({ length: 205 }, (_, index) => ({ index, kind: 'name', label: `Card ${index}`, player: 1 })) } as Decision;
    html = render(SeatPanel, { props: props(view(many)) }).html;
    expect(optionOrder(html)).toHaveLength(200);
    expect(html).toContain('205 cards — type to filter');
    const { html: generic } = render(SeatPanel, { props: props(view(modesAsk)) });
    expect(generic).not.toContain('data-name-filter');
    expect(generic).toContain('data-options');
  });

  it('a search ask renders the filter input and its faces in ALPHABETICAL display order, with the wire indexes intact', () => {
    const { html } = render(SeatPanel, { props: props(view(searchAsk)) });
    expect(html).toContain('data-search-filter');
    expect(html).toContain('data-search-grid');
    // Sorted A→Z case-insensitively (library scan order would be 0,1,2,3)
    expect(optionOrder(html)).toEqual([3, 1, 2, 0]);
    expect(html).toContain('Wooded Foothills');
    expect(html).toContain('a card');
    // the submit gate is the decision's own shape: Min 0 / Max 1 shows the Confirm button
    expect(html).toContain('data-submit');
  });

  it("filtering changes only the display: the surviving data-option indexes are the wire's", () => {
    // Pre-seed the shared state with the adopted ask and a typed filter; the
    // panel's init re-adopt early-returns on the same seq, so the filter
    // survives into the render. (The interactive typing thread is the state
    // contract pinned in lib/seatpanel.search.test.ts; here the render of a
    // narrowed filter is what is held.)
    const st = new SeatPanelState('t1', 1, ctx, null, null);
    st.adoptView(searchAsk);
    st.searchFilter = 'foo';
    const { html } = render(SeatPanel, { props: { ...props(view(searchAsk)), state: st } });
    expect(html).toContain('data-search-filter');
    expect(optionOrder(html)).toEqual([0]); // only Wooded Foothills survives, under its own wire index
  });

  it('a filter matching nothing renders the no-match line and no option buttons', () => {
    const st = new SeatPanelState('t1', 1, ctx, null, null);
    st.adoptView(searchAsk);
    st.searchFilter = 'delta';
    const { html } = render(SeatPanel, { props: { ...props(view(searchAsk)), state: st } });
    expect(optionOrder(html)).toEqual([]);
    expect(html).toContain('No card matches');
  });

  it('a NON-search decision grows no filter input — the generic list renders exactly as before', () => {
    const { html } = render(SeatPanel, { props: props(view(modesAsk)) });
    expect(html).not.toContain('data-search-filter');
    expect(html).toContain('Mode A');
  });
});
