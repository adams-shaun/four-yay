import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import RailResizer from './RailResizer.svelte';

describe('RailResizer', () => {
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
