<script lang="ts">
  /**
   * ManaPool draws the seat box's line-3 mana readout, which the product spec
   * (B1) defines as "the mana pool OR free mana one gains by tapping lands or
   * other effects". Those are two different things and the box has one row, so
   * this renders BOTH, deliberately distinct:
   *
   *   - AVAILABLE (the outlined chips) is what player could produce right now
   *     by tapping untapped permanents' free-to-tap mana abilities. It is
   *     derived from the public battlefield, so it is present on EVERY seat
   *     and EVERY visibility — and it is the content of this row in the
   *     ordinary case, because floating mana only ever exists inside a step.
   *   - POOL (the solid saturated chips) is mana already floating. It is
   *     public (CR 106.4a/106.4b: a mana pool is not one of the seven zones in
   *     CR 400.1 and holds no cards, so CR 400.2's hidden-zone framework has no
   *     purchase on it; instead 106.4a, 106.4b, 117.3d and 118.3a all require a
   *     player to ANNOUNCE what is in their pool). view.PlayerView.Pool is
   *     filled for every seat under every visibility, always a non-nil object
   *     ("{}" when empty), never a JSON null.
   *
   * A reader must never mistake the two, so they are separated by a divider
   * and styled opposite ways: available mana is hollow (a potential, not yet
   * spent) and labelled tap, floating mana is saturated (mana here) and
   * labelled pool. The labels are persistent text, not hover-only tooltips — a
   * sighted reader looking at a static board can tell the two groups apart
   * without moving the pointer and without relying on the fill difference
   * (subtle on a near-white chip, invisible to a colour-blind reader). When
   * either group is empty it renders nothing, and when both are empty the row
   * still exists (IdentityBar reserves its height) so the box does not grow a
   * line when mana appears and collapse the moment it is spent.
   *
   * The wire's two maps are exactly the six symbols poolView emits
   * (view/visibility.go), so this reads them in a fixed WUBRGC order rather
   * than iterating the object: a readout that reorders itself between frames
   * cannot be read at a glance, and object key order is not a guarantee worth
   * depending on.
   *
   * pool is now public and so is always a non-nil object from the server, but
   * the generated protocol.ts types both fields as plain Records, so neither
   * svelte-check nor the type checker can catch a caller that hands this a
   * null — the null/absent guard therefore lives here, at the one place that
   * owns "an absent group draws nothing". The pool-null branch is retained
   * defensively but is no longer reachable from the wire: the retired
   * redaction (which sent null for a hidden pool) is gone.
   */
  /**
   * poolRestrictions is the spend limit on the floating-pool chips: one entry
   * per restricted batch (view.PlayerView.PoolRestrictions). A chip alone
   * reads as spendable, so a seat holding Cavern of Souls' {B} -- usable only
   * for a creature spell of the chosen type -- sees mana it cannot spend and
   * no reason why. This renders the reason as persistent text beside the
   * chips (never a hover-only tooltip), the same legibility contract the
   * pool/tap tags follow. Absent or empty, it draws exactly what the
   * component drew before this prop existed.
   */
  let { pool, available, poolRestrictions }: {
    pool: Record<string, number> | null | undefined;
    available?: Record<string, number> | null | undefined;
    poolRestrictions?: { color: string; amount: number; text: string }[] | null | undefined;
  } = $props();

  const ORDER = ['W', 'U', 'B', 'R', 'G', 'C'] as const;
  const NAMES: Record<string, string> = {
    W: 'white', U: 'blue', B: 'black', R: 'red', G: 'green', C: 'colourless',
  };

  const held = $derived(
    ORDER.map((sym) => ({ sym, n: pool?.[sym] ?? 0 })).filter((h) => h.n > 0),
  );
  const avail = $derived(
    ORDER.map((sym) => ({ sym, n: available?.[sym] ?? 0 })).filter((h) => h.n > 0),
  );
  const poolSummary = $derived(held.map((h) => `${h.n} ${NAMES[h.sym]}`).join(', '));
  const availSummary = $derived(avail.map((h) => `${h.n} ${NAMES[h.sym]}`).join(', '));
  const restrictions = $derived((poolRestrictions ?? []).filter((r) => r.text));
