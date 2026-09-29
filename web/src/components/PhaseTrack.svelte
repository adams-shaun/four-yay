<script lang="ts">
  import type { SeatInfo, View } from '../protocol';
  import { STEPS, type Stops, type TurnSide } from '../lib/autopilot';
  import { PHASE_GROUPS } from '../lib/phases';
  import { seatColour } from '../lib/colours';
  import { pointerRelease } from '../lib/pointer';

  /**
   * PhaseTrack is the board's clock: which round of the table and where in
   * the twelve steps the game is standing right now —
   * plus the seat's stops, set by clicking the step you want to be handed
   * the game back at.
   *
   * It is THIN and stays that way. It used to carry a head row -- whose turn
   * it is, the round number, and a sentence explaining shift-click -- above
   * the cells. That is gone: whose turn it is comes from the shard's own
   * active-seat perimeter and from IdentityBar, and the shift-click modifier
   * is on every stoppable cell's title. The phase captions stay, at 9px, as
   * tick marks on the ruler. Do not reintroduce the head row.
   *
   * It decides nothing about the rules. The step order and the stoppable
   * set come from lib/autopilot via lib/phases (the client has exactly one
   * step vocabulary), the current step is view.step verbatim, and a stop is
   * a callback to the seat's own state — this component never reads or
   * writes storage and never posts.
   *
   * The shard's perimeter says one thing only: the active seat's identity
   * colour. It is full-strength when that seat is yours and recedes when it
   * is not; the current-step fill keeps the existing initiative grammar.
   */
  let { view, seats, seat = null, stops = null, onToggle = null, clock = true }: {
    view: View;
    /** clock draws the leading round segment; the centre strip names the turn itself and turns it off. */
    clock?: boolean;
    seats: SeatInfo[];
    /** seat is the viewer's own seat, or null for a spectator / a finished replay. */
    seat?: number | null;
    /** stops is the viewer's stop set. Null means nobody owns stops here, and every cell is display-only. */
    stops?: Stops | null;
    /** onToggle is called with the step and the turn side the stop belongs to. */
    onToggle?: ((step: string, side: TurnSide) => void) | null;
  } = $props();

  // Only a seated viewer with a stop set can set stops. A spectator and a
  // frozen replay get the same track with nothing focusable on it, rather
  // than buttons that quietly do nothing.
  const settable = $derived(seat !== null && stops !== null && onToggle !== null);
  const yours = $derived(seat !== null && view.active === seat);
  const side = $derived<TurnSide>(yours ? 'yours' : 'opponents');
  const other = $derived<TurnSide>(yours ? 'opponents' : 'yours');
  const activeColour = $derived(seatColour(view.active, seats));
  // STEPS is a const tuple, so its own indexOf only accepts the twelve
  // literals; view.step is whatever the wire said. Widened once here so an
  // unknown step is -1 rather than a type error.
  const ORDER: readonly string[] = STEPS;
  const at = $derived(ORDER.indexOf(view.step));
  // The set on screen is the one a click here would edit: the side whose
  // turn it is. The other side's stops are a different intention and are
  // deliberately not drawn on the same cells.
  const shown = $derived<ReadonlySet<string>>(stops === null ? new Set<string>() : stops[side]);

  /** position tells a cell whether it is behind, on, or ahead of the game. -1 for a step the wire named but STEPS does not know. */
  function position(step: string): 'past' | 'now' | 'future' {
    const i = ORDER.indexOf(step);
    if (at < 0 || i < 0) return 'future';
    if (i < at) return 'past';
    return i === at ? 'now' : 'future';
  }
</script>

<div
  class="phase-track"
  class:yours
  data-phase-track
  style={`--seat:${activeColour}`}
  aria-label="Phase"
