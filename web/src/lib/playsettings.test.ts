import { describe, expect, it } from 'vitest';
import {
  applyPreset,
  defaultSettings,
  loadSettings,
  noBreakpoints,
  normaliseWatchName,
  presetPatch,
  PRESETS,
  saveSettings,
  validate,
  withChange,
  withoutWatch,
  withWatch,
  type PlaySettings,
  type StoppableStep,
  type StepStop,
} from './playsettings';
import { STOPPABLE_STEPS } from './autopilot';

/** memStorage is a minimal in-memory Storage; throwing=true makes every method throw (private-mode browsers do). */
const memStorage = (throwing = false): Storage => {
  const map = new Map<string, string>();
  const guard = <T>(fn: () => T): T => {
    if (throwing) throw new DOMException('denied', 'SecurityError');
    return fn();
  };
  return {
    length: 0,
    clear: () => guard(() => map.clear()),
    getItem: (k: string) => guard(() => map.get(k) ?? null),
    key: (i: number) => guard(() => [...map.keys()][i] ?? null),
    removeItem: (k: string) => guard(() => void map.delete(k)),
    setItem: (k: string, v: string) => guard(() => void map.set(k, v)),
  } as unknown as Storage;
};

describe('presets', () => {
  it('defaults are casual, field for field', () => {
    expect(defaultSettings()).toEqual(PRESETS.casual);
    expect(defaultSettings().preset).toBe('casual');
  });

  it('PRESETS carries exactly the three named presets (custom is a label, not a preset)', () => {
    expect(Object.keys(PRESETS).sort()).toEqual(['casual', 'full-control', 'no-tells']);
  });

  it('casual matches the settings table exactly', () => {
    const s = PRESETS.casual;
    expect(s.version).toBe(3);
    expect(s.autoPass).toBe(true);
    expect(s.opponentSpell).toBe('if-respondable');
    expect(s.opponentAbility).toBe('if-respondable');
    expect(s.opponentTrigger).toBe('targets-me-if-respondable');
    expect(s.ownObjects).toBe('never');
    expect(s.passAfterAct).toBe(true);
    expect(s.autoOrderIdenticalTriggers).toBe(true);
    expect(s.autoOrderAllTriggers).toBe(true);
    expect(s.pacing).toEqual({ stepMs: 200, resolveMs: 400 });
    expect(s.logAutoPasses).toBe(true);
    for (const step of STOPPABLE_STEPS) {
      const expectedYours: StepStop = step === 'main1' || step === 'main2' ? 'smart' : 'off';
      const expectedOpponents: StepStop = step === 'declare-attackers' || step === 'end' ? 'smart' : 'off';
      expect(s.steps.yours[step as StoppableStep]).toBe(expectedYours);
      expect(s.steps.opponents[step as StoppableStep]).toBe(expectedOpponents);
    }
  });

  it('no-tells flips every opponent rule to always and keeps casual\u2019s steps', () => {
    const s = applyPreset('no-tells');
    expect(s.preset).toBe('no-tells');
    expect(s.autoPass).toBe(true);
    expect(s.opponentSpell).toBe('always');
    expect(s.opponentAbility).toBe('always');
    expect(s.opponentTrigger).toBe('always');
    expect(s.ownObjects).toBe('never');
    expect(s.passAfterAct).toBe(true);
    expect(s.autoOrderAllTriggers).toBe(true);
    expect(s.pacing).toEqual({ stepMs: 200, resolveMs: 400 });
    expect(s.steps).toEqual(PRESETS.casual.steps);
    expect(s.steps).not.toBe(PRESETS.casual.steps);
  });

  it('full-control is manual play with everything forced', () => {
    const s = applyPreset('full-control');
    expect(s.preset).toBe('full-control');
    expect(s.autoPass).toBe(false);
    expect(s.opponentSpell).toBe('always');
    expect(s.opponentAbility).toBe('always');
    expect(s.opponentTrigger).toBe('always');
    expect(s.ownObjects).toBe('if-respondable');
    expect(s.passAfterAct).toBe(false);
    expect(s.autoOrderIdenticalTriggers).toBe(false);
    expect(s.autoOrderAllTriggers).toBe(false);
    expect(s.pacing).toEqual({ stepMs: 0, resolveMs: 0 });
    expect(s.logAutoPasses).toBe(true);
    for (const step of STOPPABLE_STEPS) {
      expect(s.steps.yours[step as StoppableStep]).toBe('forced');
      expect(s.steps.opponents[step as StoppableStep]).toBe('forced');
    }
  });

  it('applyPreset returns an independent copy (mutating it does not touch the preset)', () => {
    const s = applyPreset('casual');
    s.steps.yours.main1 = 'off';
    s.pacing.stepMs = 999;
    s.opponentTrigger = 'never';
    expect(PRESETS.casual.steps.yours.main1).toBe('smart');
    expect(PRESETS.casual.pacing.stepMs).toBe(200);
    expect(PRESETS.casual.opponentTrigger).toBe('targets-me-if-respondable');
  });
});

