/**
 * dock.ts is the prompt dock's placement (UI rework spec §4: "docked at the
 * top of the rail by default … Floating is a layout setting; the prompt is
 * dragged by its grip, and the position is saved in the layout profile").
 * The layout profile (lib/layoutprofile.ts `panels.prompt`) owns both; this
 * module only converts between the profile's viewport fractions and the
 * dock's pixels, and keeps a dragged dock reachable. Client-side only.
 */
export type DockPlacement = 'rail' | 'floating';
export interface DockPoint { x: number; y: number }
export interface DockLayout {
  placement: DockPlacement;
  /** The floating dock's top-left in viewport pixels; null until first dragged (then it opens at a default spot). */
  position: DockPoint | null;
}

/** Viewport is the window size the fractions in a layout profile are measured against. */
export interface Viewport { w: number; h: number }

/** ProfilePrompt is the layout profile's `panels.prompt` (lib/layoutprofile.ts). */
export interface ProfilePrompt { placement: 'dock' | 'float'; x: number; y: number }

/**
 * dockFromProfile reads the dock's placement and floating position from the
 * layout profile, which stores the position as fractions of the viewport so
 * an exported profile means the same spot on another screen.
 */
export function dockFromProfile(p: ProfilePrompt, vp: Viewport): DockLayout {
  return {
    placement: p.placement === 'float' ? 'floating' : 'rail',
    position: { x: Math.round(p.x * vp.w), y: Math.round(p.y * vp.h) },
  };
}

/** profilePlacement is the profile's word for a dock placement. */
export function profilePlacement(p: DockPlacement): ProfilePrompt['placement'] {
  return p === 'floating' ? 'float' : 'dock';
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
