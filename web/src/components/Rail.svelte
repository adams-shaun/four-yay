<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { View, SeatInfo, DecisionBody } from '../protocol';
  import type { CardOptions } from '../lib/cardoptions';
  import { focusSeat } from '../lib/seattable';
  import { visibleHand } from '../lib/board';
  import { seatColour } from '../lib/colours';
  import SeatTable from './SeatTable.svelte';
  import HandList from './HandList.svelte';
  import ManaPool from './ManaPool.svelte';
  import StackTile from './StackTile.svelte';
  import PendingTray from './PendingTray.svelte';
  import ResolvedCard from './ResolvedCard.svelte';

  /**
   * The rail starts with ONE compact summary across seats, then the stack,
   * pending tray and live decision line. The summary supersedes the old split
   * between SeatTable counts and a second ZoneViewer count panel: life, hand,
   * library, graveyard and exile now appear exactly once. Disclosable pile
   * lists open in a body-portalled modal, so they cannot consume rail height.
   * The command zone remains on the board as art tiles (CZ1).
   *
   * The rail never scrolls as a whole: every section is intrinsically sized
   * or explicitly capped except the stack FRAME (history + live stack),
   * which takes the leftover height and scrolls inside itself.
   *
   * Revisited (U-rail-2, "the stack is very important in a lot of games, we
   * can hardly see 1 card"): the stack used to be capped small like every
   * other section and the detail pane (hand/pool/zones) was the one that
   * grew, which is backwards — the stack is read constantly during a
   * spectated game and the detail pane is read occasionally, on demand. So
   * the flex roles swap: the detail pane becomes a capped, collapsible
   * section like pending always was, and the stack becomes the ONE region
   * with `flex: 1` — it fills whatever height the seat table, the detail
   * pane and pending do not need, at any viewport, because it is still
   * capped by a `min-height` rather than a fixed one (a fixed height is
   * exactly the mistake this rail was rewritten to stop making: it is either
   * too small on a tall screen or overflows a short one).
   *
   * emphasizeTop (seat view) applies survey item 10 — top-of-stack by
   * contrast — while the spectator path leaves it off, unchanged.
   */
  let {
    view,
    seats,
    decision,
    stuck = null,
    onContinue = null,
    emphasizeTop = false,
    events = [],
    showLog = true,
    onToggleLog = null,
    logbar = null,
    yields = null,
    onYield = null,
    viewerSeat = null,
    options = null,
  }: {
    view: View;
    seats: SeatInfo[];
    decision: DecisionBody | null;
    /** stuck/onContinue forward the empty-answer safety net to the pending tray (see PendingTray): the viewer seat's own option-less decision, named — and, when the empty answer is legal, continuable. Optional so every existing caller renders exactly as before. */
    stuck?: { prompt: string; answerable: boolean } | null;
    onContinue?: (() => void) | null;
    emphasizeTop?: boolean;
    /** the DVR's own event list, forwarded to SeatTable for the PlayerLost cause (Task: dead seats say why) and read by ResolvedCard in the stack section (fb-20260916T225456Z — the resolved card's home is this frame, not the board). The shape is structural: any object whose `event` names a kind/player/text (and, for ResolvedCard, an obj) works, so every existing caller and test keeps rendering exactly as before. Optional so those callers stay unchanged. */
    events?: { event: { kind: string; player: number; text?: string; obj?: number } }[];
    /** showLog is whether the transcript is shown right now; the toggle below
     *  flips it. It is owned by Table.svelte (persisted per table and seat/
     *  spectator scope, the stops contract) and merely surfaced here next to
     *  the other seat controls. Optional so every existing caller/test renders
     *  as before with the control in its default state. */
    showLog?: boolean;
    /** onToggleLog is the rail's control: a real button (keyboard reachable,
     *  role switch) that asks Table to flip the transcript's visibility. */
    onToggleLog?: (() => void) | null;
    /** logbar is optional extra content for the logbar row, rendered after
     *  the LOGS toggle in normal flex flow (Table passes the concede control
     *  as a snippet — task fb-53bd45b9). The row owns the alignment and the
     *  height: the snippet content is pushed to the right edge by
     *  .logbar__extra, so a control sharing the row can never float over the
     *  seat table below it — the row simply grows if the content needs more
     *  height. Table keeps ownership of the concede STATE (the whole
     *  SeatPanelState wiring); Rail only hosts the markup in the one row
     *  that had spare room. */
    logbar?: Snippet | null;
    /** yields is the seated player's game-scoped always-pass set (prio6); null (spectator) renders no marker and no menu. */
    yields?: ReadonlySet<string> | null;
    /** onYield is the stack tile menu's write path into the seat panel's yield set. */
    onYield?: ((key: string) => void) | null;
    /** viewerSeat is the seat the stack is rendered for; an opponent-owned entry offers the always-yield menu. */
    viewerSeat?: number | null;
    /** options is the table's card-options bundle (Table.svelte's
     *  boardOptions), forwarded to SeatTable so its pile buttons wear the
     *  same tone ring the identity bar's pile icons wear
     *  (fb-20260916T225802Z). Optional and null by default so every
     *  existing caller renders exactly as before. */
    options?: CardOptions | null;
  } = $props();

  // The reader's explicit pick, or null to follow (focusSeat decides what
  // "follow" means). Pressing the row that is already focused releases the
  // pick and resumes following, so there is no separate "unpin" control.
  let picked = $state<number | null>(null);
  const focus = $derived(focusSeat(picked, view));
  const focused = $derived(view.players.find((p) => p.seat === focus) ?? null);
  // Spectators have no felt hand fan. Render every hand the projected view
  // actually exposes, while keeping seated players' own hand in its existing
  // fan and never inferring visibility from hand_size or table configuration.
  const spectatorHands: typeof view.players = [];

  // view.stack lists bottom of the stack first (push order); the rail shows
  // what resolves next at the top, so it is reversed for display only.
  const topFirst = $derived([...view.stack].reverse());

  // The live decision line names the seat being asked. `seats` is the first
  // word (the table's registered name), but on the live table route it can be
  // EMPTY while the view still carries each player's own name (the one-shot
  // seed in Table.svelte races the async tables.lookup — see the ui15
  // report); the fallback must be that name, not the 0-based `Seat N`
  // placeholder, exactly as IdentityBar does.
  const decisionWho = $derived(
    decision === null
      ? ''
      : seats[decision.player]?.name ?? view.players.find((p) => p.seat === decision.player)?.name ?? `Seat ${decision.player}`,
  );
