import { HAND_SCALE_DEFAULT, type Overflow } from './layoutprofile';

/**
 * cardsizing is the pure maths behind the table's card sizes (UI rework spec
 * §2, "Sizing rules" — these replace per-zone scale):
 *
 *  - the board's height splits into the opponents' share (the centre bar's
 *    `split`), the centre strip, the viewer's rows and a peeking hand row,
 *    the last two from ONE row unit, so there is no fixed hand band;
 *  - a seat's card height is its rows box divided by its rows, then capped
 *    so the fullest region fits with at most ~40% overlap;
 *  - every full-size opponent uses the SMALLEST opponent size, so the table
 *    reads evenly; the viewer's own board is sized separately;
 *  - below the art-tile threshold a card renders as an art tile, and the
 *    zone-piles column hides (the header counts replace it).
 *
 * No DOM: the component measures the board once (a ResizeObserver) and
 * everything else is computed here, so a drag of the centre bar only
 * changes numbers, never re-measures the board.
 */

/** card width : height */
export const CARD_RATIO = 63 / 88;
export const CARD_H_MIN = 28;
export const CARD_H_MAX = 190;
/** the most a card may be covered by its neighbour before a row stops shrinking (the ~40% rule) */
export const OVERLAP_CAP = 0.4;
/** a hand card is this many row units tall */
export const HAND_K = 1.1;
/** the hand row never gets shorter than the seat box needs */
export const HAND_ROW_MIN = 96;
/** px between rows / regions / cards */
export const GAP = 6;
/** vertical breathing room inside a row for the pills that overhang a card */
export const ROW_PAD = 8;
/** an opponent's header bar */
export const HEADER_H = 34;
/** a focus side strip is denser: a slimmer bar, a hairline of padding, smaller cards allowed */
export const STRIP_HEADER_H = 28;
export const STRIP_PAD = 2;
export const STRIP_CARD_H_MIN = 16;
/** piles narrower than this are not worth drawing */
export const PILE_MIN_W = 24;
export const PILE_MAX_W = 64;

export interface SplitInput {
  boardH: number;
  stripH: number;
  split: number;
  ownRows: number;
  hand: boolean;
  handVisible: number;
  handScale?: number;
}

export interface SplitHeights {
  oppH: number;
  ownH: number;
  handH: number;
  /** a hand card's full height */
  handCardH: number;
}

/** splitHeights divides the board's height between the opponents, the strip, the viewer's rows and the hand. */
export function splitHeights(i: SplitInput): SplitHeights {
  const avail = Math.max(0, i.boardH - i.stripH);
  const oppH = Math.round(avail * i.split);
  const rest = avail - oppH;
  if (!i.hand) return { oppH, ownH: rest, handH: 0, handCardH: 0 };
  const handScale = i.handScale ?? HAND_SCALE_DEFAULT;
  const handUnit = HAND_K * handScale;
  const unit = (rest - GAP * 2) / (i.ownRows + i.handVisible * handUnit);
  const handCardH = Math.max(60, unit * handUnit);
  const handH = Math.min(rest, Math.max(HAND_ROW_MIN, Math.round(handCardH * i.handVisible)));
  return { oppH, ownH: rest - handH, handH, handCardH };
}

/** One region's demand: its width share and how many card widths it holds. */
export interface RegionDemand {
  weight: number;
  units: number;
}

export interface SeatBoxInput {
  /** the rows box: the panel minus header, piles column and padding */
  rowsW: number;
  rowsH: number;
  rows: RegionDemand[][];
  /** vertical room per row for overhanging pills (default ROW_PAD) */
  rowPad?: number;
  /** the smallest card height allowed (default CARD_H_MIN) */
  minH?: number;
}

/** seatCardHeight is one seat's card height from its rows box, capped by its fullest region. */
export function seatCardHeight(i: SeatBoxInput): number {
  const n = Math.max(1, i.rows.length);
  let h = (i.rowsH - (n - 1) * GAP) / n - (i.rowPad ?? ROW_PAD);
  for (const row of i.rows) {
    const total = row.reduce((s, r) => s + r.weight, 0) || 1;
    const regionGaps = (row.length - 1) * GAP;
    for (const r of row) {
      if (r.units <= 0) continue;
      const w = ((i.rowsW - regionGaps) * r.weight) / total;
      // The fullest region fits when each card but the last shows (1 - cap) of itself.
      const cw = w / (r.units * (1 - OVERLAP_CAP) + OVERLAP_CAP);
      h = Math.min(h, cw / CARD_RATIO);
    }
  }
  return Math.round(Math.min(CARD_H_MAX, Math.max(i.minH ?? CARD_H_MIN, h)));
}

