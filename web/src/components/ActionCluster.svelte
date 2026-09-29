<script lang="ts">
  import type { SeatInfo, View } from '../protocol';
  import type { SeatCtx } from '../lib/seat';
  import type { SeatPanelState } from '../lib/seatpanel.svelte';
  import { postUndo } from '../lib/api';
  import { clientBreadcrumbs } from '../lib/breadcrumbs';
  import { turnSide } from '../lib/autopilot';
  import { bindingLabel } from '../lib/keymap';
  import { keymapStore } from '../lib/keymap.svelte';
  import { passLabel } from '../lib/passlabel';

  /**
   * ActionCluster is the ONE action button (UI rework spec §3): gilt, in the
   * hand row's right corner, naming what passing does ("Pass — Move to
   * Declare attackers", "Let Rhystic Study resolve"), and "Waiting" while a
   * prompt needs an answer or another seat acts. Above it sit the secondary
   * moves: Undo, Until my turn (the hard skip, offered on an opponent's
   * turn), End turn (your turn) and Resolve all (a live stack).
   *
   * It posts nothing of its own: every button is the seat state's existing
   * path (passClick, the one-shot runs, postUndo) with the same availability
   * rules the hot strip uses, and the keyboard bindings stay in that strip.
   */
  let { view, seats, state: logic, ctx, table, match }: {
    view: View;
    seats: SeatInfo[];
    state: SeatPanelState;
    ctx: SeatCtx;
    table: string;
    match: number;
  } = $props();

  let undoPosting = $state(false);
  const passAvailable = $derived(logic.passOption !== null && !logic.busy);
  const humanSeats = $derived(seats.flatMap((s, i) => (s.human ? [i] : [])));
  const undoAllowed = $derived(humanSeats.length === 1 && humanSeats[0] === ctx.seat && !undoPosting && !logic.busy);
  const yours = $derived(turnSide(view, ctx.seat) === 'yours');
  const endTurnAvailable = $derived(passAvailable && yours);
  const skipAvailable = $derived(passAvailable && !yours);
  const resolveAllAvailable = $derived(logic.active?.kind === 'priority' && view.stack.length > 0 && passAvailable);
  const runLive = $derived(logic.oneShot !== 'none');
  const prompt = $derived(logic.active !== null && logic.passOption === null);
  const waitingOn = $derived(
    view.priority !== ctx.seat && view.priority !== undefined && view.priority !== null
      ? (seats[view.priority]?.name || view.players.find((p) => p.seat === view.priority)?.name || null)
      : null,
  );
  const label = $derived(passLabel({ view, passAvailable, prompt, waitingOn }));
  const passKey = $derived(keymapStore.current.pass[0] ? bindingLabel(keymapStore.current.pass[0]) : null);
  const skipLabel = $derived(view.players.length > 2 ? 'Skip their turn' : 'Until my turn');

  async function undo(): Promise<void> {
    if (!undoAllowed) return;
    undoPosting = true;
    logic.error = null;
    clientBreadcrumbs.record('undo', { table, match });
    try {
      await postUndo(table, match, ctx);
    } catch (e) {
      logic.error = e instanceof Error ? e.message : String(e);
    } finally {
      undoPosting = false;
    }
  }
</script>

<div class="cluster" data-action-cluster>
  <div class="secondary">
    <button type="button" data-cluster-undo disabled={!undoAllowed} title={undoAllowed ? 'Undo my last action' : 'Undo is available only when you are the table’s sole human player'} onclick={() => void undo()}>Undo</button>
    {#if resolveAllAvailable}
      <button type="button" data-cluster-resolve-all class:on={logic.resolveAll} onclick={() => { logic.startResolveAll(view); logic.considerAuto(view); }}>Resolve all</button>
    {/if}
    {#if yours}
      <button type="button" data-cluster-end-turn class:on={runLive && !logic.hardSkip} disabled={!endTurnAvailable} title="Pass the rest of this turn; Esc stops" onclick={() => { logic.startEndTurn(view); logic.considerAuto(view); }}>End turn</button>
    {:else}
      <button type="button" data-cluster-skip class:on={logic.hardSkip} disabled={!skipAvailable} title="Pass everything for the rest of this turn; Esc stops" onclick={() => { logic.startHardSkip(view); logic.considerAuto(view); }}>{skipLabel}</button>
    {/if}
  </div>
  <button
    type="button"
    class="go"
    class:waiting={!label.passes}
    data-pass-action
    data-action-button
    disabled={!label.passes}
    aria-label={label.passes ? `${label.title}: ${label.sub}` : `${label.title} ${label.sub}`}
    title={logic.passOption?.label ?? label.sub}
    onclick={() => logic.passClick()}
  >
    <span class="words">
      <span class="title">{label.title}</span>
      <span class="sub">{label.sub}</span>
    </span>
    {#if passKey && label.passes}<kbd>{passKey}</kbd>{/if}
  </button>
</div>

<style>
  .cluster {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
    width: min(15rem, 100%);
  }
  .secondary {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
  }
  .secondary button {
    padding: 0.2rem 0.6rem;
    border: 1px solid var(--edge-inst);
    border-radius: 6px;
    background: var(--instrument);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    cursor: pointer;
    white-space: nowrap;
  }
  .secondary button:hover:not(:disabled) {
    border-color: var(--ink-dim);
    color: var(--ink);
  }
  .secondary button:disabled {
    color: var(--ink-faint);
    cursor: default;
  }
  .secondary button.on {
    border-color: var(--gilt);
    color: var(--gilt);
  }
  .go {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    padding: 0.55rem 0.9rem;
    border: 1px solid color-mix(in srgb, var(--gilt) 70%, #fff);
    border-radius: 10px;
    background: linear-gradient(180deg, color-mix(in srgb, var(--gilt) 88%, #fff) 0%, var(--gilt) 100%);
    color: #2a1f0e;
    cursor: pointer;
    box-shadow: 0 6px 18px rgb(0 0 0 / 0.35);
  }
  .go:hover:not(:disabled) {
    filter: brightness(1.06);
  }
  .go.waiting {
    border-color: var(--edge-inst);
    background: var(--instrument);
    color: var(--ink-dim);
    cursor: default;
    box-shadow: none;
  }
  .words {
    display: flex;
    flex-direction: column;
    align-items: center;
    flex: 1;
    min-width: 0;
  }
  .title {
    font-family: var(--font-serif);
    font-weight: 700;
    font-size: var(--t-20);
    line-height: 1.1;
  }
  .sub {
    max-width: 100%;
    font-family: var(--font-ui);
    font-size: var(--t-12);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  kbd {
    flex: none;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    background: rgb(0 0 0 / 0.12);
    font-family: var(--font-data);
    font-size: var(--t-11);
    font-weight: 600;
  }
</style>
