import { describe, expect, it } from 'vitest';
import { CARD_H_MAX, CARD_H_MIN, CARD_RATIO, GAP, HAND_ROW_MIN, pileWidth, rowFit, seatCardHeight, seatSize, shareOpponentSize, splitHeights } from './cardsizing';

describe('splitHeights — the board is one split plus one row unit', () => {
  it('the opponents take their share of everything but the strip', () => {
    const s = splitHeights({ boardH: 1000, stripH: 40, split: 0.4, ownRows: 2, hand: true, handVisible: 0.72 });
    expect(s.oppH).toBe(384);
    expect(s.ownH + s.handH).toBe(1000 - 40 - 384);
    // the hand is a fraction of a hand card, which is one row unit x 1.1
    expect(s.handH).toBe(Math.round(s.handCardH * 0.72));
  });

  it('no hand (a spectator) gives the viewer side all of the rest', () => {
    const s = splitHeights({ boardH: 800, stripH: 40, split: 0.5, ownRows: 2, hand: false, handVisible: 0.72 });
    expect(s).toEqual({ oppH: 380, ownH: 380, handH: 0, handCardH: 0 });
  });

  it('the hand row never gets shorter than the seat box', () => {
    const s = splitHeights({ boardH: 500, stripH: 40, split: 0.5, ownRows: 3, hand: true, handVisible: 0.3 });
    expect(s.handH).toBe(HAND_ROW_MIN);
  });
});

describe('seatCardHeight — rows box, then the fullest-region cap', () => {
  it('an empty board fills its rows', () => {
    const h = seatCardHeight({ rowsW: 2000, rowsH: 300, rows: [[{ weight: 1, units: 0 }], [{ weight: 1, units: 0 }]] });
    expect(h).toBe(Math.round((300 - GAP) / 2 - 8));
  });

  it('a crowded region caps the height so it fits with at most ~40% overlap', () => {
    const h = seatCardHeight({ rowsW: 600, rowsH: 400, rows: [[{ weight: 1, units: 12 }]] });
    const w = h * CARD_RATIO;
    expect(w * (12 * 0.6 + 0.4)).toBeLessThanOrEqual(600 + 1);
  });

  it('the weight decides each region\'s share of the row', () => {
    const narrow = seatCardHeight({ rowsW: 900, rowsH: 400, rows: [[{ weight: 1, units: 2 }, { weight: 0.4, units: 5 }]] });
    const wide = seatCardHeight({ rowsW: 900, rowsH: 400, rows: [[{ weight: 1, units: 2 }, { weight: 1.6, units: 5 }]] });
    expect(wide).toBeGreaterThan(narrow);
  });

  it('is clamped', () => {
    expect(seatCardHeight({ rowsW: 5000, rowsH: 5000, rows: [[{ weight: 1, units: 1 }]] })).toBe(CARD_H_MAX);
    expect(seatCardHeight({ rowsW: 50, rowsH: 30, rows: [[{ weight: 1, units: 30 }]] })).toBe(CARD_H_MIN);
  });
});

describe('seatSize, art tiles and the piles column', () => {
  const rows = [[{ weight: 1, units: 3 }], [{ weight: 1, units: 4 }, { weight: 0.7, units: 2 }]];

  it('a roomy panel keeps full cards and the piles column', () => {
    const s = seatSize({ panelW: 1200, panelH: 400, header: true, strip: false, rows, artBelow: 58 });
    expect(s.compact).toBe(false);
    expect(s.pileW).toBeGreaterThan(0);
  });

  it('a small panel turns into art tiles and hides the piles', () => {
    const s = seatSize({ panelW: 300, panelH: 160, header: true, strip: false, rows, artBelow: 58 });
    expect(s.compact).toBe(true);
    expect(s.pileW).toBe(0);
  });

  it('a strip never draws piles; artBelow 0 never compacts', () => {
    expect(seatSize({ panelW: 1200, panelH: 400, header: true, strip: true, rows, artBelow: 58 }).pileW).toBe(0);
    expect(seatSize({ panelW: 300, panelH: 160, header: true, strip: false, rows, artBelow: 0 }).compact).toBe(false);
    expect(pileWidth(30, false)).toBe(0);
  });

  it('all full-size opponents share the smallest size; strips keep theirs', () => {
    const a = { cardH: 150, cardW: 107, compact: false, pileW: 50, rowsW: 500 };
    const b = { cardH: 90, cardW: 64, compact: false, pileW: 50, rowsW: 500 };
    const c = { cardH: 40, cardW: 29, compact: true, pileW: 0, rowsW: 200 };
    const out = shareOpponentSize([a, b, c], [false, false, true], 70);
    expect(out.map((s) => s.cardH)).toEqual([90, 90, 40]);
    expect(out[0].compact).toBe(true); // 64px < 70px threshold
  });
});

describe('rowFit — overlap, scroll and wrap', () => {
  const five = [100, 100, 100, 100, 100];

  it('a row that fits keeps its gap', () => {
    expect(rowFit(600, five, 'overlap')).toEqual({ mode: 'fit', spacing: GAP, scale: 1 });
  });

  it('overlap tightens the spacing so the row ends at the region edge', () => {
    const f = rowFit(400, five, 'overlap');
    expect(f.mode).toBe('overlap');
    expect(500 + 4 * f.spacing).toBeCloseTo(400);
  });

  it('overlap never hides more than 78% of a card; past that it scrolls', () => {
    const f = rowFit(150, five, 'overlap');
    expect(f.mode).toBe('scroll');
    expect(f.spacing).toBeCloseTo(-78);
  });

  it('scroll keeps the gap', () => {
    expect(rowFit(400, five, 'scroll')).toEqual({ mode: 'scroll', spacing: GAP, scale: 1 });
  });

  it('wrap halves the cards onto two lines, and falls back to overlap when that does not fit', () => {
    expect(rowFit(400, five, 'wrap')).toEqual({ mode: 'wrap', spacing: GAP, scale: 0.5 });
    expect(rowFit(160, five, 'wrap').mode).not.toBe('wrap');
  });
});
