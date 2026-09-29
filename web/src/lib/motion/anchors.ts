import type { Recipient, ZoneRef } from '../clientmodel/types';

/**
 * anchors names where on screen a motion step starts or ends. The contract
 * for new seat UI is one attribute, `data-motion-anchor="<seat>:<zone>"` (and
 * `"<seat>:life"`, and `"stack"`). The seat box, header bars, count line,
 * board piles, battlefield quadrant, hand fan and rail stack carry it; the
 * fallbacks after it cover the rail seat table and a missing anchor. Selector lists
 * are ordered: the first that matches a visible element wins.
 */

export function objSelectors(obj: number): string[] {
  return [`[data-obj="${obj}"]`];
}

const seatFallbacks = (seat: number) => [`[data-seat-anchor="${seat}"]`, `[data-seat-row="${seat}"]`, `[data-seat="${seat}"]`];

export function zoneSelectors(z: ZoneRef): string[] {
  const own = z.seat === null ? [] : [`[data-motion-anchor="${z.seat}:${z.zone}"]`];
  if (z.zone === 'stack') return ['[data-motion-anchor="stack"]', '.rail section.stack', 'aside.rail'];
  if (z.seat === null) return own;
  const s = z.seat;
  switch (z.zone) {
    case 'graveyard':
    case 'exile':
      return [...own, `[data-seat="${s}"] [data-pile="${z.zone}"]`, `[data-seat-row="${s}"] [data-stat="${z.zone}"]`, ...seatFallbacks(s)];
    case 'hand':
    case 'library':
      return [...own, `[data-seat-row="${s}"] [data-stat="${z.zone}"]`, ...seatFallbacks(s)];
    case 'battlefield':
      return [...own, `.quadrant[data-seat="${s}"]`, ...seatFallbacks(s)];
    default:
      return [...own, ...seatFallbacks(s)];
  }
}

export function lifeSelectors(seat: number): string[] {
  return [`[data-motion-anchor="${seat}:life"]`, `[data-seat-row="${seat}"] [data-stat="life"]`, ...seatFallbacks(seat)];
}

export function recipientSelectors(r: Recipient, life: boolean): string[] {
  if ('obj' in r) return objSelectors(r.obj);
  return life ? lifeSelectors(r.seat) : seatFallbacks(r.seat);
}

/** The viewer's own hand fan: flights into or out of the viewer's hand use it when the card itself is not found. */
export const VIEWER_HAND = '.handfan';

export interface Box {
  left: number;
  top: number;
  width: number;
  height: number;
}

/** visibleBox is an element's box when it is laid out and not inside a modal. */
export function visibleBox(el: Element): Box | null {
  if (el.closest('[data-pile-modal]')) return null;
  const r = el.getBoundingClientRect();
  if (r.width <= 0 || r.height <= 0) return null;
  return { left: r.left, top: r.top, width: r.width, height: r.height };
}

/** pick finds the element for the first selector with a visible match; among a selector's matches the largest wins (a card tile over its rider chip). */
export function pick(root: ParentNode, selectors: readonly string[]): { el: Element; box: Box } | null {
  for (const sel of selectors) {
    let best: { el: Element; box: Box } | null = null;
    let nodes: NodeListOf<Element>;
    try {
      nodes = root.querySelectorAll(sel);
    } catch {
      continue;
    }
    for (const el of nodes) {
      if (el.closest('[data-motion-layer]')) continue;
      const box = visibleBox(el);
      if (box && (!best || box.width * box.height > best.box.width * best.box.height)) best = { el, box };
    }
    if (best) return best;
  }
  return null;
}