describe('withChange', () => {
  it('marks an edited copy custom', () => {
    const next = withChange(defaultSettings(), { opponentTrigger: 'always' });
    expect(next.preset).toBe('custom');
    expect(next.opponentTrigger).toBe('always');
    // the original is untouched
    expect(defaultSettings().opponentTrigger).toBe('targets-me-if-respondable');
  });

  it('restores a preset\u2019s name when the edited copy deep-equals it', () => {
    const edited = withChange(defaultSettings(), { opponentTrigger: 'always', opponentSpell: 'always', opponentAbility: 'always' });
    expect(edited).toEqual(PRESETS['no-tells']);
    expect(edited.preset).toBe('no-tells');
    const back = withChange(edited, {
      opponentTrigger: 'targets-me-if-respondable',
      opponentSpell: 'if-respondable',
      opponentAbility: 'if-respondable',
    });
    expect(back).toEqual(PRESETS.casual);
    expect(back.preset).toBe('casual');
  });

  it('merges nested step and pacing patches field-wise', () => {
    const next = withChange(defaultSettings(), { steps: { yours: { main1: 'forced' } } } as Partial<PlaySettings>);
    expect(next.steps.yours.main1).toBe('forced');
    expect(next.steps.yours.main2).toBe('smart'); // untouched siblings survive
    expect(next.steps.opponents).toEqual(PRESETS.casual.steps.opponents);
    const paced = withChange(defaultSettings(), { pacing: { stepMs: 0 } } as Partial<PlaySettings>);
    expect(paced.pacing).toEqual({ stepMs: 0, resolveMs: 400 });
    expect(paced.preset).toBe('custom');
  });
});

describe('persistence', () => {
  it('loadSettings with an absent key returns the defaults', () => {
    expect(loadSettings(memStorage())).toEqual(defaultSettings());
    expect(loadSettings(null)).toEqual(defaultSettings());
  });

  it('loadSettings with a saved round-trip returns what was saved', () => {
    const st = memStorage();
    const s = applyPreset('no-tells');
    saveSettings(st, s);
    expect(loadSettings(st)).toEqual(s);
  });

  it.each(['not json at all', '{"version":1,', '[]', '"a string"', 'null'])('corrupt value %j falls back to defaults', (raw) => {
    const st = memStorage();
    st.setItem('gorge.playsettings.v1', raw);
    expect(loadSettings(st)).toEqual(defaultSettings());
  });

  it('a wrong-version value falls back to defaults (3 is now the live version; a 4 is corrupt)', () => {
    const st = memStorage();
    st.setItem('gorge.playsettings.v1', JSON.stringify({ ...PRESETS.casual, version: 4 }));
    expect(loadSettings(st)).toEqual(defaultSettings());
  });

  it('a missing or wrong-typed field falls back to defaults (no partial merge)', () => {
    const st = memStorage();
    const broken = { ...PRESETS.casual, opponentSpell: 'sometimes' };
    st.setItem('gorge.playsettings.v1', JSON.stringify(broken));
    expect(loadSettings(st)).toEqual(defaultSettings());
    const missing = { ...PRESETS.casual } as Partial<PlaySettings>;
    delete missing.passAfterAct;
    st.setItem('gorge.playsettings.v1', JSON.stringify(missing));
    expect(loadSettings(st)).toEqual(defaultSettings());
  });

  it('a throwing storage yields defaults on load and is swallowed on save', () => {
    const st = memStorage(true);
    expect(loadSettings(st)).toEqual(defaultSettings());
    expect(() => saveSettings(st, applyPreset('full-control'))).not.toThrow();
  });

  it('saveSettings with null storage is a no-op, not a throw', () => {
    expect(() => saveSettings(null, applyPreset('casual'))).not.toThrow();
  });

  it('the legacy per-table keys are deliberately NOT imported (they encode the old mana-tap-era defaults)', () => {
    const st = memStorage();
    st.setItem('gorge.stop.t1.0', JSON.stringify({ yours: ['main1'], opponents: [] }));
    st.setItem('gorge.actpass.t1.0', '1');
    expect(loadSettings(st)).toEqual(defaultSettings());
  });
});

