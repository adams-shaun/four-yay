<script lang="ts">
  let { label, overlay = false }: { label: string; overlay?: boolean } = $props();
</script>

<span class="sticky-marker" class:overlay role="img" aria-label={label} title={label} data-sticky-marker>▲</span>

<style>
  .sticky-marker {
    display: inline-block;
    color: #ffd45c;
    background: #25221b;
    border-radius: 2px;
    padding: 0 0.2em;
    font-size: 0.75rem;
    line-height: 1.4;
  }
  .overlay { position: absolute; top: 3px; right: 3px; z-index: 2; pointer-events: none; }
</style>
