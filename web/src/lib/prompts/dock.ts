/**
 * dock.ts is the prompt dock's placement (UI rework spec §4: "docked at the
 * top of the rail by default … Floating is a layout setting; the prompt is
 * dragged by its grip, and the position is saved in the layout profile").
 * Until the layout profiles (sub-project 2) carry it, the placement and the
 * floating position are a small local default under their own key — a
 * client-side view setting, never sent to the server.
 */
export type DockPlacement = 'rail' | 'floating';
export interface DockPoint { x: number; y: number }
export interface DockLayout {
  placement: DockPlacement;
  /** The floating dock's top-left in viewport pixels; null until first dragged (then it opens at a default spot). */
  position: DockPoint | null;
}

export const DOCK_KEY = 'gorge.promptdock.v1';

export function defaultDockLayout(): DockLayout {
  return { placement: 'rail', position: null };
}

const finite = (n: unknown): n is number => typeof n === 'number' && Number.isFinite(n);

/** readDockLayout validates a stored blob; anything malformed is null (the caller falls back to the default). */
export function readDockLayout(v: unknown): DockLayout | null {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (o.version !== 1 || (o.placement !== 'rail' && o.placement !== 'floating')) return null;
  let position: DockPoint | null = null;
  if (o.position !== null && o.position !== undefined) {
    const p = o.position as Record<string, unknown>;
    if (typeof p !== 'object' || !finite(p.x) || !finite(p.y)) return null;
    position = { x: p.x, y: p.y };
  }
  return { placement: o.placement, position };
}

export function loadDockLayout(storage: Storage | null): DockLayout {
  try {
    const raw = storage?.getItem(DOCK_KEY) ?? null;
    if (raw === null) return defaultDockLayout();
    return readDockLayout(JSON.parse(raw)) ?? defaultDockLayout();
  } catch {
    return defaultDockLayout();
  }
}

export function saveDockLayout(storage: Storage | null, l: DockLayout): void {
  try {
    storage?.setItem(DOCK_KEY, JSON.stringify({ version: 1, placement: l.placement, position: l.position }));
  } catch {
    /* private mode or quota: keep the in-memory copy */
  }
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

/** defaultFloatPosition opens a never-dragged floating dock over the lower middle of the board, clear of the rail. */
export function defaultFloatPosition(size: { w: number; h: number }, viewport: { w: number; h: number }): DockPoint {
  return clampPosition({ x: (viewport.w - size.w) / 2 - 160, y: viewport.h - size.h - 200 }, size, viewport);
}
