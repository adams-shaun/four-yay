import { STOPPABLE_STEPS, type TurnSide } from './autopilot';

/**
 * playsettings is the pure settings model behind the auto-pass experience:
 * named presets (casual / no-tells / full-control), per-step stop rules, and
 * the opponent-object rules decide() (lib/autopilot) consumes. It is pure
 * TypeScript — no Svelte runes, no I/O except the two storage functions —
 * so the wiring task (prio3) can build UI on it and the tests can pin every
 * preset field exactly.
 *
 * Persistence is ONE global localStorage key, `gorge.playsettings.v1` (the
 * KEY string predates the v2 field bump and is kept — see the KEY comment),
 * not per table: the old per-table stop keys are exactly why settings appeared
 * to "reset" (every new table started from defaults again). A preference is
 * a property of the player, not of the table.
 */

export type Preset = 'casual' | 'no-tells' | 'full-control' | 'custom';

/** PresetName is the clickable presets: 'custom' is earned by editing, never picked. */
export type PresetName = Exclude<Preset, 'custom'>;

/**
 * StepStop is one step's stop rule:
 *  - 'off'    — never stop at that step;
 *  - 'smart'  — stop only if the window is actionable() (there is something
 *               to do; since lib/castable a mana-only window whose seat
 *               carries a projected potential action — the server's own
 *               legal-offer walk, rv2c — counts too); a mana-only window
 *               with an empty projection still passes;
 *  - 'forced' — stop whenever priority is posed at that step, even with
 *               nothing to do.
 */
export type StepStop = 'off' | 'smart' | 'forced';

/** How an opponent-controlled object on top of the stack is treated. */
export type OpponentObjectRule = 'if-respondable' | 'always' | 'never';

/**
 * How an opponent-controlled TRIGGER on top of the stack is treated. The
 * extra 'targets-me-if-respondable' is casual's value: an opponent trigger
 * is everyday noise (mana abilities, upkeep draws), so stop only when it
 * actually points at us AND we could respond.
 */
export type OpponentTriggerRule = 'targets-me-if-respondable' | 'if-respondable' | 'always' | 'never';

/** How one of the seat's OWN objects on top of the stack is treated. */
export type OwnObjectRule = 'never' | 'if-respondable';

/**
 * StoppableStep is STOPPABLE_STEPS's ten names as a type: the steps that
 * grant priority, so a stop there is meaningful. Kept in sync with
 * autopilot.STOPPABLE_STEPS (the single source of truth); the preset test
 * asserts the two agree.
 */
export type StoppableStep =
  | 'upkeep' | 'draw' | 'main1' | 'begin-combat' | 'declare-attackers'
  | 'declare-blockers' | 'combat-damage' | 'end-combat' | 'main2' | 'end';

/**
 * Breakpoints are the player's "pause on X" rules (UI rework spec §1). They
 * stop auto-pass even where the stack and step rules would pass. They belong
 * to the PLAYER, not to a preset: every preset ships them off, applying a
 * preset never touches them, and the preset label ignores them.
 */
export interface Breakpoints {
  /** an opponent's object on top of the stack targets you or a permanent you control */
  targetsMe: boolean;
  /** an opponent's spell or ability from one of these card names is on top of the stack (case-insensitive exact name) */
  watchlist: string[];
  /** creatures are attacking you (first priority window of that combat) */
  attacked: boolean;
  /** the stack holds at least this many objects; 0 = off */
  stackDepth: number;
}

export const MAX_WATCHLIST = 50;
export const MAX_WATCH_NAME = 60;
/** STACK_DEPTHS is every legal stackDepth: 0 (off) or 2..5. A depth of 1 would stop on every object, which is the 'always' stack rule's job. */
export const STACK_DEPTHS: readonly number[] = [0, 2, 3, 4, 5];

export function noBreakpoints(): Breakpoints {
  return { targetsMe: false, watchlist: [], attacked: false, stackDepth: 0 };
}

