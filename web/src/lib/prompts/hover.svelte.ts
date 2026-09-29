/**
 * promptHover is the prompt dock's hover hook (UI rework spec §4: a target
 * chip "highlights legal targets on the board, and draws an arrow on
 * hover"). A renderer sets the end its hovered option points at — an object
 * or a seat — and clears it on leave; Arrows.svelte draws the one hover arrow
 * from the decision's source to it, and the dock marks the board anchor.
 * It is display-only state: nothing here is ever posted.
 */
export type HoverEnd = { obj: number } | { seat: number };

class PromptHover {
  end = $state<HoverEnd | null>(null);

  set(end: HoverEnd | null): void {
    this.end = end;
  }

  clear(): void {
    this.end = null;
  }
}

export const promptHover = new PromptHover();

/** hoverEndOf is the board end an option points at: its object, else its player (a player target), else nothing. */
export function hoverEndOf(o: { obj?: number; player: number; kind: string }): HoverEnd | null {
  if (o.obj !== undefined && o.obj !== 0) return { obj: o.obj };
  if (o.kind === 'player') return { seat: o.player };
  return null;
}
