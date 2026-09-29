import type { EventBody } from '../../protocol';
import { transitionsFromEvents } from './events';
import { diffViews, lifeTransitions } from './snapshotdiff';
import type { ClientView, ModelEvent, ModelListener, Transition } from './types';

/**
 * gorge is the gorge adapter's step logic (sub-project 0). One live view step
 * covers the events in (prevSeq, nextSeq]. When the DVR already holds every
 * one of them — the spectator path, which receives events before the view —
 * they are mapped directly, in order. Otherwise — the seated path, whose
 * redacted events are backfilled concurrently with the view fetch — the two
 * views are diffed. Life always comes from the diff: damage to a player
 * changes life with no `life` event, and the tick shows the net change.
 */
export function stepTransitions(prev: ClientView, next: ClientView, prevSeq: number, nextSeq: number, events: readonly EventBody[]): Transition[] {
  const span = coveredSpan(events, prevSeq, nextSeq);
  if (span === null) return diffViews(prev, next);
  return [...transitionsFromEvents(span, prev, next), ...lifeTransitions(prev, next)];
}

/** coveredSpan is the events in (from, to] when every seq is present, else null. */
function coveredSpan(events: readonly EventBody[], from: number, to: number) {
  if (to <= from) return null;
  const want = to - from;
  const span = events.filter((b) => b.event.seq > from && b.event.seq <= to).map((b) => b.event);
  if (span.length !== want) return null;
  return span;
}

/** ModelBus is the adapters' listener set; emit is synchronous. */
export class ModelBus {
  #listeners = new Set<ModelListener>();

  on(fn: ModelListener): () => void {
    this.#listeners.add(fn);
    return () => this.#listeners.delete(fn);
  }

  emit(e: ModelEvent): void {
    for (const fn of this.#listeners) {
      try {
        fn(e);
      } catch {
        // A listener is presentation (motion, log); it must never break the
        // view assignment that follows the emit.
      }
    }
  }

  get size(): number {
    return this.#listeners.size;
  }
}