/** normaliseWatchName trims a card name; empty or longer than MAX_WATCH_NAME is rejected. */
export function normaliseWatchName(raw: string): string | null {
  if (typeof raw !== 'string') return null;
  const name = raw.trim();
  return name.length === 0 || name.length > MAX_WATCH_NAME ? null : name;
}

/** withWatch returns the list with one more name: normalised, deduped case-insensitively, capped at MAX_WATCHLIST. */
export function withWatch(list: readonly string[], raw: string): string[] {
  const name = normaliseWatchName(raw);
  if (name === null || list.length >= MAX_WATCHLIST) return [...list];
  if (list.some((n) => n.toLowerCase() === name.toLowerCase())) return [...list];
  return [...list, name];
}

/** withoutWatch drops one name, case-insensitively. */
export function withoutWatch(list: readonly string[], name: string): string[] {
  return list.filter((n) => n.toLowerCase() !== name.toLowerCase());
}

/** cloneBreakpoints is an independent copy: the watchlist array is never shared. */
export function cloneBreakpoints(b: Breakpoints): Breakpoints {
  return { ...b, watchlist: [...b.watchlist] };
}

export interface PlaySettings {
  version: 3;
  /** which named preset these settings match, or 'custom' after any edit */
  preset: Preset;
  /** master switch: false means decide() stops at every window (manual play) */
  autoPass: boolean;
  /** an opponent's spell on top of the stack */
  opponentSpell: OpponentObjectRule;
  /** an opponent's activated ability on top of the stack */
  opponentAbility: OpponentObjectRule;
  /** an opponent's triggered ability on top of the stack */
  opponentTrigger: OpponentTriggerRule;
  /** the seat's own spell/ability/trigger on top of the stack */
  ownObjects: OwnObjectRule;
  /** per-step stop rules, split by whose turn it is */
  steps: { yours: Record<StoppableStep, StepStop>; opponents: Record<StoppableStep, StepStop> };
  /** pass the seat's next priority window once after it takes a real action */
  passAfterAct: boolean;
  /** auto-order identical simultaneous triggers; consumed by prio6, not decide() */
  autoOrderIdenticalTriggers: boolean;
  /** auto-order EVERY simultaneous-trigger ask (fb-trigorder1), not only identical ones; consumed by seatpanel's auto-order path, not decide() */
  autoOrderAllTriggers: boolean;
  /** artificial pacing for auto-passing; consumed by prio5, not decide() */
  pacing: { stepMs: number; resolveMs: number };
  /** record auto-passes in the game log; consumed by prio5, not decide() */
  logAutoPasses: boolean;
  /** the player's pause-on-X rules; never part of a preset (see Breakpoints) */
  breakpoints: Breakpoints;
}

/** The ten stoppable step names, in engine order (autopilot.STOPPABLE_STEPS). */
const STEPPABLE = STOPPABLE_STEPS as readonly StoppableStep[];

/** steps builds a full Record<StoppableStep, StepStop>, defaulting 'off'. */
function steps(yours: Partial<Record<StoppableStep, StepStop>>, opponents: Partial<Record<StoppableStep, StepStop>>):
  PlaySettings['steps'] {
  const full = (partial: Partial<Record<StoppableStep, StepStop>>): Record<StoppableStep, StepStop> => {
    const out = {} as Record<StoppableStep, StepStop>;
    for (const s of STEPPABLE) out[s] = partial[s] ?? 'off';
    return out;
  };
  return { yours: full(yours), opponents: full(opponents) };
}

function mkSettings(p: Exclude<Preset, 'custom'>, fields: Omit<PlaySettings, 'version' | 'preset' | 'breakpoints'>): PlaySettings {
  return { version: 3, preset: p, ...fields, breakpoints: noBreakpoints() };
}

/** allSteps builds a full Record<StoppableStep, StepStop> with one rule everywhere. */
function allSteps(rule: StepStop): Record<StoppableStep, StepStop> {
  const out = {} as Record<StoppableStep, StepStop>;
  for (const s of STEPPABLE) out[s] = rule;
  return out;
}

