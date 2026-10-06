import { describe, expect, it } from 'vitest';
import { OBSTACLE_GAP, overlaps, placeFloating, tableAnchor } from './dock';

const vp = { w: 1000, h: 700 };
// A Feedback-shaped obstacle in the bottom-right corner.
const fb = { left: 900, right: 992, top: 664, bottom: 692 };
const size = { w: 400, h: 300 };
const rectAt = (p: { x: number; y: number }) => ({ left: p.x, right: p.x + size.w, top: p.y, bottom: p.y + size.h });

describe('overlaps', () => {
  it('needs positive area: touching edges do not overlap', () => {
    expect(overlaps({ left: 0, right: 10, top: 0, bottom: 10 }, { left: 10, right: 20, top: 0, bottom: 10 })).toBe(false);
    expect(overlaps({ left: 0, right: 10, top: 0, bottom: 10 }, { left: 9, right: 20, top: 9, bottom: 20 })).toBe(true);
  });
});

describe('placeFloating', () => {
  it('leaves a position that is already clear alone, and ignores a missing obstacle', () => {
    expect(placeFloating({ x: 100, y: 100 }, size, vp, fb)).toEqual({ x: 100, y: 100 });
    expect(placeFloating({ x: 800, y: 600 }, size, vp, null)).toEqual({ x: 800, y: 600 });
  });

  it('moves a dock over the button the shortest way out, keeping OBSTACLE_GAP clear', () => {
    const aimed = { x: 850, y: 500 };
    // Precondition: the aimed spot really covers the button.
    expect(overlaps(rectAt(aimed), fb)).toBe(true);
    const p = placeFloating(aimed, size, vp, fb);
    expect(overlaps(rectAt(p), fb)).toBe(false);
    expect(fb.top - rectAt(p).bottom >= OBSTACLE_GAP || fb.left - rectAt(p).right >= OBSTACLE_GAP).toBe(true);
    expect(p.y).toBeGreaterThanOrEqual(0);
  });

  it('is deterministic and idempotent: re-placing a placed dock does not move it', () => {
    const p = placeFloating({ x: 850, y: 600 }, size, vp, fb);
    expect(placeFloating(p, size, vp, fb)).toEqual(p);
    expect(placeFloating({ x: 850, y: 600 }, size, vp, fb)).toEqual(p);
  });

  it('goes as high as the viewport allows when neither exit fits', () => {
    const tall = { w: 400, h: 700 };
    const p = placeFloating({ x: 600, y: 0 }, tall, vp, { left: 0, right: 1000, top: 664, bottom: 692 });
    expect(p.y).toBe(0);
  });
});

describe('tableAnchor with an obstacle', () => {
  const board = { left: 0, right: 1000, top: 0, bottom: 700 };
  it('lifts a corner-fallback dock above the obstacle, and leaves a dock that already clears it', () => {
    const lifted = tableAnchor(null, board, vp, fb);
    expect(lifted.bottom).toBe(vp.h - fb.top + OBSTACLE_GAP);
    expect(tableAnchor(null, board, vp, null).bottom).toBe(12);
    const action = { left: 700, right: 800, top: 500, bottom: 560 };
    expect(tableAnchor(action, board, vp, fb).bottom).toBe(tableAnchor(action, board, vp, null).bottom);
  });
});
