<script lang="ts">
  import type { CardView, PlayerView } from '../protocol';
  import HandFan from './HandFan.svelte';

  const card = (id: number, name: string): CardView => ({
    id, name, types: 'Creature Bear', mana_cost: '1G', tapped: false,
    power: 2, toughness: 2, damage: 0, attacking: false, controller: 0,
    owner: 0, summon_sick: false, printing: { name }, token: '',
  });

  const player = $state<PlayerView>({
    seat: 0, name: 'P0', life: 20, lost: false, library_size: 60, hand_size: 3,
    graveyard_size: 0, hand: [card(7, 'Grizzly Bears'), card(8, 'Bear Cub'), card(9, 'Runeclaw Bear')],
    completed_dungeons: 0,
    battlefield: [], graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
  });

  const w = window as unknown as {
    __refreshHand: () => void;
    __replaceId: (id: number) => void;
    __removeCard: () => void;
  };
  w.__refreshHand = () => {
    player.hand = player.hand.map((c) => ({ ...c, name: c.id === 7 ? 'Refreshed Grizzly Bears' : c.name, printing: { name: c.id === 7 ? 'Refreshed Grizzly Bears' : c.name } }));
  };
  w.__replaceId = (id) => {
    player.hand = player.hand.map((c) => c.id === 7 ? card(id, 'Replacement Bear') : c);
  };
  w.__removeCard = () => {
    player.hand = player.hand.filter((c) => c.id !== 7);
  };
</script>

<HandFan {player} width={900} />
