<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte';
  import type { SeatInfo, View } from '../protocol';
  import type { SeatCtx } from '../lib/seat';
  import { SeatPanelState, autoNoteText, mulliganPhase, toneOf } from '../lib/seatpanel.svelte';
  import { promptContext, promptContextText } from '../lib/prompt';
  import { modalPickerOpen } from '../lib/modals';
  import ManaPool from './ManaPool.svelte';
  import PromptBody from './prompts/PromptBody.svelte';

  /**
   * SeatPanel is a human seat's decision surface: the status readout,
   * auto/manual controls, floating mana, prompt and options, with the primary
   * button resolved by kind (R-E4-1). The options themselves are drawn by the
   * one renderer per decision kind (components/prompts/PromptBody, UI rework
   * spec §4); this panel owns the seat's lifecycle around them — the pending
   * poll, the autopilot loop and the Escape panic key. Priority mounts under
   * the clock's ACTIONS tab; mulligan mounts over the board; every other
   * decision is answered in the prompt dock when one is mounted (the strip
   * then points at it), else here. Concede is deliberately
   * absent from this normal-turn surface and rendered by Table at the page's
   * top right, though the same SeatPanelState owns its arm/confirm/post path.
   *
   * It floats on the felt but it IS an instrument — it is where the engine
   * asks a question — so it is drawn in the instrument register: one cool,
   * flat panel divided by hairlines, not a stack of rounded cards. It is
   * opaque enough to stay legible over card art and blurs what it covers.
   *
   * Colour states what the engine wants. Warm `--initiative` means the game
   * is blocked on this seat; cool `--offered` means a priority window is open
   * and passing is a legal answer; no colour means the wait is on someone
   * else. See toneOf — the distinction comes from option kinds, not from the
   * prompt text.
   */
  let { view, seats, ctx, table, match, state = null, placement = 'board' }: {
    view: View; seats: SeatInfo[]; ctx: SeatCtx; table: string; match: number;
    /** In-game decisions live under ACTIONS; board is reserved for the opening-hand mulligan. */
    placement?: 'board' | 'flyout' | 'strip';
    /**
     * state lets the route hand in the seat's SeatPanelState so the phase
     * track above the board and this panel share ONE set of stops and ONE
     * autopilot. Left out, the panel owns its own, which is what the
     * component tests exercise.
     */
    state?: SeatPanelState | null;
  } = $props();

  // The props are constants per mount (Table keys the panel by match); the
  // state object captures only their initial values, like MatchState itself.
  // svelte-ignore state_referenced_locally
  const logic = state ?? new SeatPanelState(table, match, ctx);
  // Adopting the first view here rather than only in the effect below means
  // the panel's first paint already carries the decision — including on the
  // server, where effects never run.
  // svelte-ignore state_referenced_locally
  logic.adoptView(view.decision ?? null);

  // The pending decision comes from view.decision (the seat-scoped view at
  // head carries exactly the decision asked of this seat); fetchPending on
  // mount covers the first-view race and is the recovery path after a
  // rejected intent.
  $effect(() => {
    logic.adoptView(view.decision ?? null);
  });
  onMount(() => {
    void logic.refreshPending();
    // The settings load in the SeatPanelState constructor (storage is passed
    // there; SSR gets none and defaults to casual), so nothing to mount — a
    // preference is a property of the player, not of the table or the seat.
    // Escape is the panic key: it ends a one-shot run wherever the focus
    // happens to be. It is on the window because the player's hands are not
    // necessarily on the panel when a run does something they did not expect.
    // CAPTURE phase (r2 review): this listener must evaluate the modal guard
    // BEFORE the bubble-phase handlers — OptionPicker's and PileModal's own
    // Escape-close — can close the modal out from under the probe. While a
    // modal picker is open, Escape closes THE MODAL and the run underneath
    // it survives. This covers both OptionPicker shapes and PileModal:
    // cancelling an End Turn the player cannot see the board of would be the
    // hotkey acting on a decision the modal is blocking.
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (modalPickerOpen()) return;
      logic.onKeydown(e.key);
    };
    // A one-shot run is one shot and a human always wins. Capture makes even a
    // click outside this component (a card, stop, log control) cancel it;
    // only the button that starts the run is exempt.
    const onPointer = (e: PointerEvent) => {
      const target = e.target;
      if (target instanceof Element && target.closest('[data-end-turn]')) return;
      logic.cancelRun();
    };
    window.addEventListener('keydown', onKey, true);
    window.addEventListener('pointerdown', onPointer, true);
    // The panel used to learn about a new decision ONLY from view.decision,
    // which is refreshed by the SSE 'decision' frame. That makes the stream a
    // single point of failure for a seat: miss one frame -- a dropped
    // subscription, a reconnect, a race between the intent POST resolving and
    // the frame arriving -- and the panel sits empty forever while the server
    // holds a decision addressed to this seat, with no way back. Observed in
    // the first real game played through this client.
    //
    // A seat is one person clicking, so a slow poll costs nothing and makes
    // the stream an optimisation rather than a dependency.
    //
    // It polls whether or not a decision is on screen. Gating it on an empty
    // panel left the worse half of the same failure open: a decision the
    // server has already moved past stays on screen forever. view.decision is
    // the only other source, it is embedded ONLY at the exact head seq
    // (host/viewat.go: a view one seq either side carries none), and the
    // effect that reads it re-runs only when a NEW view object is assigned —
    // so once the view stops updating, nothing clears the stale ask and the
    // poll that would replace it was the very thing switched off. Measured on
    // the live demo: the panel held a trigger_order ask while the server was
    // asking the same seat to choose a target.
    //
    // Refetching cannot take a decision out from under the player: adopt()
    // ignores an answer for the seq already displayed, so an in-progress pick
    // survives, and only a genuinely different seq (or a 409, meaning the
    // server asks this seat nothing) replaces it. `busy` still holds the poll
    // off while this panel's own intent is in flight, so a post and a poll
    // cannot race to define the current ask.
    const t = setInterval(() => {
      if (logic.shouldPoll) void logic.refreshPending();
    }, 1000);
    return () => {
      clearInterval(t);
      window.removeEventListener('keydown', onKey, true);
      window.removeEventListener('pointerdown', onPointer, true);
    };
  });

  // The autopilot loop. The dependencies are listed explicitly and the call
  // itself is untracked: considerAuto both reads and writes the seat's
  // state, and a self-triggering effect around a thing that posts to the
  // server is exactly the runaway this task exists to prevent.
  $effect(() => {
    void logic.settings;
    void logic.oneShot;
    void logic.pending?.seq;
    // MatchState replaces its complete View on every server projection.
    // Track that object directly: every new view cancels an old pacing timer,
    // then considerAuto may start a fresh full wait. A hand-maintained field
    // list would inevitably miss the next view field autopilot learns to read.
    void view;
    untrack(() => {
      logic.expireRun(view);
      logic.considerAuto(view);
    });
  });
  // The paced pass is abandoned when the panel goes away: a timer owned by a
  // destroyed surface must not post (prio5). The state object itself is
  // per-match and survives, so this is the timer's cancellation edge only.
  onDestroy(() => logic.cancelPass());

  const decision = $derived(logic.pending && logic.pending.seq !== logic.postedSeq ? logic.pending : null);
  const tone = $derived(toneOf(decision));
  // The panel names the seat it is waiting on. `seats` is the first word
  // (the table's registered name), but on the live table route it can be
  // EMPTY while the view still carries each player's own name (the one-shot
  // seed in Table.svelte races the async tables.lookup — see the ui15
  // report); the fallback must be that name, not the 0-based `Seat N`
  // placeholder, exactly as IdentityBar does.
  const waitingName = $derived(
    seats[view.priority]?.name ?? view.players.find((p) => p.seat === view.priority)?.name ?? `Seat ${view.priority}`,
  );
  const stepLabel = $derived(view.step.charAt(0).toUpperCase() + view.step.slice(1));

  const mine = $derived(view.players.find((p) => p.seat === ctx.seat) ?? null);

  // The London round gets the wide board-centred body; its renderer is
  // MulliganPrompt (null for every other decision).
  const mull = $derived(mulliganPhase(decision));

  // One decision, one live answer surface: while a prompt dock is mounted it
  // answers every non-priority decision, and the ACTIONS strip only points
  // at it (a second copy of the same options would be a second place to
  // click the same answer).
  const deferred = $derived(decision !== null && placement === 'strip' && logic.dockCount > 0 && decision.kind !== 'priority');

  // The prompt context line (brief Job 3): who the prompt is from and what
  // shape the answer takes, from fields already on the wire (source,
  // kind/min/max). Null facts are omitted; the line itself is omitted when
  // neither is known (priority, mulligan).
  const ctxText = $derived(decision !== null ? promptContextText(promptContext(decision, view)) : null);
