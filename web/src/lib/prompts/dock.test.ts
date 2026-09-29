import { describe, expect, it } from 'vitest';
import { clampPosition, dockFromProfile, fractionOf, profilePlacement } from './dock';

describe('dock placement from the layout profile', () => {
  it('maps the profile placement and viewport fractions to the dock', () => {
    expect(dockFromProfile({ placement: 'dock', x: 0.6, y: 0.12 }, { w: 1000, h: 500 })).toEqual({ placement: 'rail', position: { x: 600, y: 60 } });
    expect(dockFromProfile({ placement: 'float', x: 0, y: 1 }, { w: 1000, h: 500 }).placement).toBe('floating');
  });

  it('round-trips a dragged position through the profile fractions', () => {
    const vp = { w: 1280, h: 800 };
    const f = fractionOf({ x: 320, y: 200 }, vp);
    expect(f).toEqual({ x: 0.25, y: 0.25 });
    expect(dockFromProfile({ placement: 'float', ...f }, vp).position).toEqual({ x: 320, y: 200 });
    expect(fractionOf({ x: -5, y: 9000 }, vp)).toEqual({ x: 0, y: 1 });
    expect(profilePlacement('floating')).toBe('float');
    expect(profilePlacement('rail')).toBe('dock');
  });
});

describe('clampPosition', () => {
  it('keeps the grip row reachable after the window shrinks', () => {
    expect(clampPosition({ x: 1800, y: 1200 }, { w: 400, h: 300 }, { w: 1280, h: 800 })).toEqual({ x: 1136, y: 752 });
    expect(clampPosition({ x: -50, y: -20 }, { w: 400, h: 300 }, { w: 1280, h: 800 })).toEqual({ x: 0, y: 0 });
  });
});