/** pileWidth sizes one zone pile from the panel body's height; 0 means the column is hidden. */
export function pileWidth(bodyH: number, compact: boolean): number {
  if (compact) return 0;
  const w = Math.min(PILE_MAX_W, ((bodyH - GAP) / 2) * CARD_RATIO);
  return w < PILE_MIN_W ? 0 : Math.floor(w);
}

/** pilesColumnWidth is the column's total width (two piles side by side plus gaps), 0 when hidden. */
export function pilesColumnWidth(pileW: number): number {
  return pileW > 0 ? pileW * 2 + GAP * 2 : 0;
}

export interface SeatPanelInput {
  panelW: number;
  panelH: number;
  header: boolean;
  /** a focus side strip: creatures only, no piles */
  strip: boolean;
  rows: RegionDemand[][];
  artBelow: number;
}

export interface SeatSize {
  cardH: number;
  cardW: number;
  compact: boolean;
  pileW: number;
  rowsW: number;
}

/**
 * seatSize sizes one seat panel. The piles column is sized first from the
 * panel's body height; the rows box is what is left. A card below the
 * art-tile threshold makes the panel compact, which hides the piles and
 * re-sizes the rows with that width back.
 */
export function seatSize(i: SeatPanelInput): SeatSize {
  const pad = i.strip ? STRIP_PAD : GAP;
  const bodyH = Math.max(0, i.panelH - (i.header ? (i.strip ? STRIP_HEADER_H : HEADER_H) : 0) - pad * 2);
  const size = (pileW: number): SeatSize => {
    const rowsW = Math.max(0, i.panelW - pilesColumnWidth(pileW) - pad * 2);
    const cardH = i.strip
      ? seatCardHeight({ rowsW, rowsH: bodyH, rows: i.rows, rowPad: STRIP_PAD, minH: STRIP_CARD_H_MIN })
      : seatCardHeight({ rowsW, rowsH: bodyH, rows: i.rows });
    const cardW = Math.round(cardH * CARD_RATIO);
    return { cardH, cardW, compact: cardW < i.artBelow, pileW, rowsW };
  };
  const first = size(i.strip ? 0 : pileWidth(bodyH, false));
  if (first.compact && first.pileW > 0) return { ...size(0), compact: true };
  return first;
}

/**
 * shareOpponentSize applies "all opponents share the smallest opponent card
 * size" to the full-size opponents (strips keep their own). The compact flag
 * is re-derived at the shared size.
 */
export function shareOpponentSize(sizes: SeatSize[], strip: boolean[], artBelow: number): SeatSize[] {
  const full = sizes.filter((_, i) => !strip[i]);
  if (full.length === 0) return sizes;
  const h = Math.min(...full.map((s) => s.cardH));
  return sizes.map((s, i) => {
    if (strip[i]) return s;
    const cardW = Math.round(h * CARD_RATIO);
    return { ...s, cardH: h, cardW, compact: cardW < artBelow };
  });
}

export interface RowFit {
  mode: 'fit' | 'overlap' | 'scroll' | 'wrap';
  /** px between one card's right edge and the next card's left edge; negative overlaps */
  spacing: number;
  /** 0.5 when a wrap halves the cards onto two lines */
  scale: number;
}

/** the least of a card that stays visible under overlap */
export const MIN_VISIBLE = 0.22;

/**
 * rowFit decides how one region lays its cards out when they may not fit:
 * overlap tightens the spacing (never hiding more than 78% of a card, then
 * scrolls), scroll keeps the gap and scrolls, wrap halves the cards onto two
 * lines and falls back to overlap when even that does not fit — so no
 * permanent is ever hidden.
 */
export function rowFit(regionW: number, widths: number[], overflow: Overflow): RowFit {
  const n = widths.length;
  const sum = widths.reduce((a, b) => a + b, 0);
  if (n <= 1 || sum + (n - 1) * GAP <= regionW) return { mode: 'fit', spacing: GAP, scale: 1 };
  const overlap = (): RowFit => {
    const spacing = (regionW - sum) / (n - 1);
    const floor = -Math.min(...widths) * (1 - MIN_VISIBLE);
    return spacing >= floor ? { mode: 'overlap', spacing, scale: 1 } : { mode: 'scroll', spacing: floor, scale: 1 };
  };
  if (overflow === 'scroll') return { mode: 'scroll', spacing: GAP, scale: 1 };
  if (overflow === 'wrap') {
    // Two lines of half-size cards: greedy line fill.
    let lines = 1;
    let x = 0;
    for (const w of widths.map((v) => v / 2)) {
      if (x > 0 && x + GAP + w > regionW) {
        lines++;
        x = w;
      } else x += (x > 0 ? GAP : 0) + w;
    }
    if (lines <= 2) return { mode: 'wrap', spacing: GAP, scale: 0.5 };
    return overlap();
  }
  return overlap();
}
