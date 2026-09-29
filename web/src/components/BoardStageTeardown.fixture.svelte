<script lang="ts">
  import type { PlayerView, SeatInfo, View } from '../protocol';
  import { SeatPanelState, mulliganPhase } from '../lib/seatpanel.svelte';
  import BoardStage from './BoardStage.svelte';
  import SeatPanel from './SeatPanel.svelte';

  const seats: SeatInfo[] = [
    { name: 'Ari', deck: 'burn', colour: '#e5484d' },
    { name: 'Bo', deck: 'stomp', colour: '#22c55e' },
  ];
  const player = (seat: number): PlayerView => ({
    seat, name: seats[seat].name, life: 20, lost: false, library_size: 40, hand_size: 0,
    graveyard_size: 0, hand: [], battlefield: [], graveyard: [], exile: [], pool: {},
    completed_dungeons: 0,
    command: [], commanders: [], commander_casts: [],
  });
  let view = $state<View>({
    viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1',
    active: 0, priority: 0, over: false, draw: false, winner: null,
    players: [player(0), player(1)], stack: [], pending: [],
    decision: { seq: 1, player: 0, kind: 'starting_player', min: 1, max: 1,
      prompt: 'Choose who plays first', options: [{ index: 0, kind: 'player', label: 'Ari', player: 0 }] },
  });
  const ctx = { seat: 0, token: 'tok' };
  const panel = new SeatPanelState('t1', 1, ctx, null, null);
  // svelte-ignore state_referenced_locally
  panel.adoptView(view.decision ?? null);
  const mulligan = $derived(mulliganPhase(panel.active));
  // Mirror Table's split: the same frame replaces view and ends liveness,
  // but the object feeding mounted child prop getters survives the teardown.
  const controls = $derived({ state: panel, ctx, table: 't1', match: 1 });
  const controlsLive = $derived(mulligan === null && !view.over);
  const w = window as unknown as { __flip: () => void; __end: () => void; __viewChanged: () => boolean; __controlsPresent: () => boolean };
  w.__controlsPresent = () => controls !== null;
  // svelte-ignore state_referenced_locally
  const initialView = view;
  w.__viewChanged = () => view !== initialView;
  w.__end = () => {
    view = { ...view, over: true, decision: null };
  };
  w.__flip = () => {
    const next = { ...view, decision: { seq: 2, player: 0, kind: 'mulligan', min: 0, max: 1,
      prompt: 'Keep or mulligan', options: [{ index: 0, kind: 'keep', label: 'Keep', player: 0 }] } } satisfies View;
    view = next;
  };
</script>

<div class="boardlike">
  <BoardStage {view} {seats} mulligan={mulligan !== null} {controls} {controlsLive} />
  <SeatPanel {view} {seats} {ctx} table="t1" match={1} state={panel} />
</div>
<style>
  .boardlike { position: relative; width: 900px; height: 600px; }
</style>