</script>

{#if avail.length > 0 || held.length > 0 || restrictions.length > 0}
  <div class="readout" data-mana-readout>
    {#if avail.length > 0}
      <div class="group avail" data-mana-available aria-label="Available by tapping: {availSummary}">
        <span class="tag" data-mana-tag="tap" aria-hidden="true">tap</span>
        {#each avail as h (h.sym)}
          <span class="sym" data-avail={h.sym} title="{h.n} {NAMES[h.sym]} available">
            <span class="chip outline" style="--pip: var(--mana-{h.sym.toLowerCase()})" aria-hidden="true"></span>
            <span class="n">{h.n}</span>
          </span>
        {/each}
      </div>
    {/if}
    {#if avail.length > 0 && held.length > 0}
      <span class="sep" data-mana-sep aria-hidden="true"></span>
    {/if}
    {#if held.length > 0 || restrictions.length > 0}
      <div class="group pool" data-mana-pool aria-label="Mana pool: {poolSummary}">
        <span class="tag" data-mana-tag="pool" aria-hidden="true">pool</span>
        {#each held as h (h.sym)}
          <span class="sym" data-mana={h.sym} title="{h.n} {NAMES[h.sym]}">
            <span class="chip" style="--pip: var(--mana-{h.sym.toLowerCase()})" aria-hidden="true"></span>
            <span class="n">{h.n}</span>
          </span>
        {/each}
        {#if restrictions.length > 0}
          <span class="restr" data-mana-restrictions>
            {#each restrictions as r, i (i)}
              <span class="restr-item" data-mana-restriction={r.color}>{r.amount} {NAMES[r.color] ?? r.color}: {r.text}</span>
            {/each}
          </span>
        {/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .readout {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
    min-width: 0;
  }
  .group {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  /* The two groups are deliberately styled opposite ways so a reader cannot
     mistake mana that COULD be tapped (hollow) for mana already floating
     (solid). The divider between them when both are present is the only
     textual separator a reader needs. */
  .sep {
    width: 1px;
    align-self: stretch;
    background: var(--edge-felt);
    flex: none;
  }
  .sym {
    display: inline-flex;
    align-items: center;
    gap: 0.25em;
  }
  /* The persistent cue: both groups already carry the hollow-vs-solid fill
     difference, but that is subtle and colour-blind-invisible, so each group
     also leads with a tiny uppercase text tag naming what it is. A tag is
     inline and shares the chips' line-height (line 3's height is reserved by
     IdentityBar), so it can only widen the row, never grow its height. */
  .tag {
    font-family: var(--font-data);
    font-size: 0.55rem;
    line-height: 1;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--ink-inst);
    opacity: 0.7;
    flex: none;
  }
  /* Saturation means mana already floating, which is exactly what the design
     system reserves it for; the ring keeps a near-white W chip from
     dissolving into the panel it sits on. */
  .chip {
    width: 0.6rem;
    height: 0.6rem;
    border-radius: 999px;
    background: var(--pip);
    box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.35);
    flex: none;
  }
  /* Available mana is a potential, not yet spent: hollow (a ring in the
     colour's hue, transparent interior) rather than a full saturated fill,
     so it reads as "could tap" against the pool's "already here". */
  .chip.outline {
    background: transparent;
    box-shadow: inset 0 0 0 1.5px var(--pip);
  }
  .n {
    font-family: var(--font-data);
    font-variant-numeric: tabular-nums;
    font-size: var(--t-12);
    line-height: 1;
    color: var(--ink-inst);
  }
  /* The spend limit on a floating chip is persistent text, never hover-only: a
     reader looking at a static board must be able to tell that a symbol in the
     pool cannot pay what they hold. It is set in the same data face as the
     counts so it reads as part of the readout, not a separate aside. */
  .restr {
    display: inline-flex;
    flex-direction: column;
    gap: 0.1rem;
    min-width: 0;
  }
  .restr-item {
    font-family: var(--font-data);
    font-size: 0.6rem;
    line-height: 1.1;
    color: var(--ink-inst);
    opacity: 0.85;
  }
</style>
