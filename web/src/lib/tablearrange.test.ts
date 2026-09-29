import { describe, expect, it } from 'vitest';
import { arrangeTable } from './tablearrange';

const seats = (n: number) => Array.from({ length: n }, (_, i) => i);

describe('arrangeTable — the one seat-placement function', () => {
  it('1v1: the viewer is at the bottom and the opponent above, whichever seat the viewer is', () => {
    expect(arrangeTable(seats(2), 0, 'columns')).toMatchObject({ own: 0, seated: true, opponents: [1] });
    expect(arrangeTable(seats(2), 1, 'columns')).toMatchObject({ own: 1, seated: true, opponents: [0] });
  });

  it('a spectator gets the lowest seat at the bottom, deterministically', () => {
    expect(arrangeTable(seats(2), 255, 'columns')).toMatchObject({ own: 0, seated: false, opponents: [1] });
    expect(arrangeTable([3, 1, 2], 255, 'columns')).toMatchObject({ own: 1, opponents: [2, 3] });
  });

  it('opponents run in turn order from the viewer, left to right (4 seats, viewer 2)', () => {
    const a = arrangeTable(seats(4), 2, 'columns');
    expect(a.opponents).toEqual([3, 0, 1]);
    expect(a.cells.map((c) => [c.seat, c.col, c.row])).toEqual([[3, 1, 1], [0, 2, 1], [1, 3, 1]]);
    expect(a.columns).toBe('repeat(3, minmax(0, 1fr))');
  });

  it('every seat of an 8-seat table is placed — no seat is ever left unpositioned', () => {
    for (const mode of ['columns', 'grid', 'focus'] as const) {
      for (let viewer = 0; viewer < 8; viewer++) {
        const a = arrangeTable(seats(8), viewer, mode);
        const placed = [a.own, ...a.cells.map((c) => c.seat)].sort((x, y) => x! - y!);
        expect(placed, `${mode} viewer ${viewer}`).toEqual(seats(8));
      }
    }
  });

  it('grid splits into two rows only past three opponents', () => {
    expect(arrangeTable(seats(4), 0, 'grid').rows).toBe('repeat(1, minmax(0, 1fr))');
    const six = arrangeTable(seats(6), 0, 'grid');
    expect(six.rows).toBe('repeat(2, minmax(0, 1fr))');
    expect(six.cells.map((c) => [c.col, c.row])).toEqual([[1, 1], [2, 1], [3, 1], [1, 2], [2, 2]]);
  });

  it('focus puts one opponent full-size and the rest in side strips; a click target can change it', () => {
    const a = arrangeTable(seats(8), 0, 'focus');
    expect(a.focused).toBe(1);
    expect(a.cells[0]).toEqual({ seat: 1, col: 1, row: 1, rowSpan: 6, strip: false });
    expect(a.cells.slice(1).every((c) => c.strip && c.col === 2)).toBe(true);
    expect(arrangeTable(seats(8), 0, 'focus', 5).focused).toBe(5);
    // A focus naming a seat that is not an opponent falls back to the first.
    expect(arrangeTable(seats(8), 0, 'focus', 0).focused).toBe(1);
  });

  it('focus with a single opponent is plain columns', () => {
    const a = arrangeTable(seats(2), 0, 'focus');
    expect(a.mode).toBe('columns');
    expect(a.cells).toEqual([{ seat: 1, col: 1, row: 1, rowSpan: 1, strip: false }]);
  });
});
