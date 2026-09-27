<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import type { SeatInfo, View } from '../protocol';
  import type { SeatCtx } from '../lib/seat';
  import { hotkeyAction } from '../lib/hotkeys';
  import { modalPickerOpen } from '../lib/modals';
  import { postUndo } from '../lib/api';
  import { clientBreadcrumbs } from '../lib/breadcrumbs';
  import { turnSide } from '../lib/autopilot';
  import { autoNoteText, isConcede, toneOf, type SeatPanelState } from '../lib/seatpanel.svelte';
  import SeatPanel from './SeatPanel.svelte';

  /** A short grace period keeps a diagonal tab-to-panel pointer path open. */
  const HOT_STRIP_CLOSE_DELAY_MS = 180;

  let { view, seats, state: logic, ctx, table, match, showLog: _showLog = false, onToggleLog: _onToggleLog = null }: {
    view: View;
    seats: SeatInfo[];
    state: SeatPanelState;
    ctx: SeatCtx;
    table: string;
    match: number;
    /** Retained during the rail migration so existing callers stay compatible. */
    showLog?: boolean;
    onToggleLog?: (() => void) | null;
  } = $props();

  type Tab = 'actions' | 'pass' | 'ffwd' | 'done' | 'options';
  let open = $state<Tab | null>(null);
  let closeTimer: ReturnType<typeof setTimeout> | null = null;
  let undoPosting = $state(false);

  const decision = $derived(logic.active);
  // The ACTIONS tab projects the seat panel's own tone (R-E4-1: resolved from
  // option KINDS, never labels or prompt text). The tab is the only part of
  // this instrument visible with the dropdown closed, so this is where "the
  // game is blocked on you" has to be visible: initiative pulses warm,
  // a declinable window sits steadily lit cool, idle is unlit. logic.active
  // is null while the answer is posted/in flight, so the affordance drops
  // the moment the decision is answered.
  const awaiting = $derived(toneOf(decision));
  // A required response is anchored to the ACTIONS control as soon as it
  // arrives. It is not a floating board overlay, and it stays open until the
  // answer changes the decision.
  $effect(() => {
    if (awaiting === 'initiative') {
      clearClose();
      open = 'actions';
    }
  });
  // Suggested payment plans replace their ordinary cast affordance while
  // Auto Mana is enabled. They are still real actionable choices: a priority
  // window containing only a payable spell must not make ACTIONS look disabled.
  const visiblePaymentActions = $derived(
    logic.autoManaAvailable && logic.autoPayMana && decision?.kind === 'priority'
      ? (decision.payment_actions ?? []).filter((action) => action.plans.length > 0)
      : [],
  );
  const paymentBaseIndexes = $derived(new Set(visiblePaymentActions.flatMap((action) =>
    action.base_option_index === undefined || action.base_option_index === null ? [] : [action.base_option_index],
  )));
  function isManualMana(option: { kind: string; label: string }): boolean {
    return option.kind === 'activate' && / for mana$/i.test(option.label);
  }
  const actionCount = $derived(
    (decision?.options.filter((option) =>
      option.kind !== 'pass' && !isConcede(option)
      && (!logic.autoPayMana || (!paymentBaseIndexes.has(option.index) && !isManualMana(option))),
    ).length ?? 0) + visiblePaymentActions.length,
  );
  const passAvailable = $derived(logic.passOption !== null && !logic.busy);
  // Undo is a whole-table rollback and has no consent flow. The server is
  // authoritative, but the snapshot's human markers let the client disable
  // the control whenever this is not the sole human seat.
  const humanSeats = $derived(seats.flatMap((s, i) => s.human ? [i] : []));
  // A just-clicked action has not necessarily reached the host log yet. An
  // Undo in that interval was admitted as "nothing to undo", even though the
  // action then landed a moment later. Wait for the shared post latch; the
  // normal rewind frame will reset the panel and leave Undo usable again.
  const undoAllowed = $derived(humanSeats.length === 1 && humanSeats[0] === ctx.seat && !undoPosting && !logic.busy);
  // Fast forward became End Turn (prio3): a one-shot to the end of the
  // CURRENT turn. It can only advance through a real pass option, and it
  // only makes sense on a turn the seat OWNS — arming it on the opponent's
  // turn would just auto-pass their turn, machinery the player never asked
  // for (the "END TURN shouldn't be available during opponents turn" report).
  // turnSide is the one-vocabulary helper for the active-seat comparison.
  // considerAuto is still the safety oracle; this gate merely avoids
  // inventing a control whose scope would be somebody else's turn.
  const endTurnAvailable = $derived(passAvailable && turnSide(view, ctx.seat) === 'yours');
  // The keyboard hard-skip (Shift+Enter) is NOT turn-gated: skipping an
  // opponent's turn is a deliberate, warned one-click move (MTGO F6) and its
  // own chip says so. It shares only the pass-option requirement with END
  // TURN. Note the accepted consequence: with the END TURN button disabled on
  // an opponent turn, shift-CLICK on it is dead there (a disabled button
  // swallows clicks) — Shift+Enter still arms the skip.
  const hardSkipAvailable = $derived(passAvailable);
  // Resolve All always owns its fixed slot. It is disabled outside a live
  // stack priority window, so its availability never shifts the strip.
  const resolveAllAvailable = $derived(decision !== null && decision.kind === 'priority' && view.stack.length > 0 && passAvailable);
  const runLive = $derived(logic.oneShot !== 'none');
  const doneAvailable = $derived(
    decision !== null && logic.showSubmit && logic.canSubmit && !logic.busy,
  );
  const doneShown = $derived(decision !== null && logic.showSubmit);
  const doneFull = $derived.by(() => {
    switch (decision?.kind) {
      case 'attackers': return 'Done selecting attackers';
      case 'blockers': return 'Done selecting blockers';
      case 'target': return 'Done selecting targets';
      case 'modes': return 'Done picking modes';
      case 'trigger_order': return 'Done ordering triggers';
      case 'choose': return 'Done picking';
      default: return 'Done selecting';
    }
  });

  function clearClose(): void {
    if (closeTimer !== null) clearTimeout(closeTimer);
    closeTimer = null;
  }
  function show(tab: Tab): void {
    clearClose();
    open = tab;
  }
  function scheduleClose(): void {
    // A decision the game is waiting on must remain attached to ACTIONS.
    // Hover is only a convenience for offered priority windows; it must never
    // make a required answer disappear while the player moves to a choice.
    if (awaiting === 'initiative') return;
    clearClose();
    closeTimer = setTimeout(() => {
      open = null;
      closeTimer = null;
    }, HOT_STRIP_CLOSE_DELAY_MS);
  }
  function escape(e: KeyboardEvent): void {
    if (e.key !== 'Escape' || open === null || awaiting === 'initiative') return;
    e.preventDefault();
    open = null;
    clearClose();
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
  }
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

  function endTurn(e: MouseEvent): void {
    if (!endTurnAvailable) return;
    // Shift-click is the hard skip: pass everything, opponent objects
    // included, for the rest of the turn (MTGO F6).
    if (e.shiftKey) logic.startHardSkip(view);
    else logic.startEndTurn(view);
    logic.considerAuto(view);
  }

  /**
   * PLAY_MODE_LABEL is the status chip's text for each data-play-mode value:
   * the preset labels speak the settings model's names, the runs speak
   * theirs. The hard skip's label carries its own warning — the chip IS the
   * "Skipping turn — Esc to stop" banner while the run is live.
   */
  const PLAY_MODE_LABEL: Record<string, string> = {
    'casual': 'Casual',
    'no-tells': 'No tells',
    'full-control': 'Full control',
    'custom': 'Custom',
    'end-turn': 'END TURN',
    'skip-turn': 'Skipping turn — Esc to stop',
    'resolve-all': 'RESOLVE ALL',
  };
  const playMode = $derived(logic.playMode);
  const playModeLabel = $derived(PLAY_MODE_LABEL[playMode] ?? 'Custom');
  const autoStatus = $derived(logic.machinePaused ? autoNoteText(logic.note) : playModeLabel);

  onMount(() => {
    // The document-level hotkeys (prio3). The grammar lives in lib/hotkeys
    // (pure, tested); this is only its wiring. The modal-picker probe reads
    // the live DOM via lib/modals (both card-action picker shapes AND the
    // pile modals — reviews found each unmarked surface could read hotkeys
    // aimed underneath it). The listener runs in the CAPTURE
    // phase so the probe is evaluated before any bubble-phase handler —
    // OptionPicker's and PileModal's own Escape-close — can close the modal
    // and make the guard see a closed modal a few ticks later.
    const onKey = (e: KeyboardEvent): void => {
      const action = hotkeyAction(e, modalPickerOpen);
      if (action === null) return;
      e.preventDefault();
      clientBreadcrumbs.record('hotkey', { key: e.key, action });
      switch (action) {
        case 'pass':
          logic.passClick();
          break;
        case 'end-turn':
          if (!endTurnAvailable) return;
          logic.startEndTurn(view);
          logic.considerAuto(view);
          break;
        case 'hard-skip':
          if (!hardSkipAvailable) return;
          logic.startHardSkip(view);
          logic.considerAuto(view);
          break;
        case 'cancel-run':
          logic.cancelRun();
          break;
        case 'toggle-full-control':
          logic.toggleFullControl();
          break;
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  });
  onDestroy(clearClose);
</script>

<div class="hot-strip" data-hot-strip role="toolbar" aria-label="Game controls" tabindex="-1" onkeydown={escape}>
  <!-- The status chip: which mode the seat is in. A live run outranks the
       preset label, and the hard skip's chip IS the warning banner. Undo's
       machine pause outranks both: this is the only always-visible status in
       the live strip (its nested SeatPanel deliberately hides the autobar),
       so the chip also becomes the Auto switch that its text says resumes. -->
  {#if logic.machinePaused}
    <button
      class="mode-chip paused"
      type="button"
      data-play-mode="paused"
      data-auto-status
      data-auto-note
      aria-live="polite"
      aria-label={autoStatus}
      title={autoStatus}
      onclick={() => logic.pressAuto()}
    ><span aria-hidden="true">AUTO</span></button>
  {:else}
    <span
      class="mode-chip"
      class:run={runLive}
      class:warning={logic.hardSkip}
      data-play-mode={playMode}
      data-auto-status
      aria-live="polite"
      aria-label={`Auto: ${autoStatus}`}
      title={`Auto: ${autoStatus}`}
    ><span aria-hidden="true">AUTO</span></span>
  {/if}
  {#if logic.autoManaAvailable}
  <button
    class="mode-chip payment-toggle"
    class:run={logic.autoPayMana}
    type="button"
    role="switch"
    aria-checked={logic.autoPayMana}
    aria-label="Auto-pay mana"
    title="Use a suggested mana plan when casting"
    data-auto-pay-toggle
    onclick={() => logic.setAutoPayMana(!logic.autoPayMana)}
  >AUTO MANA</button>
  {/if}
  <div class="hot-tab" role="presentation" onpointerenter={() => show('actions')} onpointerleave={scheduleClose} onfocusin={() => show('actions')} onfocusout={scheduleClose}>
    <button class="tab" type="button" data-hot-tab="actions" data-awaiting={awaiting} aria-label="Actions" aria-haspopup="true" aria-expanded={open === 'actions'} aria-controls="hot-panel-actions" aria-disabled={actionCount === 0} onclick={() => show('actions')}>
      <span class="full">ACTIONS</span><span class="compact" aria-hidden="true">A</span>
    </button>
    <div class="drop actions" class:open={open === 'actions'} id="hot-panel-actions" data-hot-panel="actions" role="group" aria-label="Available actions">
      <!-- SeatPanel owns the polling/autopilot lifecycle as well as this
           option list, so it stays mounted even when no action is offered.
           Hiding it in that case would also silently disable Skip Empty. -->
      <SeatPanel {view} {seats} {ctx} {table} {match} state={logic} placement="strip" />
      {#if actionCount === 0}
        <p class="unavailable">No action is offered by this decision.</p>
      {/if}
    </div>
  </div>

  <!-- A transport control, not a menu: one action behind it, so one click.
       A dropdown here made the commonest move on the board cost two. The
       title carries the wire option's own label so the glyph is never the
       only thing telling you what it does. -->
  <div class="hot-tab direct" role="presentation">
    <button
      class="tab"
      type="button"
      data-hot-tab="pass"
      data-pass-action
      aria-label="Pass"
      aria-disabled={!passAvailable}
      disabled={!passAvailable}
      title={logic.passOption?.label ?? 'Pass is not offered by this decision'}
      onclick={() => logic.passClick()}
    >
      <span class="full">PASS</span><span class="compact" aria-hidden="true">&gt;</span>
    </button>
  </div>

  <div class="hot-tab direct" role="presentation">
    <button
      class="tab"
      class:on={runLive}
      type="button"
      data-hot-tab="end-turn"
      data-end-turn
      aria-label="End turn"
      aria-pressed={runLive}
      aria-disabled={!endTurnAvailable}
      disabled={!endTurnAvailable}
      title={endTurnAvailable
        ? 'End Turn: pass the rest of this turn (Shift: skip everything, Esc stops)'
        : passAvailable ? 'End Turn is for your own turn' : 'End Turn needs a pass option'}
      onclick={endTurn}
    >
      <span class="full">END TURN</span><span class="compact" aria-hidden="true">&gt;&gt;</span>
    </button>
  </div>

  <div class="hot-tab direct" role="presentation">
      <button
        class="tab"
        class:on={logic.resolveAll}
        type="button"
        data-hot-tab="resolve-all"
        data-resolve-all
        aria-label="Resolve all"
        aria-pressed={logic.resolveAll}
        aria-disabled={!resolveAllAvailable}
        disabled={!resolveAllAvailable}
        title={resolveAllAvailable
          ? 'Resolve All: pass until the stack is empty — a new opponent play or a decision that needs you stops it'
          : 'Resolve All needs a pass option'}
        onclick={() => {
          if (!resolveAllAvailable) return;
          logic.startResolveAll(view);
          logic.considerAuto(view);
        }}
      >
        <span class="full">RESOLVE ALL</span><span class="compact" aria-hidden="true">RA</span>
      </button>
  </div>

  <div class="hot-tab direct" role="presentation">
    <button
      class="tab"
      type="button"
      data-hot-tab="undo"
      data-undo
      aria-label="Undo my last action"
      aria-disabled={!undoAllowed}
      disabled={!undoAllowed}
      title={undoAllowed ? 'Undo my last action' : 'Undo is available only when you are the table’s sole human player'}
      onclick={() => void undo()}
    >
      <span class="full">UNDO</span><span class="compact" aria-hidden="true">↶</span>
    </button>
  </div>

  <!-- Done is one action too, so it follows Pass, End Turn and Undo. Ctrl held
       while submitting a cast/ability holds priority: passAfterAct is
       skipped for that one action. -->
  <div class="hot-tab contextual direct" role="presentation">
    <button
      class="tab"
      type="button"
      data-hot-tab="done"
      data-done-action
      aria-label={doneFull}
      aria-disabled={!doneAvailable}
      disabled={!doneAvailable}
      title={doneShown ? doneFull : 'This decision does not need a separate selection submit'}
      onclick={(e) => logic.submit(e.ctrlKey)}
    >
      <span>DONE</span>
    </button>
  </div>

</div>

<style>
  /* Tabs share an edge with the phase row above: this is one instrument. Each
     wrapper contains both tab and panel, so the pointer crosses no dead felt.
     JavaScript applies HOT_STRIP_CLOSE_DELAY_MS after leaving that contiguous
     region; focus uses the same region and Escape closes it immediately. */
  .hot-strip {
    position: relative;
    z-index: 2;
    display: flex;
    justify-content: center;
    height: var(--hot-strip-h);
    min-width: 0;
    font-family: var(--font-ui);
  }
  .hot-tab {
    position: relative;
    height: 100%;
  }
  .tab {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    min-width: 4rem;
    padding: 0 var(--sp-2);
    border: var(--edge-w) solid var(--edge-inst);
    border-top: 0;
    border-right: 0;
    border-radius: 0 0 var(--radius) var(--radius);
    background: var(--instrument);
    color: var(--ink-inst);
    font-size: var(--t-11);
    font-weight: 700;
    cursor: pointer;
    white-space: nowrap;
  }
  .mode-chip + .hot-tab .tab { border-left-color: color-mix(in srgb, var(--seat) 34%, var(--edge-inst)); }
  .hot-tab:last-child .tab { border-right: var(--edge-w) solid var(--edge-inst); }
  .tab.on,
  .tab:active,
  .tab[aria-expanded='true'] {
    color: var(--ink);
    background: color-mix(in srgb, var(--offered) 22%, var(--instrument-raised));
    border-bottom-color: var(--offered);
    box-shadow: 0 0 0 1px var(--offered), 0 0 12px var(--offered);
  }
  .tab[aria-disabled='true'] {
    color: var(--ink-faint);
    cursor: default;
  }
  /* The awaiting projection: with the dropdown closed, the tab itself says
     whether the game needs this seat. initiative (the decision carries no
     pass -- nothing happens anywhere at the table until you answer) pulses
     in the warm state colour; offered (a window you may decline) sits
     steadily lit in the cool one -- pulsing every priority window would make
     the pulse meaningless, since those recur constantly. Both reuse the
     state colour variables, never a literal. The steady lit look is the
     BASE and the keyframes only modulate the glow on top of it, so under
     prefers-reduced-motion (0.01ms, one iteration) the animation ends
     immediately and the tab rests statically lit, never unlit.
     data-awaiting carries the same state machine-readably for tests. These
     rules sit after the aria-disabled grey so an initiative decision whose
     only remaining option is concede still reads as "blocked on you" -- the
     grey stays for the genuinely idle case, where data-awaiting is idle and
     no lit rule applies at all. The compact "A" glyph lives on this same
     button, so the max-width treatment inherits it for free. */
  .tab[data-awaiting='initiative'],
  .tab[data-awaiting='offered'] {
    color: var(--ink);
  }
  .tab[data-awaiting='offered'] {
    border-bottom-color: var(--offered);
    box-shadow: 0 0 0 1px var(--offered), 0 0 12px var(--offered);
  }
  .tab[data-awaiting='initiative'] {
    border-bottom-color: var(--initiative);
    box-shadow: 0 0 0 1px var(--initiative), 0 0 12px var(--initiative);
    animation: hot-await-pulse 1.8s ease-in-out infinite;
  }
  @keyframes hot-await-pulse {
    0%, 100% { box-shadow: 0 0 0 1px var(--initiative), 0 0 12px var(--initiative); }
    50% { box-shadow: 0 0 0 2px var(--initiative), 0 0 20px var(--initiative); }
  }
  .compact { display: none; }

  /* The status chip: the seat's mode, stated once, at the left edge of the
     strip. Presets read as labels; a live run takes the run register — the
     hard skip's warning colour is the danger variable, because passing
     everything unseen is the one state that can lose the game in silence.
     The undo pause uses the offered colour and a pointer because this chip is
     also the always-visible resume control. */
  .mode-chip {
    box-sizing: border-box;
    justify-content: center;
    width: 5.25rem;
    display: flex;
    align-items: center;
    padding: 0 var(--sp-2);
    border: var(--edge-w) solid var(--edge-inst);
    border-top: 0;
    border-left: var(--edge-w) solid var(--edge-inst);
    border-radius: 0 0 var(--radius) var(--radius);
    background: var(--instrument);
    color: var(--ink-dim);
    font-size: var(--t-11);
    font-weight: 700;
    letter-spacing: 0.02em;
    white-space: nowrap;
  }
  .mode-chip.run {
    color: var(--felt-sunk);
    background: var(--offered);
    border-color: var(--offered);
  }
  .mode-chip.warning {
    background: var(--danger);
    border-color: var(--danger);
  }
  .mode-chip.paused {
    color: var(--felt-sunk);
    background: var(--offered);
    border-color: var(--offered);
    cursor: pointer;
  }
  .mode-chip.paused:hover,
  .mode-chip.paused:focus-visible {
    box-shadow: 0 0 0 1px var(--offered), 0 0 12px var(--offered);
  }

  .drop {
    position: absolute;
    top: 100%;
    left: 50%;
    z-index: 9;
    width: min(24rem, calc(100vw - var(--sp-4)));
    max-height: min(42vh, 28rem);
    overflow-y: auto;
    transform: translateX(-50%);
    visibility: hidden;
    pointer-events: none;
    background: var(--instrument);
    border: var(--edge-w) solid var(--edge-inst);
    border-radius: var(--radius);
    color: var(--ink-inst);
  }
  .drop.open {
    visibility: visible;
    pointer-events: auto;
  }
  .actions :global(.seat-panel.strip) {
    width: 100%;
    min-width: 0;
    max-height: none;
    border: 0;
    border-radius: 0;
    backdrop-filter: none;
  }

  @media (max-width: 60rem) {
    .full { display: none; }
    .compact { display: inline; }
    .tab { min-width: 2.5rem; padding: 0 var(--sp-1); }
    .contextual .tab { min-width: 5.5rem; }
  }
</style>
