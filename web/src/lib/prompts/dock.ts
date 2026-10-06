import type { Decision } from '../../protocol';
import { rendererFor } from './renderer';

/**
 * dock.ts is the prompt dock's placement (UI rework spec §4: "docked at the
 * top of the rail … Floating is a layout setting; the prompt is
 * dragged by its grip, and the position is saved in the layout profile").
 * The optional near-table placement ('table') pins question prompts just
 * above the gilt action button. Payment decisions use the rail even when
 * an older layout library still selects table. The layout profile
 * (lib/layoutprofile.ts `panels.prompt`) owns all four placements; this
 * module only converts between the profile's viewport fractions and the
 * dock's pixels, and keeps a dragged dock reachable. Client-side only.
 */
export type DockPlacement = 'table' | 'rail' | 'rail-bottom' | 'floating';
export interface DockPoint { x: number; y: number }
export interface DockLayout {
  placement: DockPlacement;
  /** The floating dock's top-left in viewport pixels; null until first dragged (then it opens at a default spot). */
  position: DockPoint | null;
}

/** Viewport is the window size the fractions in a layout profile are measured against. */
export interface Viewport { w: number; h: number }

/** ProfilePrompt is the layout profile's `panels.prompt` (lib/layoutprofile.ts). */
export interface ProfilePrompt { placement: 'table' | 'dock' | 'dock-bottom' | 'float'; x: number; y: number }

/**
 * dockFromProfile reads the dock's placement and floating position from the
 * layout profile, which stores the position as fractions of the viewport so
 * an exported profile means the same spot on another screen.
 */
export function dockFromProfile(p: ProfilePrompt, vp: Viewport): DockLayout {
  return {
    placement: p.placement === 'float' ? 'floating' : p.placement === 'table' ? 'table' : p.placement === 'dock-bottom' ? 'rail-bottom' : 'rail',
    position: { x: Math.round(p.x * vp.w), y: Math.round(p.y * vp.h) },
  };
}

/** profilePlacement is the profile's word for a dock placement. */
export function profilePlacement(p: DockPlacement): ProfilePrompt['placement'] {
  return p === 'floating' ? 'float' : p === 'table' ? 'table' : p === 'rail-bottom' ? 'dock-bottom' : 'dock';
}

/**
 * Upgrade the shipped near-table default at the presentation boundary, not
 * by rewriting gorge.layouts.v1: old saved defaults and deliberate table
 * choices are indistinguishable in storage. A payment always uses the rail
 * when table is selected; other questions and explicit rail-bottom/float
 * choices keep their placement. Both the route's slot and the dock shell
 * must use this one resolver so the payment is mounted only once.
 */
export function effectivePlacement(p: ProfilePrompt, decision: Decision | null): DockPlacement {
  const placement = dockFromProfile(p, { w: 0, h: 0 }).placement;
  return placement === 'table' && decision !== null && rendererFor(decision) === 'payment' ? 'rail' : placement;
}

/** nextPlacement cycles through table, both rail slots, and floating. */
export function nextPlacement(p: DockPlacement): DockPlacement {
  return p === 'table' ? 'rail' : p === 'rail' ? 'rail-bottom' : p === 'rail-bottom' ? 'floating' : 'table';
}

/** Box is the part of a DOMRect the anchor reads. */
export interface Box { left: number; right: number; top: number; bottom: number }
/** TableAnchor is the near-table dock's fixed-position edges (px from the viewport's right and bottom) and its height cap. */
export interface TableAnchor { right: number; bottom: number; maxHeight: number }

/** OBSTACLE_GAP is the clear space a prompt keeps from the persistent Feedback button. */
export const OBSTACLE_GAP = 8;
const TABLE_GAP = 8;
const TABLE_INSET = 12;
const TABLE_TOP_ROOM = 16;
const TABLE_MIN_HEIGHT = 160;

/**
 * tableAnchor places the near-table dock: right-aligned with the action
 * button and just above it, growing upward no higher than the board's top.
 * With no action button mounted it sits in the board's bottom-right corner.
 */
export function tableAnchor(action: Box | null, board: Box, vp: Viewport, obstacle: Box | null = null): TableAnchor {
  const right = action ? vp.w - action.right : vp.w - board.right + TABLE_INSET;
  let bottom = action ? vp.h - action.top + TABLE_GAP : vp.h - board.bottom + TABLE_INSET;
  // The dock is far wider than the obstacle, so any dock whose right edge is
  // past the obstacle's left edge spans it: lift the dock above it.
  if (obstacle && vp.w - right > obstacle.left) bottom = Math.max(bottom, vp.h - obstacle.top + OBSTACLE_GAP);
  const maxHeight = Math.max(TABLE_MIN_HEIGHT, vp.h - bottom - (board.top + TABLE_TOP_ROOM));
  return { right: Math.round(right), bottom: Math.round(bottom), maxHeight: Math.round(maxHeight) };
}

