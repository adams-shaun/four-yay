<script lang="ts">
  import { ACTION_GROUPS, ACTION_LABELS, bindingLabel, matchKeymap, type Keymap } from '../lib/keymap';

  /**
   * The `?` sheet: every bound action by group with its current chords. It
   * takes focus when it opens, and its dialog role makes the table's modal
   * guard hold every other hotkey while it is open. It must therefore never
   * need focus to close: the window listener closes it on Escape or on the
   * show-keys chord (whatever it is bound to) wherever focus has gone, and a
   * backdrop closes it on a click outside it, which would otherwise have
   * acted on the board underneath while every hotkey stayed suppressed.
   */
  let { open, keymap, onClose }: { open: boolean; keymap: Keymap; onClose: () => void } = $props();

  function focusOnOpen(node: HTMLElement): void {
    node.focus();
  }

  function onWindowKey(e: KeyboardEvent): void {
    if (!open || e.metaKey) return;
    // Escape closes whatever else consumed it (the route's Options listener
    // always does). The show-keys chord checks defaultPrevented: the table's
    // hotkey listener acted on the press that OPENED the sheet, and that
    // press must not also close it.
    if (e.key === 'Escape' || (!e.defaultPrevented && matchKeymap(keymap, e) === 'show-keys')) {
      e.preventDefault();
      onClose();
    }
  }
</script>

<svelte:window onkeydown={onWindowKey} />

{#if open}
  <div class="backdrop" data-keys-backdrop aria-hidden="true" onclick={onClose}></div>
  <div class="sheet" role="dialog" aria-modal="true" aria-labelledby="keys-title" tabindex="-1" use:focusOnOpen>
    <h2 id="keys-title">Keyboard shortcuts</h2>
    <div class="cols">
      {#each ACTION_GROUPS as g (g.title)}
        <div>
          <h3>{g.title}</h3>
          <dl>
            {#each g.actions.filter((a) => keymap[a].length > 0) as a (a)}
              <dt>{#each keymap[a] as b, i (i)}<kbd>{bindingLabel(b)}</kbd>{/each}</dt>
              <dd>{ACTION_LABELS[a]}</dd>
            {/each}
          </dl>
        </div>
      {/each}
    </div>
    <button type="button" onclick={onClose}>Close</button>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 99; background: rgba(0,0,0,.35); }
  .sheet { position: fixed; inset: 10% 15%; z-index: 100; overflow: auto; padding: 20px 24px; border-radius: 12px; background: var(--instrument-raised); border: 1px solid var(--edge-inst); box-shadow: 0 20px 60px rgba(0,0,0,.6); }
  .cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 16px; }
  dl { display: grid; grid-template-columns: auto 1fr; gap: 4px 10px; margin: 0; }
  dt { display: flex; gap: 4px; }
  dd { margin: 0; color: var(--ink-dim); }
  kbd { font-family: var(--font-data); font-size: 11.5px; padding: 1px 6px; border-radius: 4px; border: 1px solid var(--edge-inst); }
</style>
