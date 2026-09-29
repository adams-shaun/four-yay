import { describe, expect, it } from 'vitest';
import { clampPosition, dockFromProfile, fractionOf, nextPlacement, profilePlacement, tableAnchor } from './dock';

describe('dock placement from the layout profile', () => {
  it('maps the profile placement and viewport fractions to the dock', () => {
    expect(dockFromProfile({ placement: 'dock', x: 0.6, y: 0.12 }, { w: 1000, h: 500 })).toEqual({ placement: 'rail', position: { x: 600, y: 60 } });
    expect(dockFromProfile({ placement: 'float', x: 0, y: 1 }, { w: 1000, h: 500 }).placement).toBe('floating');
    expect(dockFromProfile({ placement: 'table', x: 0, y: 0 }, { w: 1000, h: 500 }).placement).toBe('table');
    expect(profilePlacement('table')).toBe('table');
    expect(dockFromProfile({ placement: 'dock-bottom', x: 0, y: 0 }, { w: 1000, h: 500 }).placement).toBe('rail-bottom');
    expect(profilePlacement('rail-bottom')).toBe('dock-bottom');
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

describe('the near-table placement', () => {
  it('cycles through table, both rail docks, and floating from the dock toggle', () => {
    expect(nextPlacement('table')).toBe('rail');
    expect(nextPlacement('rail')).toBe('rail-bottom');
    expect(nextPlacement('rail-bottom')).toBe('floating');
    expect(nextPlacement('floating')).toBe('table');
    expect(nextPlacement('rail')).not.toBe('floating');
  });

  it('sits right-aligned just above the action button and grows up to the board top', () => {
    const a = tableAnchor({ left: 1000, right: 1240, top: 900, bottom: 1000 }, { left: 0, right: 1264, top: 0, bottom: 1000 }, { w: 1600, h: 1000 });
    expect(a).toEqual({ right: 360, bottom: 108, maxHeight: 876 });
  });

  it('falls back to the board bottom-right corner when no action button is mounted', () => {
    const a = tableAnchor(null, { left: 0, right: 1264, top: 40, bottom: 1000 }, { w: 1600, h: 1000 });
    expect(a).toEqual({ right: 348, bottom: 12, maxHeight: 932 });
  });

  it('keeps a usable height on a very short board', () => {
    const a = tableAnchor({ left: 100, right: 300, top: 60, bottom: 120 }, { left: 0, right: 400, top: 40, bottom: 120 }, { w: 400, h: 120 });
    expect(a.maxHeight).toBe(160);
  });
});
