<script lang="ts">
  import { clientBreadcrumbs } from '../lib/breadcrumbs';

  let { table = null, seat = null }: { table?: string | null; seat?: number | null } = $props();

  /**
   * A persistent feedback affordance, mounted once at the app root so it
   * appears above every route. Capturing a screenshot uses the browser's own
   * screen-sharing prompt (getDisplayMedia) rather than a DOM-rasterizing
   * library — one grant, one frame, no dependency. It is optional: a report
   * with just text and no screenshot is a normal, complete submission.
   */
  let open = $state(false);
  let text = $state('');
  let screenshot = $state<Blob | null>(null);
  let screenshotURL = $state<string | null>(null);
  let capturing = $state(false);
  let submitting = $state(false);
  let result = $state<'ok' | 'error' | null>(null);

  function reset(): void {
    text = '';
    screenshot = null;
    if (screenshotURL) URL.revokeObjectURL(screenshotURL);
    screenshotURL = null;
    result = null;
  }

  function toggle(): void {
    open = !open;
    if (!open) reset();
  }

  function closeOnEscape(event: KeyboardEvent): void {
    if (!open || event.key !== 'Escape') return;
    event.preventDefault();
    open = false;
    reset();
  }

  async function captureScreen(): Promise<void> {
    capturing = true;
    try {
      // preferCurrentTab/selfBrowserSurface are user-agent hints, not a
      // guarantee: they ask a supporting browser to include the current tab in
      // its source chooser and to make the focused tab prominent, so the
      // player can attach the very table they are reporting. Browsers that do
      // not know these members ignore them and keep their native behaviour.
      // Both hints are newer than the installed TypeScript DOM lib models
      // (which still puts preferCurrentTab on MediaStreamConstraints only),
      // so the literal is asserted; the browser receives it verbatim.
      const stream = await navigator.mediaDevices.getDisplayMedia({
        video: true,
        preferCurrentTab: true,
        selfBrowserSurface: 'include',
      } as DisplayMediaStreamOptions);
      const track = stream.getVideoTracks()[0];
      const bitmap = await new ImageCapture(track).grabFrame();
      track.stop();
      const canvas = document.createElement('canvas');
      canvas.width = bitmap.width;
      canvas.height = bitmap.height;
      canvas.getContext('2d')?.drawImage(bitmap, 0, 0);
      const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'));
      if (blob) {
        if (screenshotURL) URL.revokeObjectURL(screenshotURL);
        screenshot = blob;
        screenshotURL = URL.createObjectURL(blob);
      }
    } catch {
      // Permission denied or cancelled — leave any existing screenshot as is.
    } finally {
      capturing = false;
    }
  }

  function clearScreenshot(): void {
    if (screenshotURL) URL.revokeObjectURL(screenshotURL);
    screenshot = null;
    screenshotURL = null;
  }

  async function submit(): Promise<void> {
    if (!text.trim()) return;
    submitting = true;
    result = null;
    try {
      const form = new FormData();
      form.set('text', text);
      form.set('url', location.href);
      // App supplies route and seat context; do not reverse-engineer either
      // from the URL here (the URL also carries the bearer token).
      if (table !== null) form.set('table', table);
      if (seat !== null) form.set('seat', String(seat));
      form.set('client.json', JSON.stringify(clientBreadcrumbs.snapshot()));
      if (screenshot) form.set('screenshot', screenshot, 'screenshot.png');
      const res = await fetch('/api/feedback', { method: 'POST', body: form });
      result = res.ok ? 'ok' : 'error';
      if (res.ok) {
        text = '';
        clearScreenshot();
      }
    } catch {
      result = 'error';
    } finally {
      submitting = false;
    }
  }
</script>

<!-- role=dialog makes this transient surface part of the structural table-
     hotkey guard. Its own bubble-phase Escape handler closes the panel after
     that guard has preserved any live End Turn/hard-skip run underneath. -->
<svelte:window onkeydown={closeOnEscape} />

<button type="button" class="feedback-badge" data-feedback-button onclick={toggle} aria-haspopup="dialog" aria-expanded={open}>
  Feedback
</button>

