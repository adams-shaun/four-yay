import type { Arrangement } from './layoutprofile';

/**
 * tablearrange is the ONE seat-placement function (UI rework spec §2). It
 * replaces the four duplicated corner maps the board used to carry
 * (seattable.seatCorner, Board's CELL, Quadrant's OUTER/FACING and
 * IdentityBar's CORNER), which only ever indexed four corners, so seats 5–8
 * rendered unpositioned.
 *
 * The table is always drawn from where the viewer sits: the viewer's own
 * board is at the bottom, and the opponents fill the top in TURN ORDER from
 * the viewer, left to right — seat viewer+1 first — so "who is to my left"
 * still reads off the felt. A spectator (a viewer not at the table, e.g.
 * view.NoSeat 255) has no seat of their own: the lowest seat takes the
 * bottom, exactly as the old seatCorner fallback drew seat 0.
 *
 * Arrangements of the opponents:
 *  - columns: one row, one column per opponent;
 *  - grid: two rows once there are more than three opponents;
 *  - focus: one opponent full-size (`focus`, default the first in turn
 *    order), the rest as side strips in a column to its right. With one
 *    opponent there is nothing to strip and focus is plain columns.
 */

export interface SeatCell {
  seat: number;
  /** 1-based CSS grid line coordinates within the opponents area */
  col: number;
  row: number;
  rowSpan: number;
  /** a focus side strip: header plus a creatures-only row */
  strip: boolean;
}

export interface TableArrangement {
  /** the seat drawn at the bottom; null for an empty table */
  own: number | null;
  /** true when `own` is the viewer's own seat (not a spectator's stand-in) */
  seated: boolean;
  /** opponents in turn order from `own` */
  opponents: number[];
  mode: Arrangement;
  /** CSS grid-template-columns / rows for the opponents area */
  columns: string;
  rows: string;
  cells: SeatCell[];
  /** the full-size opponent in focus mode, else null */
  focused: number | null;
}

export function arrangeTable(seats: readonly number[], viewer: number, arrangement: Arrangement, focus: number | null = null): TableArrangement {
  const order = [...seats].sort((a, b) => a - b);
  if (order.length === 0) {
    return { own: null, seated: false, opponents: [], mode: arrangement, columns: '1fr', rows: '1fr', cells: [], focused: null };
  }
  const seated = order.includes(viewer);
  const own = seated ? viewer : order[0];
  const at = order.indexOf(own);
  const opponents = [...order.slice(at + 1), ...order.slice(0, at)];
  const n = opponents.length;
  const base = { own, seated, opponents };

  if (n === 0) return { ...base, mode: arrangement, columns: '1fr', rows: '1fr', cells: [], focused: null };

  if (arrangement === 'focus' && n > 1) {
    const focused = focus !== null && opponents.includes(focus) ? focus : opponents[0];
    const side = opponents.filter((s) => s !== focused);
    const cells: SeatCell[] = [
      { seat: focused, col: 1, row: 1, rowSpan: side.length, strip: false },
      ...side.map((seat, i) => ({ seat, col: 2, row: i + 1, rowSpan: 1, strip: true })),
    ];
    return { ...base, mode: 'focus', columns: '2.2fr 1fr', rows: `repeat(${side.length}, 1fr)`, cells, focused };
  }

  const rows = arrangement === 'grid' && n > 3 ? 2 : 1;
  const cols = Math.ceil(n / rows);
  const cells = opponents.map((seat, i) => ({ seat, col: (i % cols) + 1, row: Math.floor(i / cols) + 1, rowSpan: 1, strip: false }));
  return { ...base, mode: arrangement === 'focus' ? 'columns' : arrangement, columns: `repeat(${cols}, minmax(0, 1fr))`, rows: `repeat(${rows}, minmax(0, 1fr))`, cells, focused: null };
}
