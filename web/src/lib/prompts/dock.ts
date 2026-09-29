/**
 * dock.ts is the prompt dock's placement (UI rework spec §4: "docked at the
 * top of the rail by default … Floating is a layout setting; the prompt is
 * dragged by its grip, and the position is saved in the layout profile").
 * The near-table placement ('table', the default since the operator's
 * 2026-09-29 feedback: the rail's top was a long mouse trip from the board)
 * pins the dock just above the gilt action button, where the eye and the
 * pointer already are, and grows it upward over the board.
 * The layout profile (lib/layoutprofile.ts `panels.prompt`) owns all three; this
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

/** nextPlacement cycles through table, both rail slots, and floating. */
export function nextPlacement(p: DockPlacement): DockPlacement {
  return p === 'table' ? 'rail' : p === 'rail' ? 'rail-bottom' : p === 'rail-bottom' ? 'floating' : 'table';
}

/** Box is the part of a DOMRect the anchor reads. */
export interface Box { left: number; right: number; top: number; bottom: number }
/** TableAnchor is the near-table dock's fixed-position edges (px from the viewport's right and bottom) and its height cap. */
export interface TableAnchor { right: number; bottom: number; maxHeight: number }

const TABLE_GAP = 8;
const TABLE_INSET = 12;
const TABLE_TOP_ROOM = 16;
const TABLE_MIN_HEIGHT = 160;

/**
 * tableAnchor places the near-table dock: right-aligned with the action
 * button and just above it, growing upward no higher than the board's top.
 * With no action button mounted it sits in the board's bottom-right corner.
 */
export function tableAnchor(action: Box | null, board: Box, vp: Viewport): TableAnchor {
  const right = action ? vp.w - action.right : vp.w - board.right + TABLE_INSET;
  const bottom = action ? vp.h - action.top + TABLE_GAP : vp.h - board.bottom + TABLE_INSET;
  const maxHeight = Math.max(TABLE_MIN_HEIGHT, vp.h - bottom - (board.top + TABLE_TOP_ROOM));
  return { right: Math.round(right), bottom: Math.round(bottom), maxHeight: Math.round(maxHeight) };
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
