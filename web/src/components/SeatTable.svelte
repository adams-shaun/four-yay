<script lang="ts">
  import type { CardView, PlayerView, SeatInfo, View } from '../protocol';
  import { seatColour } from '../lib/colours';
  import type { CardOptions, OptionTone } from '../lib/cardoptions';
  import { pileTone } from '../lib/cardoptions';
  import { pileCards, pileLabel, pileOpener, type PileZone } from '../lib/pileopener.svelte';
  import { pointerRelease } from '../lib/pointer';
  import { lossCauses, seatRows, stateLabel, type SeatState } from '../lib/seattable';

  /**
   * SeatTable is the rail's single per-seat summary. Each row preserves the
   * established seat-colour, active, priority and loss language while putting
   * all five public counts in one compact icon strip. Hand, graveyard and
   * exile become buttons only when this view actually carries cards for them;
   * their lists open in the table's ONE shared PileModal (lib/pileopener +
   * PileHost, mounted by Table.svelte — the identity bar's pile icons open
   * the same instance, so a pile cannot open twice at once) rather than
   * expanding the rail. All four zone counts (hand, library, graveyard,
   * exile) sit on ONE line beside the life pill (fb-20260917T232028Z — the
   * player asked for a single row per seat instead of the old two-high
   * stack), so a seat box is one text-line tall and the name — not a count —
   * is what flexes when the rail is narrow.
   *
   * The rows are a `ul`/`li` list, not the old semantic `<table>`: a one-line
   * summary carries no column structure to read out, and every count keeps
   * its own aria-label, so the deliberate trade is row/cell semantics for
   * one compact line. A lost seat's cause line WRAPS (white-space: normal,
   * back to the pre-one-line table's behaviour) inside the name box — it is
   * the one element on the row that may take a second text line, and only
   * for seats that are already out of the game.
   *
   * The pile buttons wear the same tone ring the identity bar's pile icons
   * wear (fb-20260916T225802Z): when the pending decision offers something
   * to a card in the pile (pileTone over the table's card-options bundle,
   * the same data and the same rule — not gated by pile owner). options is
   * optional and null by default so every existing caller renders exactly
   * as before.
   */
  let { view, seats = [], focus = null, onFocus, events = [], options = null }: {
    view: View;
    seats?: SeatInfo[];
    focus?: number | null;
    onFocus: (seat: number) => void;
    events?: { event: { kind: string; player: number; text?: string } }[];
    options?: CardOptions | null;
  } = $props();

  const rows = $derived(seatRows(view, seats, lossCauses(events)));
  function describe(name: string, deck: string | null, state: SeatState, lostReason: string | null): string {
    const parts = [name];
    if (deck) parts.push(deck);
    if (state === 'lost') parts.push(lostReason ? `eliminated — ${lostReason}` : 'eliminated');
    else {
      const word = stateLabel(state);
      if (word) parts.push(word);
    }
    return parts.join(' — ');
  }

  function cardsFor(player: PlayerView, zone: PileZone): CardView[] {
    return pileCards(player, zone);
  }

  function showPile(seat: number, zone: PileZone, event: MouseEvent): void {
    pileOpener.open(seat, zone, event.currentTarget as HTMLElement, event.detail > 0);
  }

  /** pileToneOf is this row's tone ring for one zone pile, off the shared
   *  card-options bundle (null for a spectator / nothing pending → idle). */
  function pileToneOf(player: PlayerView, zone: PileZone): OptionTone | undefined {
    const tone = pileTone(options, cardsFor(player, zone));
    return tone === 'idle' ? undefined : tone;
  }
</script>

