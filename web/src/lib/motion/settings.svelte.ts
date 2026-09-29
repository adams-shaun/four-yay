import { safeStorage } from '../storage';
import { effectiveSpeed, loadMotionSpeed, saveMotionSpeed, type MotionSpeed } from './settings';

const REDUCE = '(prefers-reduced-motion: reduce)';

/**
 * MotionStore is the one reactive motion setting the client reads (the
 * overlay, life ticks, the speed control). `effective` is what plays:
 * the saved speed unless the OS asks for reduced motion.
 */
export class MotionStore {
  speed = $state<MotionSpeed>('normal');
  reduced = $state(false);
  #storage: Storage | null;

  constructor(storage: Storage | null = safeStorage(), media: ((q: string) => MediaQueryList) | null = typeof matchMedia === 'function' ? matchMedia : null) {
    this.#storage = storage;
    this.speed = loadMotionSpeed(storage);
    if (media) {
      try {
        const mq = media(REDUCE);
        this.reduced = mq.matches;
        mq.addEventListener?.('change', (e) => { this.reduced = e.matches; });
      } catch {
        // No media queries (an embedding without them): honour the setting.
      }
    }
  }

  get effective(): MotionSpeed {
    return effectiveSpeed(this.speed, this.reduced);
  }

  setSpeed(speed: MotionSpeed): void {
    this.speed = speed;
    saveMotionSpeed(this.#storage, speed);
  }
}

export const motionStore = new MotionStore();