/**
 * PRESETS is the canonical value of each named preset, exactly as the
 * settings table specifies. 'custom' is deliberately absent: it is a label
 * settings earn by being edited, not a preset to apply.
 */
export const PRESETS: Record<Exclude<Preset, 'custom'>, PlaySettings> = {
  'casual': mkSettings('casual', {
    autoPass: true,
    opponentSpell: 'if-respondable',
    opponentAbility: 'if-respondable',
    opponentTrigger: 'targets-me-if-respondable',
    ownObjects: 'never',
    steps: steps(
      { main1: 'smart', main2: 'smart' },
      { 'declare-attackers': 'smart', end: 'smart' },
    ),
    passAfterAct: true,
    autoOrderIdenticalTriggers: true,
    autoOrderAllTriggers: true,
    pacing: { stepMs: 200, resolveMs: 400 },
    logAutoPasses: true,
  }),
  'no-tells': mkSettings('no-tells', {
    autoPass: true,
    opponentSpell: 'always',
    opponentAbility: 'always',
    opponentTrigger: 'always',
    ownObjects: 'never',
    steps: steps(
      { main1: 'smart', main2: 'smart' },
      { 'declare-attackers': 'smart', end: 'smart' },
    ),
    passAfterAct: true,
    autoOrderIdenticalTriggers: true,
    autoOrderAllTriggers: true,
    pacing: { stepMs: 200, resolveMs: 400 },
    logAutoPasses: true,
  }),
  'full-control': mkSettings('full-control', {
    autoPass: false,
    opponentSpell: 'always',
    opponentAbility: 'always',
    opponentTrigger: 'always',
    ownObjects: 'if-respondable',
    steps: { yours: allSteps('forced'), opponents: allSteps('forced') },
    passAfterAct: false,
    autoOrderIdenticalTriggers: false,
    autoOrderAllTriggers: false,
    pacing: { stepMs: 0, resolveMs: 0 },
    logAutoPasses: true,
  }),
};

/** nextStop is one step-stop cell's three-state cycle: Off → Smart → Always → Off. */
export function nextStop(rule: StepStop): StepStop {
  return rule === 'off' ? 'smart' : rule === 'smart' ? 'forced' : 'off';
}

/** stopPatch is one cell's change as a withChange patch (deep-merged into steps field-wise). */
export function stopPatch(step: StoppableStep, side: TurnSide, rule: StepStop): Partial<PlaySettings> {
  return { steps: { [side]: { [step]: rule } } } as unknown as Partial<PlaySettings>;
}

/** stopWord names each cell state in plain words — the state is text, never colour alone. */
export function stopWord(rule: StepStop): string {
  return rule === 'off' ? 'Off' : rule === 'smart' ? 'Smart' : 'Always';
}

/**
 * clampMs parses one pacing input as an integer clamped to [0, 10000]
 * (fb-20260917T004341Z: 0 keeps the instant-post path; above 10s a
 * wedged-looking client is a footgun we don't ship). Returns null for a
 * non-numeric/empty input — the caller then leaves the current value
 * unchanged. Exported for tests; the Custom inputs' change handlers use it.
 */
export function clampMs(raw: string): number | null {
  const n = Number.parseInt(raw, 10);
  if (Number.isNaN(n)) return null;
  return Math.min(10000, Math.max(0, n));
}

/**
 * presetPatch is a whole-preset change expressed as a withChange patch:
 * every field of the named preset except its version and label. Because the
 * patch covers ALL of them, the merged result deep-equals the preset and
 * withChange relabels the preset itself — applying a preset IS an ordinary
 * settings change, not a separate write path. Reset to Casual is
 * presetPatch('casual') for the same reason. The machine-side write path
 * (seatpanel.applyNamedPreset) layers the auto re-arm effects on top of
 * this patch; it deliberately lives here in the pure model, not in the
 * component, so the state can use it without importing a .svelte module.
 */
