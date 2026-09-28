<script lang="ts">
  import type { DvrState } from '../lib/dvr';
  import { visibleLog } from '../lib/logfilter';
  import type { LogSeatIdentity } from '../lib/logcolour';
  import { parseLogLine, type CardOwnerColour } from '../lib/logrender';
  import type { AutoPassLog } from '../lib/autolog';
  import type { CardView } from '../protocol';
  import { cardById } from '../lib/board';
  import { CardHover } from '../lib/carddetail.svelte';
  import CardDetail from './CardDetail.svelte';
  import ManaSymbols from './ManaSymbols.svelte';

  /**
   * Transcript is the rules log: one line per event, the cursor's line
   * highlighted and scrolled into view, clicking a line scrubs to it. Lines
   * with no text (state-only events) are skipped. The three engine-noise
   * kinds (priority, decision_ask, decision_made) are hidden by default so
   * land plays, casts and triggers surface; the toggle reveals them. Step
   * lines ("Step: main-1") are hidden by default behind their own toggle,
   * independent of the noise toggle.
   *
   * Task 3 ("each log should note the player name in colour"): `identities`
   * is the caller's already-resolved seat name/colour list (Table.svelte
   * builds it the same way SeatTable does, off the same view+seats), and
   * every line is run through logcolour.ts's colourSegments so a seat's own
   * name is coloured wherever it reads as that PLAYER rather than as part of
   * a card's name — see logcolour.ts for how the two are told apart.
   * Optional so a caller with no seats yet (or every existing test) renders
   * every line as plain text, unchanged.
   *
   * Readable log (ui9). Beyond that task-3 colouring, each line is also run
   * through logrender.ts, which re-renders the server-described line from
   * its own structure rather than as prose:
   *   - mana symbols (`{R}{G}{W/U}`) are drawn as pips (ManaSymbols),
   *   - a card/permanent/spell reference `<Name> #<id>` is coloured by the
   *     colour of the seat that OWNS it (lc1) with its `#<id>` suppressed
   *     from the visible text and kept as a hover title (B3) — the id is
   *     load-bearing when two copies of the same card are in play, so it is
   *     reachable, not dropped;
   *   - a faceless ability reference (`<Source>'s ability #id`, or the
   *     `an ability #id` fallback) gets an italic ability treatment (lc1,
   *     B2);
   * `cardColour` is the caller's object-id -> owner-seat-colour resolver,
   * built from the current view's cards (buildCardOwnerColour) with the same
   * seat palette that colours each seat's name. It also carries the view's
   * exact card-name keys, so logrender.ts resolves a card reference by
   * longest exact match against them rather than by word shape (a comma in
   * `Jace, the Mind Sculptor` cannot split the name). It is optional, so a
   * caller with no cards (or a test) renders card names uncoloured by the
   * word-shape fallback.
   *
   * Card-name hover preview (fb-20260927T154603Z). `cards` is the SAME
   * flattened view list `cardColour` is built over — every visible zone, via
   * everyVisibleCard. A card-name span resolves its `#<id>` against it with
   * cardById and, when an object is found, becomes a trigger for the shared
   * detail panel: 250 ms pointer dwell (or keyboard focus) opens
   * CardDetail, pointer leave / blur / Escape close it. One CardHover drives
   * ALL the line triggers against ONE panel, exactly the surface CardHover's
   * own doc names; the panel renders at this container level, never inside a
   * row `<button>`.
   *
   * An id that resolves to no visible object (destroyed permanent, gone
   * token, exiled card) or the redacted id 0 ("a card") opens nothing: the
   * span keeps its `title` tooltip and degrades silently — never an invented
   * card. The panel describes the object's CURRENT live-view state even when
   * the DVR is scrubbed to an older line, because the client keeps no
   * historical views and the object id is the only handle shared by a line
   * and the view. Both are accepted approximations, recorded here rather
   * than in AGENTS.md's frozen table.
   *
   * `hover` is injectable for the repo's SSR harness (no DOM, no pointer
   * events), exactly as CardTile's is: production never passes it and the
   * default is the one CardHover this component owns.
   */
  let { dvr, onSeek, identities = [], cardColour = null, notes = [], cards = [], hover = new CardHover() }: {
    dvr: DvrState;
    onSeek: (seq: number) => void;
    identities?: LogSeatIdentity[];
    cardColour?: CardOwnerColour | null;
    /**
     * notes is the seat's own client-local auto-pass log (prio5), rendered
     * AFTER the engine's lines. These are not events: nothing here scrubs
     * or joins the DVR cursor, and they exist only in the browser that made
     * the passes (settings.logAutoPasses). Empty for a spectator.
     */
    notes?: AutoPassLog[];
    /** cards is the current view's flattened card list (everyVisibleCard). */
    cards?: CardView[];
    hover?: CardHover;
  } = $props();

  let container: HTMLDivElement | undefined;
  let revealAll = $state(false);
  let revealSteps = $state(false);
  let openSnapshots = $state<Record<number, boolean>>({});
  const lines = $derived(visibleLog(dvr.events, revealAll, revealSteps));

  // The panel's lifetime follows the objects the transcript currently shows:
  // a card that leaves every visible zone must close a panel opened for it,
  // even though no pointer event will fire (the span may be gone). This is
  // the lifecycle half of CardHover's contract; the rendering half does not
  // apply because a log line is keyed by event seq and never re-pointed at a
  // different card.
  $effect(() => {
    hover.supervise(cards);
  });

  // Escape closes a pointer-opened panel even when focus never entered the
  // card-name span: a pointer dwell does not move focus, so a keydown on the
  // span alone would miss it. The listener exists only while the panel is
  // open, so it cannot steal Escape from any other surface. (The span's own
  // onkeydown still serves the keyboard-focus path.)
  $effect(() => {
    if (!hover.hover.show) return;
    const onKey = (e: KeyboardEvent) => hover.keydown(e);
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  const resolveCard = (id: string): CardView | null => cardById(cards, id);

  $effect(() => {
    // The cursor is always a valid DVR target, but if it landed on a hidden
    // line there is no rendered row to bring into view, so the log simply
    // stays put; revealing all brings that row back and the scroll resumes.
    const seq = dvr.cursor;
    container?.querySelector<HTMLElement>(`[data-seq="${seq}"]`)?.scrollIntoView({ block: 'nearest' });
  });
</script>

<div class="transcript" bind:this={container}>
  <div class="bar">
    <button
      type="button"
      class="toggle"
      aria-pressed={revealSteps}
      title='Phase/step lines ("Step: main-1") are clock noise; they are hidden unless this is on. Independent of the engine-noise toggle.'
      onclick={() => (revealSteps = !revealSteps)}
    >
      {revealSteps ? 'Hide step lines' : 'Show step lines'}
    </button>
    <button
      type="button"
      class="toggle"
      aria-pressed={revealAll}
      title="Priority, decision asks and decision answers are engine bookkeeping; they are hidden unless this is on."
      onclick={() => (revealAll = !revealAll)}
    >
      {revealAll ? 'Hide engine noise' : 'Show engine noise'}
    </button>
  </div>
  {#each lines as e (e.event.seq)}
    <button
      type="button"
      class="line"
      class:current={e.event.seq === dvr.cursor}
      data-seq={e.event.seq}
      onclick={() => onSeek(e.event.seq)}
    >
      <span class="seq">{e.event.seq}</span>
      <span class="text">{#each parseLogLine(e.line, { identities, cardColour }) as p, i (i)}
        {#if p.kind === 'text'}{p.text}{:else if p.kind === 'mana'}<ManaSymbols cost={p.token} />{:else if p.kind === 'seat'}<span class="who" style:color={p.colour}>{p.text}</span>{:else if p.kind === 'card'}
          {@const c = resolveCard(p.id)}
          {#if c}<span
            class="obj card"
            style:color={p.colour ?? undefined}
            title="{p.name} #{p.id}"
            role="button"
            tabindex="0"
            aria-describedby={hover.hover.show && hover.card?.id === c.id ? `card-detail-${c.id}` : undefined}
            onpointerenter={(ev) => hover.arm(c, ev.currentTarget)}
            onpointerleave={() => hover.leave(c)}
            onpointerdown={(ev) => hover.pointerdown(c, ev.currentTarget)}
            onpointerup={() => hover.pointerup(c)}
            onfocus={(ev) => hover.open(c, ev.currentTarget)}
            onblur={() => hover.blur(c)}
            onkeydown={(ev) => hover.keydown(ev)}
          >{p.name}</span>{:else}<span class="obj card" style:color={p.colour ?? undefined} title="{p.name} #{p.id}">{p.name}</span>{/if}
        {:else if p.kind === 'ability'}<span class="obj ability" title="{p.name} #{p.id}">{p.name}</span>{/if}
      {/each}</span>
    </button>
  {/each}
  {#if notes.length > 0}
    {#each notes as n (n.id)}
      <div class="line local" data-auto-log>
        <span class="seq">auto</span>
        <span class="text">{n.text}</span>
        {#if n.diagnostics}
          {@const diagnostic = n.diagnostics}
          <details class="auto-detail">
            <summary>Advanced pass details</summary>
            <dl>
              <dt>Verdict</dt><dd>{diagnostic.verdict}</dd>
              <dt>Option kinds</dt><dd>{diagnostic.optionKinds.join(', ') || 'none'}</dd>
              <dt>Actionable options</dt><dd>{diagnostic.actionableOptions.join(', ') || 'none'}</dd>
              <dt>Castable after tapping</dt><dd>{diagnostic.castableAfterTap.join(', ') || 'none'}</dd>
              <dt>Respondable option / after tapping</dt><dd>{diagnostic.respondableOption} / {diagnostic.respondableAfterTap}</dd>
              <dt>Yield matched</dt><dd>{diagnostic.yieldsHit}</dd>
            </dl>
            <details bind:open={openSnapshots[n.id]}>
              <summary>Seat view snapshot</summary>
              {#if openSnapshots[n.id]}
                {@const snapshot = JSON.stringify(diagnostic.view, null, 2)}
                <button type="button" onclick={() => void navigator.clipboard?.writeText(snapshot)}>Copy JSON</button>
                <a href="data:application/json;charset=utf-8,{encodeURIComponent(snapshot)}" download="autopass-view.json">Export JSON</a>
                <pre>{snapshot}</pre>
              {/if}
            </details>
          </details>
        {/if}
      </div>
    {/each}
  {/if}
  <!-- ONE panel for the whole transcript, driven by the one CardHover every
       card-name trigger arms. It lives at the container level, after the row
       list, because each row is a <button> (the scrub/seek target) and a
       <div> inside a <button> is invalid HTML. CardDetail portals to <body>
       on mount, so the panel's own fixed coordinates resolve against the
       viewport regardless of which row opened it. -->
  {#if hover.hover.show && hover.card && hover.anchor}
    <CardDetail card={hover.card} anchor={hover.anchor} />
  {/if}
</div>

<style>
  .transcript {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  /* The bar used to carry a sentence explaining the default. A permanent
     sentence is a permanent line of a 160px band whose whole job is log
     lines, and the button already says what it does; the explanation moved
     to the button's own title, where it is one hover away and costs nothing
     when it is not wanted. Two toggles sit here now: the engine-noise one
     and the step-lines one, independent. */
  .bar {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--sp-3);
    padding: var(--sp-1) var(--sp-3);
    border-bottom: 1px solid var(--edge-inst);
    font-size: .72rem;
    flex: none;
  }
  .toggle {
    background: none;
    border: 1px solid var(--edge-inst);
    border-radius: 4px;
    color: var(--ink-inst);
    font: inherit;
    padding: 2px var(--sp-3);
    cursor: pointer;
    flex: none;
  }
  .toggle:hover {
    border-color: var(--ink-dim);
    color: var(--ink);
  }
  .line {
    display: flex;
    gap: var(--sp-3);
    text-align: left;
    background: none;
    border: none;
    color: var(--ink-dim);
    font: inherit;
    padding: 1px var(--sp-3);
    cursor: pointer;
    width: 100%;
  }
  .line:hover {
    color: var(--ink-inst);
  }
  /* The cursor line is where the DVR is pointing: marked by a rule in the
     initiative colour, the same device the pending tray uses, rather than by
     a filled band that would fight the log's density. */
  .line.current {
    color: var(--ink);
    background: var(--instrument-raised);
    box-shadow: inset 2px 0 0 var(--initiative);
  }
  .seq {
    color: var(--ink-faint);
    font-variant-numeric: tabular-nums;
    width: 3.5em;
    text-align: right;
    flex: none;
  }
  .text {
    overflow-wrap: anywhere;
  }
  /* The seat's own identity colour (Task 3), inline with the sentence rather
     than a separate column — the same "learned once" register the rest of
     the rail uses colour for. */
  .who {
    font-weight: 600;
  }
  /* A card name (lc1/B3) is coloured by its owner's seat colour — the same
     colour that seat's name renders in — and the "#<id>" is off the visible
     text and on the hover title instead. An id that resolves to a visible
     object also opens the detail panel (fb-20260927T154603Z), so the span is
     keyboard-focusable and says so on focus; the underline is the same
     affordance the rest of the client uses for "there is more here". */
  .obj.card {
    font-weight: 600;
  }
  .obj.card[tabindex]:hover,
  .obj.card[tabindex]:focus {
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .obj.card[tabindex]:focus-visible {
    outline: 1px solid var(--ink-dim);
    outline-offset: 1px;
  }
  /* An ability reference (the source-named "<Source>'s ability #id" shape)
     gets its own treatment — italic, not the card weight — because on the
     stack an ability is not a card and should not read as one. */
  .obj.ability {
    font-style: italic;
    color: var(--ink-inst);
  }
  /* The client-local auto-pass notes (prio5): not events, so no scrub and
     no cursor — a dim, quiet register under the engine's own lines, with
     the "auto" marker where an event's seq would sit. */
  .line.local {
    color: var(--ink-faint);
    font-style: italic;
    cursor: default;
  }
  .line.local .seq {
    font-size: .64rem;
  }
</style>
