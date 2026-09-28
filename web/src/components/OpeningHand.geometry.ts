import { mount } from 'svelte';
import type { CardView, Decision, PlayerView, SeatInfo, View } from '../protocol';
import { SeatPanelState } from '../lib/seatpanel.svelte';
import '../app.css';
import SeatPanel from './SeatPanel.svelte';

const card = (id: number): CardView => ({
  id, name: `Opening card ${id}`, types: 'Creature', mana_cost: '1 G',
  tapped: false, power: 2, toughness: 2, damage: 0, attacking: false,
  controller: 0, owner: 0, summon_sick: false,
  printing: { name: `Opening card ${id}` }, token: `#${id}`,
});
const requested = Number(new URLSearchParams(location.search).get('cards') ?? '7');
const handCount = Number.isInteger(requested) ? Math.max(0, Math.min(7, requested)) : 7;
const hand = Array.from({ length: handCount }, (_, i) => card(100 + i));
const player: PlayerView = {
  seat: 0, name: 'Player 1', life: 20, lost: false, library_size: 53,
  hand_size: hand.length, graveyard_size: 0, hand, battlefield: [], graveyard: [],
  completed_dungeons: 0,
  exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
};
const seats: SeatInfo[] = [{ name: 'Player 1', deck: 'fixture', colour: '#e5484d' }];
const decision: Decision = {
  seq: 1, player: 0, kind: 'mulligan', prompt: 'Keep this opening hand?', min: 1, max: 1,
  options: [
    { index: 7, kind: 'keep', label: 'keep', player: 0 },
    { index: 42, kind: 'mulligan', label: 'mulligan', player: 0 },
  ],
};
const view: View = {
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'untap', phase: 'beginning',
  active: 0, priority: 0, over: false, draw: false, winner: null,
  players: [player], stack: [], pending: [], decision,
};
const state = new SeatPanelState('fixture', 1, { seat: 0, token: 'geometry' }, null);
state.skipEmpty = false;
state.adoptView(decision);

const target = document.querySelector('#app')!;
target.innerHTML = '<main class="table"><section class="stage"></section><aside></aside></main>';
mount(SeatPanel, {
  target: target.querySelector('.stage')!,
  props: { view, seats, ctx: { seat: 0, token: 'geometry' }, table: 'fixture', match: 1, state },
});

const style = document.createElement('style');
style.textContent = `
  .table { display:grid; grid-template-columns:1fr minmax(17rem, 18%); width:100vw; height:100vh; background:var(--felt); }
  .stage { position:relative; min-width:0; overflow:hidden; }
  aside { background:var(--instrument); border-left:1px solid var(--edge-inst); }
`;
document.head.append(style);
