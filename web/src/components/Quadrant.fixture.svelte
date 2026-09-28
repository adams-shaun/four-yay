<script lang="ts">
  import type { CardView, PlayerView } from '../protocol';
  import Quadrant from './Quadrant.svelte';

  /**
   * Quadrant fixture: a real browser-mounted battlefield for the hover
   * inspector lifetime regression (fb-20260915T182335Z). Seat 0's battlefield
   * holds one Grizzly Bear (object 7); the window hooks below mutate the LIVE
   * view the way an incoming stream frame would — same-object state updates,
   * a replaced object, a removed object — so the test can prove the keyed
   * battlefield-list update path in Quadrant, not the HoverCard class in
   * isolation. Quadrant's own Svelte reactivity re-derives the stacks on each
   * mutation exactly as a live table frame does.
   */

  const bear = (over: Partial<CardView> = {}): CardView => ({
    id: 7, name: 'Grizzly Bears', types: 'Creature Bear', mana_cost: '1G',
    tapped: false, power: 2, toughness: 2, damage: 0, attacking: false,
    controller: 0, owner: 0, summon_sick: false, printing: { name: 'Grizzly Bears' }, token: '',
    ...over,
  });

  const player = $state<PlayerView>({
    seat: 0, name: 'P0', life: 20, lost: false, library_size: 60, hand_size: 7,
    graveyard_size: 0, hand: [], battlefield: [bear()], graveyard: [], exile: [],
    completed_dungeons: 0,
    pool: {}, command: [], commanders: [], commander_casts: [],
  });

  const w = window as unknown as {
    __patchCard: (patch: Partial<CardView>) => void;
    __replaceCard: (card: CardView) => void;
    __removeCard: () => void;
  };
  // A same-object state update: the equivalence key moves, the object id does not.
  w.__patchCard = (patch) => {
    Object.assign(player.battlefield[0], patch);
  };
  // A different object takes the first slot (the old one is replaced, not kept).
  w.__replaceCard = (card) => {
    player.battlefield[0] = card;
  };
  // The described object leaves the battlefield entirely.
  w.__removeCard = () => {
    player.battlefield = player.battlefield.slice(1);
  };
</script>

<Quadrant {player} colour="#e5484d" />
