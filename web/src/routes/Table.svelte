<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { session } from '../lib/session.svelte';
  import { tables } from '../lib/tables.svelte';
  import { MatchState } from '../lib/match.svelte';
  import { ManaBrewMatch } from '../lib/manabrew/source.svelte';
  import { activeWire, loadProtocol } from '../lib/manabrew/pref';
  import WireNotice from '../components/WireNotice.svelte';
  import BoardStage from '../components/BoardStage.svelte';
  import Arrows from '../components/Arrows.svelte';
  import MotionLayer from '../components/MotionLayer.svelte';
  import Rail from '../components/Rail.svelte';
  import PileHost from '../components/PileHost.svelte';
  import Transcript from '../components/Transcript.svelte';
  import DvrBar from '../components/DvrBar.svelte';
  import MatchList from '../components/MatchList.svelte';
  import ConcedeControl from '../components/ConcedeControl.svelte';
  import RestartControl from '../components/RestartControl.svelte';
  import SeatPanel from '../components/SeatPanel.svelte';
  import HandFan from '../components/HandFan.svelte';
  import PlaySettingsPanel from '../components/PlaySettingsPanel.svelte';
  import LayoutDrawer from '../components/LayoutDrawer.svelte';
  import ViewHotkeys from '../components/ViewHotkeys.svelte';
  import { layoutStore } from '../lib/layouts.svelte';
  import PromptDock from '../components/prompts/PromptDock.svelte';
  import { effectivePlacement, profilePlacement } from '../lib/prompts/dock';
  import RailResizer from '../components/RailResizer.svelte';
  import {
    SeatPanelState,
    mulliganPhase,
    toneOf,
  } from '../lib/seatpanel.svelte';
  import { laterByObj, optionsByObj, optionsByPlayer, resolveCardFollowUp, type CardOptions } from '../lib/cardoptions';
  import { isPlainManualTap, manualManaHidden } from '../lib/manualmana';
  import { castableActions } from '../lib/announcepay';
  import { rematchDecks, startRematch } from '../lib/playvsbot';
  import { stuckDecision } from '../lib/prompt';
  import { loadLogShown, saveLogShown, type LogScope } from '../lib/logshown';
  import { safeStorage } from '../lib/storage';
  import { everyVisibleCard } from '../lib/board';
  import { seatColour } from '../lib/colours';
  import { seatRows } from '../lib/seattable';
  import { buildCardOwnerColour } from '../lib/logrender';
  import { href, navigate } from '../lib/router';
  import { getSeat } from '../lib/seat';
  import { clickedOutside } from '../lib/modals';
  import { closesOptionsPanel } from '../lib/hotkeys';
  import { keymapStore } from '../lib/keymap.svelte';

  // match (from the /t/:table/m/:match route) names a specific, already-played
  // match. Task 21's finished mode replays it end to end via loadFinished:
  // no stream, no session.focus — everything from the JSON GETs.
  //
  // App.svelte keys <Table> by `${table}/${match}` (FL-38), so a route change
  // always remounts a fresh instance — these one-shot reads of table/match
  // are intentional, not a stale-binding bug.
  let { table, match = null }: { table: string; match?: number | null } = $props();
  // svelte-ignore state_referenced_locally
  const finished = match !== null;

  // The seat identity is read once from the join URL (?seat=N&token=…,
  // M2e-3's FL-99). With no seat, every line below is the spectator path
  // it always was (R-E4-4).
  const seatCtx = getSeat();
  const seated = seatCtx !== null;
  // The log starts hidden for a SEATED player (they are playing, not reading)
  // and stays shown for a spectator (they follow the game through it, and its
  // scrubbing). The choice is persisted per table and per scope — the stops
  // contract, not a component localStorage reach. `liveSeated` is a mount
  // constant, so the scope and the default are fixed for the life of this
  // mounted route. (A finished /m/:match route is a spectator replay and keeps
  // the spectator default.)
  const liveSeated = seated && !finished;
  const logScope: LogScope = liveSeated ? 'seat' : 'spectator';
  // The layout profile can pin the log shown or hidden; its default,
  // 'remember', is exactly the per-table memory above.
  let logChoice = $state(!liveSeated);
  const logMode = $derived(layoutStore.profile.panels.log);
  const showLog = $derived(logMode === 'show' ? true : logMode === 'hide' ? false : logChoice);
  function toggleLog() {
    if (logMode !== 'remember') {
      layoutStore.edit((p) => (p.panels.log = showLog ? 'hide' : 'show'));
      return;
    }
    logChoice = !logChoice;
    saveLogShown(safeStorage(), table, logScope, logChoice);
  }
  const railSide = $derived(layoutStore.profile.panels.rail);
  const railWidth = $derived(layoutStore.profile.panels.railWidth);
  let railPreview = $state<number | null>(null);
  const displayedRailWidth = $derived(railPreview ?? railWidth);
  const tableStyle = $derived(`--rail-width:${displayedRailWidth * 100}%`);
  // The game wire (ManaBrew adapter plan): a live seat whose protocol
  // setting is ManaBrew plays through ManaBrewMatch; everything else — and a
  // ManaBrew probe that fails — is the native MatchState. Both expose the
  // surface this route reads, so nothing below knows which wire is live.
  // svelte-ignore state_referenced_locally
  const mb = liveSeated && seatCtx !== null && loadProtocol() === 'manabrew' ? new ManaBrewMatch(table, seatCtx) : null;
  // svelte-ignore state_referenced_locally
  let m = $state<MatchState | ManaBrewMatch>(mb ?? new MatchState(table, seatCtx ?? undefined));
  // The seat panel answers through the ManaBrew adapter when it is live.
  const panelCtx = $derived(m instanceof ManaBrewMatch ? m.ctx : seatCtx);
  let wireFallback = $state<string | null>(null);
  const wireNotice = $derived(wireFallback ?? (m instanceof ManaBrewMatch ? m.notice : null));
  function dismissWireNotice() {
    wireFallback = null;
    if (m instanceof ManaBrewMatch) m.notice = null;
  }

  // idle: the table has no live match and none imminent, so the match list
  // is the whole page rather than a strip under a "waiting" placeholder.
  const idle = $derived.by(() => {
    if (finished) return false;
    const state = tables.list.find((t) => t.info.id === table)?.info.state;
    return state === 'idle' || state === 'halted';
  });

  // One SeatPanelState per match, created here rather than inside SeatPanel
  // so the phase track above the board and the panel share ONE stop set and
  // ONE autopilot. Built in a $derived (not a $effect) because the SSR pass
  // renders the seat surface and effects never run there; the cache keeps a
  // recompute from throwing away the seat's live decision, and only a new
  // match number replaces the instance.
  let panelCache: { match: number; state: SeatPanelState } | null = null;
  const panel = $derived.by(() => {
    const mm = m.match;
    if (!seated || panelCtx === null || mm === null || finished) return null;
    if (panelCache === null || panelCache.match !== mm) {
      const state = new SeatPanelState(table, mm, panelCtx);
      state.onFollowUpArm = (arm) => {
        expectedCardFollowUp = arm;
      };
      // Seed the first SSR/client paint. SeatPanel's effect keeps later views
      // adopted; doing the first one here lets the route place mulligan and
      // the page-level concede control correctly before that child mounts.
      state.adoptView(m.view?.decision ?? null);
      panelCache = { match: mm, state };
    }
    return panelCache.state;
  });

  // The slot and shell use the same effective placement: a payment with an
  // older persisted near-table layout occupies the rail instead of the board.
  const promptPlacement = $derived(profilePlacement(effectivePlacement(layoutStore.prompt, panel?.active ?? null)));

  // The seated player's own player view — the one whose hand is never
  // redacted (view.go fills Hand for the viewer's seat under every
  // visibility). HandFan renders it as real card faces along the bottom edge;
  // a spectator has no `ownPlayer` and mounts no hand at all.
  const ownPlayer = $derived(
    m.view && seatCtx ? (m.view.players.find((p) => p.seat === seatCtx.seat) ?? null) : null,
  );

  // The ACTIONS surface is the single in-game response anchor. Mulligan is
  // intentionally separate because its opening hand needs the whole board.
  // Read the wire view first: the opening hand must suppress the ACTIONS
  // instrument in the very frame it arrives, before the shared panel state has
  // adopted it. Falling back keeps the recovery-poll path equally correct.
  const mulligan = $derived(mulliganPhase(m.view?.decision ?? panel?.active ?? null));
  const concede = $derived(panel?.concedeOption ?? null);

  // The table's own public config (format, bot_policy, mulligans), for the
  // restart control's POST. TableInfo always carries all three.
  const tableInfo = $derived(tables.list.find((t) => t.info.id === table)?.info ?? null);
  // The table capability is authoritative: with it off this state remains the
  // manual baseline even if a stale response carried a payment extension.
  $effect(() => {
    panel?.setAutoManaAvailable(tableInfo?.auto_mana === true);
  });

  // Restart is offered only on the play-vs-bot shape createGame builds:
  // a live seated 2-seat table, exactly one of those seats a real person,
  // and that person is the viewer. A spectator, a finished /m/:match
  // replay, a two-human table and the pre-game mulligan round get none
  // (the mulligan round is not yet a game to restart). Offering it does
  // NOT depend on a concede being pending — the useful moment is any time
  // mid-game. The seat that is the human is seat 0 in createGame's
  // rotation, but the predicate reads the wire flags, not a hard-coded
  // index, so an inverted assignment still finds it.
  const restartable = $derived.by(() => {
    if (!liveSeated || seatCtx === null || m.match === null || mulligan !== null) return false;
    if (m.seats.length !== 2) return false;
    // Exactly ONE human seat, and it is the viewer (a two-human table and a
    // viewer sitting in the bot seat both fail here).
    if (m.seats.filter((s) => s.human).length !== 1) return false;
    return m.seats.findIndex((s) => s.human) === seatCtx.seat;
  });

  let restartConfirming = $state(false);
  let restartBusy = $state(false);
  let restartError = $state<string | null>(null);

  // Options stays with its control in the rail. The panel is deliberately a
  // popover: its long editor scrolls inside a bounded surface instead of
  // expanding the rail or the whole table.
  let optionsOpen = $state(false);
  let optionsPopover = $state<HTMLDivElement | null>(null);
  // The outside-click test needs the WHOLE control (button + popover). It
  // used to share optionsPopover's binding, which the popover's own
  // bind:this took over the moment it mounted, so the opening click itself
  // bubbled to the window as "outside" and closed it again.
  let optionsRoot = $state<HTMLDivElement | null>(null);
  let optionsButton = $state<HTMLButtonElement | null>(null);
  // A keyboard close hands focus back to the Options button. A pointer close
  // takes focus off it instead: a focused button owns Space and Enter
  // (lib/hotkeys focusOwnsKey), so the next Space would reopen the panel
  // rather than pass.
  function dismissOptions(byPointer = false): void {
    if (!optionsOpen) return;
    optionsOpen = false;
    void tick().then(() => (byPointer ? optionsButton?.blur() : optionsButton?.focus()));
  }
  function toggleOptions(event?: MouseEvent): void {
    // detail is 0 when Enter or Space activated the button; the hotkey
    // strip calls this with no event.
    if (optionsOpen) dismissOptions((event?.detail ?? 0) > 0);
    else optionsOpen = true;
  }
  // Escape, or the toggle-options chord: the panel is a role=dialog, so the
  // strip's hotkey is held by the modal guard while it is open and the same
  // key closes it here instead (lib/hotkeys closesOptionsPanel).
  function closeOptions(event: KeyboardEvent): void {
    if (event.key === 'Escape' || (optionsOpen && closesOptionsPanel(event, keymapStore.current, keymapStore.capturing))) {
      event.preventDefault();
      dismissOptions();
    }
  }
  function closeOptionsOutside(event: MouseEvent): void {
    if (optionsOpen && optionsRoot && clickedOutside(event, optionsRoot)) dismissOptions(true);
  }
  $effect(() => {
    if (optionsOpen) void tick().then(() => optionsPopover?.focus());
  });

  async function restart() {
    if (tableInfo === null) return;
    restartBusy = true;
    restartError = null;
    try {
      // The human may occupy either seat on a hosted table; preserve deck
      // roles rather than assuming createGame's usual seat-0 assignment.
      const { humanDeck, botDeck } = rematchDecks(m.seats, seatCtx!.seat);
      const join = await startRematch(
        tableInfo.format as 'constructed' | 'commander',
        humanDeck,
        botDeck,
        tableInfo.bot_policy,
        tableInfo.mulligans,
      );
      window.location.href = join;
    } catch (e) {
      restartError = e instanceof Error ? e.message : String(e);
      restartConfirming = false;
      restartBusy = false;
    }
  }
  // fb-20260917T231628Z: the log show/hide control moved into the OPTIONS drop
  // (PlaySettingsPanel's Layout section) for ordinary seated play. The drop is
  // mounted only while BoardStage's controls are live, so the rail's
  // LOGS toggle must survive in exactly the states where the drop does not exist
  // (spectator, mulligan round, game over, the finished /m/:match replay) — a
  // spectator whose saved preference is "hidden" could otherwise never get the
  // log back. Reachability is DEFINED AS `controlsLive`, so the rail's
  // toggle and the drop's switch can never both be absent. Keep the controls
  // object stable during teardown: child prop getters may re-read it mid-flush.
  const controls = $derived(
    panel && seatCtx && m.match !== null && m.view !== null
      ? { state: panel, ctx: panelCtx ?? seatCtx, table, match: m.match, onToggleOptions: toggleOptions }
      : null,
  );
  const controlsLive = $derived(controls !== null && !finished && mulligan === null && !m.view?.over);
  const optionsReachable = $derived(controlsLive);

  // A route can become non-interactive while the popover is open (game over,
  // mulligan, or a seat change). Do not leave an orphaned floating panel.
  $effect(() => {
    if (!optionsReachable) optionsOpen = false;
  });
  // The empty-answer safety net (the Squadron Hawk fail-to-find soft-lock):
  // a decision for THIS seat that carries no options is one no picker can
  // render, so the Pending tray names it — and, when the empty answer is
  // legal (Min 0), carries a Continue that submits it through the seat
  // panel's ordinary posting path (SeatPanelState.continueEmpty). The engine
  // resolves every such shape silently since the empty-choose fix, so a live
  // server should never hold one; the tray entry is belt-and-braces (an older
  // server, a new engine shape) so a pending decision for this seat is never
  // invisible. A spectator has no seat and no answerable decision: the rail's
  // own decision line already names who is being asked.
  const stuck = $derived(seated ? stuckDecision(m.view?.decision ?? null) : null);
  const onContinue = $derived(
    panel !== null && stuck !== null && stuck.answerable ? () => panel.continueEmpty() : null,
  );
  // A direct card action can hand the server a first-stage choice and receive
  // a second decision for the same object (Underground Sea's activate -> Add
  // U / Add B flow; a multi-ability mana source's stage-1 ability pick -> its
  // stage-2 colour wheel, fb-e079def5; a Treasure's activate -> its colour
  // ask, fb-20260923T050205Z). The expectation is ARMED by SeatPanelState
  // itself, on the accepted hand post -- this route no longer owns that
  // write, because the tile path (boardOptions.post) and the seat panel's own
  // option buttons (SeatPanel.svelte's onclick -> panel.click) both post
  // through panel.click, and only one of them used to arm. Remember only that
  // one network continuation: the picker itself is unmounted while the posted
  // decision is hidden, so it cannot carry open state across the round trip.
  // The effect is the one decoder of the expectation -- resolveCardFollowUp
  // opens the picker only when the next decision really carries 2-6 options
  // on the expected object, and disarms otherwise.
  //
  // The panel hands each arm to expectedCardFollowUp through its
  // onFollowUpArm callback (wired where the panel is built). The effect reads
  // only this route's own state -- never the panel -- so its dependencies are
  // the decision and the mirror: an arm that lands AFTER SSE already
  // delivered the follow-up still retriggers it (a fresh object each arm),
  // and no reactive edge reaches the panel's derived UI graph (the null-ctx
  // hang, agent-20260924T114750Z-ba517e3b).
  let expectedCardFollowUp = $state<{ seq: number; obj: number } | null>(null);
  let autoOpenCardDecision = $state<{ seq: number; obj: number } | null>(null);
  $effect(() => {
    const d = m.view?.decision ?? null;
    const expected = expectedCardFollowUp;
    if (d === null || expected === null || d.seq === expected.seq) return;
    autoOpenCardDecision = resolveCardFollowUp(expected, d);
    expectedCardFollowUp = null;
  });

  // The board's card-options index (ui21): the pending decision grouped by
  // the object each option concerns. For a seated view this is exactly the
  // decision the seat must answer now — the same decision the seat panel
  // surfaces (SeatPanelState.active), so the board and the panel can never
  // disagree about what this seat is being asked, and both drop it once
  // answered. A spectator has no seat panel and no pending decision, and a
  // seat that has nothing pending gets null — in both cases no tile is
  // marked and no tile carries a menu. Building it once here, from the
  // active decision, is what makes one index serve both 'cards with valid
  // options' and 'valid targets' (R-E4-2: the client only regroups options
  // the server already sent, and posts each one by its own index, R-E4-1).
  const boardOptions = $derived.by((): CardOptions | null => {
    if (!seated || panel === null) return null;
    const d = panel.active;
    if (d === null) return null;
    const byObj = optionsByObj(d);
    // Auto Mana owns mana activations while it is enabled.  Do not leave a
    // manual tap badge on a source: that made the setting look ineffective
    // and let a player spend the source outside the offered payment plan.
    // This is presentation-only filtering; the server remains authoritative.
    // The taps stay whenever a play the window can reach needs them -- a
    // non-cast action or a cast no plan pays, including one offered only
    // once the mana floats -- and a costly activation (Lion's Eye Diamond, a
    // Treasure) is a play of its own, never hidden. WHEN and WHICH are the
    // one shared rule the option list and the hot strip read too
    // (lib/manualmana.ts, §8).
    if (manualManaHidden(d, m.view, seatCtx?.seat, panel.autoPayMana)) {
      for (const [obj, offered] of byObj) {
        const visible = offered.filter((option) => !isPlainManualTap(option));
        if (visible.length === 0) byObj.delete(obj);
        else if (visible.length !== offered.length) byObj.set(obj, visible);
      }
    }
    return {
      source: d.source,
      byObj,
      byPlayer: optionsByPlayer(d),
      picked: [...panel.picked],
      tone: toneOf(d),
      autoOpenObj: autoOpenCardDecision?.seq === d.seq ? autoOpenCardDecision.obj : undefined,
      // The card's abilities that need mana floated first (fb-20260923T033148Z):
      // the seat's own potential_actions, regrouped onto the tiles this
      // priority decision already offers something.
      later: laterByObj(d, ownPlayer?.potential_actions),
      post: (index: number, _expectFollowUp = false, holdPriority = false) => {
        // The tile path shares the arm site with the panel: panel.click arms
        // the card-follow-up expectation itself, so this route no longer
        // writes it. The expectFollowUp flag stays on the signature because
        // every tile affordance speaks it (cardoptions.CardOptions.post) --
        // the CONTRACT that a card-anchored post may hand back a follow-up --
        // even though the arm now rides the click, not the flag.
        autoOpenCardDecision = null;
        panel.click(index, { holdPriority });
      },
    };
  });

  // Task 3's colour-coded log: the same name/colour resolution SeatTable's
  // rows use (seats[seat].name falling back to the wire's own player name),
  // off whichever view is currently on screen. Guarded for the pre-snapshot
  // instant when m.view is still null — the template only mounts Transcript
  // once m.view is truthy, but this $derived is read at module scope, not
  // inside that block, so it has to handle null itself.
  const logIdentities = $derived(
    m.view ? seatRows(m.view, m.seats).map((r) => ({ name: r.name, colour: r.colour || seatColour(r.seat, m.seats) })) : [],
  );

  // Task lc1: colour each card name in the log by the colour of the seat
  // that OWNS it — the same colour that seat's name renders in — resolved by
  // the card's object id. The described line carries the id (and the name)
  // but not the owner's colour, so it is looked up from the current view's
  // cards (every visible zone, via everyVisibleCard — which defends the
  // public-spectator null hand) by id -> owner -> that seat's colour. An id
  // absent from the view (a card that has left every visible zone) resolves
  // null and renders uncoloured. Rebuilt each render.
  //
  // The SAME flattened list is handed to the transcript as `cards` so a
  // card-name hover can resolve its id to a CardView for the detail panel
  // (fb-20260927T154603Z) — one flatten, two consumers.
  const logCards = $derived(m.view ? everyVisibleCard(m.view.players) : []);
  const logCardColour = $derived(
    m.view
      ? buildCardOwnerColour(
          logCards,
          (owner) => m.seats[owner]?.colour || seatColour(owner, m.seats),
        )
      : null,
  );

  onMount(() => {
    if (match !== null) {
      if (m instanceof MatchState) void m.loadFinished(match);
      return;
    }
    let closed = false;
    let offNative: (() => void) | null = null;
    const native = (nm: MatchState) => {
      activeWire.current = 'native';
      const off = session.stream.onFrame((f) => {
        // MatchState owns the board/DVR half of rewind; the panel owns pending
        // posts and timers. A reconnect represents a rewind as a shorter
        // snapshot, so use MatchState's classification rather than only the
        // wire frame name. This all runs in one synchronous stream callback,
        // before Svelte can expose the restored lower sequence to the panel.
        if (nm.apply(f)) panelCache?.state.rewind();
      });
      void session.focus(table);
      const t = tables.list.find((x) => x.info.id === table);
      if (t) nm.seats = t.seats;
      offNative = () => {
        off();
        void session.unfocus(table);
      };
    };
    if (mb) {
      activeWire.current = 'manabrew';
      mb.onRewind = () => panelCache?.state.rewind();
      void mb.start().then((r) => {
        if (r.ok || closed) return;
        mb.close();
        wireFallback = `${r.reason} — playing over Native.`;
        const nm = new MatchState(table, seatCtx ?? undefined);
        m = nm;
        native(nm);
      });
    } else if (m instanceof MatchState) native(m);
    return () => {
      closed = true;
      mb?.close();
      offNative?.();
    };
  });
  // The log visibility loads where storage exists (onMount, never SSR), the
  // same discipline as stops. An absent/corrupt value (null) leaves the
  // view-specific default in place.
  onMount(() => {
    const saved = loadLogShown(safeStorage(), table, logScope);
    if (saved !== null) logChoice = saved;
  });
