<script lang="ts">
  import type { SeatPanelState } from '../lib/seatpanel.svelte';
  import { pointerRelease } from '../lib/pointer';
  import { MAX_WATCHLIST, STACK_DEPTHS, withWatch, withoutWatch, type Breakpoints } from '../lib/playsettings';

  // The prop is `state` (the panel's own prop name) but binds locally as
  // `logic`: a local named `state` collides with the `$state` rune.
  let { state: logic }: { state: SeatPanelState } = $props();
  const bp = $derived(logic.settings.breakpoints);
  let draft = $state('');

  function set(patch: Partial<Breakpoints>): void {
    logic.editSettings({ breakpoints: { ...bp, ...patch } });
  }
  function add(): void {
    const next = withWatch(bp.watchlist, draft);
    if (next.length !== bp.watchlist.length) set({ watchlist: next });
    draft = '';
  }
</script>

<section class="sec" data-breakpoints-section>
  <h3>Pause when…</h3>
  <p class="blurb">These stop automatic passing even when your other rules would pass. Each one pauses once for the thing it caught.</p>
  <label class="row chk">
    <input type="checkbox" data-bp="targets-me" checked={bp.targetsMe} onchange={(e) => set({ targetsMe: e.currentTarget.checked })} />
    <span>an opponent's spell or ability targets me or my permanent</span>
  </label>
  <label class="row chk">
    <input type="checkbox" data-bp="attacked" checked={bp.attacked} onchange={(e) => set({ attacked: e.currentTarget.checked })} />
    <span>creatures attack me</span>
  </label>
  <label class="row sel">
    <span>the stack holds</span>
    <select data-select="stack-depth" onchange={(e) => set({ stackDepth: Number(e.currentTarget.value) })}>
      {#each STACK_DEPTHS as d (d)}
        <option value={String(d)} selected={bp.stackDepth === d}>{d === 0 ? 'Off' : `${d} or more`}</option>
      {/each}
    </select>
  </label>
  <div class="watch">
    <span class="lbl">an opponent casts or activates a card on my watchlist</span>
    {#if bp.watchlist.length === 0}
      <p class="empty">No cards on your watchlist.</p>
    {:else}
      <ul class="chips">
        {#each bp.watchlist as name (name)}
          <li data-watch-item>
            {name}
            <button type="button" aria-label={`Remove ${name} from watchlist`} data-watch-remove use:pointerRelease onclick={() => set({ watchlist: withoutWatch(bp.watchlist, name) })}>×</button>
          </li>
        {/each}
      </ul>
    {/if}
    <form class="add" onsubmit={(e) => { e.preventDefault(); add(); }}>
      <input type="text" data-watch-input aria-label="Card name to watch" placeholder="Card name" maxlength="60" bind:value={draft} disabled={bp.watchlist.length >= MAX_WATCHLIST} />
      <button type="submit" data-watch-add use:pointerRelease disabled={draft.trim() === '' || bp.watchlist.length >= MAX_WATCHLIST}>Add</button>
    </form>
  </div>
</section>

<style>
  .blurb, .empty { color: var(--ink-dim); font-size: 12px; margin: 0 0 6px; }
  .chk { display: flex; gap: 8px; align-items: center; }
  .watch { margin-top: 8px; }
  .lbl { display: block; margin-bottom: 4px; }
  .chips { list-style: none; display: flex; flex-wrap: wrap; gap: 6px; padding: 0; margin: 0 0 6px; }
  .chips li { display: flex; align-items: center; gap: 4px; padding: 2px 4px 2px 10px; border-radius: 12px; border: 1px solid var(--edge-inst); }
  .chips button { border: 0; background: none; cursor: pointer; color: var(--ink-dim); }
  .add { display: flex; gap: 6px; }
</style>
