import { mount } from 'svelte';
import type { CardView, PlayerView, SeatInfo, View } from '../protocol';
import '../app.css';
import SeatBox from './SeatBox.svelte';
import PileHost from './PileHost.svelte';

// The viewer's own seat as captured in fb-20261006T101047Z-cd69ff29: one card
// in hand, 72 in the library, 9 in the graveyard, 10 in exile, and a floating
// pool of three black mana that can only pay for Demon/Cleric/Vampire spells
// (the persistent restriction text is what used to crowd the counts line).
// ?pool=none drops the pool so the counts line is measured alone.
const card = (id: number): CardView => ({
  id, name: `Exiled Card ${id}`, types: 'Creature — Vampire', printing: { name: `Exiled Card ${id}` }, token: `#${id}`,
  tapped: false, power: 1, toughness: 1, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
});
const params = new URLSearchParams(location.search);
document.documentElement.style.setProperty('--slot-w', `${params.get('w') ?? '190'}px`);
const noPool = params.get('pool') === 'none';

const graveyard = Array.from({ length: 9 }, (_, i) => card(i + 1));
const exile = Array.from({ length: 10 }, (_, i) => card(i + 101));
const player: PlayerView = {
  seat: 0, name: 'You', life: 20, lost: false, library_size: 72, hand_size: 1, graveyard_size: graveyard.length,
  completed_dungeons: 0,
  hand: [], battlefield: [], graveyard, exile, command: [], commanders: [], commander_casts: [],
  pool: noPool ? {} : { B: 3 },
  pool_restrictions: noPool ? [] : [{ color: 'B', amount: 3, text: 'Spell.Demon,Spell.Cleric,Spell.Vampire' }],
};
const view: View = {
  viewer: 0, visibility: 'seat', turn: 15, round: 8, step: 'main1', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, stack: [], pending: [], players: [player],
};
const seats: SeatInfo[] = [{ name: 'You', deck: 'fixture', colour: '#e5484d' }];

mount(SeatBox, { target: document.querySelector('#slot')!, props: { player, name: 'You', colour: '#e5484d' } });
mount(PileHost, { target: document.querySelector('#host')!, props: { view, seats, options: null } });
