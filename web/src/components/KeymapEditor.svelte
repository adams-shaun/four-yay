<script lang="ts">
  import { onDestroy } from 'svelte';
  import type { KeymapStore } from '../lib/keymap.svelte';
  import { ACTION_GROUPS, ACTION_LABELS, bindingFromEvent, bindingLabel, conflictsFor, isModifierOrLockCode, MAX_BINDINGS, type Binding, type KeyAction } from '../lib/keymap';
  import { exportKeymap, importKeymap } from '../lib/transfer';
  import { downloadText } from '../lib/download';

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
        // a bare modifier or lock key keeps waiting; a reserved chord is refused
        if (!isModifierOrLockCode(e.code)) refused = true;
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

  // Export/import of the keymap as a file: the hidden picker and the last import's result.
  let keyFile = $state<HTMLInputElement | null>(null);
  let message = $state<string | null>(null);

  /** importKeysFile reads the picked file and replaces the keymap with it, or says why not. */
  async function importKeysFile(input: HTMLInputElement): Promise<void> {
    const file = input.files?.[0];
    if (file === undefined) return;
    try {
      const r = importKeymap(await file.text());
      if ('error' in r) message = r.error;
      else {
        store.replace(r.keymap);
        message = r.note;
      }
    } catch {
      message = 'Could not read that file.';
    } finally {
      // cleared either way, so picking the same file again fires onchange
      input.value = '';
    }
  }

  /** alwaysOn is Esc on cancel-run: the hard-coded panic key (lib/hotkeys), so removing it would do nothing. */
  const alwaysOn = (a: KeyAction, b: Binding): boolean => a === 'cancel-run' && b.code === 'Escape';

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
              {#if alwaysOn(a, b)}
                <span class="cap"><kbd>{bindingLabel(b)}</kbd><span class="always">(always)</span></span>
              {:else}
                <span class="cap"><kbd>{bindingLabel(b)}</kbd><button type="button" aria-label={`Remove ${bindingLabel(b)} from ${ACTION_LABELS[a]}`} onclick={() => store.remove(a, i)}>×</button></span>
              {/if}
            {/each}
            {#if waiting === a}
              <span class="wait" aria-live="polite">{refused ? 'That chord is reserved or already bound here — try another, or Esc' : 'Press a key… (Esc cancels)'}</span>
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
  <button type="button" class="reset" data-keys-export onclick={() => downloadText('gorge-keys.json', exportKeymap(store.current))}>Export keys…</button>
  <button type="button" class="reset" data-keys-import onclick={() => keyFile?.click()}>Import keys…</button>
  <input type="file" accept="application/json" hidden bind:this={keyFile} onchange={(e) => importKeysFile(e.currentTarget)} />
  <!-- Mounted empty so a screen reader is already watching it when the first result arrives. -->
  <p class="note" data-keys-transfer role="status">{message ?? ''}</p>
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
  .note { font-size: 12px; color: var(--ink-dim); margin: 6px 0 0; }
  .note:empty { margin: 0; }
  .always { font-size: 11px; color: var(--ink-dim); margin-left: 4px; }
</style>