describe('persistence migration — blob v1 to field-set v2 (fb-trigorder1)', () => {
  /** v1Blob builds a REAL v1-shaped blob: the pre-fb-trigorder1 PlaySettings — version 1, no autoOrderAllTriggers field at all. */
  const v1Blob = (base: Omit<PlaySettings, 'autoOrderAllTriggers'>): Record<string, unknown> => {
    const { autoOrderAllTriggers: _dropped, ...rest } = base as PlaySettings;
    void _dropped;
    return { ...rest, version: 1 } as unknown as Record<string, unknown>;
  };

  it('a real v1 casual blob loads with the new field defaulted to casual\'s value and every old field preserved', () => {
    const st = memStorage();
    st.setItem('gorge.playsettings.v1', JSON.stringify(v1Blob(PRESETS.casual)));
    const loaded = loadSettings(st);
    expect(loaded).toEqual(PRESETS.casual); // nothing lost, nothing re-labelled
    expect(loaded.autoOrderAllTriggers).toBe(true); // casual's value is the v1 default
    expect(loaded.autoOrderIdenticalTriggers).toBe(true);
    expect(loaded.preset).toBe('casual');
  });

  it('a real v1 blob of a NON-default configuration migrates field for field (preset label preserved, not reset to casual)', () => {
    const st = memStorage();
    const v1noTells = v1Blob({ ...PRESETS['no-tells'], preset: 'custom' }); // a blob edited off its label, as real storage carries
    st.setItem('gorge.playsettings.v1', JSON.stringify(v1noTells));
    const loaded = loadSettings(st);
    expect(loaded.preset).toBe('custom'); // the label is preserved as written, never rewritten by migration
    expect(loaded.opponentSpell).toBe('always');
    expect(loaded.opponentAbility).toBe('always');
    expect(loaded.opponentTrigger).toBe('always');
    expect(loaded.steps).toEqual(PRESETS['no-tells'].steps);
    expect(loaded.pacing).toEqual({ stepMs: 200, resolveMs: 400 });
    expect(loaded.autoOrderAllTriggers).toBe(true); // the only change: casual's default for the new field
  });

  it('a v2 blob missing the new field is corrupt (the field is REQUIRED at v2) and falls back to defaults', () => {
    const st = memStorage();
    const { autoOrderAllTriggers: _dropped, ...rest } = PRESETS.casual;
    void _dropped;
    st.setItem('gorge.playsettings.v1', JSON.stringify({ ...rest, version: 2 }));
    expect(loadSettings(st)).toEqual(defaultSettings());
  });

  it('saveSettings writes the live-version (3) blob to the SAME key (the key is not renamed — renaming would orphan every existing player)', () => {
    const st = memStorage();
    saveSettings(st, applyPreset('casual'));
    expect(st.getItem('gorge.playsettings.v1')).not.toBeNull();
    expect(st.getItem('gorge.playsettings.v2')).toBeNull();
    const written = JSON.parse(st.getItem('gorge.playsettings.v1') as string) as Record<string, unknown>;
    expect(written.version).toBe(3);
    expect(written.autoOrderAllTriggers).toBe(true);
    // and it round-trips through loadSettings unchanged
    expect(loadSettings(st)).toEqual(PRESETS.casual);
  });

  it('withChange over a migrated v1 blob relabels presets exactly as a native v2 copy would', () => {
    const st = memStorage();
    st.setItem('gorge.playsettings.v1', JSON.stringify(v1Blob(PRESETS.casual)));
    const migrated = loadSettings(st);
    expect(withChange(migrated, { opponentTrigger: 'always' }).preset).toBe('custom');
    expect(withChange(migrated, { opponentTrigger: 'always', opponentSpell: 'always', opponentAbility: 'always' }))
      .toEqual(PRESETS['no-tells']);
  });
});

