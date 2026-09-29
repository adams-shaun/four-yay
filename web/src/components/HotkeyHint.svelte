<script lang="ts">
  import { onDestroy } from 'svelte';
  import { hotkeyHinter } from '../lib/hotkeyhint.svelte';

  /**
   * HotkeyHint is the transient cue for the hotkey grammar's focus
   * suppression (fb-20260929T080219Z): a player Tabs to a control — the PASS
   * button, a layout pill, a <summary> — presses Space expecting the pass
   * hotkey, and gets nothing, silently, because the focused control's native
   * activation owns Space and Enter (lib/hotkeys focusOwnership). This element
   * says why.
   *
   * It is a plain <span>, never a button: a focusable cue would itself become
   * the focused activatable that suppresses the key, and the chip would then
   * suppress the hotkey it is explaining. aria-live="polite" announces the
   * text without stealing focus.
   *
   * EXPIRY lives here, not in the store: the store is a module singleton that
   * outlives any mount, so a timer there would fire against a torn-down tree.
   * A fresh note carries a new id; this restarts the timeout on it. A
   * successful hotkey clears the store, which hides the chip at once.
   *
   * Mounted by HotButtonStrip (the seat's strip row) AND by Table.svelte
   * beside ViewHotkeys, so a spectator — who has no seat strip — gets the
   * same cue for the VIEW/LAYOUT keys.
   */
  /** HINT_MS is how long the cue stays before it fades on its own. */
  const HINT_MS = 4000;

  const hint = $derived(hotkeyHinter.current);
  let timer: ReturnType<typeof setTimeout> | null = null;

  function clearTimer(): void {
    if (timer !== null) clearTimeout(timer);
    timer = null;
  }

  // Restart on every fresh hint id; when the store is cleared, drop the timer.
  // The effect reads the STORE's $state directly (not the template's derived),
  // so teardown never reads a derived belonging to a destroyed effect.
  $effect(() => {
    const id = hotkeyHinter.current?.id ?? null;
    clearTimer();
    if (id !== null) {
      timer = setTimeout(() => {
        // Only expire the hint we armed for; a newer note clears and re-arms.
        if (hotkeyHinter.current?.id === id) hotkeyHinter.clear();
        timer = null;
      }, HINT_MS);
    }
  });

  onDestroy(clearTimer);
</script>

{#if hint !== null}
  <span class="hotkey-hint" data-hotkey-hint aria-live="polite">
    Space/Enter act on the focused control — Tab away or click the felt
  </span>
{/if}

<style>
  /* The house chip look (see HotButtonStrip's .mode-chip): same instrument
     plate and edge. Non-interactive, so it takes no hover/focus affordance. */
  .hotkey-hint {
    display: flex;
    align-items: center;
    align-self: center;
    margin-left: var(--sp-2);
    padding: 0 var(--sp-2);
    max-width: min(22rem, 60vw);
    height: 1.5rem;
    border: var(--edge-w) solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument-raised);
    color: var(--ink-dim);
    font-family: var(--font-ui);
    font-size: var(--t-11);
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    pointer-events: none;
  }
</style>
