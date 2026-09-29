<script lang="ts">
  import { setContext } from 'svelte';
  import type { Decision, Intent, View } from '../protocol';
  import { SeatPanelState } from '../lib/seatpanel.svelte';
  import { REPEAT_ARM, type RepeatArmContext } from '../lib/repeat-context';
  import { ruleFromAnswer, saveSticky } from '../lib/sticky';
  import { repeatCard, repeatDecision, repeatManaDecision, repeatStack, repeatView } from '../lib/repeat.fixture';
  import HotButtonStrip from './HotButtonStrip.svelte';
  import CardTile from './CardTile.svelte';
  import StackTile from './StackTile.svelte';

  const ctx = { seat: 0, token: 'fixture' };
  const table = 'repeat-ui';
  const panel = new SeatPanelState(table, 1, ctx, null, sessionStorage);
  panel.settings = { ...panel.settings, autoPass: false, passAfterAct: false, pacing: { stepMs: 999, resolveMs: 999 } };
  panel.skipEmpty = false;
  const mana = new URLSearchParams(location.search).has('mana');
  const card = mana ? { ...repeatCard, name: 'Phyrexian Altar' } : repeatCard;
  const priority = mana ? repeatManaDecision : repeatDecision;
  const makeView = (d: Decision | null = priority(), stack: View['stack'] = []): View => {
    const v = repeatView(d, stack);
    v.players[0].battlefield = [card];
    return v;
  };
  let view = $state<View>(makeView());
  setContext<RepeatArmContext>(REPEAT_ARM, {
    candidate: () => panel.repeatCandidate,
    disabled: () => panel.busy || panel.machinePaused,
    arm: (n) => panel.armRepeat(n),
  });
  // One existing per-game sticky lets the focus test prove 5 did not clear it.
  const choice: Decision = { ...repeatDecision(2), kind: 'choose', source: 20, prompt: 'Sacrifice', options: [
    { index: 5, kind: 'sacrifice', label: 'Miner', obj: 21, player: 0 },
  ] };
  const colour: Decision = { ...choice, prompt: 'Choose mana colour', options: [
    { index: 6, kind: 'mana', label: 'R', mana_symbol: 'R', player: 0 },
    { index: 7, kind: 'mana', label: 'B', mana_symbol: 'B', player: 0 },
  ] };
  const rule = ruleFromAnswer(choice, makeView(), [5])!;
  saveSticky(table, 1, new Map([[rule.key, rule]]), sessionStorage);
  const posts: Intent[] = [];
  window.fetch = async (input, init) => {
    if (String(input).endsWith('/intent')) {
      posts.push(JSON.parse(String(init?.body)));
      return new Response('{}', { status: 200 });
    }
    if (String(input).endsWith('/undo')) {
      panel.rewind();
      view = makeView();
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
    __panel: SeatPanelState;
    __deliver: (seq: number, kind: 'priority' | 'choose' | 'colour' | 'allocation' | 'wait', stack?: boolean) => void;
  };
  w.__posts = posts;
  w.__panel = panel;
  w.__deliver = (seq, kind, stack = false) => {
    const d = kind === 'wait' ? null : kind === 'choose' ? { ...choice, seq }
      : kind === 'colour' ? { ...colour, seq }
      : kind === 'allocation' ? { ...colour, seq, min: 2, max: 2 } : priority(seq);
    view = makeView(d, stack ? [repeatStack()] : []);
  };
</script>

<HotButtonStrip {view} seats={[{ name: 'Pilot', deck: '', colour: '', human: true }]} state={panel} {ctx} {table} match={1} />
<div id="source" style="margin-top: 350px"><CardTile {card} /></div>
{#each view.stack as stack (stack.id)}
  <div id="stack-source"><StackTile {stack} {view} /></div>
{/each}
<div id="unrelated"><CardTile card={{ ...card, id: 99 }} /></div>
