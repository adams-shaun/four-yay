import { mount } from 'svelte';
import type { CardView, Decision, PlayerView, SeatInfo, View } from '../protocol';
import { SeatPanelState, toneOf } from '../lib/seatpanel.svelte';
import { optionsByObj, optionsByPlayer, type CardOptions } from '../lib/cardoptions';
import '../app.css';
import BoardStage from './BoardStage.svelte';
import { layoutStore } from '../lib/layouts.svelte';
import { emptyLibrary } from '../lib/layoutlibrary';
import { PRESET_IDS, type PresetId } from '../lib/layoutprofile';

const params = new URLSearchParams(location.search);
const count = Math.min(8, Math.max(2, Number(params.get('seats') ?? '2') || 2));
const pool: Record<string, number> = params.has('pool') ? { C: 1, W: 2 } : {};
// Each page starts from the shipped layout (the shared browser keeps
// localStorage between pages), then applies ?preset= when given.
layoutStore.replace(emptyLibrary());
const preset = params.get('preset');
if (preset !== null && (PRESET_IDS as readonly string[]).includes(preset)) layoutStore.applyPreset(preset as PresetId);
const colours = ['#e5484d', '#30a46c', '#4a8fd4', '#d8a24a', '#a855f7', '#f97316', '#14b8a6', '#ec4899'];
const card = (id: number, seat: number): CardView => ({
  id, name: `Commander ${seat + 1}`, types: 'Legendary Creature', mana_cost: '2 W',
  tapped: false, power: 2, toughness: 2, damage: 0, attacking: false,
  controller: seat, owner: seat, summon_sick: false, printing: { name: `Commander ${seat + 1}` }, token: `#${id}`,
});
const players: PlayerView[] = Array.from({ length: count }, (_, seat) => {
  const commander = card(seat + 1, seat);
  return {
    seat, name: `Player ${seat + 1}`, life: 40, lost: false, library_size: 90, hand_size: 7, graveyard_size: 0,
    completed_dungeons: 0,
    hand: [], battlefield: [], graveyard: [], exile: [], pool: seat === 0 ? pool : {}, command: [commander], commanders: [commander], commander_casts: [],
  };
});
const seats: SeatInfo[] = players.map((p, seat) => ({ name: p.name, deck: 'fixture', colour: colours[seat] }));
const kind = params.get('decision');
const noPass = kind === 'choose';
const decision: Decision = kind === 'target'
  ? {
      seq: 9, player: 0, kind: 'target', prompt: 'Choose a target', min: 1, max: 1,
      options: [
        { index: 3, kind: 'player', label: 'Player 2', player: 1 },
        { index: 4, kind: 'player', label: 'Player 1', player: 0 },
      ],
    }
  : noPass
  ? {
      seq: 8, player: 0, kind: 'choose', prompt: 'Choose two', min: 0, max: 2,
      options: [
        { index: 7, kind: 'choose', label: 'Choose one', player: 0 },
        { index: 19, kind: 'choose', label: 'Choose two', player: 0 },
      ],
    }
  : {
      seq: 7, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
      options: [
        { index: 1, kind: 'cast', label: 'Cast a spell', player: 0 },
        { index: 42, kind: 'pass', label: 'Pass priority', player: 0 },
        { index: 99, kind: 'concede', label: 'Concede', player: 0 },
      ],
    };
const view: View = {
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, stack: [], pending: [], players, decision,
};
// A target decision marks the offered players (their header bar / seat box glow verdigris).
const options: CardOptions | null = kind === 'target'
  ? { byObj: optionsByObj(decision), byPlayer: optionsByPlayer(decision), picked: [], tone: toneOf(decision), post: () => {} }
  : null;
const panel = new SeatPanelState('fixture', 1, { seat: 0, token: 'geometry' }, null);
panel.skipEmpty = false;
panel.adoptView(decision);

const target = document.querySelector('#app')!;
target.innerHTML = '<main class="table"><section class="stage"></section><aside></aside></main>';
mount(BoardStage, {
  target: target.querySelector('.stage')!,
  props: {
    view, seats, seat: 0, controlsLive: true, options,
    stops: { yours: new Set<string>(), opponents: new Set<string>() },
    onToggle: () => {},
    controls: { state: panel, ctx: { seat: 0, token: 'geometry' }, table: 'fixture', match: 1 },
    hand: null,
  },
});

const style = document.createElement('style');
style.textContent = `
  .table { display:grid; grid-template-columns:1fr minmax(17rem, 18%); width:100vw; height:100vh; background:var(--felt); }
  .stage { min-width:0; overflow:hidden; }
`;
document.head.append(style);
