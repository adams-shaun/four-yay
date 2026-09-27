<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte';
  import type { CardView, Option, PaymentAction, SeatInfo, View } from '../protocol';
  import type { SeatCtx } from '../lib/seat';
  import { SeatPanelState, autoNoteText, isConcede, mulliganPhase, paymentPlanSummary, toneOf } from '../lib/seatpanel.svelte';
  import { promptContext, promptContextText } from '../lib/prompt';
  import { arrangeCard } from '../lib/arrange';
  import { discardCard, isDiscardPick } from '../lib/discard';
  import { isSearchPick, searchCard, searchOptions } from '../lib/search';
  import { isNamePick, nameOptions, NAME_PICK_RENDER_LIMIT } from '../lib/name-pick';
  import { modalPickerOpen } from '../lib/modals';
  import ArrangeModal from './ArrangeModal.svelte';
  import DiscardModal from './DiscardModal.svelte';
  import CardDetail from './CardDetail.svelte';
  import CardImage from './CardImage.svelte';
  import CardTile from './CardTile.svelte';
  import ManaPool from './ManaPool.svelte';
  import { CardHover } from '../lib/carddetail.svelte';

  /**
   * SeatPanel is a human seat's decision surface: the status readout,
   * auto/manual controls, floating mana, prompt and options, with the primary
   * button resolved by kind (R-E4-1). Every in-game decision mounts under the
   * clock's ACTIONS tab; mulligan alone mounts over the board. Concede is deliberately
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
  const primary = $derived(logic.primary());
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

  // This seat's own player row, found by seat number. Seat index and array
  // position are not guaranteed equal, and a seat missing from the view is a
  // real state (a spectator token pointed at a seat that left), so it is a
  // null the template handles rather than an assumption.
  const mine = $derived(view.players.find((p) => p.seat === ctx.seat) ?? null);

  // The London round gets its own body. mull is null for every other
  // decision, and also for a mulligan decision carrying an option kind this
  // layout does not cover — the generic list renders that one instead, so no
  // option is ever unreachable.
  const mull = $derived(mulliganPhase(decision));
  const handById = $derived(new Map((mine?.hand ?? []).map((c) => [c.id, c])));
  const bottomCount = $derived(decision?.min ?? 0);
  // PaymentActions are an additive extension of priority.  Grouping happens
  // here, at the one generic option surface, so a cast with a BaseOptionIndex
  // cannot appear as two unrelated actions.
  // Payment witnesses are an Auto Mana affordance.  With the toggle off the
  // priority list is intentionally the untouched legacy/manual list.
  const paymentActions = $derived(logic.autoManaAvailable && logic.autoPayMana && decision?.kind === 'priority' ? (decision.payment_actions ?? []) : []);
  const paymentBases = $derived(new Set(paymentActions.flatMap((action) => action.base_option_index === undefined || action.base_option_index === null ? [] : [action.base_option_index])));
  // Auto Mana is a mode switch, not an extra choice beside manual mana. The
  // planner's cast button is the remaining route while it is on; turning the
  // switch off deliberately restores every normal activation.
  function isManualMana(opt: { kind: string; label: string }): boolean {
    return opt.kind === 'activate' && / for mana$/i.test(opt.label);
  }

  function castSuggested(action: PaymentAction, planID: string): void {
    const plan = action.plans.find((candidate) => candidate.id === planID);
    if (plan !== undefined) logic.submitPayment(action, plan);
  }

  // The arrange family (brief Job 4): kind 'arrange' is the ordered-subset
  // ask — a top-of-library reorder (Min == Max == N) or Scry/Surveil (Min 0).
  // It renders its own card-face row instead of the generic option list and
  // opens ArrangeModal for drag/drop ordering; the posted intent is the same
  // picked-order array either way.
  const arrange = $derived(decision !== null && decision.kind === 'arrange' ? decision : null);
  // The popup's open flag lives on the shared SeatPanelState, NOT on this
  // component: a local `$state` cannot be declared here (the component's own
  // `state` prop makes the rune ambiguous — Svelte would read it as a store
  // subscription and throw store_invalid_shape), and two surfaces (strip and
  // board) mount against the one state object anyway.
  function openArrange(): void {
    logic.arrangeOpen = true;
  }
  /** Submit both ordered piles when offered; legacy arrange answers still use the ordinary picked-order path. */
  function submitArrange(order: number[], rest?: number[]): void {
    logic.arrangeOpen = false;
    if (rest !== undefined) logic.submitArrange(order, rest);
    else {
      logic.setPicked(order);
      logic.submit();
    }
  }

  /** openDiscard opens the discard-pick ask's big card view (fb-20260918T201739Z) — the affordance the arrange ask's button gives. */
  function openDiscard(): void {
    logic.discardOpen = true;
  }
  /** submitDiscard closes the popup and posts the picked set through the ordinary submit — the same one posting path the inline row uses. */
  function submitDiscard(): void {
    logic.discardOpen = false;
    logic.submit();
  }

  // ---- the arrange strip's hover inspector ----------------------------
  // The arrange strip is the one card surface that had NO hover detail:
  // bare CardImages in `.pick` buttons, while CardTile (the mulligan keep
  // hand in this very panel), HandList and StackTile all open the shared
  // CardDetail inspector on hover/focus. A tile-sized face is the reading
  // affordance at exactly this prompt, so the strip gets the same HoverCard
  // dwell/focus mechanism (fb-20260914T063020Z Job 1). CardDetail portals
  // itself to <body> (see the NOTE in the style block: this panel is a
  // containing block for fixed descendants), so the detail escapes the
  // panel's own clipping and blur.
  const arrangeHover = new CardHover();

  // The cards the strip currently shows — the present list the lifecycle
  // contract supervises against.
  const arrangeCards = $derived(arrange !== null ? arrange.options.map((o) => arrangeCard(arrange, o)) : []);
  // PANEL LIFETIME FOLLOWS THE ASK, NOT THE POINTER. The decision on screen
  // can change under a stationary pointer (the seat's next ask answered from
  // another tab; the seqswap case) — the row re-renders, pointerleave never
  // fires, and the panel would describe a card the engine no longer offers.
  // Whenever the shown card is no longer among the present cards the hover
  // closes. (Runs only client-side; the SSR render has no open panel to
  // supervise — a test drives CardHover directly, the way the CardTile and
  // StackTile tests do.)
  $effect(() => {
    arrangeHover.supervise(arrangeCards);
  });

  // The discard-pick family (fb-20260914T120705Z): a decision whose options
  // are card picks (kind "discard", Obj set) — the Thoughtseize/Mind Rot
  // discard ask, the cleanup-step discard, the cast-cost discard. It renders
  // its card faces like the arrange strip instead of the generic text list:
  // the decision is the payload that carries the cards (the target's hand is
  // redacted in the view), so each face is synthesized from {obj, label} and
  // gets the shared hover inspector. isDiscardPick is kind-agnostic on the
  // DECISION (the four shapes span KModes and KChoose) and strict on the
  // OPTIONS, so a mixed decision falls through to the generic list.
  const discard = $derived(isDiscardPick(decision) ? decision : null);

  // The discard row's hover inspector: the same CardHover dwell/focus
  // mechanism the arrange strip got (fb-20260914T063020Z Job 1) — one shared
  // panel for the whole row, portalled CardDetail, real oracle text by exact
  // card name. The arrange strip and this row are disjoint branches (an
  // arrange decision's options are never "discard" picks), so two instances
  // never fight for one pointer.
  const discardHover = new CardHover();

  // The cards the row currently shows — the present list the lifecycle
  // contract supervises against (same contract as arrangeCards above).
  const discardCards = $derived(discard !== null ? discard.options.map((o) => discardCard(discard, o)) : []);
  $effect(() => {
    discardHover.supervise(discardCards);
  });

  // The library-search ask (fb-20260916T181754Z): a decision whose options
  // are card picks (kind "search", Obj set) — the fetchland / tutor / Hawk
  // ask over a hidden library. It renders a filter input plus its card faces
  // SORTED A→Z and FILTERED by the text typed into logic.searchFilter
  // (display-only state on the shared SeatPanelState, reset on every newly
  // adopted decision) instead of the generic unsorted text list. Selection
  // is untouched: the click path and the picked ordinals are the generic
  // ones, the answer is the picked wire indexes in click order, and the
  // sort/filter exist only between the player and the list. isSearchPick is
  // strict on the OPTIONS, so a mixed decision falls through to the generic
  // list. The strip never reaches this branch: a search carries no pass
  // option, so its tone is initiative and the strip shows the
  // required-prompt pointer above, exactly as every other blocked decision
  // already does.
  const search = $derived(isSearchPick(decision) ? decision : null);
  const namePick = $derived(isNamePick(decision) ? decision : null);
  const nameOpts = $derived(namePick !== null ? nameOptions(namePick, logic.searchFilter) : []);

  // The search grid's hover inspector: the same CardHover mechanism the
  // arrange strip and the discard row use. The three branches are disjoint
  // by option kind (arrange / "discard" / "search"), so three instances
  // never fight for one pointer.
  const searchHover = new CardHover();

  // The options the grid currently shows — filtered and sorted. The CardView
  // list beside it (same list, as synthesized faces) is what the lifecycle
  // contract supervises against, exactly as the discard row's does.
  const searchOpts = $derived(search !== null ? searchOptions(search, logic.searchFilter) : []);
  const searchCards = $derived(searchOpts.map((o) => (search !== null ? searchCard(search, o) : null)).filter((c) => c !== null));
  $effect(() => {
    searchHover.supervise(searchCards);
  });

  // The prompt context line (brief Job 3): who the prompt is from and what
  // shape the answer takes, from fields already on the wire (source,
  // kind/min/max). Null facts are omitted; the line itself is omitted when
  // neither is known (priority, mulligan).
  const ctxText = $derived(decision !== null ? promptContextText(promptContext(decision, view)) : null);

  // The engine labels these "keep" and "mulligan"; a button says what
  // pressing it does. Resolved by kind, with the server's own label as the
  // fallback for anything unrecognised.
  const CHOICE_LABEL: Record<string, string> = { keep: 'Keep this hand', mulligan: 'Mulligan' };
  function choiceLabel(o: Option): string {
    return CHOICE_LABEL[o.kind] ?? o.label;
  }

  function cardFor(o: Option): CardView | undefined {
    return o.obj === undefined ? undefined : handById.get(o.obj);
  }
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
    data-answer-surface={placement === 'strip' || placement === 'board' ? 'true' : null}
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

    {#if decision}
      <p class="prompt" data-prompt>{decision.prompt}</p>
      {#if ctxText !== null}
        <p class="ctx" data-prompt-ctx>{ctxText}</p>
      {/if}

      {#if mull !== null && mull.phase === 'keep'}
        <div class="hand" data-opening-hand data-card-count={(mine?.hand ?? []).length} style={`--n:${Math.max(1, (mine?.hand ?? []).length)}`} aria-label="Your opening hand">
          {#each mine?.hand ?? [] as c (c.id)}<CardTile card={c} />{/each}
        </div>
        <div class="choices" data-options>
          {#each mull.choices as opt (opt.index)}
            <button
              class="choice"
              class:keep={opt.kind === 'keep'}
              type="button"
              data-option={opt.index}
              onclick={() => logic.click(opt.index)}
              disabled={logic.busy}
            >{choiceLabel(opt)}</button>
          {/each}
        </div>
      {:else if mull !== null && mull.phase === 'bottom'}
        <div class="hand picking" data-opening-hand data-card-count={mull.cards.length} style={`--n:${Math.max(1, mull.cards.length)}`} data-options aria-label="Choose cards to put on the bottom">
          {#each mull.cards as opt (opt.index)}
            {@const card = cardFor(opt)}
            {@const at = logic.picked.indexOf(opt.index)}
            <button
              class="pick"
              class:picked={at >= 0}
              type="button"
              data-option={opt.index}
              aria-pressed={at >= 0}
              aria-label={card ? card.name : opt.label}
              onclick={() => logic.toggle(opt.index)}
              disabled={logic.busy}
            >
              {#if card}<CardImage {card} />{:else}<span class="fallback">{opt.label}</span>{/if}
              {#if at >= 0}<span class="order">{at + 1}</span>{/if}
            </button>
          {/each}
        </div>
        <div class="choices">
          <button
            class="choice keep"
            type="button"
            data-submit
            onclick={() => logic.submit()}
            disabled={!logic.canSubmit || logic.busy}
          >
            Bottom {bottomCount} {bottomCount === 1 ? 'card' : 'cards'}
          </button>
        </div>
      {:else if arrange !== null}
        <!-- The arrange ask (brief Job 4): card faces in OFFERED order, each
             click toggling it into/out of the keep pile (the picked array, in
             its order, IS the keep pile — the ordinal says where it lands);
             the popup gives drag/drop reordering of that pile. Submit posts
             the picked order through the ordinary submit path. -->
        <div class="arrange" data-arrange>
          <div class="hand picking" data-arrange-row data-options data-card-count={arrange.options.length} style={`--n:${Math.max(1, arrange.options.length)}`} aria-label="Top of the library, in keep order">
            {#each arrange.options as opt (opt.index)}
              {@const card = arrangeCard(arrange, opt)}
              {@const at = logic.picked.indexOf(opt.index)}
              <button
                class="pick"
                class:picked={at >= 0}
                type="button"
                data-option={opt.index}
                aria-pressed={at >= 0}
                aria-label={opt.label}
                onpointerenter={(e) => arrangeHover.arm(card, e.currentTarget)}
                onpointerleave={() => arrangeHover.leave(card)}
                onpointerdown={(e) => arrangeHover.pointerdown(card, e.currentTarget)}
                onpointerup={() => arrangeHover.pointerup(card)}
                onfocus={(e) => arrangeHover.open(card, e.currentTarget)}
                onblur={() => arrangeHover.blur(card)}
                onkeydown={(e) => arrangeHover.keydown(e)}
                aria-describedby={arrangeHover.hover.show && arrangeHover.card?.id === card.id ? `card-detail-${card.id}` : undefined}
                onclick={() => logic.toggle(opt.index)}
                disabled={logic.busy}
              >
                {#if card}<CardImage {card} />{:else}<span class="fallback">{opt.label}</span>{/if}
                {#if at >= 0}<span class="order">{at + 1}</span>{/if}
              </button>
            {/each}
          </div>
          <div class="choices">
            <button class="choice" type="button" data-arrange-open onclick={openArrange} disabled={logic.busy}>Open the card view</button>
            {#if logic.showSubmit}
              <button
                class="choice keep"
                type="button"
                data-submit
                onclick={() => logic.submit()}
                disabled={!logic.canSubmit || logic.busy}
              >{arrange.min === arrange.max ? 'Confirm order' : 'Confirm'}</button>
            {/if}
          </div>
          {#if arrangeHover.hover.show && arrangeHover.card && arrangeHover.anchor}<CardDetail card={arrangeHover.card} anchor={arrangeHover.anchor} />{/if}
        </div>
      {:else if discard !== null}
        <!-- The discard-pick ask (fb-20260914T120705Z): card faces instead of
             the generic text list, for every engine shape whose options are
             card picks — the Thoughtseize/Mind Rot discard ask (KModes), the
             cleanup-step discard and the cast-cost discard (KChoose). Same
             face-then-submit pattern as the mulligan-bottom row; the posting
             path is the ordinary one (click posts straight through on a
             min==max==1 ask and toggles into `picked` otherwise, submit gated
             by canSubmit), so the wire intent is byte-identical to the text
             list this replaces. The strip does not reach this branch: an
             initiative-tone ask is bounced to the board above it, exactly as
             before. -->
        <div class="discard" data-discard>
          <div class="hand picking" data-discard-row data-options data-card-count={discard.options.length} style={`--n:${Math.max(1, discard.options.length)}`} aria-label="Cards to discard">
            {#each discard.options as opt (opt.index)}
              {@const card = discardCard(discard, opt)}
              {@const at = logic.picked.indexOf(opt.index)}
              <button
                class="pick"
                class:picked={at >= 0}
                type="button"
                data-option={opt.index}
                aria-pressed={at >= 0}
                aria-label={card.name}
                onpointerenter={(e) => discardHover.arm(card, e.currentTarget)}
                onpointerleave={() => discardHover.leave(card)}
                onfocus={(e) => discardHover.open(card, e.currentTarget)}
                onblur={() => discardHover.blur(card)}
                onkeydown={(e) => discardHover.keydown(e)}
                aria-describedby={discardHover.hover.show && discardHover.card?.id === card.id ? `card-detail-${card.id}` : undefined}
                onclick={() => logic.click(opt.index)}
                disabled={logic.busy}
              >
                <CardImage {card} />
                {#if at >= 0}<span class="order">{at + 1}</span>{/if}
              </button>
            {/each}
          </div>
          <div class="choices">
            <button class="choice" type="button" data-discard-open onclick={openDiscard} disabled={logic.busy}>Open the card view</button>
            {#if logic.showSubmit}
              <button
                class="choice keep"
                type="button"
                data-submit
                onclick={() => logic.submit()}
                disabled={!logic.canSubmit || logic.busy}
              >{discard.min === 0 ? 'Confirm' : discard.min === discard.max ? `Choose ${discard.min}` : `Choose ${discard.min}–${discard.max}`}</button>
            {/if}
          </div>
          {#if discardHover.hover.show && discardHover.card && discardHover.anchor}<CardDetail card={discardHover.card} anchor={discardHover.anchor} />{/if}
        </div>
      {:else if search !== null}
        <!-- The library-search ask (fb-20260916T181754Z): the fetchland /
             tutor / Hawk ask over a hidden library. The complaint was the
             flat unsorted wall of card names; the answer is a filter input
             (display-only text on the shared state, reset on every newly
             adopted decision) above the card faces SORTED A→Z. Selection is
             untouched — the click path and the picked ordinals are the
             generic ones and the posted intent is the picked wire indexes in
             click order; sorting the display cannot move the answer because
             a search carries no pass/resolve primary and the engine reads
             the chosen cards' order from the click sequence. The strip never
             reaches this branch (a search's tone is initiative, and the
             strip shows the required-prompt pointer above) and the input is
             therefore not cramped there. -->
        <div class="search" data-search>
          <div class="search-bar">
              <input
                class="search-filter"
                type="search"
                data-search-filter
                placeholder={`Filter ${search.options.length} cards…`}
                aria-label="Filter the offered cards by name"
                autocomplete="off"
                spellcheck="false"
                bind:value={logic.searchFilter}
                disabled={logic.busy}
                onkeydown={(e) => {
                  if (e.key !== 'Escape') return;
                  // Escape in the filter clears the filter — it never
                  // submits or closes anything. The panel's window-capture
                  // Escape handler (the one-shot run's panic key) has
                  // already run by the time this does: a key handler on the
                  // input cannot stop a capture listener on window, and the
                  // panic key keeps working. Every other hotkey is already
                  // guarded off while focus is in an input (hotkeys.ts's
                  // focus check), so typing filters, never fires, a table
                  // hotkey.
                  e.preventDefault();
                  logic.searchFilter = '';
                }}
              />
            </div>
          <div class="search-grid" data-search-grid data-options data-card-count={searchOpts.length} aria-label="Cards the search offers, sorted and filtered">
            {#each searchOpts as opt (opt.index)}
              {@const card = searchCard(search, opt)}
              {@const at = logic.picked.indexOf(opt.index)}
              <button
                class="pick"
                class:picked={at >= 0}
                type="button"
                data-option={opt.index}
                aria-pressed={at >= 0}
                aria-label={card.name}
                onpointerenter={(e) => searchHover.arm(card, e.currentTarget)}
                onpointerleave={() => searchHover.leave(card)}
                onpointerdown={(e) => searchHover.pointerdown(card, e.currentTarget)}
                onpointerup={() => searchHover.pointerup(card)}
                onfocus={(e) => searchHover.open(card, e.currentTarget)}
                onblur={() => searchHover.blur(card)}
                onkeydown={(e) => searchHover.keydown(e)}
                aria-describedby={searchHover.hover.show && searchHover.card?.id === card.id ? `card-detail-${card.id}` : undefined}
                onclick={(e) => logic.click(opt.index, { holdPriority: e.ctrlKey })}
                disabled={logic.busy}
              >
                <CardImage {card} />
                {#if at >= 0}<span class="order">{at + 1}</span>{/if}
              </button>
            {/each}
            {#if searchCards.length === 0}
              <p class="search-empty">No card matches “{logic.searchFilter}”.</p>
            {/if}
          </div>
          {#if logic.showSubmit}
            <div class="choices">
              <button
                class="choice keep"
                type="button"
                data-submit
                onclick={() => logic.submit()}
                disabled={!logic.canSubmit || logic.busy}
              >{search.min === 0 ? 'Confirm' : search.min === search.max ? `Choose ${search.min}` : `Choose ${search.min}–${search.max}`}</button>
            </div>
          {/if}
          {#if searchHover.hover.show && searchHover.card && searchHover.anchor}<CardDetail card={searchHover.card} anchor={searchHover.anchor} />{/if}
        </div>
      {:else if namePick !== null}
        <div class="name-pick" data-name-pick>
          <input class="search-filter" type="search" data-name-filter placeholder="Filter names…" aria-label="Filter card names" autocomplete="off" spellcheck="false" bind:value={logic.searchFilter} disabled={logic.busy} onkeydown={(e) => { if (e.key !== 'Escape') return; e.preventDefault(); logic.searchFilter = ''; }} />
          <p data-name-count>{logic.searchFilter.trim() === '' ? `${nameOpts.length.toLocaleString()} cards — type to filter` : `Showing first ${Math.min(nameOpts.length, NAME_PICK_RENDER_LIMIT)} of ${nameOpts.length.toLocaleString()} matches`}</p>
          <div class="list" data-name-list data-options>
            {#each nameOpts.slice(0, NAME_PICK_RENDER_LIMIT) as opt (opt.index)}
              <button class="option" type="button" data-option={opt.index} onclick={(e) => logic.click(opt.index, { holdPriority: e.ctrlKey })} disabled={logic.busy}><span class="label">{opt.label}</span></button>
            {/each}
            {#if nameOpts.length === 0}<p class="search-empty">No card matches “{logic.searchFilter}”.</p>{/if}
          </div>
        </div>
      {:else}
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
                        onclick={() => castSuggested(action, plan.id)}
                        disabled={logic.busy}
                      >{i === 0 ? 'Cast with suggested mana' : 'Cast with this mana plan'}</button>
                    {/each}
                  {:else}
                    <p class="payment-summary">Suggested payment is unavailable; use the manual mana controls.</p>
                  {/if}
                  {#if !logic.autoPayMana && action.base_option_index !== undefined && action.base_option_index !== null}
                    <button class="option" type="button" data-payment-manual={action.id} onclick={(e) => logic.click(action.base_option_index!, { holdPriority: e.ctrlKey })} disabled={logic.busy}>Pay manually</button>
                  {:else}
                    <p class="payment-summary" data-payment-manual-needed>Tap mana manually, then cast.</p>
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
          {#if decision.kind === 'trigger_optional'}
            <!-- The remember affordance (fb-20260914T062319Z-88b4069a B4):
                 default OFF; when checked, click()'s post-on-click stores the
                 chosen index under the full-prompt key, and every identical
                 future prompt is auto-answered with it (manageable in GAME
                 OPTIONS). It sits above the options because a min==max==1 ask
                 posts on the click itself — the box must be settable first. -->
            <label class="remember" data-remember-answer>
              <input type="checkbox" bind:checked={logic.rememberChoice} disabled={logic.busy} />
              <span>Remember this answer for identical future prompts</span>
            </label>
          {/if}
          {#if decision.repeatable && logic.picked.length > 0}
            <!-- A repeatable modal ask (a CanRepeatModes$ Charm, CR 601.2b)
                 is an ordered multiset: the list button APPENDS an instance
                 (pickOption), so removal needs its own affordance — one chip
                 per picked instance, in click order, each removing exactly
                 that instance (unpick). -->
            <div class="picked-chips" data-picked-modes>
              {#each logic.picked as pi, i (i)}
                {@const popt = decision.options.find((o) => o.index === pi)}
                <button
                  class="chip"
                  type="button"
                  data-picked-chip={i}
                  onclick={() => logic.unpick(pi)}
                  disabled={logic.busy}
                >{popt?.label} ✕</button>
              {/each}
            </div>
          {/if}
          <div class="list">
            {#each decision.options.filter((opt) => !isConcede(opt) && opt.index !== primary?.index && !paymentBases.has(opt.index) && (!logic.autoPayMana || !isManualMana(opt))) as opt (opt.index)}
              {@const pickedAt = logic.picked.indexOf(opt.index)}
              {@const pickedCount = decision.repeatable ? logic.picked.filter((i) => i === opt.index).length : 0}
              <button
                class="option"
                class:picked={pickedAt >= 0}
                type="button"
                data-option={opt.index}
                onclick={(e) => logic.click(opt.index, { holdPriority: e.ctrlKey })}
                disabled={logic.busy}
              >
                {#if pickedAt >= 0 && (decision.max > 1 || decision.repeatable)}<span class="order inline">{decision.repeatable ? `×${pickedCount}` : pickedAt + 1}</span>{/if}
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
      {/if}
    {:else if logic.postedSeq !== null}
      <p class="prompt waiting">Answer sent — waiting for the game to advance</p>
    {:else}
      <p class="prompt waiting" data-waiting>{stepLabel} — waiting for {waitingName}</p>
    {/if}
  </div>

  {#if logic.arrangeOpen && arrange !== null}
    <ArrangeModal open={logic.arrangeOpen} decision={arrange} seed={logic.picked} onSubmit={submitArrange} onClose={() => (logic.arrangeOpen = false)} />
  {/if}

  {#if logic.discardOpen && discard !== null}
    <!-- The discard-pick modal (fb-20260918T201739Z): presentational only —
         picks go through logic.click (a Min==Max==1 ask posts straight
         through on the click, which also closes the ask and this modal),
         submit goes through the ordinary logic.submit. The wire intent is
         byte-identical to the inline strip's, because it IS the strip's
         posting path. -->
    <DiscardModal
      open={logic.discardOpen}
      decision={discard}
      picked={logic.picked}
      showSubmit={logic.showSubmit}
      canSubmit={logic.canSubmit}
      busy={logic.busy}
      onPick={(index) => logic.click(index)}
      onSubmit={submitDiscard}
      onClose={() => (logic.discardOpen = false)}
    />
  {/if}
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

  .payment-actions {
    display: grid;
    gap: var(--sp-2);
    width: 100%;
  }
  .payment-action {
    display: grid;
    gap: var(--sp-1);
    padding: var(--sp-2);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument-raised);
  }
  .payment-title { color: var(--ink); }
  .payment-summary {
    margin: 0;
    color: var(--ink-dim);
    font-size: var(--t-12);
  }
  .payment-plan { width: 100%; justify-content: center; }

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

  .arrange,
  .discard,
  .search {
    display: flex;
    flex-direction: column;
    width: 100%;
  }

  /* The library-search picker's filter input (fb-20260916T181754Z): one
     quiet bar above the grid, panel chrome rather than a dialog field. The
     grid below it scrolls — the wall of cards the report describes is the
     one thing the panel must not grow unbounded for. */
  .search-bar {
    padding: var(--sp-2) var(--sp-3) 0;
  }
  .search-filter {
    width: 100%;
    box-sizing: border-box;
    background: var(--instrument-raised);
    color: var(--ink);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    padding: var(--sp-1) var(--sp-2);
    font-family: var(--font-ui);
    font-size: var(--t-12);
  }
  .search-filter::placeholder {
    color: var(--ink-faint);
  }
  .search-filter:focus {
    outline: 1px solid var(--offered);
    outline-offset: -1px;
  }
  .search-grid {
    display: flex;
    flex-wrap: wrap;
    align-content: flex-start;
    justify-content: center;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-3);
    width: 100%;
    /* The filtered wall is exactly what scrolls: the panel's own max-height
       stays honest, and the grid scrolls inside it. */
    max-height: 16rem;
    overflow-y: auto;
    min-height: 0;
    /* A picker thumbnail, not the board's large face: at 72px the name is
       still legible on the no-art blank and five cards fit the panel's row. */
    --card-w: 72px;
  }
  .search-empty {
    margin: 0;
    padding: var(--sp-2);
    font-size: var(--t-12);
    line-height: 1.35;
    color: var(--ink-dim);
    width: 100%;
    text-align: center;
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

  .options {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    padding: var(--sp-2);
    width: 100%;
    min-height: 0;
  }
  /* Only the option list scrolls. The primary button used to scroll away with
     it on a long priority window, which defeats the point of having one. */
  .list {
    display: flex;
    flex-direction: column;
    gap: 1px;
    max-height: 11rem;
    overflow-y: auto;
    min-height: 0;
  }
  /* The remember checkbox (fb part B): one quiet line above the option list,
     styled as panel chrome rather than a form control so it reads as part of
     the ask, not as a separate dialog. */
  .remember {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-1) var(--sp-2);
    color: var(--ink-dim);
    font-size: var(--t-11);
    line-height: 1.35;
    cursor: pointer;
  }
  .remember:hover { color: var(--ink); }
  .remember input {
    margin: 0;
    accent-color: var(--offered);
  }
  .primary {
    background: var(--offered);
    color: var(--felt-sunk);
    border: 0;
    border-radius: var(--radius);
    padding: var(--sp-2) var(--sp-3);
    font-family: var(--font-ui);
    font-size: var(--t-14);
    font-weight: 600;
    cursor: pointer;
  }
  .seat-panel[data-tone='initiative'] .primary {
    background: var(--initiative);
  }
  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }

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
    font-family: var(--font-ui);
    font-size: var(--t-12);
    line-height: 1.35;
    cursor: pointer;
  }
  .option:hover {
    background: color-mix(in srgb, var(--ink) 7%, var(--instrument-raised));
    border-left-color: var(--ink-dim);
    color: var(--ink);
  }
  .option.picked {
    border-left-color: var(--initiative);
    color: var(--ink);
  }
  .order {
    font-family: var(--font-data);
    font-variant-numeric: tabular-nums;
    font-size: 0.6875rem;
    line-height: 1;
    color: var(--felt-sunk);
    background: var(--initiative);
    border-radius: 2px;
    padding: 0.15em 0.3em;
  }
  .order.inline {
    flex: none;
  }
  /* A repeatable modal ask's picked-so-far chips (CanRepeatModes$): one chip
     per picked instance in click order, each removing that instance. Styled
     as quiet panel chrome matching the option rows. */
  .picked-chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
  }
  .picked-chips .chip {
    background: var(--instrument-raised);
    color: var(--ink-inst);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    padding: 0 var(--sp-2);
    font-family: var(--font-ui);
    font-size: var(--t-11);
    line-height: 1.6;
    cursor: pointer;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .picked-chips .chip:hover {
    color: var(--ink);
    border-color: var(--ink-dim);
  }

  .submit {
    background: var(--instrument-raised);
    color: var(--ink-inst);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    padding: var(--sp-1) var(--sp-2);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    cursor: pointer;
  }
  .submit:disabled {
    opacity: 0.4;
    cursor: default;
  }

  /* The opening hand is one row by contract, from seven cards down to zero.
     At a narrow board the faces become thumbnails; CardTile's existing
     hover/focus detail is the reading affordance. Every direct card wrapper
     shares the available width, capped at the old comfortable 150px size.
     Nothing wraps and nothing scrolls sideways. */
  .hand {
    display: flex;
    flex-wrap: nowrap;
    justify-content: center;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: var(--sp-3);
    width: 100%;
    overflow: hidden;
    min-height: 0;
    /* A container, so the divide below has something to divide. A percentage
       here would be circular -- --card-w IS the card's width, so its own
       containing block would depend on it and resolve to zero. 100cqw is the
       row's inline size, which does not. */
    container-type: inline-size;
    --card-w: min(
      var(--play-card-w),
      calc((100cqw - (var(--n) - 1) * var(--sp-2)) / var(--n))
    );
  }
  /* The row divides its own width by the number of cards and hands the answer
     to CardTile as --card-w. Its cap is the shared --play-card-w token, so
     opening-hand cards use the same gameplay scale as the hand and board.
     That is the ONLY thing that changes size here.
     Overriding the widths of .card-tile / .slot / .card-image instead was
     wrong and looked it: CardTile positions its art, its keyword chips and
     its P/T badge from --card-w, so a slot that changed width while --card-w
     stayed at its 90px default left a small card floating in a large slot at
     1440 and a card overflowing its slot at 650, with the badges detached
     from the card they belong to.
     The shared gameplay cap keeps three cards from becoming billboards just
     because the opening-hand panel has room. */
  .hand > :global(.tile-wrap),
  .hand > .pick {
    flex: none;
    overflow: visible;
  }
  /* Below this the card is a thumbnail: there is no room for the blank
     card's mana pips or its foot rule, and forcing them in is what makes
     them spill out of the card. The reading affordance at this size is the
     detail panel on hover, not a legible face. */
  @container (max-width: 26rem) {
    .hand :global(.mana-symbols),
    .hand :global(.blank__foot) {
      display: none;
    }
  }

  .pick {
    position: relative;
    display: block;
    padding: 0;
    background: transparent;
    border: 0;
    border-radius: var(--radius-card);
    cursor: pointer;
    line-height: 0;
    flex: none;
  }
  /* A chosen card leaves the hand, so it reads as leaving: dimmed and sunk,
     with the ordinal that says where in the library's bottom it lands. */
  .pick.picked {
    opacity: 0.55;
    box-shadow: 0 0 0 2px var(--initiative);
  }
  .pick .order {
    position: absolute;
    top: -0.4em;
    left: -0.4em;
    line-height: 1.3;
  }
  .fallback {
    display: inline-block;
    padding: var(--sp-2);
    font-size: var(--t-12);
    line-height: 1.35;
    color: var(--ink-inst);
  }

  .choices {
    display: flex;
    justify-content: center;
    gap: var(--sp-2);
    padding: var(--sp-3);
    width: 100%;
  }
  .choice {
    background: transparent;
    color: var(--ink-inst);
    border: 1px solid var(--ink-faint);
    border-radius: var(--radius);
    padding: var(--sp-2) var(--sp-6);
    font-family: var(--font-ui);
    font-size: var(--t-14);
    font-weight: 600;
    cursor: pointer;
  }
  .choice.keep {
    background: var(--initiative);
    border-color: var(--initiative);
    color: var(--felt-sunk);
  }
  .choice:hover:not(:disabled) {
    border-color: var(--ink-dim);
  }
  .choice.keep:hover:not(:disabled) {
    border-color: var(--initiative);
  }
  .choice:disabled {
    opacity: 0.5;
    cursor: default;
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
</style>