<section class="seats" data-seat-table aria-label="Seats: life, hand, library, graveyard and exile">
  <ul>
    {#each rows as r (r.seat)}
      {@const player = view.players.find((p) => p.seat === r.seat)}
      {@const handCards = player ? cardsFor(player, 'hand') : []}
      {@const graveyardCards = player ? cardsFor(player, 'graveyard') : []}
      {@const exileCards = player ? cardsFor(player, 'exile') : []}
      <li
        data-seat-row={r.seat}
        data-state={r.state}
        class:lost={r.lost}
        class:active={r.active}
        class:selected={focus === r.seat}
        style={`--seat:${r.colour || seatColour(r.seat, seats)}`}
      >
        <div class="who">
          <button
            type="button"
            class="pick"
            aria-pressed={focus === r.seat}
            title={`${describe(r.name, r.deck, r.state, r.lostReason)} — press to focus this seat`}
            aria-label={describe(r.name, r.deck, r.state, r.lostReason)}
            use:pointerRelease
            onclick={() => onFocus(r.seat)}
          >
            <span class="name" class:priority={r.priority}>{r.name}</span>
          </button>
          {#if r.lost}
            <p class="eliminated" data-eliminated>
              <span class="eliminated__tag">Eliminated</span>{#if r.lostReason}<span class="eliminated__cause"> — {r.lostReason}</span>{/if}
            </p>
          {/if}
        </div>

        <span data-stat="life" aria-label={`Life: ${r.life}`}>
          <span class="stat life pill">
            <svg data-icon="heart" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 14S2 10.2 2 5.6C2 2.4 6 1.4 8 4c2-2.6 6-1.6 6 1.6C14 10.2 8 14 8 14Z"/></svg>
            <span>{r.life}</span>
          </span>
        </span>
        <!-- All four zone counts on ONE line (hand, library, graveyard,
             exile) so a seat row is one text-line tall. The counts never
             flex: the name box takes the slack and ellipsizes, so the
             rail's floor is the counts' content. -->
        <div class="zone-line">
          <span data-stat="hand" data-hand-hidden={r.handVisible ? undefined : ''} aria-label={`Hand: ${r.hand}`}>
            {#if r.hand > 0 && handCards.length > 0}
              <button type="button" class="pile pill" data-pile="hand" data-tone={player ? pileToneOf(player, 'hand') : undefined} aria-label={pileLabel(r.name, 'hand', r.hand)} use:pointerRelease onclick={(e) => showPile(r.seat, 'hand', e)}>
                <svg data-icon="hand" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 8V4.5a1 1 0 0 1 2 0V7 3.5a1 1 0 0 1 2 0V7 3a1 1 0 0 1 2 0v4-3a1 1 0 0 1 2 0v4.2l.7-.7a1.2 1.2 0 0 1 1.7 1.7L11 12.6A4 4 0 0 1 8 14H7a4 4 0 0 1-4-4V8Z"/></svg>
                <span>{r.hand}</span><svg class="caret" viewBox="0 0 8 12" aria-hidden="true"><path d="m2 2 4 4-4 4"/></svg>
              </button>
            {:else}
              <span class="count pill"><svg data-icon="hand" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 8V4.5a1 1 0 0 1 2 0V7 3.5a1 1 0 0 1 2 0V7 3a1 1 0 0 1 2 0v4-3a1 1 0 0 1 2 0v4.2l.7-.7a1.2 1.2 0 0 1 1.7 1.7L11 12.6A4 4 0 0 1 8 14H7a4 4 0 0 1-4-4V8Z"/></svg><span>{r.hand}</span></span>
            {/if}
          </span>
          <span data-stat="library" aria-label={`Library: ${r.library}`}>
            <span class="count pill"><svg data-icon="book" viewBox="0 0 16 16" aria-hidden="true"><path d="M2 3.2C4 2.6 6 3 8 4v9c-2-1-4-1.4-6-.8v-9Zm12 0c-2-.6-4-.2-6 .8v9c2-1 4-1.4 6-.8v-9Z"/></svg><span>{r.library}</span></span>
          </span>
          <span data-stat="graveyard" aria-label={`Graveyard: ${r.graveyard}`}>
            {#if r.graveyard > 0 && graveyardCards.length > 0}
              <button type="button" class="pile pill" data-pile="graveyard" data-tone={player ? pileToneOf(player, 'graveyard') : undefined} aria-label={pileLabel(r.name, 'graveyard', r.graveyard)} use:pointerRelease onclick={(e) => showPile(r.seat, 'graveyard', e)}>
                <svg data-icon="skull" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 7a5 5 0 1 1 10 0c0 2-1 3-2 3.8V14H5v-3.2C4 10 3 9 3 7Zm3-1.5a1 1 0 1 0 0 2 1 1 0 0 0 0-2Zm4 0a1 1 0 1 0 0 2 1 1 0 0 0 0-2ZM7 9l1-1 1 1-1 1-1-1Z"/></svg>
                <span>{r.graveyard}</span><svg class="caret" viewBox="0 0 8 12" aria-hidden="true"><path d="m2 2 4 4-4 4"/></svg>
              </button>
            {:else}
              <span class="count pill"><svg data-icon="skull" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 7a5 5 0 1 1 10 0c0 2-1 3-2 3.8V14H5v-3.2C4 10 3 9 3 7Zm3-1.5a1 1 0 1 0 0 2 1 1 0 0 0 0-2Zm4 0a1 1 0 1 0 0 2 1 1 0 0 0 0-2ZM7 9l1-1 1 1-1 1-1-1Z"/></svg><span>{r.graveyard}</span></span>
            {/if}
          </span>
          <span data-stat="exile" aria-label={`Exile: ${r.exile}`}>
            {#if r.exile > 0 && exileCards.length > 0}
              <button type="button" class="pile pill" data-pile="exile" data-tone={player ? pileToneOf(player, 'exile') : undefined} aria-label={pileLabel(r.name, 'exile', r.exile)} use:pointerRelease onclick={(e) => showPile(r.seat, 'exile', e)}>
                <svg data-icon="exile" viewBox="0 0 16 16" aria-hidden="true"><path d="m3 3 10 10M13 3 3 13"/></svg>
                <span>{r.exile}</span><svg class="caret" viewBox="0 0 8 12" aria-hidden="true"><path d="m2 2 4 4-4 4"/></svg>
              </button>
            {:else}
              <span class="count pill"><svg data-icon="exile" viewBox="0 0 16 16" aria-hidden="true"><path d="m3 3 10 10M13 3 3 13"/></svg><span>{r.exile}</span></span>
            {/if}
          </span>
        </div>
      </li>
    {/each}
  </ul>
</section>

<style>
  /* One flex row per seat: the name box is the row's ONLY flexible box
     (min-width 0, so its nowrap text contributes nothing to the row's
     min-content width and the ellipsis does the truncation work at any rail
     width); the life pill and the four-count line never flex. The old table
     with its fixed 2.45rem life band and 5.1rem two-column zones cell is
     gone with the two-high stack (fb-20260917T232028Z) — a seat row is one
     text-line tall and the rail's floor is the counts' content. */
  ul { margin: 0; padding: 0; list-style: none; }
  li[data-seat-row] { display: flex; align-items: center; padding: 1px 0; color: var(--ink-inst); white-space: nowrap; }
  .who { flex: 1 1 auto; min-width: 0; overflow: hidden; }
  .pick { display: flex; align-items: center; width: 100%; min-width: 0; background: none; border: 0; border-left: 3px solid var(--seat); padding: 1px var(--sp-2); font-size: var(--t-12); line-height: 1.6; color: var(--ink-inst); text-align: left; cursor: pointer; }
  li.active .pick { border-left-width: 6px; padding-left: calc(var(--sp-2) - 3px); }
  li.selected .pick, .pick:hover { background: var(--instrument-raised); }
  li.selected .pick { color: var(--ink); }
  .name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .name.priority { color: var(--initiative); text-decoration: underline 2px dotted; text-underline-offset: 0.16em; }
  [data-stat='life'] { flex: none; }
  .zone-line { display: flex; justify-content: flex-end; gap: 0.18rem; flex: none; }
  .stat, .count, .pile { display: flex; align-items: center; justify-content: flex-end; gap: 0.12rem; font-family: var(--font-data); font-size: var(--t-10); font-variant-numeric: tabular-nums; color: var(--ink-dim); }
  .life { font-weight: 600; color: var(--ink-inst); }
  .stat svg, .count svg, .pile svg { width: 0.58rem; height: 0.58rem; flex: none; fill: currentColor; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
  .caret { width: 0.36rem; fill: none; }
  .pile { width: 100%; background: none; cursor: pointer; }
  /* The count itself is the pill in both the button and non-button paths;
     its icon and number stay together, without changing pile actionability. */
  .pill { box-sizing: border-box; min-width: 12px; height: 12px; padding: 0 2px; border: 1px solid var(--ink-dim); border-radius: 999px; line-height: 1; }
  .pile:hover { color: var(--ink-inst); }
  /* The perimeter tone ring (fb-20260916T225802Z), the card-tile register:
     a pending decision offering something to a card in this pile. */
  .pile[data-tone='initiative'] {
    box-shadow: 0 0 0 2px var(--initiative);
  }
  .pile[data-tone='offered'] {
    box-shadow: 0 0 0 2px var(--offered);
  }
  [data-hand-hidden] { color: var(--ink-faint); }
  li.lost .name, li.lost .life { text-decoration: line-through; color: var(--ink-faint); }
  /* The row is nowrap for the name/counts; the lost-cause line must WRAP
     inside the name box instead (fb-20260917T232028Z round-2 finding): the
     longest real cause ("commander damage (21 or more from one commander)",
     lib/seattable.ts) painted past the counts and the rail at the floor
     while inheriting nowrap — the pre-one-line table wrapped it, so wrap it
     again. .who's overflow: hidden is the second guard for a long word. */
  .eliminated { margin: 1px 0 0; padding-left: var(--sp-2); font-size: var(--t-10); color: var(--danger); line-height: 1.3; white-space: normal; }
  .eliminated__tag { font-weight: 700; text-transform: uppercase; letter-spacing: 0.02em; }
  .eliminated__cause { color: color-mix(in srgb, var(--danger) 82%, var(--ink-inst)); }
</style>