</script>

<svelte:window onkeydown={closeOptions} onclick={closeOptionsOutside} />

{#if idle}
  <main class="matches-page">
    <MatchList {table} />
  </main>
{:else}
  <main class="table rail-{railSide}" style={tableStyle} class:log-hidden={!showLog} class:rail-peek={optionsOpen}>
    <!-- Motion overlay (spec sub-project 5): renders nothing here; it plays
         the client model's transitions in a fixed layer on <body>. Mounted
         outside the view guard so a match change does not remount it. -->
    {#key m}<MotionLayer source={m} viewerSeat={seated ? (seatCtx?.seat ?? null) : null} />{/key}
    {#if m.halted}<div class="halted">Table halted: {m.halted}</div>{/if}
    {#if wireNotice}<WireNotice text={wireNotice} onDismiss={dismissWireNotice} />{/if}
    {#if m.view}
      <section class="board">
        <!-- The table clock is the board's full-width centre lane, ringed by
             the ACTIVE seat's one established identity colour. BoardStage
             reserves that lane in the seat geometry, so no card row continues
             beneath the band. It remains display-only for spectators and owns
             the same stop set/callback for a live seat. -->
        <BoardStage
          view={m.view}
          seats={m.seats}
          options={boardOptions}
          seat={panel ? seatCtx?.seat ?? null : null}
          stops={panel ? panel.stops : null}
          onToggle={panel ? (step, side) => panel.toggleStop(step, side) : null}
          mulligan={mulligan !== null}
          {controls}
          {controlsLive}
          onOpenFlow={optionsReachable ? () => toggleOptions() : null}
          hand={seated && seatCtx && ownPlayer ? handFan : null}
        />
        <!-- The last resolved card's artwork lives in the rail's stack
             section now (fb-20260916T225456Z): Rail renders ResolvedCard
             from the same m.dvr.events it already receives, and the old
             RecentStrip board overlay — absolutely positioned over the
             board's bottom centre, a patchwork of two earlier complaints
             about the same element — is deleted. -->
        <!-- `finished` is the /t/:table/m/:match route: loadFinished paints a
             FROZEN replay of an already-played match, with no stream and no
             session.focus, so view.decision is whatever was pending at that
             point in history and never advances. A seat panel there offers
             buttons that post a long-stale seq, the server rejects every one,
             and the page looks hung while the live game waits elsewhere --
             which is exactly what happened the first time this was played.
             A seat acts only on the live table route. -->
        {#if seated && seatCtx && m.match !== null && !finished && (mulligan !== null || m.view.over)}
          {#key m.match}
            <SeatPanel view={m.view} seats={m.seats} ctx={panelCtx ?? seatCtx} table={table} match={m.match} state={panel} />
          {/key}
        {/if}
      </section>
      {#snippet handFan({ cardWidth, visible, raise }: { cardWidth: number; visible: number; raise: boolean })}
        <!-- The seated player's own hand (Task ui17), in the hand row the
             layout sizes from its row unit. ui23: the hand gets the same
             card-options index the board does (one mechanism, one index, one
             post path). -->
        {#if ownPlayer}
          <HandFan player={ownPlayer} {cardWidth} {visible} {raise} options={boardOptions} paymentActions={castableActions(panel?.active ?? null, panel?.autoManaAvailable ?? false)} autoPay={panel?.autoPayMana ?? false} onCastPayment={(action, holdPriority) => panel?.castAction(action, holdPriority)} />
        {/if}
      {/snippet}
      <aside class="rail" data-rail-side={railSide}>
        <!-- The prompt dock's mount point (UI rework §4, lane C): the
             decision prompt docks at the top of the rail, next to the stack
             it concerns, unless the layout profile floats it
             (layoutStore.prompt, which PromptDock reads). ONE dock: priority
             stays with the ACTIONS / gilt action button. -->
        {#if promptPlacement !== 'dock-bottom'}
          <div class="prompt-dock" data-prompt-dock-slot data-placement={promptPlacement}>
            {#if panel && seatCtx && controlsLive && m.view}<PromptDock view={m.view} logic={panel} seat={seatCtx.seat} />{/if}
          </div>
        {/if}
        <div class="rail-main">
        <!-- The concede control (when a concede option is pending) is passed
             to Rail as a logbar snippet: it renders inside the rail's own
             LOGS row, in normal flex flow at the row's right edge. fb-53bd45b9:
             it used to be absolutely positioned at top: 3rem inside the rail,
             a hand-calibrated offset that cleared the logbar but landed on
             seat row 0 (the stacked zone counts made each row taller), and
             its z-index: 9 painted over the row's life and pile counts —
             eating the pile buttons' clicks in the covered band. In flow
             inside the logbar row, "floating" is structurally impossible:
             the row is the anchor and grows if the control needs height.
             The state stays here (SeatPanelState wiring); only the markup's
             host row moved. -->
        <!-- fb-20260917T231628Z: while the OPTIONS drop is reachable the log
             switch lives there, so the rail renders no second control (its
             own {#if onToggleLog} hides the row's button when the prop is
             null). The drop is NOT mounted for a spectator, the mulligan
             round, a game-over board or the finished replay — exactly the
             states where the rail keeps its toggle. -->
        <Rail
          view={m.view}
          seats={m.seats}
          decision={seated ? null : m.decision}
          {stuck}
          {onContinue}
          emphasizeTop={seated}
          events={m.dvr.events}
          showLog={showLog}
          onToggleLog={optionsReachable ? null : toggleLog}
          yields={panel?.yields ?? null}
          onYield={panel ? (key) => panel.addYield(key) : null}
          viewerSeat={seated ? (seatCtx?.seat ?? null) : null}
          options={boardOptions}
        >
          {#snippet logbar()}
            {#if restartable}
              <RestartControl
                confirming={restartConfirming}
                busy={restartBusy}
                onArm={() => { restartConfirming = true; restartError = null; }}
                onConfirm={() => void restart()}
              />
              {#if restartError}
                <span class="restart-error" role="alert">{restartError}</span>
              {/if}
            {/if}
            {#if panel && concede}
              <ConcedeControl
                confirming={panel.confirming}
                busy={panel.busy}
                onArm={() => panel.click(concede.index)}
                onConfirm={() => panel.confirmConcede()}
              />
            {/if}
            {#if panel && optionsReachable}
              <div class="rail-options" bind:this={optionsRoot} data-rail-options>
                <button
                  type="button"
                  class="rail-options__button"
                  aria-haspopup="dialog"
                  aria-expanded={optionsOpen}
                  aria-controls="play-options-popover"
                  bind:this={optionsButton}
                  onclick={toggleOptions}
                >
                  Options
                </button>
                {#if optionsOpen}
                  <div id="play-options-popover" class="rail-options__popover" role="dialog" aria-label="Play options" aria-modal="false" tabindex="-1" bind:this={optionsPopover}>
                    <div class="rail-options__body">
                      <PlaySettingsPanel state={panel} {showLog} onToggleLog={toggleLog} />
                    </div>
                  </div>
                {/if}
              </div>
            {/if}
          {/snippet}
        </Rail>
        </div>
        <!-- The log lives in the rail beneath the stack (the v3 layout),
             shown or hidden by the per-table choice or the layout profile. -->
        <section class="transcript" class:hidden={!showLog} aria-label="Game log">
          {#if !seated}
            <DvrBar dvr={m.dvr} onAction={(a) => m.dispatch(a)} {finished} />
          {/if}
          <div class="log"><Transcript dvr={m.dvr} identities={logIdentities} cardColour={logCardColour} cards={logCards} notes={panel?.autoLog ?? []} onSeek={seated ? () => {} : (seq) => m.dispatch({ type: 'scrub', seq })} /></div>
        </section>
        {#if promptPlacement === 'dock-bottom'}
          <div class="prompt-dock bottom" data-prompt-dock-slot data-placement={promptPlacement}>
            {#if panel && seatCtx && controlsLive && m.view}<PromptDock view={m.view} logic={panel} seat={seatCtx.seat} />{/if}
          </div>
        {/if}
        {#if railSide !== 'hidden'}
          <RailResizer side={railSide} width={railWidth} onDrag={(width) => (railPreview = width)} onWidth={(width) => layoutStore.setRailWidth(width)} onReset={() => layoutStore.resetRailWidth(m.seats.length)} />
        {/if}
      </aside>
      <ViewHotkeys view={m.view} onToggleLog={toggleLog} />
      {#if layoutStore.drawerOpen}
        <LayoutDrawer />
      {/if}
      <!-- fb-20260914T121642Z: the one arrows overlay lives HERE, at the
           table root, not inside the felt subtree. section.board clips its
           own content (overflow: hidden bounds the felt/cards), so an overlay
           mounted under it cannot draw a line that reaches the stack rail —
           which is why every stack-to-stack target arrow (a counterspell's
           arrow to the spell beneath it) was computed but invisible. Hosted
           by the table root the overlay's box contains both endpoints, and
           .table below is its positioned containing block. Still
           pointer-events: none; arrowsFor/previewArrowsFor are unchanged. -->
      <!-- fb-20260916T225802Z: the table's ONE pile modal. The rail's pile
           buttons and the identity bar's graveyard/exile icons open it
           through the shared pileOpener store; PileHost renders the single
           instance and hands it boardOptions, so pile cards are actionable
           exactly where the cards are (tone ring, badge, menu — each item
           posting the option's own wire index). -->
      <PileHost view={m.view} seats={m.seats} options={boardOptions} />
      <Arrows view={m.view} options={boardOptions} />
    {:else if finished && m.loadError}
      <div class="load-error">
        <p>Match {match} isn't available on {table} ({m.loadError}).</p>
        <a href={href({ kind: 'table', table })} onclick={(e) => { e.preventDefault(); navigate({ kind: 'table', table }); }}>Back to {table}</a>
      </div>
    {:else if finished}
      <p class="waiting">Loading match {match}…</p>
    {:else}
      <div class="idle-inline">
        <p class="waiting">Waiting for {table}…</p>
        <MatchList {table} />
      </div>
    {/if}
  </main>
{/if}

<style>
  /*
   * Two registers on one grid. The board is felt — warm, dark, card art
   * dominant, chrome absent. The rail is instrument — cooler, denser, hairline
   * structure. The temperature difference between them is the seam, and the
   * seam is the identity (design plan).
   *
   * The transcript spans the full width beneath both, because the log is the
   * one thing that describes the whole table rather than either half of it.
   */
  .matches-page {
    min-height: 100vh;
  }
  .table {
    display: grid;
    /* The overlay's containing block: Arrows mounts here (fb-20260914T121642Z)
       and positions itself absolute/inset 0 against this box, so its
       coordinates are measured over the whole table — felt and rail both. */
    position: relative;
    /* The rail's floor is what its content measures: since fb-20260917T232028Z
       the seat summary is one text-line tall and its register — the life
       pill, the four one-line zone counts and the row's own chrome — measures
       ~141px (SeatTable.svelte.test.ts's geometry harness). That now sits
       just under the old binding constraint, the stack tile's 144px art
       column, instead of well under it; both clear 176px. The rail's saved
       viewport fraction drives the width, with a compact lower bound on
       narrow screens so the splitter remains usable. */
    grid-template-columns: minmax(0, 1fr) minmax(min(15rem, var(--rail-width)), var(--rail-width));
    grid-template-rows: minmax(0, 1fr);
    height: 100vh;
    background: radial-gradient(ellipse at 50% 50%, var(--felt-lit) 0%, var(--felt) 70%);
  }
  /* The rail's side is the layout profile's (panels.rail). Left swaps the
     columns; hidden turns the rail into an edge drawer that slides in on
     hover or focus (and while the Options popover it hosts is open), so the
     stack, the Options control and concede are never out of reach. */
  .table.rail-left {
    grid-template-columns: minmax(min(15rem, var(--rail-width)), var(--rail-width)) minmax(0, 1fr);
  }
  .table.rail-left .rail {
    grid-column: 1;
    grid-row: 1;
    border-left: 0;
    border-right: 1px solid var(--edge-inst);
  }
  .table.rail-left .board {
    grid-column: 2;
    grid-row: 1;
  }
  .table.rail-hidden {
    grid-template-columns: minmax(0, 1fr);
  }
  .table.rail-hidden .rail {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    z-index: 30;
    width: min(22rem, 85vw);
    transform: translateX(calc(100% - 8px));
    transition: transform 0.15s ease-out;
    box-shadow: -10px 0 28px rgb(0 0 0 / 0.4);
  }
  .table.rail-hidden .rail:hover,
  .table.rail-hidden .rail:focus-within,
  .table.rail-hidden.rail-peek .rail {
    transform: none;
  }
  .board {
    position: relative;
    overflow: hidden;
    min-width: 0;
    min-height: 0;
  }

  .rail {
    position: relative;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--instrument);
    border-left: 1px solid var(--edge-inst);
    overflow: visible;
    color: var(--ink-inst);
  }
  .prompt-dock:empty {
    display: none;
  }
  .prompt-dock.bottom { flex: 0 0 auto; max-height: 45%; overflow: auto; border-top: 1px solid var(--edge-inst); }
  .rail-main {
    flex: 1 1 0;
    min-height: 0;
  }
  /* Concede lives INSIDE the logbar row (passed to Rail as a snippet), at
     the row's right edge in normal flex flow — the row is its anchor, so it
     cannot paint over the seat table below the row (fb-53bd45b9: the old
     top: 3rem absolute anchor landed on seat row 0 and its z-index ate the
     row's pile-button clicks), and it cannot read as unanchored. The
     .logbar__extra wrapper in Rail pushes this to the right; nothing here
     is absolutely positioned. The control's markup and styles live in
     ConcedeControl.svelte so the geometry fixture renders the real thing
     (SeatTable.svelte.test.ts measures it there). */

  .transcript {
    flex: 0 0 38%;
    min-height: 0;
    background: var(--instrument);
    border-top: 1px solid var(--edge-inst);
    display: flex;
    flex-direction: column;
    font-family: var(--font-data);
    font-size: var(--t-12);
    overflow: hidden;
  }
  .transcript.hidden {
    display: none;
  }
  .log {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
  /* A halted table is the one thing that must interrupt: it is the only
     element in the client allowed to sit over the board. */
  .halted {
    position: absolute;
    inset: 0 auto auto 0;
    background: var(--danger);
    color: var(--felt-sunk);
    font-weight: 600;
    padding: var(--sp-2) var(--sp-4);
    z-index: 10;
  }
  .idle-inline {
    grid-column: 1 / -1;
    grid-row: 1 / -1;
    overflow-y: auto;
  }
  .waiting {
    padding: var(--sp-8);
    color: var(--ink-dim);
  }
  .load-error {
    padding: var(--sp-8);
  }
  .load-error a {
    color: var(--mana-u);
  }
  /* The restart POST's error, shown inline in the rail's logbar row beside
     the control that raised it (a 404 when CreateGame is not armed). */
  .restart-error {
    margin-left: var(--sp-2);
    color: var(--danger);
    font-size: var(--t-12);
  }

  /* The Options control owns its panel. Keeping its containing block here
     means the editor stays attached to the button if the rail changes size;
     bottom: 100% makes it grow upward into the board instead of covering the
     rail controls below it. */
  .rail-options {
    position: relative;
    z-index: 31;
  }
  .rail-options__button {
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    font-weight: 600;
    cursor: pointer;
  }
  .rail-options__button {
    padding: var(--sp-1) var(--sp-3);
  }
  .rail-options__button:hover,
  .rail-options__button[aria-expanded='true'] {
    border-color: var(--ink-dim);
    color: var(--ink);
  }
  /* The Options control sits at the TOP of the rail, so the popover drops
     down from it; anchored with bottom it opened above the viewport. */
  .rail-options__popover {
    position: absolute;
    right: 0;
    top: calc(100% + var(--sp-2));
    width: min(25rem, calc(100vw - var(--sp-4)));
    max-height: min(38rem, calc(100vh - 5rem));
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument);
    box-shadow: 0 0.75rem 2rem rgb(0 0 0 / 45%);
    overflow: hidden;
  }
  .rail-options__body {
    overflow: auto;
    overscroll-behavior: contain;
  }
</style>