>
  <!-- Round survives only as a small leading segment in the cell row. Whose
       turn and the duplicated click instructions are gone: the active-seat
       perimeter/IdentityBar and each stoppable cell already carry them. -->
  {#if clock}
    <div class="clock" data-clock aria-label={`Round ${view.round}`}>
      <span class="tk">R</span><span class="tn">{view.round}</span>
    </div>
  {/if}

  <div class="groups">
    {#each PHASE_GROUPS as g (g.key)}
      <!-- Each group takes width in proportion to the steps it holds, so
           every cell is the same width and the track reads as one twelve-step
           timeline cut into five labelled sections — not five equal boxes,
           one of which happens to contain five steps. -->
      <!-- Group captions used to consume a second line. The hairline group
           divisions and the cells' own step names carry the same structure
           without spending board height on BEGINNING / COMBAT / ENDING. -->
      <div class="group" data-group={g.key} aria-label={g.label} style={`flex-grow:${g.steps.length}`}>
        <div class="cells">
          {#each g.steps as c (c.step)}
            {@const pos = position(c.step)}
            {#if c.stoppable && settable}
              {@const on = shown.has(c.step)}
              <button
                class="cell"
                class:past={pos === 'past'}
                class:now={pos === 'now'}
                type="button"
                data-step={c.step}
                aria-pressed={on}
                aria-current={pos === 'now' ? 'step' : undefined}
                title={`${c.label} — ${on ? 'stop set' : 'no stop'} on ${yours ? 'your' : 'this'} turn. Shift-click for the other side.`}
                use:pointerRelease
                onclick={(e) => onToggle?.(c.step, e.shiftKey ? other : side)}
              >
                <span class="label">{c.label}</span>
                <span class="mark" aria-hidden="true"></span>
              </button>
            {:else}
              <span
                class="cell"
                class:inert={!c.stoppable}
                class:past={pos === 'past'}
                class:now={pos === 'now'}
                data-step={c.step}
                data-inert={!c.stoppable ? '' : undefined}
                aria-current={pos === 'now' ? 'step' : undefined}
              >
                <span class="label">{c.label}</span>
              </span>
            {/if}
          {/each}
        </div>
      </div>
    {/each}
  </div>
</div>

<style>
  /*
   * The instrument register: cool, flat, hairline-divided. The track is a
   * full-width centre band across the felt, not a page-wide band across the rail — the phase groups are separated by rules
   * rather than by gaps, so `beginning`, `combat` and `ending` read as units
   * at a glance and the twelve cells never read as twelve equal boxes.
   */
  .phase-track {
    display: flex;
    width: 100%;
    height: var(--phase-track-row-h);
    min-width: 0;
    color: var(--ink-inst);
    overflow: hidden;
    /* The current step is the one gilt fill on the track: full on your turn,
       receded on someone else's. */
    --now: color-mix(in srgb, var(--gilt) 55%, var(--instrument-raised));
  }
  .phase-track.yours {
    --now: var(--gilt);
  }

  .clock {
    display: flex;
    align-items: baseline;
    justify-content: center;
    gap: 0.3em;
    flex: 0 0 2.5rem;
    padding: var(--sp-1);
    border-right: 1px solid var(--edge-inst);
    min-width: 0;
  }
  .tk {
    font-size: var(--t-10);
    color: var(--ink-faint);
  }
  .tn {
    font-family: var(--font-data);
    font-variant-numeric: tabular-nums;
    font-size: var(--t-12);
    color: var(--ink);
  }

  /* The track scrolls inside itself on a narrow board rather than pushing
     the page sideways. */
  .groups {
    display: flex;
    align-items: stretch;
    flex: 1 1 auto;
    min-width: 0;
    overflow-x: auto;
    scrollbar-width: thin;
  }
  .group {
    display: flex;
    padding: 2px 1px;
    flex: 1 1 0;
    min-width: 0;
  }
  .group:last-child {
    border-right: 0;
  }
  .cells {
    display: flex;
    flex: 1 1 auto;
    min-width: 0;
    gap: 1px;
  }

  .cell {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 1 1 auto;
    min-width: 0;
    padding: 1px var(--sp-1) var(--sp-1);
    background: transparent;
    border: 0;
    border-radius: 4px;
    font-family: var(--font-ui);
    font-size: var(--t-12);
    line-height: 1.25;
    color: var(--ink-dim);
    white-space: nowrap;
  }
  button.cell {
    cursor: pointer;
  }
  button.cell:hover {
    background: color-mix(in srgb, var(--ink) 8%, transparent);
    color: var(--ink);
  }
  /* Passed steps recede; steps still to come are neutral. */
  .cell.past {
    background: transparent;
    color: var(--ink-faint);
  }
  /* The current step is unmistakable: it is the only filled cell on the
     track, and its fill is the state colour. */
  .cell.now {
    background: var(--now);
    color: var(--felt-sunk);
    font-weight: 600;
  }
  .cell.now .label {
    color: var(--felt-sunk);
  }
  /* untap and cleanup grant no priority, so a stop there would mean nothing.
     They are drawn, because the track is the engine's twelve steps and
     hiding two of them would be a lie, but they are visibly inert. */
  .cell.inert {
    background: transparent;
    color: var(--ink-faint);
    opacity: 0.55;
    font-size: 0.6875rem;
  }

  /* Your stops are dotted: one gilt dot under the step's name. */
  .cell[aria-pressed='true'] {
    color: var(--ink);
  }
  .mark {
    position: absolute;
    left: 50%;
    bottom: 4px;
    width: 0;
    height: 0;
    transform: translateX(-50%);
  }
  .cell[aria-pressed='true'] .mark {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--gilt);
  }
  .cell.now[aria-pressed='true'] .mark {
    background: var(--felt-sunk);
  }

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* Four seats on a laptop: the track never wraps or pushes the page sideways. */
  @media (max-width: 60rem) {
    .clock { flex-basis: 2rem; }
    .cell {
      padding-left: var(--sp-1);
      padding-right: var(--sp-1);
    }
  }
</style>
