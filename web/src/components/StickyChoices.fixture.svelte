<script lang="ts">
  import { setContext } from 'svelte';
  import type { CardView, Decision, Intent, View } from '../protocol';
  import { SeatPanelState } from '../lib/seatpanel.svelte';
  import { STICKY_SOURCES, type StickySources } from '../lib/sticky-context';
  import HotButtonStrip from './HotButtonStrip.svelte';
  import PlaySettingsPanel from './PlaySettingsPanel.svelte';
  import CardTile from './CardTile.svelte';
  import StackTile from './StackTile.svelte';

  const ctx = { seat: 0, token: 'fixture' };
  const table = 'sticky-ui';
  const panel = new SeatPanelState(table, 1, ctx, null, sessionStorage);
  panel.settings = { ...panel.settings, autoPass: false, autoOrderAllTriggers: false };
  setContext<StickySources>(STICKY_SOURCES, () => panel.stickySourceNames);
  const card: CardView = {
    id: 20, name: 'Miner', types: 'Creature', printing: { name: 'Miner' }, token: '',
    tapped: false, power: 1, toughness: 1, damage: 0, attacking: false,
    controller: 0, owner: 0, summon_sick: false,
  };
  const ask: Decision = {
    seq: 10, kind: 'trigger_optional', player: 0, prompt: 'Return Miner?', source: 20,
    min: 1, max: 1, options: [
      { index: 3, kind: 'yes', label: 'Yes', player: 0 },
      { index: 7, kind: 'no', label: 'No', player: 0 },
    ],
  };
  let view = $state<View>({
    viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'upkeep', phase: 'beginning',
    active: 0, priority: 0, over: false, draw: false, winner: null,
    players: [{ seat: 0, name: 'Pilot', life: 20, lost: false, library_size: 40, hand_size: 0,
      graveyard_size: 0, hand: [], battlefield: [card], graveyard: [], exile: [], pool: {},
      command: [], commanders: [], commander_casts: [], completed_dungeons: 0 }],
    stack: [], pending: [], decision: ask,
  });
  const posts: Intent[] = [];
  // Use the actual API path; keep /pending in sync without a server.
  window.fetch = async (input, init) => {
    if (String(input).endsWith('/intent')) {
      posts.push(JSON.parse(String(init?.body)));
      return new Response('{}', { status: 200 });
    }
    if (String(input).endsWith('/pending')) {
      if (!view.decision || posts.some((p) => p.seq === view.decision?.seq)) return new Response('', { status: 409 });
      return new Response(JSON.stringify(view.decision), { status: 200 });
    }
    return new Response('', { status: 404 });
  };
  const w = window as unknown as {
    __posts: Intent[];
    __deliver: (overrides?: Partial<Decision>) => void;
  };
  w.__posts = posts;
  w.__deliver = (overrides = {}) => {
    view = { ...view, decision: { ...ask, seq: (view.decision?.seq ?? 10) + 1, ...overrides } };
  };
</script>

<HotButtonStrip {view} seats={[]} state={panel} {ctx} {table} match={1} />
<div id="source" style="margin-top: 350px"><CardTile {card} /></div>
<div id="stack-source"><StackTile stack={{ id: 30, name: 'Miner', kind: 'trigger', controller: 0, text: 'Return', targets: [], optional: false }} {view} /></div>
<PlaySettingsPanel state={panel} />
