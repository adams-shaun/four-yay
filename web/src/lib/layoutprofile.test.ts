import { describe, expect, it } from 'vitest';
import {
  cloneProfile,
  compactRows,
  defaultProfile,
  defaultSplit,
  migrateLegacy,
  PRESET_IDS,
  presetOf,
  rowCount,
  sameProfile,
  validate,
  withPreset,
} from './layoutprofile';

describe('layout profile model', () => {
  it('the default profile validates and is the Duel preset with the mirrored orientation', () => {
    const p = defaultProfile();
    expect(validate(JSON.parse(JSON.stringify(p)))).toEqual(p);
    expect(presetOf(p)).toBe('duel');
    expect(p.table.orientation).toBe('mirrored');
    expect(p.cards.stacking).toBe(true);
    expect(p.cards.overflow).toBe('overlap');
  });

  it('every preset validates and is recognised as itself', () => {
    for (const id of PRESET_IDS) {
      const p = withPreset(defaultProfile(), id);
      expect(validate(JSON.parse(JSON.stringify(p))), id).toEqual(p);
      expect(presetOf(p), id).toBe(id);
    }
  });

  it('a preset keeps the player\'s cards, hand and panels', () => {
    const p = defaultProfile();
    p.cards.overflow = 'wrap';
    p.hand.visible = 0.5;
    p.panels.rail = 'left';
    const q = withPreset(p, 'focus8');
    expect(q.table.arrangement).toBe('focus');
    expect(q.cards.overflow).toBe('wrap');
    expect(q.hand.visible).toBe(0.5);
    expect(q.panels.rail).toBe('left');
  });

  it('rejects corrupt blobs rather than half-applying them', () => {
    const good = JSON.parse(JSON.stringify(defaultProfile()));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- a deliberately malformed blob
    const bad = (mut: (o: Record<string, any>) => void) => {
      const o = JSON.parse(JSON.stringify(good));
      mut(o);
      return validate(o);
    };
    expect(bad((o) => (o.version = 2))).toBeNull();
    expect(bad((o) => (o.table.split = 0.9))).toBeNull();
    expect(bad((o) => (o.table.arrangement = 'spiral'))).toBeNull();
    expect(bad((o) => (o.regions.lands.row = 3))).toBeNull();
    expect(bad((o) => (o.regions.lands.weight = 'L'))).toBeNull();
    expect(bad((o) => (o.cards.overflow = 'shrink'))).toBeNull();
    expect(bad((o) => delete o.panels.prompt)).toBeNull();
    expect(bad((o) => (o.hand.visible = 0.1))).toBeNull();
    expect(validate(null)).toBeNull();
    expect(validate([])).toBeNull();
  });

  it('compacts a gap in the rows instead of rejecting it', () => {
    const o = JSON.parse(JSON.stringify(defaultProfile()));
    o.regions.creatures.row = 0;
    o.regions.lands.row = 2;
    o.regions.others.row = 2;
    const p = validate(o)!;
    expect(p.regions.lands.row).toBe(1);
    expect(rowCount(p)).toBe(2);
    expect(compactRows({ creatures: { ...p.regions.creatures, row: 2 }, lands: { ...p.regions.lands, row: 2 }, others: { ...p.regions.others, row: 2 } }).creatures.row).toBe(0);
  });

  it('sameProfile is deep equality and clone is independent', () => {
    const a = defaultProfile();
    const b = cloneProfile(a);
    expect(sameProfile(a, b)).toBe(true);
    b.panels.prompt.x = 0.1;
    expect(sameProfile(a, b)).toBe(false);
    expect(a.panels.prompt.x).not.toBe(0.1);
  });

  it('defaults payment prompts to the rail and round-trips rail width while accepting old profiles', () => {
    const p = defaultProfile();
    expect(p.panels.prompt.placement).toBe('dock');
    expect(p.panels.prompt.placement).not.toBe('table');
    p.panels.railWidth = 0.32;
    expect(p.panels.railWidth).not.toBe(defaultProfile().panels.railWidth);
    const roundTrip = validate(JSON.parse(JSON.stringify(p)));
    expect(roundTrip?.panels.railWidth).toBe(0.32);
    const old = JSON.parse(JSON.stringify(p));
    delete old.panels.railWidth;
    expect(validate(old)?.panels.railWidth).toBe(defaultProfile().panels.railWidth);
  });

  it('the splitter reset follows the seat count', () => {
    expect(defaultSplit(2)).toBe(0.4);
    expect(defaultSplit(4)).toBe(0.46);
    expect(defaultSplit(6)).toBe(0.52);
    expect(defaultSplit(8)).toBe(0.5);
  });
});

/**
 * Real pre-migration blobs: the exact strings the old layoutsettings.ts
 * saveLayout wrote (JSON.stringify of its cloneLayout, field order included),
 * one from the current shipped shape and one saved before the command zone
 * and the stepper toggle existed.
 */
const SHIPPED_BLOB =
  '{"version":1,"scale":{"creatures":1.2,"others":1,"lands":0.8,"command":1,"hand":1.1},"align":{"creatures":"center","others":"right","lands":"left","command":"left","hand":"center"},"handPeek":"always","steppersOnBoard":true}';
const PRE_COMMAND_BLOB =
  '{"version":1,"scale":{"creatures":1,"others":1,"lands":1,"hand":1},"align":{"creatures":"left","others":"left","lands":"right","hand":"center"},"handPeek":"never"}';

describe('migrating the legacy gorge.layoutsettings.v1 blob', () => {
  it('keeps each battlefield row\'s alignment as its region anchor and the peek as the hand', () => {
    const p = migrateLegacy(JSON.parse(SHIPPED_BLOB))!;
    expect(p).not.toBeNull();
    expect(p.regions.creatures.anchor).toBe('center');
    expect(p.regions.others.anchor).toBe('end');
    expect(p.regions.lands.anchor).toBe('start');
    expect(p.hand).toEqual({ visible: 1, raise: true });
    // The result is a valid profile in its own right.
    expect(validate(JSON.parse(JSON.stringify(p)))).toEqual(p);
  });

  it('a blob saved before the command zone existed still migrates', () => {
    const p = migrateLegacy(JSON.parse(PRE_COMMAND_BLOB))!;
    expect(p.regions.creatures.anchor).toBe('start');
    expect(p.regions.lands.anchor).toBe('end');
    expect(p.hand).toEqual({ visible: 0.5, raise: false });
  });

  it('hover peek is today\'s half-visible, rising hand', () => {
    const p = migrateLegacy({ ...JSON.parse(SHIPPED_BLOB), handPeek: 'hover' })!;
    expect(p.hand).toEqual({ visible: 0.5, raise: true });
  });

  it('something that is not a v1 layout blob does not migrate', () => {
    expect(migrateLegacy(null)).toBeNull();
    expect(migrateLegacy({ version: 2 })).toBeNull();
    expect(migrateLegacy('x')).toBeNull();
  });
});
