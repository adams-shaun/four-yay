<script lang="ts">
  import type { Snippet } from 'svelte';

  /**
   * PromptRailSlot is the rail's mount point for the prompt dock: the top
   * slot ('dock') or the lower slot ('dock-bottom', the shipped default).
   * Table.svelte mounts it for both; it is its own component so the
   * Feedback-clearance geometry is the production CSS the geometry fixture
   * measures, not a copy of it.
   *
   * The persistent Feedback button is fixed in the viewport's bottom-right
   * corner, which is the lower slot's corner whenever the rail is on the
   * right (or is the hidden drawer, which slides in from the right).
   * `clearFeedback` reserves that corner as bottom margin so the prompt can
   * never sit under the button; a left rail does not reach it.
   */
  let { placement, clearFeedback, children }: {
    placement: 'dock' | 'dock-bottom';
    clearFeedback: boolean;
    children: Snippet;
  } = $props();
</script>

<div class="prompt-dock" class:bottom={placement === 'dock-bottom'} class:clear-feedback={clearFeedback} data-prompt-dock-slot data-placement={placement}>{@render children()}</div>

<style>
  .prompt-dock:empty {
    display: none;
  }
  .prompt-dock.bottom { flex: 0 0 auto; max-height: 45%; overflow: auto; border-top: 1px solid var(--edge-inst); }
  .prompt-dock.bottom.clear-feedback { margin-bottom: var(--feedback-clear, 0px); }
</style>
