<script lang="ts">
  import { onMount } from 'svelte';
  import { session } from '../lib/session.svelte';
  import { tables } from '../lib/tables.svelte';
  import { lastNotableByTable, pushFeed, type FeedLine } from '../lib/feed';
  import { gridMetrics, sectionTables } from '../lib/lobby';
  import TableCell from '../components/TableCell.svelte';
  import Feed from '../components/Feed.svelte';
  import PlayVsBot from '../components/PlayVsBot.svelte';
  import DeckLeaderboard from '../components/DeckLeaderboard.svelte';
  import type { Widget } from '../protocol';

  let feed = $state<FeedLine[]>([]);
  const live = $derived(tables.list.filter((t) => t.info.state === 'live').length);
  // Sections are Commander first, then constructed (lib/lobby.ts); a server
  // running one format gets one section and no heading at all.
  const sections = $derived(sectionTables(tables.list));
  const metrics = $derived(gridMetrics(tables.list.length));
  // The rail's rows follow the grid's order, so a table is in the same
  // relative position in both.
  const order = $derived(sections.flatMap((s) => s.tables.map((t) => t.info.id)));
  // "What last happened here", per table, for the roomy cells.
  const notes = $derived(lastNotableByTable(feed));
  onMount(() => {
    session.ensureOverview();
    tables.load().catch((err: unknown) => console.error('overview: failed to load tables', err));
    return session.stream.onFrame((f) => {
      if (f.t === 'widget' && f.table) feed = pushFeed(feed, { table: f.table, match: f.match ?? 0, seq: f.seq, line: (f.body as Widget).last });
    });
  });
</script>

<main class="overview">
  <div
    class="tables"
    style:--cell-min={metrics.cellMin}
    style:--row-min={metrics.rowMin}
    style:--cols={metrics.cols}
    style:--life-size={metrics.lifeSize}
  >
    <header class="masthead">
      <h1>gorge</h1>
      <p class="what">
        {live} of {tables.list.length}
        {tables.list.length === 1 ? 'table' : 'tables'} playing
      </p>
    </header>
    <!-- Task ui11: sit down 1v1 against a bot, alongside the spectator lobby
         — not replacing it. The panel POSTs /api/games and follows the join
         path. The server decides whether the flow is enabled; a server that
         did not arm -vsbot answers 404 and the panel renders it. -->
    <div class="playstrip"><PlayVsBot /></div>
    <div class="playstrip"><DeckLeaderboard /></div>
    <div class="sections">
      {#each sections as s (s.format)}
        <section class="section" style:--grow={s.tables.length}>
          {#if s.heading}
            <h2 class="head">
              <span class="title">{s.title}</span>
              <span class="count">{s.live} of {s.tables.length} playing</span>
            </h2>
          {/if}
          <div class="grid">
            {#each s.tables as t (t.info.id)}
              <TableCell table={t} roomy={metrics.roomy} note={notes.get(t.info.id) ?? ''} />
            {/each}
          </div>
        </section>
      {/each}
    </div>
  </div>
  <aside class="rail"><Feed lines={feed} {order} /></aside>
</main>

<style>
  /*
   * The grid IS the hero: eight games of Magic running at once with their life
   * totals moving. The masthead states what you are looking at and then gets
   * out of the way — a large marketing header above a live board would be
   * decoration competing with the actual subject.
   *
   * The composition rule that follows from that (Task L2): the cells fill the
   * page. A four-table server used to draw four ~18rem chips across the top
   * and leave three quarters of the viewport empty beneath them, which read as
   * a page waiting for content it did not have. It has the content; it was
   * drawing it at a size chosen for a much busier server. So the grid takes
   * the height, the cells grow into it (bounded by lobby.ts's steps, which cap
   * the columns so four tables land as a 2x2 rather than a 4-wide row), and
   * the seat names appear once a cell is roomy enough to hold them. As tables
   * multiply the same page steps back down to the compact state widget the UI
   * survey argues for, and starts scrolling instead of stretching.
   */
  .overview {
    display: grid;
    grid-template-columns: 1fr minmax(18rem, 22rem);
    height: 100vh;
    background: var(--felt);
  }
  .tables {
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow-y: auto;
  }
  .masthead {
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
    padding: var(--sp-6) var(--sp-4) var(--sp-4);
    flex: none;
  }
  h1 {
    margin: 0;
    font-size: var(--t-20);
    font-weight: 600;
    letter-spacing: -0.02em;
  }
  .what {
    margin: 0;
    font-family: var(--font-data);
    font-size: var(--t-12);
    color: var(--ink-dim);
  }
  .sections {
    display: flex;
    flex-direction: column;
    gap: var(--sp-6);
    padding: 0 var(--sp-4) var(--sp-4);
    /* Sections share what the masthead leaves, so the last row of cells ends
       at the bottom of the page instead of halfway up it. */
    flex: 1;
    min-height: 0;
  }
  /* The play-vs-bot card sits between the masthead and the grid: a compact
     action, not part of the grid or the rail, so the lobby below it is
     untouched. It is flex:none — it never steals the grid's share of the
     height the way a growing footnote would (Task L2's composition rule). */
  .playstrip {
    flex: none;
    padding: 0 var(--sp-4) var(--sp-2);
  }
  /* Spare height is shared between sections in proportion to how many tables
     each holds, so a one-table Commander section beside a five-table
     constructed one does not end up with the taller cells. */
  .section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    flex: var(--grow, 1) 1 auto;
  }
  /*
   * A section heading is a hairline and two words: the format, and its own
   * live count. It divides; it does not announce. (The masthead keeps the
   * page total — one authority per fact, so the two counters can never
   * disagree the way Forge's stack tab and status bar do.)
   */
  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--sp-3);
    margin: 0;
    padding-bottom: var(--sp-2);
    border-bottom: 1px solid var(--edge-felt);
    flex: none;
  }
  .title {
    font-size: var(--t-14);
    font-weight: 600;
    color: var(--ink);
  }
  .count {
    font-family: var(--font-data);
    font-size: var(--t-11);
    color: var(--ink-dim);
  }
  /*
   * `--cols` is a maximum, not a fixed count: the max() floor keeps a track
   * from shrinking below --cell-min, so a narrow window still collapses to
   * fewer columns, but a wide one stops at --cols instead of stringing every
   * table out in one row.
   */
  .grid {
    display: grid;
    grid-template-columns: repeat(
      auto-fill,
      minmax(max(var(--cell-min), calc((100% - (var(--cols) - 1) * var(--sp-3)) / var(--cols))), 1fr)
    );
    /*
     * `auto` as the maximum, not `1fr`: a row stretches into free space the
     * same way, but it can also grow past --row-min when the cell's own
     * content needs more. With `1fr` a short viewport squeezed the row below
     * the cell's min-content height and the footer printed on top of the life
     * grid — a cell must never be drawn smaller than it is.
     */
    grid-auto-rows: minmax(var(--row-min), auto);
    gap: var(--sp-3);
    flex: 1;
  }
  .rail {
    background: var(--instrument);
    border-left: 1px solid var(--edge-inst);
    overflow: hidden;
    color: var(--ink-inst);
  }
</style>
