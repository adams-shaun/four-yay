<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { Decision, Option } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import { digitMap, orderedOptions } from '../../lib/prompts/order';
  import { primaryOf } from '../../lib/prompts/decision';
  import { selectionStatus, submitLabel } from '../../lib/prompts/anatomy';
  import { hoverLinkOf, promptHover } from '../../lib/prompts/hover.svelte';
  import Digit from './Digit.svelte';
  import ManaSymbols from '../ManaSymbols.svelte';
  import { manaOptionPips } from '../../lib/announcepay';
  import PromptFooter, { type FooterButton } from './PromptFooter.svelte';

  /**
   * ListPrompt is the numbered-rows renderer: modes (multi-select with a
   * count), choose and replacement (single-select), trigger_order (picked
   * order shown), trigger_optional, commander_zone, starting_player and any
   * kind without a renderer of its own. Selection is the seat's ordinary
   * path — a min==max==1 click posts, anything else toggles into `picked`
   * and the footer's commit posts — so the wire intent is exactly what the
   * old generic list sent. Rows are numbered by the decision's rendered
   * order (lib/prompts/order.ts), the same list the pick hotkeys resolve.
   *
   * The combat renderers reuse it with their own row words (`rowText`), a
   * summary above the rows (`children`), their quick answers
   * (`extraButtons`) and their live state (`status`).
   */
  let { decision, logic, rowText = null, extraButtons = [], status = undefined, children }: {
    decision: Decision;
    logic: SeatPanelState;
    rowText?: ((o: Option) => string) | null;
    extraButtons?: FooterButton[];
    status?: string | null;
    children?: Snippet;
  } = $props();

  const rows = $derived(orderedOptions(decision, { filter: logic.searchFilter }) ?? []);
  const digits = $derived(digitMap(rows.map((o) => o.index)));
  const primary = $derived(primaryOf(decision));
  const multi = $derived(decision.max > 1 || decision.repeatable === true);
  // A full non-repeatable modal pick dims the rest (the mockup's "Choose
  // two"): picking a third is not an answer. Grouped options (blocks) are
  // exempt — a pick there REPLACES its group member.
  const full = $derived(decision.kind === 'modes' && !decision.repeatable && decision.max > 1 && logic.picked.length >= decision.max);

  const footer = $derived.by((): FooterButton[] => {
    const out: FooterButton[] = [...extraButtons];
    if (primary) out.push({ label: primary.label, onclick: (e) => logic.primaryClick(e.ctrlKey), disabled: logic.busy, primary: !logic.showSubmit, data: { 'data-primary': true } });
    if (logic.showSubmit) {
      out.push({
        label: submitLabel(decision),
        onclick: (e) => logic.submit(e.ctrlKey),
        disabled: !logic.canSubmit || logic.busy,
        primary: true,
        data: { 'data-submit': true },
      });
    }
    return out;
  });
  const liveStatus = $derived(status !== undefined ? status : selectionStatus(decision, logic.picked.length));
</script>