{#if open}
  <div class="feedback-panel" role="dialog" aria-label="Send feedback">
    <textarea
      class="feedback-text"
      placeholder="What happened?"
      bind:value={text}
      rows="4"
    ></textarea>
    <div class="feedback-shot">
      {#if screenshotURL}
        <img src={screenshotURL} alt="Captured screenshot" class="feedback-thumb" />
        <button type="button" class="feedback-link" onclick={clearScreenshot}>Remove screenshot</button>
      {:else}
        <button type="button" class="feedback-link" onclick={captureScreen} disabled={capturing}>
          {capturing ? 'Choose what to share…' : 'Attach a screenshot'}
        </button>
      {/if}
    </div>
    <div class="feedback-actions">
      {#if result === 'ok'}<span class="feedback-status feedback-status--ok">Sent — thank you.</span>{/if}
      {#if result === 'error'}<span class="feedback-status feedback-status--error">Couldn't send. Try again.</span>{/if}
      <button type="button" class="feedback-cancel" onclick={toggle}>Cancel</button>
      <button type="button" class="feedback-submit" onclick={submit} disabled={submitting || !text.trim()}>
        {submitting ? 'Sending…' : 'Send'}
      </button>
    </div>
  </div>
{/if}

<style>
  /* Bottom-right, NOT top-right. The seated table's rail puts its own
     controls in the top-right corner, and a fixed badge there sat directly on
     top of [data-log-toggle] -- same y, overlapping x -- so elementFromPoint
     at the toggle's centre returned this button and the log switch could not
     be clicked at all. The smoke gate caught it as a 2-minute timeout rather
     than a visible error, which is exactly how an overlay bug presents. */
  .feedback-badge {
    position: fixed;
    bottom: var(--sp-2);
    right: var(--sp-2);
    z-index: 30;
    box-sizing: border-box;
    height: var(--feedback-h, 1.75rem);
    padding: 0 var(--sp-3);
    border: var(--edge-w, 1px) solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    font-weight: 600;
    cursor: pointer;
  }
  .feedback-badge:hover,
  .feedback-badge[aria-expanded='true'] {
    border-color: var(--ink-dim);
    color: var(--ink);
  }
  .feedback-panel {
    position: fixed;
    bottom: calc(var(--sp-2) + 2rem);
    right: var(--sp-2);
    z-index: 30;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    width: 280px;
    padding: var(--sp-3);
    background: var(--instrument-raised);
    border: var(--edge-w, 1px) solid var(--edge-inst);
    border-radius: var(--radius);
    box-shadow: var(--shadow-lift);
  }
  .feedback-text {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    background: var(--instrument);
    color: var(--ink);
    border: var(--edge-w, 1px) solid var(--edge-inst);
    border-radius: var(--radius);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    padding: var(--sp-2);
  }
  .feedback-shot { display: flex; flex-direction: column; gap: var(--sp-1); }
  .feedback-thumb {
    width: 100%;
    border-radius: var(--radius);
    border: var(--edge-w, 1px) solid var(--edge-inst);
  }
  .feedback-link {
    background: none;
    border: 0;
    padding: 0;
    color: var(--ink-dim);
    font-family: var(--font-ui);
    font-size: var(--t-11);
    text-align: left;
    cursor: pointer;
  }
  .feedback-link:hover { color: var(--ink); }
  .feedback-actions { display: flex; align-items: center; justify-content: flex-end; gap: var(--sp-2); }
  .feedback-status { font-size: var(--t-11); margin-right: auto; }
  .feedback-status--ok { color: var(--ink-dim); }
  .feedback-status--error { color: var(--danger); }
  .feedback-cancel,
  .feedback-submit {
    padding: var(--sp-1) var(--sp-3);
    border-radius: var(--radius);
    border: var(--edge-w, 1px) solid var(--edge-inst);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    cursor: pointer;
  }
  .feedback-cancel { background: none; color: var(--ink-dim); }
  .feedback-submit { background: var(--ink); color: var(--felt-sunk); border-color: var(--ink); }
  .feedback-submit:disabled { opacity: 0.5; cursor: default; }
</style>