/**
 * rectContains reports whether `outer` fully contains `inner` (edges
 * included). This is the near-table dock's whole overlap rule: the dock may
 * partly cover a board option carrier — its chrome is pointer-transparent
 * and the visible part stays clickable — but if a carrier lies ENTIRELY
 * inside one of the dock's interactive rects, no pixel of it is reachable.
 */
export function rectContains(outer: Box, inner: Box): boolean {
  return inner.left >= outer.left && inner.right <= outer.right && inner.top >= outer.top && inner.bottom <= outer.bottom;
}

/**
 * dockYields is the near-table auto-yield rule, and the ONE home for it.
 * `covers` are the dock's own interactive surfaces (the control rects that
 * keep `pointer-events: auto`); `carriers` are the board elements that
 * afford options. The dock yields when some carrier is FULLY covered by one
 * of its controls, because a click aimed at that carrier can never reach the
 * board. Partial overlap never yields.
 *
 * A carrier spanning two stacked controls is not detected here: a click may
 * still land in the gap between them, so it is not provably unreachable —
 * the conservative direction is to keep the dock put.
 */
export function dockYields(covers: Box[], carriers: Box[]): boolean {
  return carriers.some((carrier) => covers.some((cover) => rectContains(cover, carrier)));
}

/** fractionOf turns a dragged dock's pixel position into the profile's viewport fractions (0..1). */
export function fractionOf(p: DockPoint, vp: Viewport): { x: number; y: number } {
  const f = (n: number, d: number) => (d > 0 ? Math.min(1, Math.max(0, n / d)) : 0);
  return { x: f(p.x, vp.w), y: f(p.y, vp.h) };
}

/** MIN_VISIBLE is how much of a floating dock must stay on screen so its grip can always be reached. */
export const MIN_VISIBLE = 48;

/**
 * clampPosition keeps a floating dock reachable: its grip row stays inside
 * the viewport however the window was resized since the position was saved.
 */
export function clampPosition(p: DockPoint, size: { w: number; h: number }, viewport: { w: number; h: number }): DockPoint {
  const maxX = Math.max(0, viewport.w - Math.min(size.w, MIN_VISIBLE * 3));
  const maxY = Math.max(0, viewport.h - MIN_VISIBLE);
  return {
    x: Math.round(Math.min(Math.max(p.x, Math.min(0, viewport.w - size.w)), maxX)),
    y: Math.round(Math.min(Math.max(p.y, 0), maxY)),
  };
}

/** overlaps reports a positive-area intersection (touching edges do not count). */
export function overlaps(a: Box, b: Box): boolean {
  return a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
}

/**
 * placeFloating is the ONE rule for where a floating dock may sit, shared by
 * restore, resize, pointer drag and keyboard nudge: clampPosition keeps the
 * grip reachable, then a dock that would cover `obstacle` (the persistent
 * Feedback button, with OBSTACLE_GAP around it) is moved the shortest way
 * out — up above it or left of it, whichever stays on screen. The saved
 * fractions are never rewritten by restore or resize (only a drag or nudge
 * saves a position). The floating CSS caps the height
 * at the obstacle's top, so moving up is always possible in practice; if
 * neither exit fits, the dock goes as high as the viewport allows.
 */
export function placeFloating(p: DockPoint, size: { w: number; h: number }, vp: Viewport, obstacle: Box | null): DockPoint {
  const c = clampPosition(p, size, vp);
  if (obstacle === null) return c;
  const o = { left: obstacle.left - OBSTACLE_GAP, right: obstacle.right + OBSTACLE_GAP, top: obstacle.top - OBSTACLE_GAP, bottom: obstacle.bottom + OBSTACLE_GAP };
  if (!overlaps({ left: c.x, right: c.x + size.w, top: c.y, bottom: c.y + size.h }, o)) return c;
  const up = { x: c.x, y: Math.round(o.top - size.h) };
  const left = { x: Math.round(o.left - size.w), y: c.y };
  const exits = [up, left].filter((e) => e.y >= 0 && e.x >= Math.min(0, vp.w - size.w));
  if (exits.length === 0) return { x: c.x, y: 0 };
  const dist = (e: DockPoint) => Math.abs(e.x - c.x) + Math.abs(e.y - c.y);
  return exits.reduce((best, e) => (dist(e) < dist(best) ? e : best));
}
