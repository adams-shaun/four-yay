<script lang="ts">
  import { onMount, tick } from 'svelte';
  import type { ClientModelSource, ModelEvent } from '../lib/clientmodel/types';
  import { planBatch } from '../lib/motion/plan';
  import { MotionQueue } from '../lib/motion/queue';
  import { capture, play } from '../lib/motion/dom';
  import { motionStore } from '../lib/motion/settings.svelte';

  /**
   * MotionLayer is the table's motion overlay (spec sub-project 5). It
   * listens to the client model: each live step's transitions become one
   * batch in the queue, played over the board in a fixed, pointer-events-none
   * layer on <body> (above the rail and the board, below nothing that takes
   * input). It renders no markup of its own.
   *
   * Motion never blocks input:
   * - any pointerdown or keydown finishes the queue at once (capture phase,
   *   before the click reaches its target);
   * - the viewer's own new decision finishes whatever is still playing
   *   before the step that carries it is queued;
   * - a reset (new match, snapshot, rewind, scrub) finishes the queue.
   */
  let { source, viewerSeat = null }: { source: ClientModelSource; viewerSeat?: number | null } = $props();

  onMount(() => {
    const layer = document.createElement('div');
    layer.className = 'motion-layer';
    layer.setAttribute('data-motion-layer', '');
    layer.setAttribute('aria-hidden', 'true');
    document.body.appendChild(layer);
    const queue = new MotionQueue();
    let epoch = 0;

    const finish = () => queue.finishAll();
    const onModel = (e: ModelEvent) => {
      if (e.type === 'reset') {
        epoch++;
        finish();
        return;
      }
      const speed = motionStore.effective;
      const mine = viewerSeat !== null ? e.next.decision : null;
      if (mine && mine.player === viewerSeat && mine.seq !== e.prev.decision?.seq) finish();
      if (speed === 'off') return;
      const plan = planBatch(e.transitions, speed);
      if (plan.steps.length === 0) return;
      // Still the old board: record where things start.
      const captured = capture(plan, viewerSeat);
      const at = epoch;
      void tick().then(() => {
        if (at !== epoch) return;
        queue.enqueue(plan.duration, () => play(plan, captured, layer, viewerSeat));
      });
    };
    const off = source.onModel(onModel);
    window.addEventListener('pointerdown', finish, true);
    window.addEventListener('keydown', finish, true);
    return () => {
      off();
      window.removeEventListener('pointerdown', finish, true);
      window.removeEventListener('keydown', finish, true);
      queue.finishAll();
      layer.remove();
    };
  });
</script>

<style>
  :global(.motion-layer) {
    position: fixed;
    inset: 0;
    pointer-events: none;
    z-index: 60;
    overflow: hidden;
  }
  :global(.motion-flyer) {
    position: fixed;
    border-radius: 6px;
    overflow: hidden;
    box-shadow: 0 14px 34px rgba(0, 0, 0, 0.7);
    will-change: transform, opacity;
  }
  :global(.motion-flyer img) {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  :global(.motion-flyer--back) {
    background:
      radial-gradient(ellipse at 50% 45%, #6b4a2a 0 30%, transparent 31%),
      linear-gradient(160deg, #3b2a1c, #1f1812);
    border: 2px solid #0d0b09;
  }
  :global(.motion-flyer__count) {
    position: absolute;
    right: 4px;
    top: 4px;
    padding: 1px 6px;
    border-radius: 999px;
    background: #d4ad62;
    color: #1c1915;
    font: 700 13px/1.3 'IBM Plex Sans', system-ui, sans-serif;
  }
  :global(.motion-float) {
    position: fixed;
    font: 700 26px/1 'Cormorant Garamond', Georgia, serif;
    color: #fff;
    text-shadow: 0 2px 6px rgba(0, 0, 0, 0.85), 0 0 2px #000;
    white-space: nowrap;
    will-change: transform, opacity;
  }
  :global(.motion-float--damage),
  :global(.motion-float--loss) {
    color: #ff8a78;
  }
  :global(.motion-float--gain) {
    color: #9fe0a8;
  }
  :global(.motion-float--counter) {
    font: 600 15px/1.2 'IBM Plex Sans', system-ui, sans-serif;
    color: #d4ad62;
  }
</style>
