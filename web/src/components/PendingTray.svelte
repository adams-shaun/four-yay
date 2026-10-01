<script lang="ts">
  import type { PendingView } from '../protocol';
  import { pointerRelease } from '../lib/pointer';

  /**
   * PendingTray lists the triggers/replacement effects waiting on a decision, each labelled and, when optional, saying who decides.
   *
   * `stuck` is the empty-answer safety net (the Squadron Hawk fail-to-find
   * soft-lock): a pending decision for this seat that carries NO options is
   * one no picker can render, so without this entry the tray would read
   * "Nothing waiting" while the game is blocked on an invisible question.
   * When it is present the tray names the decision and — when the minimum
   * legal answer is the empty one (Min 0) — offers a Continue that submits
   * it, so the seat can always answer something the engine will accept.
   * The engine resolves every such shape silently since the empty-choose
   * fix, so this is belt-and-braces against a server that holds one anyway.
   */
  let { pending, stuck = null, onContinue = null }: { pending: PendingView[]; stuck?: { prompt: string; answerable: boolean } | null; onContinue?: (() => void) | null } = $props();
</script>

{#if pending.length === 0 && !stuck}
  <p class="empty">Nothing waiting</p>
{:else}
  <ul class="pending">
    {#each pending as p, i (`${p.source}:${i}`)}
      <li data-obj={p.source} class:optional={p.optional}>
        <span class="label">{p.label}</span>
        {#if p.optional}
          <span class="who">Seat {p.decider ?? p.controller} chooses</span>
        {/if}
      </li>
    {/each}
    {#if stuck}
      <li class="stuck" data-stuck>
        <span class="label">{stuck.prompt}</span>
        {#if stuck.answerable && onContinue}
          <button class="continue" type="button" data-continue use:pointerRelease onclick={onContinue}>Continue</button>
        {:else}
          <span class="who">This decision offers no choices</span>
        {/if}
      </li>
    {/if}
  </ul>
{/if}

<style>
  /*
   * The one thing no surveyed platform does: showing what is ABOUT to hit the
   * stack, in the same visual language as the stack itself. The tray sits
   * directly above the transcript and below the stack, so adjacency encodes
   * the mechanic — these entries descend into the list above them.
   *
   * This is where the design spends its boldness (design plan, principle 4):
   * the leading rule is the only place in the rail that carries the initiative
   * colour, so the eye finds it before anything else in the instrument.
   */
  .pending {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .pending li {
    position: relative;
    font-size: var(--t-12);
    line-height: 1.35;
    padding: var(--sp-2) var(--sp-2) var(--sp-2) var(--sp-3);
    background: var(--instrument-raised);
    border-left: 2px solid var(--initiative);
  }
  /* An optional trigger is a question, not an inevitability: the rule goes
     hollow so the two read differently at a glance. */
  .pending li.optional {
    border-left-color: var(--offered);
  }
  .label {
    display: block;
    color: var(--ink-inst);
  }
  .who {
    display: block;
    margin-top: 2px;
    font-family: var(--font-data);
    font-size: 0.6875rem;
    color: var(--ink-faint);
  }
  .empty {
    margin: 0;
    font-size: var(--t-12);
    color: var(--ink-faint);
  }
  /* The stuck entry is an anomaly, not a queued trigger: it says so in its
     own words and carries the one control that unblocks the seat. */
  .pending li.stuck {
    border-left-color: var(--offered);
  }
  .continue {
    margin-top: 4px;
    font-size: 0.6875rem;
    padding: 2px 10px;
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument-raised);
    color: var(--ink-inst);
    cursor: pointer;
  }
  .continue:hover {
    border-color: var(--initiative);
  }
</style>
