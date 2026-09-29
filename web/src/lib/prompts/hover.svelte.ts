import type { Option } from '../../protocol';

/**
 * promptHover is the prompt dock's hover hook (UI rework spec §4: a target
 * chip "highlights legal targets on the board, and draws an arrow on
 * hover"). A renderer sets what its hovered option points at — `to`, an
 * object or a seat, and optionally where the arrow starts (`from`; the
 * decision's source when absent) — and clears it on leave. Arrows.svelte
 * draws the one hover arrow and the dock marks the board anchors. It is
 * display-only state: nothing here is ever posted.
 */
export type HoverEnd = { obj: number } | { seat: number };
export interface HoverLink {
  from: HoverEnd | null;
  to: HoverEnd;
}

class PromptHover {
  link = $state<HoverLink | null>(null);

  set(link: HoverLink | null): void {
    this.link = link;
  }

  clear(): void {
    this.link = null;
  }
}

export const promptHover = new PromptHover();

/**
 * hoverLinkOf is what an option points at on the board:
 *  - a block pairing: from the blocker to the attacker it would block;
 *  - an attack pairing: from the creature to the player (or battle/planeswalker);
 *  - anything with an object: to that object (from the decision's source);
 *  - a player option: to that seat.
 */
export function hoverLinkOf(o: Pick<Option, 'kind' | 'obj' | 'player' | 'attacker' | 'battle'>): HoverLink | null {
  if (o.kind === 'block' && o.obj !== undefined && o.attacker !== undefined) return { from: { obj: o.obj }, to: { obj: o.attacker } };
  if (o.kind === 'attacker' && o.obj !== undefined) {
    return { from: { obj: o.obj }, to: o.battle !== undefined && o.battle !== 0 ? { obj: o.battle } : { seat: o.player } };
  }
  if (o.obj !== undefined && o.obj !== 0) return { from: null, to: { obj: o.obj } };
  if (o.kind === 'player') return { from: null, to: { seat: o.player } };
  return null;
}
