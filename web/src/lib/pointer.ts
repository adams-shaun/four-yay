/** A pointer click must not leave a game button owning the next Space/Enter hotkey.
 * Keyboard activation (click.detail === 0) keeps focus and native button semantics.
 */
export function blurAfterPointer(e: MouseEvent): void {
  if (e.detail > 0) (e.currentTarget as HTMLElement).blur();
}
