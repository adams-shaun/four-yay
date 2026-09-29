/**
 * hovered.svelte.ts tracks what the pointer is over, table-wide: the object
 * (any element carrying data-obj — a board tile, a hand card, a stack tile)
 * and the seat (the nearest data-seat / data-seat-anchor). It exists for the
 * keymap's view actions — zoom the card under the pointer, open the
 * graveyard or exile of the hovered seat — and for the arrows overlay, which
 * draws a stack item's target arrows only while that item is hovered.
 *
 * One delegated pointerover listener on the document, attached by the table
 * route, instead of a handler on every tile.
 */
export class Hovered {
  obj = $state<number | null>(null);
  seat = $state<number | null>(null);

  /** track reads one pointer target (exported for tests). */
  track(target: EventTarget | null): void {
    const el = target as Element | null;
    if (!el || typeof el.closest !== 'function') {
      this.obj = null;
      this.seat = null;
      return;
    }
    const o = el.closest('[data-obj]')?.getAttribute('data-obj') ?? null;
    const s = el.closest('[data-seat-anchor], [data-seat]');
    const seat = s?.getAttribute('data-seat-anchor') ?? s?.getAttribute('data-seat') ?? null;
    this.obj = o !== null && Number.isInteger(Number(o)) ? Number(o) : null;
    this.seat = seat !== null && Number.isInteger(Number(seat)) ? Number(seat) : null;
  }

  /** attach listens on a document; returns the detach. */
  attach(doc: Document): () => void {
    const on = (e: Event) => this.track(e.target);
    doc.addEventListener('pointerover', on, true);
    return () => doc.removeEventListener('pointerover', on, true);
  }
}

export const hovered = new Hovered();
