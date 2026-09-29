<script lang="ts">
  import type { Decision, View } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import {
    attackLines, attackOptionText, blockLines, blockOptionText, damageText, noAttackAllowed, noBlocksAllowed, unblockedDamage,
  } from '../../lib/prompts/combat';
  import ListPrompt from './ListPrompt.svelte';
  import type { FooterButton } from './PromptFooter.svelte';

  /**
   * CombatPrompt is the attackers/blockers renderer (UI rework spec §4:
   * "assigned by clicking the board, and the dock summarises the
   * assignments"). The board tiles are the primary surface — they carry the
   * same options through the shared card-options index and post through the
   * same click path — so this renderer is the summary: who attacks whom, or
   * each attacker facing you with its blockers and the damage you would
   * take, above the numbered pairings (the keyboard's route) and the quick
   * answers "Attack with all" / "No attack" / "No blocks".
   */
  let { decision, view, logic }: { decision: Decision; view: View; logic: SeatPanelState } = $props();

  const attackers = $derived(decision.kind === 'attackers');
  const lines = $derived(attackers ? attackLines(decision, view, logic.picked) : []);
  const blocks = $derived(attackers ? [] : blockLines(decision, view, logic.picked));
  const damage = $derived(attackers ? null : damageText(unblockedDamage(decision, view, logic.picked)));
  const status = $derived.by(() => {
    if (attackers) return lines.length === 0 ? 'No attackers yet' : `${lines.length} attacking`;
    const head = `${blocks.length} ${blocks.length === 1 ? 'attacker' : 'attackers'}`;
    return damage ? `${head} · ${damage}` : head;
  });
  const extra = $derived.by((): FooterButton[] => attackers
    ? [
        { label: 'Attack with all', onclick: () => logic.attackWithAll(), disabled: logic.busy, data: { 'data-attack-all': true } },
        ...(noAttackAllowed(decision) ? [{ label: 'No attack', onclick: () => logic.declareNone(), disabled: logic.busy, data: { 'data-no-attack': true } }] : []),
      ]
    : noBlocksAllowed(decision)
    ? [{ label: 'No blocks', onclick: () => logic.declareNone(), disabled: logic.busy, data: { 'data-no-blocks': true } }]
    : []);
</script>

<ListPrompt
  {decision}
  {logic}
  rowText={(o) => (attackers ? attackOptionText(decision, view, o) : blockOptionText(decision, view, o))}
  extraButtons={extra}
  {status}
>
  <div class="summary" data-combat-summary>
    {#if attackers}
      {#if lines.length === 0}
        <p class="none">Nothing is attacking yet.</p>
      {:else}
        {#each lines as line (line)}<p class="line attack">{line}</p>{/each}
      {/if}
    {:else}
      {#each blocks as b (b.attacker)}
        <div class="block" class:blocked={b.blocked} data-block-line={b.attacker}>
          <p class="head">{b.head}</p>
          <p class="detail">{b.blocked ? b.detail : damage ? `Unblocked — ${damage}` : b.detail}</p>
        </div>
      {/each}
    {/if}
  </div>
</ListPrompt>

<style>
  .summary { display: grid; gap: 4px; }
  .summary p { margin: 0; }
  .none { font-size: var(--t-12); color: var(--ink-faint); }
  .line { font-size: var(--t-12); color: var(--ink); }
  .line.attack::before { content: ''; display: inline-block; width: 6px; height: 6px; border-radius: 50%; background: var(--ember, #e0533f); margin-right: 0.45rem; vertical-align: 1px; }
  .block {
    padding: 0.45rem 0.6rem;
    border-radius: 8px;
    border: 1px solid color-mix(in srgb, var(--ember, #e0533f) 45%, var(--edge-inst));
    background: var(--instrument-raised);
  }
  .block.blocked { border-color: var(--edge-inst); }
  .head { font: 500 var(--t-12) var(--font-ui); color: var(--ink); }
  .detail { font-size: var(--t-11); color: var(--ink-dim); }
</style>
