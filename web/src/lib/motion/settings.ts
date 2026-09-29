/**
 * Motion speed (spec sub-project 5): a user setting, off / fast / normal,
 * default normal. `prefers-reduced-motion: reduce` forces off whatever is
 * saved. Client-side only: the value lives in this browser under
 * `gorge.motion.v1` and is never sent to the server.
 */
export type MotionSpeed = 'off' | 'fast' | 'normal';

export const MOTION_SPEEDS: readonly MotionSpeed[] = ['off', 'fast', 'normal'];
export const MOTION_SPEED_LABELS: Record<MotionSpeed, string> = { off: 'Off', fast: 'Fast', normal: 'Normal' };
export const DEFAULT_MOTION_SPEED: MotionSpeed = 'normal';
export const MOTION_KEY = 'gorge.motion.v1';

/** Timings, in ms, for one speed. */
export interface MotionTimings {
  /** one card's flight */
  flight: number;
  /** gap between successive beats of one batch */
  stagger: number;
  /** a floating number's life */
  float: number;
  /** a damage shake */
  shake: number;
  /** a life total counting to its new value */
  tick: number;
  /** a pile's landing bump */
  bump: number;
}

export const TIMINGS: Record<Exclude<MotionSpeed, 'off'>, MotionTimings> = {
  normal: { flight: 460, stagger: 80, float: 900, shake: 380, tick: 600, bump: 420 },
  fast: { flight: 220, stagger: 35, float: 520, shake: 220, tick: 280, bump: 240 },
};

export function isMotionSpeed(v: unknown): v is MotionSpeed {
  return typeof v === 'string' && (MOTION_SPEEDS as readonly string[]).includes(v);
}

/** loadMotionSpeed reads the saved speed; a missing, unreadable or foreign blob is the default. */
export function loadMotionSpeed(storage: Storage | null): MotionSpeed {
  try {
    const raw = storage?.getItem(MOTION_KEY);
    if (!raw) return DEFAULT_MOTION_SPEED;
    const blob = JSON.parse(raw) as { version?: unknown; speed?: unknown };
    return blob && blob.version === 1 && isMotionSpeed(blob.speed) ? blob.speed : DEFAULT_MOTION_SPEED;
  } catch {
    return DEFAULT_MOTION_SPEED;
  }
}

export function saveMotionSpeed(storage: Storage | null, speed: MotionSpeed): void {
  try {
    storage?.setItem(MOTION_KEY, JSON.stringify({ version: 1, speed }));
  } catch {
    // A browser refusing site data keeps the choice for this page only.
  }
}

/** effectiveSpeed applies the OS reduced-motion preference, which always wins. */
export function effectiveSpeed(speed: MotionSpeed, reducedMotion: boolean): MotionSpeed {
  return reducedMotion ? 'off' : speed;
}
