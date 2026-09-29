<script lang="ts">
  import MotionSpeedControl from './MotionSpeedControl.svelte';
  import { layoutStore } from '../lib/layouts.svelte';
  import {
    ANCHOR_LABELS,
    ANCHORS,
    ARRANGEMENT_LABELS,
    ARRANGEMENTS,
    ART_MAX,
    HAND_MAX,
    HAND_MIN,
    LOG_LABELS,
    LOG_MODES,
    ORDER_LABELS,
    ORIENTATION_LABELS,
    ORIENTATIONS,
    OVERFLOW_LABELS,
    OVERFLOWS,
    PRESET_IDS,
    PRESET_LABELS,
    PROMPT_LABELS,
    PROMPT_PLACEMENTS,
    RAIL_LABELS,
    RAIL_SIDES,
    REGION_KEYS,
    REGION_LABELS,
    REGION_ORDERS,
    SPLIT_MAX,
    SPLIT_MIN,
    WEIGHT_STEPS,
    MAX_ROWS,
  } from '../lib/layoutprofile';
  import { exportLayouts, importLayouts } from '../lib/transfer';
  import { downloadText } from '../lib/download';

  /**
   * LayoutDrawer is every layout choice without drag and drop (UI rework
   * spec §2, principle 3): presets, the table (arrangement, the opponents'
   * share, orientation), the board template's regions, cards (stacking,
   * overflow, art tiles, the hand) and panels (the rail, the log, the prompt
   * placement), plus saving named profiles and exporting/importing them as a
   * gorge-layout file. Every control writes the working copy at once
   * (layoutStore.edit), so the table behind the drawer is the preview.
   *
   * It is a side panel, not a modal: the table's hotkeys keep working while
   * it is open (a text field still owns its own keys).
   */
  let { onClose = () => (layoutStore.drawerOpen = false) }: { onClose?: () => void } = $props();

  const p = $derived(layoutStore.profile);
  let saveName = $state('');
  let note = $state<string | null>(null);
  let fileInput = $state<HTMLInputElement | null>(null);

  function save(): void {
    const name = saveName.trim() || layoutStore.lib.active || '';
    if (!name) {
      note = 'Name the profile to save it.';
      return;
    }
    layoutStore.saveAs(name);
    note = `Saved “${name}”.`;
    saveName = '';
  }
  function exportFile(): void {
    if (layoutStore.names.length === 0) {
      note = 'Save a profile first: the export holds your saved profiles.';
      return;
    }
    downloadText('gorge-layouts.json', exportLayouts(layoutStore.lib));
  }
  async function importFile(e: Event): Promise<void> {
    const f = (e.currentTarget as HTMLInputElement).files?.[0];
    if (!f) return;
    const got = importLayouts(await f.text(), layoutStore.lib);
    if ('error' in got) note = got.error;
    else {
      layoutStore.replace(got.lib);
      note = `Imported ${got.added.length} ${got.added.length === 1 ? 'profile' : 'profiles'}${got.skipped ? `; ${got.skipped} could not be read` : ''}.`;
    }
    if (fileInput) fileInput.value = '';
  }
  const pct = (v: number) => `${Math.round(v * 100)}%`;
</script>

