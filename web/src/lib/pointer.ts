/** A pointer click must not leave a game button owning the next Space/Enter hotkey.
 * Keyboard activation (click.detail === 0) keeps focus and native button semantics.
 */
export function blurAfterPointer(e: MouseEvent): void {
  if (e.detail > 0) (e.currentTarget as HTMLElement).blur();
}

/**
 * pointerRelease is the `use:` action form of blurAfterPointer, for the table
 * controls outside the hot strip (hotkey-focus2): attach it once per
 * focusable control — `use:pointerRelease` — instead of threading the event
 * through every inline handler. The contract is the same one the strip's
 * buttons carry (fb-20260929T080219Z):
 *
 *  - a POINTER click (detail > 0) releases focus afterwards, so Space and
 *    Enter return to the table hotkeys instead of re-activating whatever the
 *    player last clicked;
 *  - KEYBOARD activation (detail === 0, Enter/Space on a focused control)
 *    keeps focus and the native semantics — the hotkey grammar
 *    (lib/hotkeys.ts focusOwnsKey) gives the focused control those two keys
 *    only while it holds them.
 *
 * The listener is registered in the CAPTURE phase on the node itself, and
 * that ordering is deliberate: a capture-phase listener on the event target
 * runs before the target's bubble-phase listeners, and before Svelte 5's
 * delegated inline handlers on the mount root. So the blur happens BEFORE
 * the component's own handler, and a handler that intentionally moves focus
 * (opening a dialog and focusing its first control; PileModal's keyboard
 * focus return) still wins — we never rip focus away from a surface the
 * click just opened. A document-level delegated handler would have the
 * opposite, racy order and is deliberately not used.
 */
export function pointerRelease(node: HTMLElement): { destroy: () => void } {
  const onClick = (e: MouseEvent): void => {
    if (e.detail > 0) node.blur();
  };
  node.addEventListener('click', onClick, true);
  return { destroy: () => node.removeEventListener('click', onClick, true) };
}