export function presetPatch(id: PresetName): Partial<PlaySettings> {
  const p = PRESETS[id];
  return {
    autoPass: p.autoPass,
    opponentSpell: p.opponentSpell,
    opponentAbility: p.opponentAbility,
    opponentTrigger: p.opponentTrigger,
    ownObjects: p.ownObjects,
    steps: { yours: { ...p.steps.yours }, opponents: { ...p.steps.opponents } },
    passAfterAct: p.passAfterAct,
    autoOrderIdenticalTriggers: p.autoOrderIdenticalTriggers,
    autoOrderAllTriggers: p.autoOrderAllTriggers,
    pacing: { stepMs: p.pacing.stepMs, resolveMs: p.pacing.resolveMs },
    logAutoPasses: p.logAutoPasses,
  };
}

/** defaultSettings is the out-of-the-box experience: casual. */
export function defaultSettings(): PlaySettings {
  return cloneSettings(PRESETS.casual);
}

/** applyPreset returns a fresh, independent copy of the named preset. */
export function applyPreset(p: Exclude<Preset, 'custom'>): PlaySettings {
  return cloneSettings(PRESETS[p]);
}

function cloneSettings(s: PlaySettings): PlaySettings {
  return {
    ...s,
    steps: { yours: { ...s.steps.yours }, opponents: { ...s.steps.opponents } },
    pacing: { ...s.pacing },
    breakpoints: cloneBreakpoints(s.breakpoints),
  };
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a !== typeof b || a === null || b === null) return false;
  if (Array.isArray(a) || Array.isArray(b)) return JSON.stringify(a) === JSON.stringify(b);
  if (typeof a !== 'object') return false;
  const ka = Object.keys(a as object).sort();
  const kb = Object.keys(b as object).sort();
  if (ka.length !== kb.length || ka.some((k, i) => k !== kb[i])) return false;
  return ka.every((k) => deepEqual((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]));
}

/**
 * cloneValue deep-copies a settings value: arrays and plain objects are
 * rebuilt, primitives are returned as-is. Arrays (breakpoints.watchlist) are
 * replaced wholesale by a patch, never merged element-wise.
 */
function cloneValue(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(cloneValue);
  if (isPlainObject(v)) return deepMerge(v, {});
  return v;
}

/**
 * deepMerge copies base with patch applied, recursively cloning every plain-
 * object and array value, so a nested patch (one step's rule, one pacing
 * field) merges field-wise, untouched siblings are deep copies, a patched
 * array is a copy of the patch's array (no shared references with the
 * caller's settings or the caller's patch), and the result is safe to mutate.
 */
function deepMerge(base: Record<string, unknown>, patch: Record<string, unknown>): Record<string, unknown> {
  const out = { ...base };
  for (const [k, v] of Object.entries(patch)) {
    if (v === undefined) continue;
    const cur = out[k];
    out[k] = isPlainObject(v) && isPlainObject(cur) ? deepMerge(cur as Record<string, unknown>, v) : cloneValue(v);
  }
  // Clone every branch the patch did not touch so nothing is shared with the
  // caller's settings.
  for (const k of Object.keys(out)) {
    if (out[k] === base[k] && (isPlainObject(out[k]) || Array.isArray(out[k]))) out[k] = cloneValue(out[k]);
  }
  return out;
}

/**
 * withChange returns a copy of settings with the patch applied (nested
 * objects — steps, pacing — are merged field-wise, so a caller can patch one
 * step's rule without restating the others). The copy's preset is 'custom'
 * unless the result deep-equals a named preset, in which case it carries
 * that preset's name again: undoing an edit back to a known configuration
 * restores the preset label instead of leaving it stuck at 'custom'.
 */
