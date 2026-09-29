import type { View } from '../protocol';
import type { CardOptions } from './cardoptions';

export type End = { obj: number } | { seat: number };
export interface Arrow { from: End; to: End; kind: 'target' | 'target-preview' | 'target-hover' | 'attack' | 'block' }

/** Pending decisions name legal candidates but have not resolved a target
 * relationship yet. Expose those board anchors as proposed arrows; the DOM
 * layer remains responsible for silently dropping anchors not on screen. */
export function previewArrowsFor(options: CardOptions | null): Arrow[] {
  if (options === null || options.source === undefined) return [];
  const source = options.source;
  const out: Arrow[] = [];
  for (const obj of options.byObj.keys()) {
    if (obj === source) continue;
    out.push({ from: { obj: source }, to: { obj }, kind: 'target-preview' });
  }
  for (const seat of options.byPlayer.keys()) {
    out.push({ from: { obj: source }, to: { seat }, kind: 'target-preview' });
  }
  return out;
}

/** arrowsFor reads relationships the server already resolved; it decides nothing about legality. */
export function arrowsFor(view: View): Arrow[] {
  const out: Arrow[] = [];
  for (const s of view.stack) {
    for (const t of s.targets) out.push({ from: { obj: s.id }, to: t.is_player ? { seat: t.player } : { obj: t.obj ?? 0 }, kind: 'target' });
  }
  for (const p of view.players) {
    for (const c of [...p.battlefield].sort((a, b) => a.id - b.id)) {
      if (c.attacking && c.attacking_player !== undefined && c.attacking_player !== null) out.push({ from: { obj: c.id }, to: { seat: c.attacking_player }, kind: 'attack' });
    }
  }
  for (const p of view.players) {
    for (const c of [...p.battlefield].sort((a, b) => a.id - b.id)) {
      for (const b of c.blocked_by ?? []) out.push({ from: { obj: b }, to: { obj: c.id }, kind: 'block' });
    }
  }
  return out;
}

/**
 * hoverArrowFor is the prompt dock's one hover arrow (UI rework spec §3/§4:
 * arrows are drawn for the pending decision, and a hovered option draws its
 * own): from the hovered option's own start (a blocker, an attacker) or else
 * the decision's source, to what it points at. Null when nothing is hovered
 * or the arrow has no start.
 */
export function hoverArrowFor(options: CardOptions | null, link: { from: End | null; to: End } | null): Arrow | null {
  if (link === null) return null;
  const from = link.from ?? (options?.source !== undefined && options.source !== 0 ? { obj: options.source } : null);
  if (from === null) return null;
  return { from, to: link.to, kind: 'target-hover' };
}
