import type { View } from '../protocol';
import type { CardOptions } from './cardoptions';

export type End = { obj: number } | { seat: number };
export interface Arrow { from: End; to: End; kind: 'target' | 'target-preview' | 'attack' | 'block' }

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

/**
 * arrowsFor reads relationships the server already resolved; it decides
 * nothing about legality. Declared combat (attacks, blocks) is always drawn.
 * A stack item's targets are drawn for `stackTargets`: every item ('all',
 * the historic behaviour), only the item with that id (the table passes the
 * HOVERED stack item — UI rework spec §3: arrows only for the pending
 * decision, the hovered stack item or declared combat, never XMage's
 * spaghetti), or none (null).
 */
export function arrowsFor(view: View, stackTargets: 'all' | number | null = 'all'): Arrow[] {
  const out: Arrow[] = [];
  for (const s of view.stack) {
    if (stackTargets !== 'all' && stackTargets !== s.id) continue;
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
