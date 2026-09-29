<script lang="ts">
  import type { Decision, PaymentAction, View } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import type { Placement } from '../../lib/prompts/renderer';
  import { genericListOptions, paymentPlanSummary, primaryOf } from '../../lib/prompts/decision';
  import { manualManaHidden } from '../../lib/manualmana';
  import { announceActions } from '../../lib/announcepay';

  /**
   * PriorityOptions is the priority window's action list under the ACTIONS
   * strip (UI rework spec §4: "priority is the action button and does not
   * use the dock"). It is deliberately NOT numbered: the pass, concede and
   * payment actions sit outside the list, so no digit could name the Nth
   * thing the player sees.
   *
   * PaymentActions are an additive extension of priority; grouping happens
   * here so a cast with a BaseOptionIndex cannot appear as two unrelated
   * actions. They are an Auto Mana affordance: with the toggle off this is
   * the untouched manual list. The manual taps are hidden only by the one
   * shared rule (lib/manualmana.ts, spec §8). With Auto-pay OFF, a cast the
   * planner can pay but the pool alone cannot is listed as a cast that
   * opens the select-mana window (announce-then-pay §8); with Auto-pay ON a
   * plan-less ManaBrew action renders the same announce fallback inside its
   * payment block — it has no plan to submit, and announce-then-pay is its
   * only legal route (never a synthesized plan).
   */
  let { decision, view, logic, seat, placement }: { decision: Decision; view: View; logic: SeatPanelState; seat: number; placement: Placement } = $props();

  const primary = $derived(primaryOf(decision));
  const paymentActions = $derived(logic.autoManaAvailable && logic.autoPayMana ? (decision.payment_actions ?? []) : []);
  const paymentBases = $derived(new Set(paymentActions.flatMap((action) => action.base_option_index === undefined || action.base_option_index === null ? [] : [action.base_option_index])));
  const hideManualMana = $derived(manualManaHidden(decision, view, seat, logic.autoPayMana));
  const announceCasts = $derived(announceActions(decision, logic.autoManaAvailable, logic.autoPayMana));

  // holdPriority is the Ctrl modifier, exactly as on every other option
  // button: a Ctrl-held planned cast skips the pass-after-acting arming.
  function castSuggested(action: PaymentAction, planID: string, holdPriority = false): void {
    const plan = action.plans.find((candidate) => candidate.id === planID);
    if (plan !== undefined) logic.submitPayment(action, plan, holdPriority);
  }
</script>

