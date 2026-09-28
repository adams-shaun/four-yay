import { mount } from 'svelte';
import type { CardView, PlayerView, SeatInfo, View } from '../protocol';
import '../app.css';
import IdentityBar from './IdentityBar.svelte';
import SeatTable from './SeatTable.svelte';
import RailFixture from './RailFixture.svelte';

const card = (id: number, name = `Card ${id}`): CardView => ({
  id, name, types: 'Instant', printing: { name }, token: `#${id}`, tapped: false,
  power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0,
  summon_sick: false,
});

const player = (seat: number, name: string): PlayerView => ({
  seat, name, life: 40, lost: false, library_size: 60, hand_size: 7, graveyard_size: 0,
  completed_dungeons: 0,
  hand: [], battlefield: [], graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
});

const players = [player(0, 'Ari'), player(1, 'Bo')];
const seats: SeatInfo[] = [
  { name: 'Ari', deck: 'red', colour: '#e5484d' },
  { name: 'Bo', deck: 'green', colour: '#22c55e' },
];
const view = (priority: number): View => ({
  viewer: 255, visibility: 'public', turn: 1, round: 1, step: 'main1', phase: 'main1', active: 1, priority,
  over: false, draw: false, winner: null, stack: [], pending: [], players,
});

mount(SeatTable, { target: document.querySelector('#seat-idle')!, props: { view: view(1), seats, onFocus: () => {} } });
mount(SeatTable, { target: document.querySelector('#seat-priority')!, props: { view: view(0), seats, onFocus: () => {} } });
mount(IdentityBar, { target: document.querySelector('#identity-idle')!, props: { player: players[0], players, colour: '#e5484d', active: false, priority: false, corner: 'tl' } });
mount(IdentityBar, { target: document.querySelector('#identity-priority')!, props: { player: players[0], players, colour: '#e5484d', active: false, priority: true, corner: 'tl' } });

// The rail fixture mounts the WHOLE instrument column — seat table, mana pool
// focus pane, decision line, stack tile, pending tray — at a fixed width, so
// the floor the rail actually needs is measured rather than guessed. The
// content is deliberately representative: four seats, one with a name long
// enough to exercise the ellipsis, a stack entry, an optional pending trigger
// and a live decision prompt.
const longName = 'Bartholomew Q. Fineganworth';
const railPlayers = [
  player(0, longName),
  player(1, 'Ari'),
  player(2, 'Bo'),
  player(3, 'Cy'),
];
const showHands = new URLSearchParams(window.location.search).get('hands') === '1';
if (showHands) {
  railPlayers[1].hand = [card(51, 'Ari Visible Card'), card(52, 'Ari Second Card')];
  railPlayers[2].hand = [card(53, 'Bo Visible Card'), card(54, 'Bo Second Card')];
}
railPlayers[0].pool = { W: 3, U: 2, B: 1, R: 4, G: 0, C: 2 };
railPlayers[1].hand_size = 9;
railPlayers[1].library_size = 41;
railPlayers[1].graveyard_size = 12;
railPlayers[1].exile = [card(11), card(12)];
const railSeats: SeatInfo[] = railPlayers.map((p, i) => ({ name: p.name, deck: 'WURGc Control Mirror — long deck label ' + i, colour: '#e5484d' }));
const railView: View = {
  viewer: 255, visibility: showHands ? 'omniscient' : 'public', turn: 4, round: 2, step: 'main1', phase: 'main1', active: 1, priority: 1,
  over: false, draw: false, winner: null,
  stack: [{
    id: 900, kind: 'spell', name: 'Slow but Absolutely Inevitable Zooming Doomwhisper', controller: 1,
    text: 'Deal 3 damage to any target. If a creature died this turn, draw a card.', targets: [], optional: false,
  }],
  pending: [{ source: 901, controller: 2, label: 'Longwinded Ambush Elemental trigger', optional: true, decider: 2 }],
  players: railPlayers,
};
// ?lost=1 makes seat 1 a LOST seat whose cause is the longest real loss text
// the engine emits (lib/seattable.ts lossCauses doc) — the fixture variant
// the eliminated-line overflow test measures at the rail floor. Seat 1 keeps
// its populated piles (hand 9 / graveyard 12 / exile 2) so the row is the
// widest lost seat the rail can hold.
const lostSeat = new URLSearchParams(window.location.search).get('lost') === '1';
if (lostSeat) railPlayers[1].lost = true;
const resolved = new URLSearchParams(window.location.search).get('resolved') === '1';
if (resolved) railPlayers[1].graveyard = [card(42, 'Resolved Thing')];
const railEvents: { event: { kind: string; player: number; text?: string; obj?: number } }[] = lostSeat
  ? [{ event: { kind: 'player_lost', player: 1, text: 'commander damage (21 or more from one commander)' } }]
  : resolved ? [{ event: { kind: 'stack_resolve', player: 1, obj: 42 } }] : [];
// Browser test hook: replace the projection after mount, including a visible
// empty hand and a changed card identity, to exercise Rail's live derivation.
(window as Window & { updateFixtureHands?: () => void }).updateFixtureHands = () => {
  railPlayers[1].hand = [card(55, 'Ari Updated Card')];
  railPlayers[2].hand = [];
  window.dispatchEvent(new CustomEvent('fixture-view-update', { detail: { ...railView, players: [...railPlayers] } }));
};

// Rail is mounted through RailFixture so the fixture can render the REAL
// concede control (ConcedeControl) as Rail's logbar snippet — the same seam
// Table.svelte uses on the live route — when the page URL asks for it
// (?concede=idle|confirm; default none, byte-identical to a bare Rail).
const concedeState = (new URLSearchParams(window.location.search).get('concede') ?? 'none') as
  | 'none'
  | 'idle'
  | 'confirm';
mount(RailFixture, {
  target: document.querySelector('#rail')!,
  props: {
    view: railView,
    seats: railSeats,
    decision: { player: 1, kind: 'priority', prompt: 'Pass priority, cast a spell, or hold up mana for something clever later this turn?' },
    emphasizeTop: true,
    onToggleLog: () => {},
    concede: concedeState,
    events: railEvents,
  },
});
