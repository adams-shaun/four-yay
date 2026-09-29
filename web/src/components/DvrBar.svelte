<script lang="ts">
  import type { DvrAction, DvrState } from '../lib/dvr';
  import { behindLive, turnOf } from '../lib/dvr';
  import { pointerRelease } from '../lib/pointer';
  let { dvr, onAction, finished = false }: { dvr: DvrState; onAction: (a: DvrAction) => void; finished?: boolean } = $props();
  const turn = $derived(turnOf(dvr, dvr.cursor));
</script>

<div class="dvr" data-cursor={dvr.cursor} data-live={dvr.live}>
  <button onclick={() => onAction({ type: 'step', by: -1 })} aria-label="step back" use:pointerRelease>⏮</button>
  {#if dvr.live && !finished}
    <button onclick={() => onAction({ type: 'pause' })} aria-label="pause" use:pointerRelease>⏸</button>
  {:else if !finished}
    <button onclick={() => onAction({ type: 'live' })} aria-label="return to live" use:pointerRelease>▶ live</button>
  {/if}
  <button onclick={() => onAction({ type: 'step', by: 1 })} aria-label="step forward" use:pointerRelease>⏭</button>
  <input type="range" min="0" max={dvr.head} value={dvr.cursor} list="turn-ticks"
    oninput={(e) => onAction({ type: 'scrub', seq: Number((e.target as HTMLInputElement).value) })} aria-label="scrub" />
  <datalist id="turn-ticks">{#each dvr.turnStarts as t (t)}<option value={t}></option>{/each}</datalist>
  <span class="badge" class:live={dvr.live}>
    {#if finished}REPLAY{:else if dvr.live}LIVE{:else}PAUSED · {behindLive(dvr)} behind{/if}
  </span>
  <span class="seq">seq {dvr.cursor} / {dvr.head} · turn {turn + 1}</span>
</div>

<style>
  .dvr { display: flex; gap: .5rem; align-items: center; padding: .25rem .5rem; background: #111; color: #ddd; }
  input[type=range] { flex: 1; min-width: 3rem; }
  /* The rail is narrow: the counter stays one line and never squeezes into a column. */
  .seq { white-space: nowrap; font-size: .8em; flex: 0 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; }
  .badge { font-weight: 700; color: #f66; }
  .badge.live { color: #6f6; }
</style>
