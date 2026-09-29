/**
 * MotionQueue plays batches one after another (spec sub-project 5: "a queue
 * plays transitions in order"). It knows nothing about the DOM: a batch is a
 * duration plus a `start` callback that begins its animations and returns a
 * handle to finish them early. The clock is injected so tests are exact.
 *
 * Motion never blocks input, so the queue is always interruptible:
 * `finishAll` completes the running batch at once and drops everything
 * pending (the board already shows the end state of every batch — motion
 * only replays how it got there). A backlog longer than `maxPending` drops
 * its oldest waiting batches for the same reason: falling behind the game is
 * worse than skipping a replay.
 */

export interface MotionClock {
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(id: unknown): void;
}

export interface Running {
  finish(): void;
}

interface Batch {
  duration: number;
  start: () => Running;
}

export const DEFAULT_MAX_PENDING = 2;

const realClock: MotionClock = {
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (id) => clearTimeout(id as Parameters<typeof clearTimeout>[0]),
};

export class MotionQueue {
  #clock: MotionClock;
  #maxPending: number;
  #pending: Batch[] = [];
  #running: { handle: Running; timer: unknown } | null = null;

  constructor(clock: MotionClock = realClock, opts: { maxPending?: number } = {}) {
    this.#clock = clock;
    this.#maxPending = opts.maxPending ?? DEFAULT_MAX_PENDING;
  }

  get busy(): boolean {
    return this.#running !== null;
  }

  get pending(): number {
    return this.#pending.length;
  }

  enqueue(duration: number, start: () => Running): void {
    if (duration <= 0) return;
    this.#pending.push({ duration, start });
    while (this.#pending.length > this.#maxPending) this.#pending.shift();
    if (!this.#running) this.#next();
  }

  finishAll(): void {
    this.#pending = [];
    const r = this.#running;
    this.#running = null;
    if (r) {
      this.#clock.clearTimeout(r.timer);
      r.handle.finish();
    }
  }

  #next(): void {
    const b = this.#pending.shift();
    if (!b) return;
    let handle: Running;
    try {
      handle = b.start();
    } catch {
      this.#next();
      return;
    }
    const timer = this.#clock.setTimeout(() => {
      if (this.#running?.timer !== timer) return;
      this.#running = null;
      handle.finish();
      this.#next();
    }, b.duration);
    this.#running = { handle, timer };
  }
}
