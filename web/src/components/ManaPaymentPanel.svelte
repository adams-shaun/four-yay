<script lang="ts">
  import type { Decision, ManaPaymentWindow, View } from '../protocol';
  import ManaSymbols from './ManaSymbols.svelte';
  import {
    manaAmountText, manaOptionAccessibleName, manaOptionPips, manaSourceRows, owesNothing, paymentCostText, windowAction,
  } from '../lib/announcepay';

  /**
   * ManaPaymentPanel is the "select mana" prompt: the announced CR 601.2g
   * window of announce-then-pay (docs/superpowers/specs/
   * 2026-09-27-announce-then-pay.md §4, §8). It reads the window's readout
   * (decision.mana_payment: total cost, still owed, pool, the Auto-fill
   * sources) and regroups the offered "mana" options by source. Every button
   * posts one offered option by its own index (R-E4-1) through onPick, the
   * seat panel's ordinary click path; nothing here decides what a source can
   * pay (R-E4-2). The same options also mark and answer on the battlefield
   * through the shared card-options index, so the board is the second
   * surface of this one decision.
   *
   * It sits inside the seat panel's instrument register: hairline sections,
   * the raised option rows, mana pips in their own identity colours.
   */
  let { decision, view, busy = false, onPick }: {
    decision: Decision & { mana_payment: ManaPaymentWindow };
    view: View | null;
    busy?: boolean;
    onPick: (index: number) => void;
  } = $props();

  const mp = $derived(decision.mana_payment);
  const rows = $derived(manaSourceRows(decision, view));
  const cost = $derived(paymentCostText(mp.cost));
  const owed = $derived(paymentCostText(mp.owed));
  const pool = $derived(manaAmountText(mp.pool));
  const autofill = $derived(windowAction(decision, 'autofill'));
  const undo = $derived(windowAction(decision, 'undo_tap'));
  const pay = $derived(windowAction(decision, 'done'));
  const cancel = $derived(windowAction(decision, 'cancel_cast'));
</script>

<section class="mana-pay" data-mana-payment aria-label={decision.prompt}>
  <dl class="mp-readout">
    <div class="mp-cell" data-mp-cost>
      <dt>Cost</dt>
      <dd>{#if cost}<ManaSymbols {cost} />{:else}<span class="mp-none">free</span>{/if}</dd>
    </div>
    <div class="mp-cell" data-mp-owed>
      <dt>Still owed</dt>
      <dd>{#if !owesNothing(mp.owed)}<ManaSymbols cost={owed} />{:else}<span class="mp-none">nothing</span>{/if}</dd>
    </div>
    <div class="mp-cell" data-mp-pool>
      <dt>Pool</dt>
      <dd>{#if pool}<ManaSymbols cost={pool} />{:else}<span class="mp-none">empty</span>{/if}</dd>
    </div>
  </dl>

  {#if rows.length > 0}
    <ul class="mp-sources" aria-label="Available mana abilities">
      {#each rows as row (row.obj)}
        <li class="mp-source" class:suggested={row.suggested} data-mana-source={row.obj}>
          <span class="mp-name" title={row.suggested ? `${row.name} (Auto-fill would tap it)` : row.name}>{row.name}</span>
          <span class="mp-abilities">
            {#each row.options as o (o.index)}
              {@const pips = manaOptionPips(o.label)}
              <button
                class="mp-ability"
                class:text={pips === null}
                type="button"
                data-option={o.index}
                aria-label={manaOptionAccessibleName(row.name, o)}
                title={o.label}
                disabled={busy}
                onclick={() => onPick(o.index)}
              >{#if pips !== null}<ManaSymbols cost={pips} />{:else}{o.label}{/if}</button>
            {/each}
          </span>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="mp-empty" data-mp-no-sources>No untapped mana source can help. Undo a tap or cancel the cast.</p>
  {/if}

  <div class="mp-actions">
    {#if autofill}
      <button class="mp-act mp-fill" type="button" data-mp-autofill={autofill.index} title={autofill.label} disabled={busy} onclick={() => onPick(autofill.index)}>{autofill.label}</button>
    {/if}
    {#if pay}
      <button class="mp-act mp-fill" type="button" data-mp-pay={pay.index} disabled={busy} onclick={() => onPick(pay.index)}>{pay.label}</button>
    {/if}
    {#if undo}
      <button class="mp-act" type="button" data-mp-undo={undo.index} title={undo.label} disabled={busy} onclick={() => onPick(undo.index)}>Undo last tap</button>
    {/if}
    {#if cancel}
      <button class="mp-act mp-cancel" type="button" data-mp-cancel={cancel.index} disabled={busy} onclick={() => onPick(cancel.index)}>Cancel cast</button>
    {/if}
  </div>
  {#if decision.payment_fallback}
    <p class="mp-fallback" data-payment-fallback>Auto-fill stopped ({decision.payment_fallback.reason}); finish paying by hand.</p>
  {/if}
</section>

<style>
  .mana-pay {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding: var(--sp-2);
    width: 100%;
    min-height: 0;
  }
  .mp-readout {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 1px;
    margin: 0;
    background: var(--edge-inst);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .mp-cell {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    padding: var(--sp-1);
    background: var(--instrument-raised);
    min-width: 0;
  }
  .mp-cell dt {
    font-size: var(--t-11);
    color: var(--ink-dim);
  }
  .mp-cell dd {
    margin: 0;
    min-height: 15px;
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
  }
  [data-mp-owed] dt { color: var(--initiative); }
  .mp-none {
    font-size: var(--t-11);
    color: var(--ink-faint);
  }
  .mp-sources {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
    max-height: 11rem;
    overflow-y: auto;
  }
  .mp-source {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    padding: var(--sp-1) var(--sp-2);
    background: var(--instrument-raised);
    border-left: 2px solid transparent;
  }
  .mp-source.suggested { border-left-color: var(--offered); }
  .mp-name {
    font-size: var(--t-12);
    color: var(--ink-inst);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .mp-abilities {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--sp-1);
    flex: none;
    max-width: 65%;
  }
  .mp-ability {
    display: inline-flex;
    align-items: center;
    min-width: 1.75rem;
    min-height: 1.75rem;
    justify-content: center;
    padding: 2px var(--sp-1);
    background: transparent;
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-11);
    cursor: pointer;
  }
  .mp-ability.text { max-width: 100%; text-align: left; }
  .mp-ability:hover:not(:disabled) {
    border-color: var(--offered);
    background: color-mix(in srgb, var(--offered) 12%, var(--instrument-raised));
  }
  .mp-ability:disabled, .mp-act:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .mp-empty {
    margin: 0;
    font-size: var(--t-12);
    color: var(--ink-dim);
    text-align: center;
  }
  .mp-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
  }
  .mp-act {
    flex: 1 1 auto;
    background: var(--instrument-raised);
    color: var(--ink-inst);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    padding: var(--sp-1) var(--sp-2);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mp-fill {
    flex-basis: 100%;
    background: var(--offered);
    border-color: var(--offered);
    color: var(--felt-sunk);
    font-weight: 600;
  }
  .mp-cancel:hover:not(:disabled) { border-color: var(--initiative); color: var(--ink); }
  .mp-act:not(.mp-fill):hover:not(:disabled) { border-color: var(--ink-dim); }
  .mp-fallback {
    margin: 0;
    font-size: var(--t-11);
    color: var(--ink-dim);
  }
</style>
