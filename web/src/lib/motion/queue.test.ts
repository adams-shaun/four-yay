import { describe, expect, it } from 'vitest';
import { MotionQueue, type MotionClock } from './queue';

function fakeClock() {
  let now = 0;
  let id = 0;
  const timers = new Map<number, { at: number; fn: () => void }>();
  const clock: MotionClock = {
    setTimeout: (fn, ms) => { timers.set(++id, { at: now + ms, fn }); return id; },
    clearTimeout: (t) => { timers.delete(t as number); },
  };
  const advance = (ms: number) => {
    const until = now + ms;
    for (;;) {
      const due = [...timers.entries()].filter(([, t]) => t.at <= until).sort((a, b) => a[1].at - b[1].at || a[0] - b[0])[0];
      if (!due) break;
      timers.delete(due[0]);
      now = due[1].at;
      due[1].fn();
    }
    now = until;
  };
  return { clock, advance };
}

function recorder(log: string[], name: string) {
  return () => { log.push(`start ${name}`); return { finish: () => log.push(`finish ${name}`) }; };
}

describe('MotionQueue', () => {
  it('plays batches in order, each after the last ends', () => {
    const { clock, advance } = fakeClock();
    const q = new MotionQueue(clock);
    const log: string[] = [];
    q.enqueue(100, recorder(log, 'a'));
    q.enqueue(50, recorder(log, 'b'));
    expect(log).toEqual(['start a']);
    advance(99);
    expect(log).toEqual(['start a']);
    advance(1);
    expect(log).toEqual(['start a', 'finish a', 'start b']);
    advance(50);
    expect(log).toEqual(['start a', 'finish a', 'start b', 'finish b']);
    expect(q.busy).toBe(false);
  });

  it('finishAll completes the running batch and drops the pending ones', () => {
    const { clock, advance } = fakeClock();
    const q = new MotionQueue(clock);
    const log: string[] = [];
    q.enqueue(100, recorder(log, 'a'));
    q.enqueue(100, recorder(log, 'b'));
    q.finishAll();
    advance(500);
    expect(log).toEqual(['start a', 'finish a']);
    expect(q.busy).toBe(false);
    expect(q.pending).toBe(0);
  });

  it('a backlog past the cap drops its oldest waiting batches', () => {
    const { clock, advance } = fakeClock();
    const q = new MotionQueue(clock, { maxPending: 1 });
    const log: string[] = [];
    q.enqueue(100, recorder(log, 'a'));
    q.enqueue(100, recorder(log, 'b'));
    q.enqueue(100, recorder(log, 'c'));
    advance(300);
    expect(log).toEqual(['start a', 'finish a', 'start c', 'finish c']);
  });

  it('an empty batch never starts; a throwing start is skipped', () => {
    const { clock, advance } = fakeClock();
    const q = new MotionQueue(clock);
    const log: string[] = [];
    q.enqueue(0, recorder(log, 'empty'));
    q.enqueue(10, () => { throw new Error('no dom'); });
    q.enqueue(10, recorder(log, 'b'));
    advance(10);
    expect(log).toEqual(['start b', 'finish b']);
  });
});
