<script lang="ts">
  import type { Decision, View } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import { rendererFor, type Placement } from '../../lib/prompts/renderer';
  import { manaWindow } from '../../lib/announcepay';
  import { digitMap, renderedOrder } from '../../lib/prompts/order';
  import ManaPaymentPanel from '../ManaPaymentPanel.svelte';
  import ArrangePrompt from './ArrangePrompt.svelte';
  import CombatPrompt from './CombatPrompt.svelte';
  import DiscardPrompt from './DiscardPrompt.svelte';
  import ListPrompt from './ListPrompt.svelte';
  import MulliganPrompt from './MulliganPrompt.svelte';
  import NamePickPrompt from './NamePickPrompt.svelte';
  import PriorityOptions from './PriorityOptions.svelte';
  import SearchPrompt from './SearchPrompt.svelte';
  import TargetChips from './TargetChips.svelte';

  /**
   * PromptBody is the one dispatch from a decision to its renderer (UI
   * rework spec §4: "one renderer per decision kind"). The seat panel (the
   * ACTIONS strip and the board-centred mulligan) and the prompt dock both
   * mount it, so a decision looks and answers the same wherever it is
   * shown. rendererFor holds the precedence; every renderer answers through
   * the shared SeatPanelState, so the wire intent never depends on where the
   * click happened.
   */
  let { decision, view, logic, seat, placement }: { decision: Decision; view: View; logic: SeatPanelState; seat: number; placement: Placement } = $props();

  const kind = $derived(rendererFor(decision));
  const pay = $derived(manaWindow(decision));
</script>

{#if kind === 'mulligan'}
  <MulliganPrompt {decision} {view} {logic} {seat} />
{:else if kind === 'arrange'}
  <ArrangePrompt {decision} {logic} />
{:else if kind === 'discard'}
  <DiscardPrompt {decision} {logic} />
{:else if kind === 'search'}
  <SearchPrompt {decision} {logic} />
{:else if kind === 'name'}
  <NamePickPrompt {decision} {logic} />
{:else if kind === 'payment' && pay !== null}
  <ManaPaymentPanel decision={pay} {view} busy={logic.busy} onPick={(index) => logic.click(index)} digits={digitMap(renderedOrder(decision, { filter: '' }))} />
{:else if kind === 'priority'}
  <PriorityOptions {decision} {view} {logic} {seat} {placement} />
{:else if kind === 'target'}
  <TargetChips {decision} {view} {logic} />
{:else if kind === 'attackers' || kind === 'blockers'}
  <CombatPrompt {decision} {view} {logic} />
{:else}
  <ListPrompt {decision} {logic} />
{/if}
