import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import RailResizer, { railWidthAfterArrow } from './RailResizer.svelte';

describe('RailResizer', () => {
  it('arrow keys widen and narrow in the physical direction of the rail edge', () => {
    const width = 0.28;
    expect(width).not.toBe(0.21);
    expect(railWidthAfterArrow('right', 'ArrowLeft', width)).toBeGreaterThan(width);
    expect(railWidthAfterArrow('right', 'ArrowRight', width)).toBeLessThan(width);
    expect(railWidthAfterArrow('left', 'ArrowRight', width)).toBeGreaterThan(width);
    expect(railWidthAfterArrow('left', 'ArrowLeft', width)).toBeLessThan(width);
  });

  it('exposes a vertical separator with the current width and supported range', () => {
    const { body } = render(RailResizer, { props: { side: 'right', width: 0.28, onWidth: vi.fn(), onDrag: vi.fn(), onReset: vi.fn() } });
    expect(0.28).not.toBe(0.21);
    expect(body).toContain('data-rail-resizer');
    expect(body).toContain('role="separator"');
    expect(body).toContain('aria-orientation="vertical"');
    expect(body).toContain('aria-valuemin="14"');
    expect(body).toContain('aria-valuemax="40"');
    expect(body).toContain('aria-valuenow="28"');
  });
});