<div class="options" data-options>
  {#if paymentActions.length > 0}
    <div class="payment-actions" data-payment-actions>
      {#each paymentActions as action (action.id)}
        <div class="payment-action" data-payment-action={action.id}>
          <strong class="payment-title">{action.label}</strong>
          {#if action.plans.length > 0}
            {#each action.plans as plan, i (plan.id)}
              <p class="payment-summary">{paymentPlanSummary(plan)}</p>
              <button
                class="option payment-plan"
                class:primary={logic.autoPayMana && i === 0}
                type="button"
                data-payment-plan={plan.id}
                title={paymentPlanSummary(plan)}
                onclick={(e) => castSuggested(action, plan.id, e.ctrlKey)}
                disabled={logic.busy}
              >{i === 0 ? 'Cast with suggested mana' : 'Cast with this mana plan'}</button>
            {/each}
          {:else}
            <!-- The announce fallback (announce-then-pay §8): this action has
                 no plan to submit, so the click opens the select-mana window.
                 Under Auto-pay ON the announce list above is empty, so this
                 is the action's only control and its only data-announce
                 node; under Auto-pay OFF the payment block does not render
                 at all (announceActions serves the button instead). -->
            <button class="option payment-plan" type="button" data-announce={action.id} title="Cast, then choose the mana to pay with" onclick={(e) => logic.submitAnnounce(action, e.ctrlKey)} disabled={logic.busy}>Cast — choose mana</button>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
  {#if primary && !((placement === 'flyout' || placement === 'strip') && primary.kind === 'pass')}
    <button class="primary" type="button" data-primary onclick={(e) => logic.primaryClick(e.ctrlKey)} disabled={logic.busy}>
      {primary.label}
    </button>
  {/if}
  <div class="list">
    {#each announceCasts as action (action.id)}
      <button class="option" type="button" data-announce={action.id} title="Cast, then choose the mana to pay with" onclick={(e) => logic.submitAnnounce(action, e.ctrlKey)} disabled={logic.busy}><span class="label">{action.label}</span></button>
    {/each}
    {#each genericListOptions(decision, primary, paymentBases, hideManualMana) as opt (opt.index)}
      {@const pickedAt = logic.picked.indexOf(opt.index)}
      <button class="option" class:picked={pickedAt >= 0} type="button" data-option={opt.index} onclick={(e) => logic.click(opt.index, { holdPriority: e.ctrlKey })} disabled={logic.busy}>
        {#if pickedAt >= 0 && decision.max > 1}<span class="order">{pickedAt + 1}</span>{/if}
        <span class="label">{opt.label}</span>
      </button>
    {/each}
  </div>
  {#if logic.showSubmit}
    <button class="submit" type="button" data-submit onclick={(e) => logic.submit(e.ctrlKey)} disabled={!logic.canSubmit || logic.busy}>
      {decision.min === 0 ? 'Confirm' : decision.min === decision.max ? `Choose ${decision.min}` : `Choose ${decision.min}–${decision.max}`}
    </button>
  {/if}
  {#if decision.payment_fallback}
    <p class="payment-fallback" data-payment-fallback>Suggested mana could not be used ({decision.payment_fallback.reason}). Continue with the manual payment decision.</p>
  {/if}
</div>

<style>
  .options {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    padding: var(--sp-2);
    width: 100%;
    min-height: 0;
  }
  /* Only the option list scrolls; the primary button never scrolls away. */
  .list {
    display: flex;
    flex-direction: column;
    gap: 1px;
    max-height: 11rem;
    overflow-y: auto;
    min-height: 0;
  }
  .payment-actions { display: grid; gap: var(--sp-2); width: 100%; }
  .payment-action {
    display: grid;
    gap: var(--sp-1);
    padding: var(--sp-2);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument-raised);
  }
  .payment-title { color: var(--ink); }
  .payment-summary { margin: 0; color: var(--ink-dim); font-size: var(--t-12); }
  .payment-plan { width: 100%; justify-content: center; }
  .primary {
    background: var(--offered);
    color: var(--felt-sunk);
    border: 0;
    border-radius: var(--radius);
    padding: var(--sp-2) var(--sp-3);
    font: 600 var(--t-14) var(--font-ui);
    cursor: pointer;
  }
  .primary:disabled { opacity: 0.5; cursor: default; }
  .option {
    display: flex;
    gap: var(--sp-2);
    align-items: baseline;
    text-align: left;
    background: var(--instrument-raised);
    color: var(--ink-inst);
    border: 0;
    border-left: 2px solid transparent;
    border-radius: 0;
    padding: var(--sp-1) var(--sp-2);
    font: var(--t-12)/1.35 var(--font-ui);
    cursor: pointer;
  }
  .option:hover {
    background: color-mix(in srgb, var(--ink) 7%, var(--instrument-raised));
    border-left-color: var(--ink-dim);
    color: var(--ink);
  }
  .option.picked { border-left-color: var(--initiative); color: var(--ink); }
  .order {
    flex: none;
    font: 0.6875rem/1 var(--font-data);
    color: var(--felt-sunk);
    background: var(--initiative);
    border-radius: 2px;
    padding: 0.15em 0.3em;
  }
  .submit {
    background: var(--instrument-raised);
    color: var(--ink-inst);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    padding: var(--sp-1) var(--sp-2);
    font: var(--t-12) var(--font-ui);
    cursor: pointer;
  }
  .submit:disabled { opacity: 0.4; cursor: default; }
  .payment-fallback { margin: 0; font-size: var(--t-11); color: var(--ink-dim); }
</style>