{#snippet seg(label: string, values: readonly string[], labels: Record<string, string>, current: string, set: (v: never) => void, key: string)}
  <div class="f">
    <span class="lbl" id="lay-{key}">{label}</span>
    <div class="seg" role="group" aria-labelledby="lay-{key}" data-layout-control={key}>
      {#each values as v (v)}
        <button type="button" class:on={current === v} aria-pressed={current === v} data-value={v} onclick={() => set(v as never)}>{labels[v]}</button>
      {/each}
    </div>
  </div>
{/snippet}

<aside class="drawer" role="region" aria-label="Layout" data-layout-drawer>
  <header>
    <h2>Layout</h2>
    <select aria-label="Saved layout profiles" onchange={(e) => { const v = e.currentTarget.value; if (v) layoutStore.applyNamed(v); }}>
      <option value="" selected={layoutStore.lib.active === null}>{layoutStore.label}</option>
      {#each layoutStore.names as n (n)}
        <option value={n} selected={layoutStore.lib.active === n}>{n}</option>
      {/each}
    </select>
    <button type="button" class="done" data-layout-done onclick={onClose}>Done</button>
  </header>
  <div class="body">
    <h3>Presets</h3>
    <div class="presets">
      {#each PRESET_IDS as id (id)}
        <button type="button" data-layout-preset={id} onclick={() => layoutStore.applyPreset(id)}>{PRESET_LABELS[id]}</button>
      {/each}
    </div>

    <h3>Table</h3>
    {@render seg('Opponents', ARRANGEMENTS, ARRANGEMENT_LABELS, p.table.arrangement, (v) => layoutStore.edit((q) => (q.table.arrangement = v)), 'arrangement')}
    <div class="f">
      <label class="lbl" for="lay-split">Opponent share <b>{pct(p.table.split)}</b></label>
      <input id="lay-split" type="range" min={SPLIT_MIN} max={SPLIT_MAX} step="0.01" value={p.table.split} oninput={(e) => layoutStore.setSplit(Number(e.currentTarget.value))} />
    </div>
    {@render seg('Opponent boards', ORIENTATIONS, ORIENTATION_LABELS, p.table.orientation, (v) => layoutStore.edit((q) => (q.table.orientation = v)), 'orientation')}
    <p class="note">Tip: drag the centre bar, or double-click it to reset. Dragging is never required.</p>

    <h3>Board regions</h3>
    <table class="regions">
      <thead><tr><th></th><th>Row</th><th>Width</th><th>Fill from</th><th>Order</th></tr></thead>
      <tbody>
        {#each REGION_KEYS as k (k)}
          {@const r = p.regions[k]}
          <tr data-region-row={k}>
            <th scope="row">{REGION_LABELS[k].replace(' permanents', '')}</th>
            <td>
              <select aria-label="{REGION_LABELS[k]} row" onchange={(e) => layoutStore.setRegion(k, { row: Number(e.currentTarget.value) })}>
                {#each Array.from({ length: MAX_ROWS }, (_, i) => i) as i (i)}<option value={i} selected={r.row === i}>{i + 1}</option>{/each}
              </select>
            </td>
            <td>
              <select aria-label="{REGION_LABELS[k]} width" onchange={(e) => layoutStore.setRegion(k, { weight: Number(e.currentTarget.value) })}>
                {#each WEIGHT_STEPS as w (w.value)}<option value={w.value} selected={r.weight === w.value}>{w.label}</option>{/each}
                {#if !WEIGHT_STEPS.some((w) => w.value === r.weight)}<option value={r.weight} selected>{r.weight}</option>{/if}
              </select>
            </td>
            <td>
              <select aria-label="{REGION_LABELS[k]} fill from" onchange={(e) => layoutStore.setRegion(k, { anchor: e.currentTarget.value as typeof r.anchor })}>
                {#each ANCHORS as a (a)}<option value={a} selected={r.anchor === a}>{ANCHOR_LABELS[a]}</option>{/each}
              </select>
            </td>
            <td>
              <select aria-label="{REGION_LABELS[k]} order" onchange={(e) => layoutStore.setRegion(k, { order: e.currentTarget.value as typeof r.order })}>
                {#each REGION_ORDERS as o (o)}<option value={o} selected={r.order === o}>{ORDER_LABELS[o]}</option>{/each}
              </select>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
    <p class="note">Only entry order never moves existing cards; name and power slot a newcomer between them. Ties always keep entry order.</p>
    {@render seg('Region outlines', ['show', 'hide'] as const, { show: 'Show', hide: 'Hide' }, p.cards.outlines ? 'show' : 'hide', (v) => layoutStore.edit((q) => (q.cards.outlines = v === 'show')), 'outlines')}

    <h3>Cards</h3>
    {@render seg('Stack identical', ['on', 'off'] as const, { on: 'On', off: 'Off' }, p.cards.stacking ? 'on' : 'off', (v) => layoutStore.edit((q) => (q.cards.stacking = v === 'on')), 'stacking')}
    {@render seg('When a row is full', OVERFLOWS, OVERFLOW_LABELS, p.cards.overflow, (v) => layoutStore.edit((q) => (q.cards.overflow = v)), 'overflow')}
    <div class="f">
      <label class="lbl" for="lay-art">Art tiles below <b>{p.cards.artBelow === 0 ? 'never' : `${p.cards.artBelow}px wide`}</b></label>
      <input id="lay-art" type="range" min="0" max={ART_MAX} step="1" value={p.cards.artBelow} oninput={(e) => layoutStore.edit((q) => (q.cards.artBelow = Number(e.currentTarget.value)))} />
    </div>
    <div class="f">
      <label class="lbl" for="lay-hand">Hand visible <b>{pct(p.hand.visible)}</b></label>
      <input id="lay-hand" type="range" min={HAND_MIN} max={HAND_MAX} step="0.01" value={p.hand.visible} oninput={(e) => layoutStore.edit((q) => (q.hand.visible = Number(e.currentTarget.value)))} />
    </div>
    {@render seg('Hand rises on hover', ['yes', 'no'] as const, { yes: 'Yes', no: 'No' }, p.hand.raise ? 'yes' : 'no', (v) => layoutStore.edit((q) => (q.hand.raise = v === 'yes')), 'raise')}

    <h3>Panels</h3>
    {@render seg('Stack & log rail', RAIL_SIDES, RAIL_LABELS, p.panels.rail, (v) => layoutStore.edit((q) => (q.panels.rail = v)), 'rail')}
    {@render seg('Log', LOG_MODES, LOG_LABELS, p.panels.log, (v) => layoutStore.edit((q) => (q.panels.log = v)), 'log')}
    {@render seg('Prompt', PROMPT_PLACEMENTS, PROMPT_LABELS, p.panels.prompt.placement, (v) => layoutStore.edit((q) => (q.panels.prompt.placement = v)), 'prompt')}

    <h3>Motion</h3>
    <!-- Animation speed is a per-browser view setting (lib/motion), not part
         of a layout profile: switching boards never changes it. -->
    <MotionSpeedControl />

    <h3>Profiles</h3>
    <div class="f">
      <label class="lbl" for="lay-name">Save as</label>
      <div class="row">
        <input id="lay-name" type="text" maxlength="24" placeholder={layoutStore.lib.active ?? 'Profile name'} bind:value={saveName} onkeydown={(e) => { if (e.key === 'Enter') save(); }} />
        <button type="button" class="btn" data-layout-save onclick={save}>Save</button>
      </div>
    </div>
    {#if layoutStore.lib.active}
      {@const active = layoutStore.lib.active}
      <button type="button" class="btn quiet" data-layout-delete onclick={() => { layoutStore.remove(active); note = `Deleted “${active}”.`; }}>Delete “{active}”</button>
    {/if}
    <div class="row">
      <button type="button" class="btn" data-layout-export onclick={exportFile}>Export file…</button>
      <button type="button" class="btn" data-layout-import onclick={() => fileInput?.click()}>Import file…</button>
      <input type="file" accept="application/json,.json" hidden bind:this={fileInput} onchange={importFile} />
    </div>
    {#if note}<p class="note" role="status">{note}</p>{/if}
  </div>
</aside>

<style>
  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    z-index: 40;
    width: min(24rem, 100vw);
    display: flex;
    flex-direction: column;
    background: var(--instrument);
    border-left: 1px solid var(--edge-inst);
    box-shadow: -12px 0 32px rgb(0 0 0 / 0.45);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-14);
  }
  header {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-3) var(--sp-4);
    border-bottom: 1px solid var(--edge-inst);
  }
  h2 {
    margin: 0 auto 0 0;
    font-family: var(--font-serif);
    font-size: var(--t-28);
    font-weight: 600;
    color: var(--ink);
  }
  h3 {
    margin: var(--sp-4) 0 var(--sp-2);
    font-family: var(--font-serif);
    font-size: var(--t-20);
    font-weight: 600;
    color: var(--ink);
  }
  .body {
    flex: 1;
    overflow: auto;
    padding: 0 var(--sp-4) var(--sp-6);
  }
  .f {
    display: grid;
    grid-template-columns: 9rem 1fr;
    align-items: center;
    gap: var(--sp-2);
    margin: var(--sp-2) 0;
  }
  .lbl b {
    color: var(--ink);
    font-weight: 500;
  }
  .seg {
    display: flex;
    border: 1px solid var(--edge-inst);
    border-radius: 6px;
    overflow: hidden;
  }
  .seg button {
    flex: 1;
    padding: 0.3rem 0.4rem;
    border: 0;
    border-right: 1px solid var(--edge-inst);
    background: none;
    color: var(--ink-inst);
    font-size: var(--t-12);
    cursor: pointer;
  }
  .seg button:last-child {
    border-right: 0;
  }
  .seg button.on {
    background: var(--gilt);
    color: #2a1f0e;
    font-weight: 600;
  }
  .presets,
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
  }
  .presets button,
  .btn {
    padding: 0.35rem 0.7rem;
    border: 1px solid var(--edge-inst);
    border-radius: 6px;
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font-size: var(--t-12);
    cursor: pointer;
  }
  .presets button:hover,
  .btn:hover {
    border-color: var(--gilt);
    color: var(--ink);
  }
  .btn.quiet {
    margin: var(--sp-2) 0;
    background: none;
  }
  .done {
    padding: 0.35rem 0.9rem;
    border: 0;
    border-radius: 6px;
    background: var(--gilt);
    color: #2a1f0e;
    font-weight: 600;
    cursor: pointer;
  }
  select,
  input[type='text'] {
    min-width: 0;
    padding: 0.25rem 0.4rem;
    border: 1px solid var(--edge-inst);
    border-radius: 6px;
    background: var(--instrument-raised);
    color: var(--ink);
    font: inherit;
    font-size: var(--t-12);
  }
  input[type='text'] {
    flex: 1;
  }
  input[type='range'] {
    width: 100%;
    accent-color: var(--gilt);
  }
  .regions {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--t-12);
  }
  .regions th {
    font-weight: 400;
    color: var(--ink-dim);
    text-align: left;
    padding: 2px;
  }
  .regions th[scope='row'] {
    color: var(--ink-inst);
  }
  .regions td {
    padding: 2px;
  }
  .regions select {
    width: 100%;
  }
  .note {
    margin: var(--sp-1) 0;
    color: var(--ink-faint);
    font-size: var(--t-12);
  }
</style>
