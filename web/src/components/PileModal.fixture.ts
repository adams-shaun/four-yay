import { mount } from 'svelte';
import type { CardView, Decision, PlayerView, SeatInfo, View } from '../protocol';
import { optionsByObj, optionsByPlayer, type CardOptions } from '../lib/cardoptions';
import { toneOf } from '../lib/seatpanel.svelte';
import '../app.css';
import PileFixture from './PileModal.fixture.svelte';
import IdentityBar from './IdentityBar.svelte';
import PileHost from './PileHost.svelte';
import SeatTable from './SeatTable.svelte';

// `?case=shrink` mounts the reactive wrapper (PileModal.fixture.svelte): a
// PileModal open on a live $state pile a test can shrink through
// window.__pileRemoveFirst, for the removal-while-open and Escape-layering
// cases. `?case=identity` mounts the identity-bar pile affordances
// (fb-20260916T225802Z): IdentityBar + the shared PileHost, wired exactly as
// Table.svelte wires them, over a pile the pending decision touches — the
// tone ring and the actionable pile cards a static SeatTable mount cannot
// carry. Without a param the fixture is the real rail path: SeatTable, as
// the player reaches the modal (plus PileHost, the table's one modal
// instance — the rail's buttons now open it through the shared opener).

const card = (id: number): CardView => ({
  id, name: `Archive Card ${id}`, types: id % 2 === 0 ? 'Creature — Wizard' : 'Instant',
  printing: { name: `Archive Card ${id}` }, token: `#${id}`, tapped: false,
  power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0,
  summon_sick: false,
});
const graveyard = Array.from({ length: 52 }, (_, i) => card(i + 1));
const player: PlayerView = {
  seat: 0, name: 'Alice', life: 37, lost: false, library_size: 40, hand_size: 1,
  graveyard_size: graveyard.length, hand: [card(100)], battlefield: [], graveyard,
  completed_dungeons: 0,
  exile: [card(200)], pool: {}, command: [], commanders: [], commander_casts: [],
};
const view: View = {
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1',
  active: 0, priority: 0, over: false, draw: false, winner: null, stack: [], pending: [], players: [player],
};
const seats: SeatInfo[] = [{ name: 'Alice', deck: 'archive', colour: '#e5484d' }];

if (new URLSearchParams(location.search).get('case') === 'shrink') {
  mount(PileFixture, { target: document.querySelector('#fixture')! });
} else if (new URLSearchParams(location.search).get('case') === 'identity') {
  // Two seats: Alice owns the touched graveyard/exile piles; Bob owns a
  // graveyard holding card 300, which the decision TARGETS — proving the
  // glow is not gated by pile owner. Tone comes through toneOf's register
  // exactly as Table.svelte resolves it (the decision carries a pass, so
  // the whole bundle reads offered).
  const alice: PlayerView = {
    seat: 0, name: 'Alice', life: 37, lost: false, library_size: 40, hand_size: 1,
    graveyard_size: 2, hand: [card(100)], battlefield: [], graveyard: [card(1), card(2)],
    completed_dungeons: 0,
    exile: [card(200)], pool: {}, command: [], commanders: [], commander_casts: [],
  };
  const bob: PlayerView = {
    seat: 1, name: 'Bob', life: 20, lost: false, library_size: 40, hand_size: 0,
    graveyard_size: 1, hand: null as unknown as CardView[], battlefield: [],
    completed_dungeons: 0,
    graveyard: [card(300)], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
  };
  const both: View = { ...view, players: [alice, bob] };
  const bothSeats: SeatInfo[] = [
    { name: 'Alice', deck: 'archive', colour: '#e5484d' },
    { name: 'Bob', deck: 'dimir', colour: '#3b82f6' },
  ];
  const decision: Decision = {
    seq: 1, player: 0, kind: 'priority', prompt: 'pile affordance fixture', min: 0, max: 1,
    options: [
      { index: 7, kind: 'cast', label: 'Flashback Archive Card 1', obj: 1, player: 0 },
      { index: 11, kind: 'cast', label: 'Recast A', obj: 2, player: 0 },
      { index: 12, kind: 'cast', label: 'Recast B', obj: 2, player: 0 },
      { index: 21, kind: 'permanent', label: 'Archive Card 300', obj: 300, player: 1 },
      { index: 99, kind: 'pass', label: 'Pass', player: 0 },
    ],
  };
  const bundle: CardOptions = {
    byObj: optionsByObj(decision),
    byPlayer: optionsByPlayer(decision),
    picked: [],
    tone: toneOf(decision),
    post: (index: number) => {
      (window as unknown as { __pilePosted: number }).__pilePosted = index;
    },
  };
  const target = document.querySelector('#fixture')!;
  mount(IdentityBar, {
    target,
    props: { player: alice, seat: bothSeats[0], colour: bothSeats[0].colour, active: true, priority: false, corner: 'tl', players: both.players, options: bundle },
  });
  mount(IdentityBar, {
    target,
    props: { player: bob, seat: bothSeats[1], colour: bothSeats[1].colour, active: false, priority: false, corner: 'tr', players: both.players, options: bundle },
  });
  mount(PileHost, { target, props: { view: both, seats: bothSeats, options: bundle } });
} else {
  mount(SeatTable, {
    target: document.querySelector('#fixture')!,
    props: { view, seats, onFocus: () => {} },
  });
  mount(PileHost, { target: document.querySelector('#fixture')!, props: { view, seats, options: null } });
}