<div class="list-prompt" data-list-prompt>
  {@render children?.()}
  {#if decision.kind === 'trigger_optional'}
    <!-- The remember affordance (fb-20260914T062319Z-88b4069a B4): default
         OFF; when checked, click()'s post-on-click stores the chosen index
         under the full-prompt key and every identical future prompt is
         answered with it (manageable in GAME OPTIONS). It sits above the
         rows because a min==max==1 ask posts on the click itself. -->
    <label class="remember" data-remember-answer>
      <input type="checkbox" bind:checked={logic.rememberChoice} disabled={logic.busy} />
      <span>Remember this answer for identical future prompts</span>
    </label>
  {/if}
  {#if decision.repeatable && logic.picked.length > 0}
    <!-- A repeatable modal ask (CanRepeatModes$, CR 601.2b) is an ordered
         multiset: a row click APPENDS an instance, so removal needs its own
         affordance — one chip per picked instance, each removing exactly
         that instance (unpick). -->
    <div class="picked-chips" data-picked-modes>
      {#each logic.picked as pi, i (i)}
        {@const popt = decision.options.find((o) => o.index === pi)}
        <button class="chip" type="button" data-picked-chip={i} onclick={() => logic.unpick(pi)} disabled={logic.busy}>{popt?.label} ✕</button>
      {/each}
    </div>
  {/if}
  <div class="rows" data-options>
    {#each rows as opt (opt.index)}
      {@const at = logic.picked.indexOf(opt.index)}
      {@const count = decision.repeatable ? logic.picked.filter((i) => i === opt.index).length : 0}
      {@const link = hoverLinkOf(opt)}
      {@const pips = rowText === null && opt.kind === 'mana' ? manaOptionPips(opt.label) : null}
      <button
        class="row"
        class:picked={at >= 0}
        class:dim={full && at < 0}
        type="button"
        data-option={opt.index}
        aria-pressed={multi ? at >= 0 : undefined}
        onclick={(e) => logic.click(opt.index, { holdPriority: e.ctrlKey })}
        onpointerenter={() => link && promptHover.set(link)}
        onpointerleave={() => promptHover.clear()}
        disabled={logic.busy || (full && at < 0)}
      >
        <Digit n={digits.get(opt.index)} on={at >= 0} />
        <span class="label">{#if pips !== null}Add <ManaSymbols cost={pips} />{:else}{rowText ? rowText(opt) : opt.label}{/if}</span>
        {#if at >= 0 && multi}<span class="order" data-pick-order>{decision.repeatable ? `×${count}` : at + 1}</span>{/if}
      </button>
    {/each}
  </div>
  {#if decision.payment_fallback}
    <p class="fallback" data-payment-fallback>Suggested mana could not be used ({decision.payment_fallback.reason}). Continue with the manual payment decision.</p>
  {/if}
  <PromptFooter status={liveStatus} buttons={footer} />
</div>

<style>
  .list-prompt {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    width: 100%;
    min-height: 0;
  }
  .rows {
    display: grid;
    gap: 5px;
    max-height: 18rem;
    overflow-y: auto;
    min-height: 0;
  }
  .row {
    display: flex;
    gap: 0.6rem;
    align-items: flex-start;
    text-align: left;
    padding: 0.45rem 0.6rem;
    border-radius: 8px;
    background: var(--instrument-raised);
    border: 1px solid var(--edge-inst);
    color: var(--ink-inst);
    font: 500 var(--t-12)/1.4 var(--font-ui);
    cursor: pointer;
    transition: border-color 0.12s, background 0.12s;
  }
  .row:hover:not(:disabled) {
    border-color: color-mix(in srgb, var(--ink) 30%, var(--edge-inst));
    background: color-mix(in srgb, var(--ink) 5%, var(--instrument-raised));
    color: var(--ink);
  }
  .row.picked {
    border-color: var(--gilt, #d4ad62);
    background: color-mix(in srgb, var(--gilt, #d4ad62) 9%, var(--instrument-raised));
    color: var(--ink);
  }
  .row.dim { opacity: 0.4; }
  .row:disabled { cursor: default; }
  .label { flex: 1; min-width: 0; overflow-wrap: anywhere; }
  .order {
    flex: none;
    font: 600 0.6875rem/1 var(--font-data);
    color: var(--felt-sunk);
    background: var(--gilt, #d4ad62);
    border-radius: 3px;
    padding: 0.2em 0.35em;
  }
  .remember {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    color: var(--ink-dim);
    font-size: var(--t-11);
    cursor: pointer;
  }
  .remember:hover { color: var(--ink); }
  .remember input { margin: 0; accent-color: var(--verdigris, #6fb7ae); }
  .picked-chips { display: flex; flex-wrap: wrap; gap: var(--sp-1); }
  .chip {
    background: var(--instrument-raised);
    color: var(--ink-inst);
    border: 1px solid var(--edge-inst);
    border-radius: 999px;
    padding: 0 var(--sp-2);
    font: var(--t-11)/1.6 var(--font-ui);
    cursor: pointer;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .fallback { margin: 0; font-size: var(--t-11); color: var(--ink-dim); }
</style>