</script>

<div class="rail-inner">
  <div class="logbar">
    {#if onToggleLog}
      <button
        class="logbar__toggle"
        class:on={showLog}
        type="button"
        role="switch"
        aria-checked={showLog}
        aria-label="Show the game log"
        data-log-toggle
        onclick={() => onToggleLog()}
      >
        <span class="dot" aria-hidden="true"></span>
        <span class="word">Logs</span>
      </button>
    {/if}
    {#if logbar}<span class="logbar__extra">{@render logbar()}</span>{/if}
  </div>
  <SeatTable {view} {seats} {focus} {events} {options} onFocus={(s) => (picked = picked === s ? null : s)} />

  <section class="focus" data-focus-pane data-focus-seat={focused?.seat}>
    {#if focused}
      <ManaPool pool={focused.pool} poolRestrictions={focused.pool_restrictions} />
    {/if}
  </section>

  {#if decision}
    <p class="decision">
      <span class="who">{decisionWho}</span>
      {decision.prompt}
    </p>
  {/if}

  <!-- The last resolved object's artwork (fb-20260916T225456Z; lifetime
       narrowed by fb-20260917T231516Z; placement fb-20260923T015554Z): a
       card popping OFF this stack shows in its own rail band, BEFORE the
       Stack heading — recent history the player asked to read separately,
       not a current stack member (under the heading it read as one). It
       clears on the next resolve, the next turn/step boundary, or the
       event window bound, and renders nothing while nothing is showing.
       Both history and current stack share the one scroll budget, so the
       history row cannot push Pending below the viewport. The component's
       internal divider is replaced by spacing before the Stack heading. -->
  <div class="history-frame">
    <ResolvedCard {view} {events} {seats} />
    <section class="stack">
      <h3>Stack{#if topFirst.length > 0} <span class="count">{topFirst.length}</span>{/if}</h3>
      {#each topFirst as s, i (s.id)}
        <StackTile stack={s} {view} emphasized={emphasizeTop && i === 0} dimmed={emphasizeTop && i > 0} {yields} {onYield} {viewerSeat} />
      {/each}
    </section>
  </div>

  <!-- The pending tray sits directly above the transcript and directly below
       the stack, because it is literally what is about to become stack
       (design system, "Layout"); the transcript is the band under this rail. -->

  <section class="pending">
    <h3>Pending</h3>
    <PendingTray pending={view.pending} {stuck} {onContinue} />
  </section>
</div>

<style>
  /*
   * The instrument register: cooler and flatter than the felt, structured by
   * hairlines rather than by cards. Sections are divided, not boxed — a
   * stack of identically-rounded panels would read as chrome, and this rail
   * is meant to read as an instrument face.
   *
   * The column is the height contract. Everything is capped or intrinsically
   * sized except the stack frame, which takes the leftover height and
   * scrolls history AND live stack together. The detail pane and pending
   * tray are capped and scroll inside themselves, so neither history nor
   * stack tiles can push Pending off the bottom (U2).
   */
  .rail-inner {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .rail-inner > :global(section),
  .rail-inner > :global(*) {
    flex: none;
    padding: var(--sp-2) var(--sp-3);
    border-bottom: 1px solid var(--edge-inst);
  }
  .rail-inner > :global(*:last-child) {
    border-bottom: 0;
  }
  /* The log visibility control sits at the rail's top, beside the seat
     controls. It names itself — LOGS, rendered uppercase like the row's
     old label — while the state stays in the affordances a switch already
     carries: the dot fills/colours with class:on and aria-checked carries
     it for assistive tech. The state WORD was dropped after a seated
     player rejected "Hidden" as unintelligible (ui21, player feedback:
     "the hidden button doesn't make sense. Make it LOGS text instead"). */
  .logbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    padding: var(--sp-2) var(--sp-3);
    border-bottom: 1px solid var(--edge-inst);
    flex: none;
  }
  /* Extra logbar content (today: the concede control snippet Table passes
     in) is pushed to the right edge of the row and stays IN FLOW — the row
     is the control's anchor, so it cannot paint over the seat table or the
     toggle, and the row grows if the content ever needs more height. The
     wrap lets a wide state (the armed "Concede — confirm" label, 129px
     measured) drop to the row's own second line at the 11rem floor instead
     of overflowing the rail horizontally. */
  .logbar__extra {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    flex: none;
  }
  .logbar__toggle {
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
  .logbar__toggle.on {
    color: var(--felt-sunk);
    background: var(--offered);
    border-color: var(--offered);
  }
  .logbar__toggle .word {
    text-transform: uppercase;
  }
  .logbar__toggle .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
    opacity: 0.5;
  }
  .logbar__toggle.on .dot {
    opacity: 1;
  }
  /* Written as element+class deliberately: the rule above is
     `.rail-inner > section`, which carries an element's worth of specificity,
     so a bare `.focus` loses to it and the whole column silently reverts to
     flex: none — which is exactly how the first cut of this still overflowed
     by 250px. */
  section.revealed-hands {
    flex: 0 1 auto;
    min-height: 0;
    max-height: 8rem;
    overflow-y: auto;
  }
  section.focus {
    flex: 0 1 auto;
    min-height: 2.25rem;
    max-height: 11rem;
    overflow-y: auto;
  }
  /* History and live stack share the same flexible, scrollable region.
     In a short viewport, the history row consumes scroll space rather than
     shrinking the Pending tray or overflowing the non-scrolling rail. */
  .rail-inner > .history-frame {
    flex: 1 1 8rem;
    min-height: 6rem;
    overflow-y: auto;
  }
  /* The resolved row is a separate history band BEFORE the Stack heading;
     its own border and the gap distinguish it from the live tiles. Suppress
     ResolvedCard's old internal divider at this embedding only. */
  .history-frame > :global(div.resolved) {
    margin-bottom: var(--sp-2);
  }
  .history-frame > :global(div.resolved__divider) {
    display: none;
  }
  section.pending {
    /* Pending is an always-readable control surface, not part of the rail's
       shrink budget. Hands and history scroll instead of compressing it. */
    flex: 0 0 auto;
    min-height: 2.25rem;
    max-height: 5rem;
    overflow-y: auto;
  }
  h3 {
    margin: 0 0 var(--sp-1);
    font-size: var(--t-12);
    font-weight: 600;
    color: var(--ink-dim);
    display: flex;
    align-items: baseline;
    gap: 0.4em;
  }
  /* The depth is always visible, panel open or closed (survey #11). */
  .count {
    font-family: var(--font-data);
    font-size: 0.6875rem;
    color: var(--ink-faint);
  }
  .decision {
    margin: 0;
    font-size: var(--t-12);
    line-height: 1.4;
    color: var(--ink-inst);
  }
  .who {
    display: block;
    font-family: var(--font-data);
    font-size: 0.6875rem;
    color: var(--ink-faint);
  }
</style>