</script>

{#if view.over}
  <div class="seat-panel over" data-tone="idle" role="status">
    <p class="prompt result">{view.draw ? 'Draw' : `${seats[view.winner ?? -1]?.name ?? view.players.find((p) => p.seat === view.winner)?.name ?? `Seat ${view.winner}`} wins`}</p>
    <p class="sub">Match over</p>
  </div>
{:else}
  <div
    class="seat-panel"
    class:wide={mull !== null}
    class:flyout={placement === 'flyout'}
    class:strip={placement === 'strip'}
    data-seat-panel
    data-answer-surface={(placement === 'strip' || placement === 'board') && !deferred ? 'true' : null}
    data-concede={logic.concedeOption?.index}
    data-tone={tone}
  >
    {#if mull === null && placement !== 'strip'}
      <div class="readout" role="status">
        <div class="fact">
          <span class="k">Priority</span>
          <span class="v">{view.priority === ctx.seat ? 'you' : waitingName}</span>
        </div>
        <div class="fact">
          <span class="k">Stack</span>
          <span class="v num">{view.stack.length}</span>
        </div>
        {#if mine}<ManaPool pool={mine.pool} poolRestrictions={mine.pool_restrictions} />{/if}
      </div>
    {/if}

    {#if mull === null}
      <div class="autobar" data-autobar>
        <div class="autoline">
          <button
            class="autotoggle"
            class:on={logic.auto}
            type="button"
            role="switch"
            aria-checked={logic.auto}
            data-auto-toggle
            onclick={() => logic.pressAuto()}
          >
            <span class="dot" aria-hidden="true"></span>
            <span class="word">{logic.machinePaused ? 'Paused' : logic.auto ? 'Auto' : 'Manual'}</span>
          </button>
          {#if logic.autoManaAvailable}
          <button
            class="skiptoggle"
            class:on={logic.autoPayMana}
            type="button"
            role="switch"
            aria-checked={logic.autoPayMana}
            aria-label="Auto-pay mana"
            title="Use a suggested mana plan when casting"
            data-auto-pay-toggle
            onclick={() => logic.setAutoPayMana(!logic.autoPayMana)}
          >
            <span class="dot" aria-hidden="true"></span>
            <span class="word">Auto-pay mana</span>
          </button>
          {/if}
          <!-- The empty-window floor's own switch, beside auto's rather than
               buried in a menu: it changes whether the game stops for you, so
               it has to be visible where you would look to ask why it did
               not. It is on by default and it is NOT the auto toggle -- a
               Manual seat still answers every window that offers it an
               action. -->
          <button
            class="skiptoggle"
            class:on={logic.skipEmpty}
            type="button"
            role="switch"
            aria-checked={logic.skipEmpty}
            aria-label="Skip priority windows where you have no action"
            title="Skip priority windows where you have no action"
            data-skip-toggle
            onclick={() => logic.setSkipEmpty(!logic.skipEmpty)}
          >
            <span class="dot" aria-hidden="true"></span>
            <span class="word">Skip empty</span>
          </button>
          {#if logic.autoPassed > 0}
            <span class="passed" data-auto-count>
              auto-passed <span class="num">{logic.autoPassed}</span>
              {logic.autoPassed === 1 ? 'priority window' : 'priority windows'}
            </span>
          {/if}
          {#if logic.emptySkipped > 0}
            <span class="passed" data-skip-count>
              skipped <span class="num">{logic.emptySkipped}</span>
              {logic.emptySkipped === 1 ? 'empty window' : 'empty windows'}
            </span>
          {/if}
        </div>
        <p class="autonote" data-auto-note>{autoNoteText(logic.note)}</p>
      </div>
    {/if}

    {#if logic.error}
      <p class="error" role="alert" data-error>{logic.error}</p>
    {/if}

    {#if decision && deferred}
      <p class="prompt pointer" data-dock-pointer>{decision.prompt} — answer in the prompt panel</p>
    {:else if decision}
      <p class="prompt" data-prompt>{decision.prompt}</p>
      {#if ctxText !== null}
        <p class="ctx" data-prompt-ctx>{ctxText}</p>
      {/if}
      <div class="body" class:wide={mull !== null}>
        <PromptBody {decision} {view} {logic} seat={ctx.seat} {placement} />
      </div>
    {:else if logic.postedSeq !== null}
      <p class="prompt waiting">Answer sent — waiting for the game to advance</p>
    {:else}
      <p class="prompt waiting" data-waiting>{stepLabel} — waiting for {waitingName}</p>
    {/if}
  </div>
{/if}

<style>
  /*
   * One instrument panel, not three floating cards. Hairlines divide the
   * readout from the prompt from the options, the way the rail divides its
   * sections — a stack of identically rounded boxes would read as chrome,
   * and this panel is the engine's own face.
   *
   * The tone rule on the left edge is the same grammar the rail already
   * speaks: PendingTray marks a waiting trigger with a rule, IdentityBar
   * marks a seat with one. Here it says whose move it is, and it is stated
   * ONCE — the panel's edge and the primary button share the tone colour
   * because they are one signal read at two distances, not two decorations.
   */
  /* NOTE: this panel is a containing block for `position: fixed`
     descendants -- backdrop-filter promotes exactly like transform does, and
     both are here. The mulligan hand puts real CardTiles inside the panel and
     their hover detail is such a descendant, so CardDetail portals itself to
     <body> rather than trusting `position: fixed` to mean the viewport. Do
     not "fix" that by removing the blur: any of transform, filter,
     backdrop-filter, will-change or contain would re-break it. */
  .seat-panel {
    position: absolute;
    top: var(--sp-2);
    left: 50%;
    transform: translateX(-50%);
    z-index: 8;
    width: 21rem;
    max-width: 92%;
    max-height: calc(100% - var(--sp-4));
    display: flex;
    flex-direction: column;
    background: color-mix(in srgb, var(--instrument) 92%, transparent);
    border: 1px solid var(--edge-inst);
    border-left: 3px solid var(--edge-inst);
    border-radius: var(--radius);
    backdrop-filter: blur(8px);
    color: var(--ink-inst);
    overflow: hidden;
  }
  /* Warm: the table is stopped until this seat answers, and there is no pass.
     Cool: a window is open and passing is a legal answer. Neither: the wait
     is on someone else, and the panel says nothing louder than its hairline. */
  .seat-panel[data-tone='initiative'] {
    border-color: color-mix(in srgb, var(--initiative) 40%, var(--edge-inst));
    border-left-color: var(--initiative);
  }
  .seat-panel[data-tone='offered'] {
    border-left-color: var(--offered);
  }
  /* The mulligan round is not a heads-up display: nothing else is happening,
     the hand is the whole content, and the panel takes the middle of the
     board for the one moment it exists. */
  /* Generic surfaces use a definite width rather than max-content, so a long
     server option cannot push its instrument panel beyond the viewport. */
  .seat-panel.flyout,
  .seat-panel.strip {
    position: static;
    transform: none;
    width: min(21rem, calc(100vw - 24.5rem));
    min-width: 15rem;
    max-width: none;
    max-height: min(32vh, 24rem);
  }
  .seat-panel.strip {
    width: 100%;
    min-width: 0;
    max-height: none;
  }
  .seat-panel.wide {
    /* Leave the shard's compact top lane clear even when seven opening cards
       make this panel tall on a narrow board. It remains the central body,
       shifted down by one small instrument lane rather than competing with it. */
    top: calc(50% + 3rem);
    transform: translate(-50%, -50%);
    width: min(94%, 1120px);
    max-height: calc(100% - 7rem);
    align-items: center;
  }
  .seat-panel > :global(*) {
    border-bottom: 1px solid var(--edge-inst);
  }
  .seat-panel > :global(*:last-child) {
    border-bottom: 0;
  }
  /* An instrument readout: labels above their values in four columns, read
     across in one glance, rather than four rows of a form read down. */
  .readout {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--sp-1) var(--sp-4);
    padding: var(--sp-2) var(--sp-3);
    width: 100%;
  }
  .fact {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .k {
    font-size: 0.6875rem;
    line-height: 1.2;
    color: var(--ink-faint);
  }
  .v {
    font-size: var(--t-12);
    line-height: 1.3;
    color: var(--ink-inst);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .num {
    font-family: var(--font-data);
    font-variant-numeric: tabular-nums;
  }
  /* The auto control is a switch, not a button that looks like an action:
     its own state is the message, and the line under it is the only place
     the panel explains what auto is doing. Never an enum — see
     autoNoteText. */
  .autobar {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--sp-2) var(--sp-3);
    width: 100%;
  }
  .autoline {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
  }
  .autotoggle,
  .skiptoggle {
    display: inline-flex;
    align-items: center;
    gap: 0.45em;
    background: var(--instrument-raised);
    color: var(--ink-dim);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    padding: 0.15rem var(--sp-2);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    font-weight: 600;
    cursor: pointer;
    flex: none;
  }
  .autotoggle.on,
  .skiptoggle.on {
    color: var(--felt-sunk);
    background: var(--offered);
    border-color: var(--offered);
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
    opacity: 0.5;
  }
  .autotoggle.on .dot,
  .skiptoggle.on .dot {
    opacity: 1;
  }
  .passed {
    font-size: 0.6875rem;
    color: var(--ink-dim);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }
  .autonote {
    margin: 0;
    font-size: 0.6875rem;
    line-height: 1.35;
    color: var(--ink-faint);
  }
  .prompt {
    margin: 0;
    padding: var(--sp-2) var(--sp-3);
    font-size: var(--t-14);
    font-weight: 600;
    line-height: 1.35;
    color: var(--ink);
    width: 100%;
    text-align: center;
  }
  .seat-panel[data-tone='initiative'] .prompt {
    color: var(--initiative);
  }
  .prompt.waiting {
    font-weight: 400;
    font-size: var(--t-12);
    color: var(--ink-dim);
  }
  /* The context line (Job 3): who the prompt is from and the shape of the
     answer — smaller and quieter than the prompt itself, because it is
     metadata about the ask, not the ask. */
  .ctx {
    margin: 0;
    padding: 0 var(--sp-3) var(--sp-2);
    font-size: var(--t-11);
    line-height: 1.35;
    color: var(--ink-dim);
    text-align: center;
    width: 100%;
  }
  /* An error is the one thing allowed to outrank the tone: the seat's last
     answer did not land, and nothing else on the panel matters until it is
     read. */
  .error {
    margin: 0;
    padding: var(--sp-2) var(--sp-3);
    background: color-mix(in srgb, var(--danger) 22%, var(--instrument));
    border-left: 3px solid var(--danger);
    color: var(--ink);
    font-size: var(--t-12);
    line-height: 1.35;
    width: 100%;
  }
  /* The match is over: a result, stated plainly. No new colour — a win is
     neither mana, card colour nor seat identity. */
  .over {
    align-items: center;
  }
  .result {
    font-size: var(--t-20);
    font-weight: 600;
    color: var(--ink);
  }
  .sub {
    margin: 0;
    padding: var(--sp-2) var(--sp-3);
    font-size: var(--t-12);
    color: var(--ink-dim);
    width: 100%;
    text-align: center;
  }
  .body {
    padding: var(--sp-2) var(--sp-3) var(--sp-3);
    width: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .body.wide { padding: 0; }
  .prompt.pointer {
    font-weight: 400;
    font-size: var(--t-12);
    color: var(--ink-dim);
  }
</style>