describe('breakpoints (settings v3)', () => {
  it('every preset ships breakpoints all off', () => {
    for (const p of Object.values(PRESETS)) expect(p.breakpoints).toEqual(noBreakpoints());
    expect(noBreakpoints()).toEqual({ targetsMe: false, watchlist: [], attacked: false, stackDepth: 0 });
  });

  it('a v2 blob migrates: every field kept, breakpoints defaulted', () => {
    const v2 = { ...PRESETS['no-tells'], version: 2 } as Record<string, unknown>;
    delete v2.breakpoints;
    const st = memStorage();
    st.setItem('gorge.playsettings.v1', JSON.stringify(v2));
    const got = loadSettings(st);
    expect(got.version).toBe(3);
    expect(got.opponentSpell).toBe(PRESETS['no-tells'].opponentSpell);
    expect(got.breakpoints).toEqual(noBreakpoints());
  });

  it('a v3 blob round-trips its breakpoints', () => {
    const s = withChange(defaultSettings(), { breakpoints: { targetsMe: true, watchlist: ["Thassa's Oracle"], attacked: true, stackDepth: 3 } });
    const st = memStorage();
    saveSettings(st, s);
    expect(loadSettings(st).breakpoints).toEqual({ targetsMe: true, watchlist: ["Thassa's Oracle"], attacked: true, stackDepth: 3 });
  });

  it('a v3 blob with a bad breakpoints block is corrupt (defaults)', () => {
    for (const bad of [
      { targetsMe: 'yes', watchlist: [], attacked: false, stackDepth: 0 },
      { targetsMe: false, watchlist: 'x', attacked: false, stackDepth: 0 },
      { targetsMe: false, watchlist: [], attacked: false, stackDepth: 1 },
      { targetsMe: false, watchlist: [], attacked: false, stackDepth: 99 },
    ]) {
      expect(validate({ ...PRESETS.casual, breakpoints: bad })).toBeNull();
    }
  });

  it('validate normalises the watchlist: trims, drops empties, dedupes case-insensitively', () => {
    const got = validate({ ...PRESETS.casual, breakpoints: { ...noBreakpoints(), watchlist: ['  Oracle ', 'oracle', '', 'Rift'] } });
    expect(got?.breakpoints.watchlist).toEqual(['Oracle', 'Rift']);
  });

  it('preset relabel ignores breakpoints: a casual config with a watchlist still reads casual', () => {
    const s = withChange(defaultSettings(), { breakpoints: { ...noBreakpoints(), watchlist: ['Oracle'] } });
    expect(s.preset).toBe('casual');
  });

  it('applying a preset keeps the player’s breakpoints', () => {
    const mine = withChange(defaultSettings(), { breakpoints: { ...noBreakpoints(), attacked: true } });
    const next = withChange(mine, presetPatch('full-control'));
    expect(next.preset).toBe('full-control');
    expect(next.breakpoints.attacked).toBe(true);
  });

  it('watch helpers: add normalises and dedupes, remove is case-insensitive, cap is enforced', () => {
    expect(normaliseWatchName('  Rhystic Study ')).toBe('Rhystic Study');
    expect(normaliseWatchName('   ')).toBeNull();
    expect(normaliseWatchName('x'.repeat(61))).toBeNull();
    expect(withWatch(['Oracle'], 'oracle')).toEqual(['Oracle']);
    expect(withWatch(['Oracle'], 'Rift')).toEqual(['Oracle', 'Rift']);
    expect(withoutWatch(['Oracle', 'Rift'], 'ORACLE')).toEqual(['Rift']);
    const full = Array.from({ length: 50 }, (_, i) => `c${i}`);
    expect(withWatch(full, 'one more')).toEqual(full);
  });
});
