<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import type { SeatInfo, View } from '../protocol';
  import type { SeatCtx } from '../lib/seat';
  import { hotkeyAction } from '../lib/hotkeys';
  import { keymapStore } from '../lib/keymap.svelte';
  import { modalPickerOpen } from '../lib/modals';
  import { postUndo } from '../lib/api';
  import { clientBreadcrumbs } from '../lib/breadcrumbs';
  import { turnSide } from '../lib/autopilot';
  import { autoNoteText, isConcede, toneOf, type SeatPanelState } from '../lib/seatpanel.svelte';
  import { isPlainManualTap, manualManaHidden } from '../lib/manualmana';
  import { announceActions } from '../lib/announcepay';
  import SeatPanel from './SeatPanel.svelte';
  import { dockAnswers } from '../lib/prompts/renderer';
  import KeyCheatSheet from './KeyCheatSheet.svelte';

  /**
   * PRESET_CYCLE is the hotkey cycle's leading entries: the three shipped
   * presets in picker order, ahead of the player's saved profiles. Kept here
   * (the strip already knows the presets through the panel's picker) as a
   * literal list so the pure cycle order is independent of any rendered panel.
   */
  const PRESET_CYCLE = ['casual', 'no-tells', 'full-control'] as const;

  /** A short grace period keeps a diagonal tab-to-panel pointer path open. */
  const HOT_STRIP_CLOSE_DELAY_MS = 180;

  let { view, seats, state: logic, ctx, table, match, onToggleOptions = null, transport = true }: {
    /** transport draws the PASS / END TURN / RESOLVE ALL / UNDO tabs. The
     *  table turns it off because the gilt action cluster (ActionCluster)
     *  offers those moves in its fixed corner; the hotkeys stay here either
     *  way. */
    transport?: boolean;
    view: View;
    seats: SeatInfo[];
    state: SeatPanelState;
    ctx: SeatCtx;
    table: string;
    match: number;
    /** Ctrl+Shift+O's write path. The Game Options popover is owned by the
     *  route (Table.svelte owns optionsOpen and its outside-click/Escape
     *  teardown), so the strip does NOT own that state: the route passes its
     *  own toggle down through this callback. Absent/null leaves the hotkey a
     *  no-op (a context with no options panel). */
    onToggleOptions?: (() => void) | null;
  } = $props();

  type Tab = 'actions' | 'pass' | 'ffwd' | 'done' | 'options';
  let open = $state<Tab | null>(null);
  let closeTimer: ReturnType<typeof setTimeout> | null = null;
  let undoPosting = $state(false);
  // The `?` cheat sheet's open state (show-keys hotkey); the sheet itself
  // mounts in the next step of the keymap work.
  let showKeys = $state(false);

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
  // arrives — unless the prompt dock answers it (UI rework spec §4: the
  // dock is the answer surface for every non-priority decision while it is
  // mounted, so ACTIONS stays shut instead of opening a second copy over
  // the board). Otherwise it stays open until the answer changes it.
  const pinned = $derived(awaiting === 'initiative' && !(logic.dockCount > 0 && dockAnswers(decision)));
  // pinnedOpened records that the panel is open because `pinned` opened it,
  // not because the player hovered or clicked the tab. The prompt dock mounts
  // a beat AFTER the decision lands (its dockCount is set in onMount), so on
  // a fresh mount into a live non-priority decision the first effect run sees
  // pinned=true and the second sees pinned=false; without this latch the
  // panel would pop open over the board and stay open forever, which is
  // exactly the state the contract above forbids while a dock is mounted. A
  // player-opened panel is left untouched.
  let pinnedOpened = $state(false);
  $effect(() => {
    if (pinned) {
      clearClose();
      pinnedOpened = true;
      open = 'actions';
    } else if (pinnedOpened) {
      pinnedOpened = false;
      if (open === 'actions') open = null;
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
  // The strip counts exactly what its nested seat-panel list shows: the one
  // shared manual-mana rule (lib/manualmana.ts, spec §8) decides whether the
  // manual taps are hidden, never a rule of the strip's own.
  const hideManualMana = $derived(manualManaHidden(decision, view, ctx.seat, logic.autoPayMana));
  const actionCount = $derived(
    (decision?.options.filter((option) =>
      option.kind !== 'pass' && !isConcede(option)
      && (!logic.autoPayMana || !paymentBaseIndexes.has(option.index))
      && !(hideManualMana && isPlainManualTap(option)),
    ).length ?? 0) + visiblePaymentActions.length
      + announceActions(decision, logic.autoManaAvailable, logic.autoPayMana).length,
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
    if (pinned) return;
    clearClose();
    closeTimer = setTimeout(() => {
      open = null;
      closeTimer = null;
    }, HOT_STRIP_CLOSE_DELAY_MS);
  }
  function escape(e: KeyboardEvent): void {
    if (e.key !== 'Escape' || open === null || pinned) return;
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
    // A key whose action is unavailable right now `return`s out of the
    // switch WITHOUT preventDefault, so it is not swallowed from the page;
    // only a key the strip actually acted on is consumed and breadcrumbed.
    const onKey = (e: KeyboardEvent): void => {
      // The Keys editor is recording a chord: that chord must not also fire.
      if (keymapStore.capturing) return;
      const action = hotkeyAction(e, modalPickerOpen, keymapStore.current);
      if (action === null) return;
      // A held key auto-repeats. Only pass (holding Space passes window after
      // window, as before the keymap) and cancel-run act on a repeat: a held
      // digit would answer the next prompt before the player has seen it, a
      // held undo chord would rewind again and again, a held profile or
      // options key would flicker. The repeat is still consumed.
      if (e.repeat && action !== 'pass' && action !== 'cancel-run') {
        e.preventDefault();
        return;
      }
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
        case 'next-profile':
          logic.cycleProfile(1, PRESET_CYCLE);
          break;
        case 'prev-profile':
          logic.cycleProfile(-1, PRESET_CYCLE);
          break;
        case 'toggle-options':
          if (onToggleOptions === null) return;
          onToggleOptions();
          break;
        case 'undo':
          if (!undoAllowed) return;
          void undo();
          break;
        case 'resolve-all':
          if (!resolveAllAvailable) return;
          logic.startResolveAll(view);
          logic.considerAuto(view);
          break;
        case 'confirm':
          if (!doneAvailable) return;
          logic.submit(false);
          break;
        case 'show-keys':
          showKeys = !showKeys;
          break;
        case 'attack-all':
          if (!logic.attackWithAll()) return;
          break;
        case 'no-blocks':
          if (!logic.noBlocks()) return;
          break;
        case 'auto-pay':
          if (!logic.autoPay()) return;
          break;
        default:
          if (action.startsWith('pick-')) {
            if (!logic.pickHotkey(Number(action.slice(5)))) return;
          } else if (action.startsWith('profile-')) {
            if (logic.applyProfileAt(Number(action.slice(8)) - 1, PRESET_CYCLE) === null) return;
          } else {
            return;
          }
          break;
      }
      e.preventDefault();
      clientBreadcrumbs.record('hotkey', { key: e.key, action });
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
  <button
    class="mode-chip"
    class:paused={logic.machinePaused}
    class:run={runLive}
    class:warning={logic.hardSkip}
    type="button"
    role="switch"
    aria-checked={logic.auto && !logic.machinePaused}
    data-play-mode={logic.machinePaused ? 'paused' : playMode}
    data-auto-status
    data-auto-note={logic.machinePaused ? '' : undefined}
    aria-live="polite"
    aria-label={`Auto: ${autoStatus}`}
    title={`Auto: ${autoStatus}`}
    onclick={() => logic.pressAuto()}
  ><span aria-hidden="true">AUTO</span></button>
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

  {#if transport}
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

  {/if}

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

<!-- Outside the strip, so the strip's own Escape handler never sees the
     sheet's keys; the sheet focuses itself on open and closes on Escape or ?. -->
<KeyCheatSheet open={showKeys} keymap={keymapStore.current} onClose={() => (showKeys = false)} />

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

  /* The status chip is also the always-visible Auto switch. Presets read as
     labels; a live run takes the run register — the hard skip's warning
     colour is the danger variable, because passing everything unseen is the
     one state that can lose the game in silence. */
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
    cursor: pointer;
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
  }
  .mode-chip:hover,
  .mode-chip:focus-visible {
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
