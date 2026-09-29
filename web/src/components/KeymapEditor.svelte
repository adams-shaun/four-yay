<script lang="ts">
  import { onDestroy } from 'svelte';
  import type { KeymapStore } from '../lib/keymap.svelte';
  import { ACTION_GROUPS, ACTION_LABELS, bindingFromEvent, bindingLabel, conflictsFor, MAX_BINDINGS, type KeyAction } from '../lib/keymap';

  /**
   * The Keys section of GAME OPTIONS: every rebindable action by group, its
   * chords as key caps, and a capture flow for adding one. While capturing,
   * store.capturing makes the table's hotkey listener stand down, and this
   * capture-phase listener swallows the chord so it never also fires.
   */
  let { store }: { store: KeymapStore } = $props();
  let waiting = $state<KeyAction | null>(null);
  let refused = $state(false);
  let stop: (() => void) | null = null;

  function capture(a: KeyAction): void {
    stop?.();
    waiting = a;
    refused = false;
    store.capturing = true;
    const onKey = (e: KeyboardEvent): void => {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (e.key === 'Escape') return done();
      const b = bindingFromEvent(e);
      if (b === null) {
        // a bare modifier keeps waiting; a reserved chord is refused
        if (!/^(Shift|Control|Alt|Meta)/.test(e.code)) refused = true;
        return;
      }
      // The store refuses some chords (a duplicate on this action, Escape
      // outside cancel-run): an unchanged binding list means refused, so keep
      // waiting and say so rather than finishing silently. Capture is offered
      // only below MAX_BINDINGS, so an accepted chord always grows the list.
      const before = store.current[a].length;
      store.add(a, b);
      if (store.current[a].length === before) {
        refused = true;
        return;
      }
      done();
    };
    const done = (): void => {
      window.removeEventListener('keydown', onKey, true);
      store.capturing = false;
      waiting = null;
      stop = null;
    };
    stop = done;
    window.addEventListener('keydown', onKey, true);
  }

  onDestroy(() => stop?.());
</script>

<section class="sec" data-keymap-editor>
  <h3>Keys</h3>
  {#each ACTION_GROUPS as g (g.title)}
    <h4>{g.title}</h4>
    <ul class="rows">
      {#each g.actions as a (a)}
        {@const clash = conflictsFor(store.current, a)}
        <li data-key-action={a}>
          <span class="lbl">{ACTION_LABELS[a]}</span>
          <span class="caps">
            {#each store.current[a] as b, i (i)}
              <span class="cap"><kbd>{bindingLabel(b)}</kbd><button type="button" aria-label={`Remove ${bindingLabel(b)} from ${ACTION_LABELS[a]}`} onclick={() => store.remove(a, i)}>×</button></span>
            {/each}
            {#if waiting === a}
              <span class="wait" aria-live="polite">{refused ? 'That chord is reserved — try another, or Esc' : 'Press a key… (Esc cancels)'}</span>
            {:else if store.current[a].length < MAX_BINDINGS}
              <button type="button" class="add" onclick={() => capture(a)}>Add key</button>
            {/if}
          </span>
          {#if clash.length > 0}
            <span class="warn">Also used by: {clash.map((c) => ACTION_LABELS[c]).join(', ')}</span>
          {/if}
        </li>
      {/each}
    </ul>
  {/each}
  <button type="button" class="reset" onclick={() => store.reset()}>Reset all keys</button>
</section>

<style>
  h4 { margin: 10px 0 4px; font-size: 13px; color: var(--ink-dim); font-weight: 500; }
  .rows { list-style: none; margin: 0; padding: 0; }
  .rows li { display: grid; grid-template-columns: 1fr auto; gap: 2px 10px; padding: 3px 0; align-items: center; }
  .caps { display: flex; gap: 6px; align-items: center; }
  .cap { display: inline-flex; align-items: center; gap: 2px; }
  .cap button { border: 0; background: none; cursor: pointer; color: var(--ink-dim); }
  kbd { font-family: var(--font-data); font-size: 11.5px; padding: 1px 6px; border-radius: 4px; border: 1px solid var(--edge-inst); }
  .warn { grid-column: 1 / -1; color: var(--danger); font-size: 12px; }
  .wait { font-size: 12px; color: var(--ink-dim); }
</style>