export function withChange(settings: PlaySettings, patch: Partial<PlaySettings>): PlaySettings {
  const merged = deepMerge(settings as unknown as Record<string, unknown>, patch as Record<string, unknown>);
  const next = merged as unknown as PlaySettings;
  for (const name of Object.keys(PRESETS) as Exclude<Preset, 'custom'>[]) {
    // Compare with the label normalised to the candidate's name: the RULES
    // decide which preset a configuration is, not the label it came in with.
    if (deepEqual({ ...next, preset: name, breakpoints: PRESETS[name].breakpoints }, PRESETS[name])) {
      next.preset = name;
      return next;
    }
  }
  next.preset = 'custom';
  return next;
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/**
 * Storage key: ONE global key, deliberately not per table (see module doc).
 * The KEY string stays `gorge.playsettings.v1` across the v1→v2 field bump
 * (fb-trigorder1): the version lives INSIDE the blob, so renaming the key
 * would orphan every existing player's settings — exactly the silent wipe
 * the version-2 migration exists to avoid. The same holds for the v2→v3
 * bump (breakpoints): validate accepts blob versions 1, 2 and 3;
 * saveSettings writes v3 into the same key.
 * Migration: the legacy per-table keys (`gorge.stop.<table>.<seat>` from
 * stops.ts and the actpass key from actpass.ts) are NOT imported. They
 * encode the old mana-tap-era defaults — stop sets written when a mana tap
 * counted as an action — and importing them would resurrect those defaults
 * under the new model. A player migrating simply starts at casual.
 */
const KEY = 'gorge.playsettings.v1';

const STEP_STOPS: readonly StepStop[] = ['off', 'smart', 'forced'];
const OBJECT_RULES: readonly OpponentObjectRule[] = ['if-respondable', 'always', 'never'];
const TRIGGER_RULES: readonly OpponentTriggerRule[] = ['targets-me-if-respondable', 'if-respondable', 'always', 'never'];
const OWN_RULES: readonly OwnObjectRule[] = ['never', 'if-respondable'];

/**
 * validate returns a PlaySettings from a parsed value, or null when the
 * value is corrupt: wrong version, a missing or wrong-typed field, an
 * unknown rule word. Corrupt means defaults, not a partial merge — a
 * half-understood settings blob should not half-apply.
 *
 * Version migration (fb-trigorder1): blob version 1 predates
 * `autoOrderAllTriggers`, and rejecting it outright would reset every
 * existing player to casual — a silent settings wipe. A v1 blob is
 * migrated instead: every old field is preserved as-is and the new field
 * defaults to casual's value (true). Blob version 2 requires the field.
 *
 * Version 3 adds `breakpoints` (flow profiles). Blob versions 1 and 2
 * predate it and migrate to the in-memory version 3 with breakpoints off
 * (noBreakpoints) and every other field kept; a v3 blob requires a valid
 * breakpoints block, and its watchlist is normalised (trimmed, empties
 * dropped, deduped case-insensitively, capped). Every blob migrates to the
 * in-memory version 3; the next save writes v3.
 */
export function validate(v: unknown): PlaySettings | null {
  if (!isPlainObject(v)) return null;
  if (v.version !== 1 && v.version !== 2 && v.version !== 3) return null;
  if (!isRule(v.opponentSpell, OBJECT_RULES) || !isRule(v.opponentAbility, OBJECT_RULES)) return null;
  if (!isRule(v.opponentTrigger, TRIGGER_RULES) || !isRule(v.ownObjects, OWN_RULES)) return null;
  if (typeof v.autoPass !== 'boolean' || typeof v.passAfterAct !== 'boolean') return null;
  if (typeof v.autoOrderIdenticalTriggers !== 'boolean' || typeof v.logAutoPasses !== 'boolean') return null;
  // Required from v2; defaulted (to casual's value) on a v1 blob.
  if (v.version >= 2 && typeof v.autoOrderAllTriggers !== 'boolean') return null;
  if (typeof v.pacing !== 'object' || v.pacing === null) return null;
  const pacing = v.pacing as Record<string, unknown>;
  if (typeof pacing.stepMs !== 'number' || typeof pacing.resolveMs !== 'number') return null;
  if (typeof v.steps !== 'object' || v.steps === null) return null;
  const st = v.steps as Record<string, unknown>;
  const readSide = (side: unknown): Record<StoppableStep, StepStop> | null => {
    if (typeof side !== 'object' || side === null) return null;
    const o = side as Record<string, unknown>;
    const out = {} as Record<StoppableStep, StepStop>;
    for (const s of STEPPABLE) {
      if (!isRule(o[s], STEP_STOPS)) return null;
      out[s] = o[s] as StepStop;
    }
    return out;
  };
  const yours = readSide(st.yours);
  const opponents = readSide(st.opponents);
  if (yours === null || opponents === null) return null;
  // preset is a label, not a rule the engine obeys: accept any of the four
  // words but never trust the blob beyond that.
  if (typeof v.preset !== 'string' || !['casual', 'no-tells', 'full-control', 'custom'].includes(v.preset)) return null;
  let breakpoints: Breakpoints;
  if (v.version === 3) {
    const b = readBreakpoints(v.breakpoints);
    if (b === null) return null;
    breakpoints = b;
  } else {
    // v1/v2 predate breakpoints: migrate with them off, keep everything else.
    breakpoints = noBreakpoints();
  }
  return {
    version: 3,
    preset: v.preset as Preset,
    autoPass: v.autoPass,
    opponentSpell: v.opponentSpell as OpponentObjectRule,
    opponentAbility: v.opponentAbility as OpponentObjectRule,
    opponentTrigger: v.opponentTrigger as OpponentTriggerRule,
    ownObjects: v.ownObjects as OwnObjectRule,
    steps: { yours, opponents },
    passAfterAct: v.passAfterAct,
    autoOrderIdenticalTriggers: v.autoOrderIdenticalTriggers,
    // v1 blobs predate the field: default it to casual's value, preserve
    // everything else exactly.
    autoOrderAllTriggers:
      typeof v.autoOrderAllTriggers === 'boolean' ? v.autoOrderAllTriggers : PRESETS.casual.autoOrderAllTriggers,
    pacing: { stepMs: pacing.stepMs, resolveMs: pacing.resolveMs },
    logAutoPasses: v.logAutoPasses,
    breakpoints,
  };
}

function isRule<T extends string>(v: unknown, allowed: readonly T[]): v is T {
  return typeof v === 'string' && (allowed as readonly string[]).includes(v);
}

function readBreakpoints(v: unknown): Breakpoints | null {
  if (!isPlainObject(v)) return null;
  if (typeof v.targetsMe !== 'boolean' || typeof v.attacked !== 'boolean') return null;
  if (typeof v.stackDepth !== 'number' || !STACK_DEPTHS.includes(v.stackDepth)) return null;
  if (!Array.isArray(v.watchlist)) return null;
  let watchlist: string[] = [];
  for (const raw of v.watchlist) {
    if (typeof raw !== 'string') return null;
    watchlist = withWatch(watchlist, raw);
  }
  return { targetsMe: v.targetsMe, attacked: v.attacked, stackDepth: v.stackDepth, watchlist };
}

/**
 * loadSettings reads the global key. Absent, corrupt, wrong-version or a
 * throwing storage all yield the defaults (casual); a throw must never
 * escape — private-mode browsers throw on getItem too. Same pattern as
 * stops.ts / images.ts.
 */
export function loadSettings(storage: Storage | null): PlaySettings {
  try {
    const raw = storage?.getItem(KEY) ?? null;
    if (raw === null) return defaultSettings();
    const validated = validate(JSON.parse(raw));
    return validated ?? defaultSettings();
  } catch {
    return defaultSettings();
  }
}

/** saveSettings writes the global key. A throw (private mode, quota) is swallowed: the caller's in-memory copy is the source of truth until a later save lands. */
export function saveSettings(storage: Storage | null, s: PlaySettings): void {
  try {
    storage?.setItem(KEY, JSON.stringify(s));
  } catch {
    /* private mode or quota: keep the in-memory copy */
  }
}
