<script lang="ts">
  import { onMount } from 'svelte';
  import type { View } from '../protocol';
  import { everyVisibleCard } from '../lib/board';
  import { cardById } from '../lib/board';
  import { hotkeyAction, focusOwnership } from '../lib/hotkeys';
  import { eventCode } from '../lib/keymap';
  import { hotkeyHinter } from '../lib/hotkeyhint.svelte';
  import { clientBreadcrumbs } from '../lib/breadcrumbs';
  import { hovered } from '../lib/hovered.svelte';
  import { keymapStore } from '../lib/keymap.svelte';
  import { layoutStore } from '../lib/layouts.svelte';
  import { modalPickerOpen } from '../lib/modals';
  import { pileOpener } from '../lib/pileopener.svelte';
  import ZoomCard from './ZoomCard.svelte';

  /**
   * ViewHotkeys wires the keymap's VIEW and LAYOUT actions (UI rework spec
   * §1, delivered with §2/§3): toggle the log, toggle stacking, zoom the card
   * under the pointer, open the hovered seat's graveyard or exile, and switch
   * layout profile (1–9, next, previous). They are view preferences, so they
   * work for a spectator too — this is mounted for every table view, unlike
   * the seat's hot strip, which owns the priority/decision actions. Same
   * guards (lib/hotkeys): Meta, an open modal, focus in a text field, the
   * Keys editor recording a chord.
   */
  let { view, onToggleLog = null }: { view: View; onToggleLog?: (() => void) | null } = $props();

  let zoomed = $state<number | null>(null);
  const zoomCard = $derived(zoomed === null ? null : cardById([...everyVisibleCard(view.players), ...view.stack.flatMap((s) => (s.card ? [s.card] : []))], zoomed));

  function trigger(): HTMLElement {
    return document.activeElement instanceof HTMLElement ? document.activeElement : document.body;
  }

  onMount(() => {
    const detach = hovered.attach(document);
    const onKey = (e: KeyboardEvent): void => {
      if (keymapStore.capturing || e.repeat) return;
      const action = hotkeyAction(e, modalPickerOpen, keymapStore.current);
      if (action === null) {
        // Space/Enter on a focused activatable control was suppressed as a
        // table hotkey because the control's native activation owns it. Say
        // so — this route is mounted for spectators too, who have no seat
        // strip, so the cue would otherwise never reach them
        // (fb-20260929T080219Z). Text-entry suppression stays silent: every
        // keystroke while typing would flood the breadcrumb ring, and a
        // keystroke landing in a textbox is no surprise.
        const owned = focusOwnership(e);
        if (owned === 'activatable-space-enter') {
          hotkeyHinter.note(owned);
          clientBreadcrumbs.record('hotkey-suppressed', { key: e.key, code: eventCode(e), reason: owned });
        }
        return;
      }
      switch (action) {
        case 'toggle-log':
          if (onToggleLog === null) return;
          onToggleLog();
          break;
        case 'toggle-stacking':
          layoutStore.toggleStacking();
          break;
        case 'zoom-card':
          if (hovered.obj === null) return;
          zoomed = hovered.obj;
          break;
        case 'open-grave':
        case 'open-exile':
          if (hovered.seat === null) return;
          pileOpener.open(hovered.seat, action === 'open-grave' ? 'graveyard' : 'exile', trigger(), false);
          break;
        case 'next-layout':
          layoutStore.cycle(1);
          break;
        case 'prev-layout':
          layoutStore.cycle(-1);
          break;
        default:
          if (!action.startsWith('layout-') || !layoutStore.applyAt(Number(action.slice(7)))) return;
          break;
      }
      e.preventDefault();
      hotkeyHinter.clear();
    };
    window.addEventListener('keydown', onKey, true);
    return () => {
      detach();
      window.removeEventListener('keydown', onKey, true);
    };
  });
</script>

<ZoomCard card={zoomCard} onClose={() => (zoomed = null)} />
