<script lang="ts">
  import { onMount } from 'svelte';
  import type { View, SeatInfo, DecisionBody } from '../protocol';
  import Rail from './Rail.svelte';
  import ConcedeControl from './ConcedeControl.svelte';

  /**
   * Geometry-fixture host for the whole rail (SeatTable.geometry.ts mounts it
   * into #rail). It exists only so the fixture can render Rail with the SAME
   * logbar snippet the live route uses — a ConcedeControl passed as Rail's
   * `logbar` snippet, exactly as Table.svelte passes it (task fb-53bd45b9) —
   * so the harness measures the real rendered control, not a probe button
   * pasted into the row. No production caller; not used by any route.
   */
  let {
    view,
    seats,
    decision,
    emphasizeTop = false,
    showLog = true,
    onToggleLog = null,
    /** 'none' renders no concede control (the fixture's default, matching
     *  every rail with nothing pending, byte-identical to mounting Rail
     *  directly); 'idle' renders the unarmed Concede button (the control's
     *  first-paint state); 'confirm' renders the armed "Concede — confirm"
     *  button — the widest state the control ever takes. */
    concede = 'none',
    /** Transcript events handed straight through to Rail (and so to
     *  SeatTable's lossCauses) — the lost-seat fixture variant uses one real
     *  `player_lost` event carrying the longest real cause text so the
     *  eliminated line is measured with the widest content it ever holds
     *  (fb-20260917T232028Z round-2 overflow finding). */
    events = [],
  }: {
    view: View;
    seats: SeatInfo[];
    decision: DecisionBody | null;
    emphasizeTop?: boolean;
    showLog?: boolean;
    onToggleLog?: (() => void) | null;
    concede?: 'none' | 'idle' | 'confirm';
    events?: { event: { kind: string; player: number; text?: string; obj?: number } }[];
  } = $props();
  // Plain $state, not $state.raw: a raw assignment from this fixture's own
  // window event handler does not invalidate the $derived below in Svelte
  // 5.57 (measured: the listener fires, the new View is in hand, and the
  // mounted Rail never re-renders), so the live-update geometry test would
  // silently measure the initial projection. The fixture is test-only and
  // the projection is small, so the deep proxy is free.
  let updatedView = $state<View | null>(null);
  const currentView = $derived(updatedView ?? view);

  // The browser geometry test can deliver a replacement projection to this
  // mounted fixture, matching the live table's view-prop updates.
  onMount(() => {
    const update = (event: Event) => {
      updatedView = (event as CustomEvent<View>).detail;
    };
    window.addEventListener('fixture-view-update', update);
    (window as Window & { fixtureRailReady?: boolean }).fixtureRailReady = true;
    return () => window.removeEventListener('fixture-view-update', update);
  });
</script>

{#if concede === 'none'}
  <Rail view={currentView} {seats} {events} {decision} {emphasizeTop} {showLog} {onToggleLog} />
{:else}
  <Rail view={currentView} {seats} {events} {decision} {emphasizeTop} {showLog} {onToggleLog}>
    {#snippet logbar()}
      <ConcedeControl confirming={concede === 'confirm'} onArm={() => {}} onConfirm={() => {}} />
    {/snippet}
  </Rail>
{/if}
