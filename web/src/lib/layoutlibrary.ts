import {
  cloneProfile,
  defaultProfile,
  LEGACY_LAYOUT_KEY,
  migrateLegacy,
  PRESET_IDS,
  PRESET_LABELS,
  presetOf,
  sameProfile,
  validate,
  withPreset,
  type LayoutProfile,
  type PresetId,
} from './layoutprofile';
import { MAX_PROFILE_NAME } from './profiles';

/**
 * layoutlibrary is the persisted library of layout profiles (UI rework spec
 * §2), at ONE localStorage key, `gorge.layouts.v1`:
 *
 *   - `current` is the working copy. Every edit — a drawer control, a drag
 *     of the centre bar, the stacking hotkey — writes here, so the table
 *     looks the same after a reload without an explicit save.
 *   - `profiles`/`order` are the player's named saves, `active` the one last
 *     applied or saved (null when none).
 *
 * It absorbs the old `gorge.layoutsettings.v1` model: when the new key is
 * absent, `loadLibrary` migrates the legacy blob into `current` (the legacy
 * key is read, never written or deleted, so a rollback still finds it).
 *
 * Pure: no runes, no I/O beyond the two storage functions, the profiles.ts
 * house pattern (prototype-free maps, reserved names refused, `order`
 * rebuilt from what survived validation).
 */

export const LAYOUTS_KEY = 'gorge.layouts.v1';

export interface LayoutLibrary {
  version: 1;
  current: LayoutProfile;
  profiles: Record<string, LayoutProfile>;
  order: string[];
  active: string | null;
}

const RESERVED = ['__proto__', 'constructor', 'prototype'];

function map(src?: Record<string, LayoutProfile>): Record<string, LayoutProfile> {
  const out = Object.create(null) as Record<string, LayoutProfile>;
  if (src) for (const k of Object.keys(src)) out[k] = src[k];
  return out;
}

export function hasLayout(lib: LayoutLibrary, name: string): boolean {
  return Object.hasOwn(lib.profiles, name);
}

/** normaliseLayoutName trims a name and returns it, or null when empty, too long or reserved. */
export function normaliseLayoutName(raw: unknown): string | null {
  if (typeof raw !== 'string') return null;
  const name = raw.trim();
  if (name.length === 0 || name.length > MAX_PROFILE_NAME || RESERVED.includes(name)) return null;
  return name;
}

export function emptyLibrary(current: LayoutProfile = defaultProfile()): LayoutLibrary {
  return { version: 1, current, profiles: map(), order: [], active: null };
}

/**
 * validateLibrary reads a parsed blob. A bad `current` falls back to the
 * default; a bad named profile drops only itself (never the library).
 */
export function validateLibrary(v: unknown): LayoutLibrary | null {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) return null;
  const o = v as Record<string, unknown>;
  if (o.version !== 1) return null;
  const out = emptyLibrary(validate(o.current) ?? defaultProfile());
  const profiles = typeof o.profiles === 'object' && o.profiles !== null && !Array.isArray(o.profiles) ? (o.profiles as Record<string, unknown>) : {};
  for (const [raw, p] of Object.entries(profiles)) {
    const name = normaliseLayoutName(raw);
    const prof = validate(p);
    if (name !== null && prof !== null) out.profiles[name] = prof;
  }
  const seen = new Set<string>();
  if (Array.isArray(o.order)) {
    for (const n of o.order) {
      if (typeof n === 'string' && hasLayout(out, n) && !seen.has(n)) {
        out.order.push(n);
        seen.add(n);
      }
    }
  }
  for (const n of Object.keys(out.profiles)) if (!seen.has(n)) out.order.push(n);
  out.active = typeof o.active === 'string' && hasLayout(out, o.active) ? o.active : null;
  return out;
}

/**
 * loadLibrary reads the library key, migrating the legacy layout blob when
 * the library key has never been written. Absent, corrupt or a throwing
 * storage all yield defaults; nothing throws out of here.
 */
export function loadLibrary(storage: Storage | null): LayoutLibrary {
  try {
    const raw = storage?.getItem(LAYOUTS_KEY) ?? null;
    if (raw !== null) return validateLibrary(JSON.parse(raw)) ?? emptyLibrary();
    const legacy = storage?.getItem(LEGACY_LAYOUT_KEY) ?? null;
    if (legacy !== null) {
      let parsed: unknown = null;
      try {
        parsed = JSON.parse(legacy);
      } catch {
        parsed = null;
      }
      const migrated = migrateLegacy(parsed);
      if (migrated !== null) return emptyLibrary(migrated);
    }
    return emptyLibrary();
  } catch {
    return emptyLibrary();
  }
}

export function saveLibrary(storage: Storage | null, lib: LayoutLibrary): void {
  try {
    storage?.setItem(LAYOUTS_KEY, JSON.stringify({ version: 1, current: lib.current, profiles: lib.profiles, order: lib.order, active: lib.active }));
  } catch {
    /* private mode or quota: keep the in-memory copy */
  }
}

/** withCurrent replaces the working copy (the active name stays: the label notices the drift). */
export function withCurrent(lib: LayoutLibrary, p: LayoutProfile): LayoutLibrary {
  return { ...lib, current: cloneProfile(p) };
}

/** saveAs stores the working copy under a name (overwriting a same-named save) and marks it active. */
export function saveAs(lib: LayoutLibrary, name: string): LayoutLibrary {
  const clean = normaliseLayoutName(name);
  if (clean === null) return lib;
  return {
    ...lib,
    profiles: Object.assign(map(lib.profiles), { [clean]: cloneProfile(lib.current) }),
    order: lib.order.includes(clean) ? [...lib.order] : [...lib.order, clean],
    active: clean,
  };
}

/** addProfile stores a profile under a name without touching the working copy or the active name (the import path). */
export function addProfile(lib: LayoutLibrary, name: string, p: LayoutProfile): LayoutLibrary {
  const clean = normaliseLayoutName(name);
  if (clean === null) return lib;
  return {
    ...lib,
    profiles: Object.assign(map(lib.profiles), { [clean]: cloneProfile(p) }),
    order: lib.order.includes(clean) ? [...lib.order] : [...lib.order, clean],
  };
}

/** applyNamed copies a saved profile into the working copy and marks it active. */
export function applyNamed(lib: LayoutLibrary, name: string): LayoutLibrary {
  if (!hasLayout(lib, name)) return lib;
  return { ...lib, current: cloneProfile(lib.profiles[name]), active: name };
}

/** applyPreset puts a preset's table and template into the working copy and clears the active name. */
export function applyPreset(lib: LayoutLibrary, id: PresetId): LayoutLibrary {
  return { ...lib, current: withPreset(lib.current, id), active: null };
}

export function removeNamed(lib: LayoutLibrary, name: string): LayoutLibrary {
  if (!hasLayout(lib, name)) return lib;
  const profiles = map(lib.profiles);
  delete profiles[name];
  return { ...lib, profiles, order: lib.order.filter((n) => n !== name), active: lib.active === name ? null : lib.active };
}

export function renameNamed(lib: LayoutLibrary, from: string, to: string): LayoutLibrary {
  const clean = normaliseLayoutName(to);
  if (!hasLayout(lib, from) || clean === null || clean === from || hasLayout(lib, clean)) return lib;
  const profiles = map(lib.profiles);
  profiles[clean] = profiles[from];
  delete profiles[from];
  return {
    ...lib,
    profiles,
    order: lib.order.map((n) => (n === from ? clean : n)),
    active: lib.active === from ? clean : lib.active,
  };
}

/** A cycle entry: the presets first, in drawer order, then the player's saves. */
export type CycleEntry = { kind: 'preset'; id: PresetId; label: string } | { kind: 'named'; name: string; label: string };

export function cycleList(lib: LayoutLibrary): CycleEntry[] {
  return [
    ...PRESET_IDS.map((id): CycleEntry => ({ kind: 'preset', id, label: PRESET_LABELS[id] })),
    ...lib.order.map((name): CycleEntry => ({ kind: 'named', name, label: name })),
  ];
}

/** applyEntry applies one cycle entry. */
export function applyEntry(lib: LayoutLibrary, e: CycleEntry): LayoutLibrary {
  return e.kind === 'preset' ? applyPreset(lib, e.id) : applyNamed(lib, e.name);
}

/** currentIndex is the cycle position the working copy matches, or -1. */
export function currentIndex(lib: LayoutLibrary): number {
  const list = cycleList(lib);
  if (lib.active !== null && hasLayout(lib, lib.active) && sameProfile(lib.current, lib.profiles[lib.active])) {
    return list.findIndex((e) => e.kind === 'named' && e.name === lib.active);
  }
  const p = presetOf(lib.current);
  return p === null ? -1 : list.findIndex((e) => e.kind === 'preset' && e.id === p);
}

/** cycled applies the entry `delta` steps from the current one (wrapping); from no match it starts at the first. */
export function cycled(lib: LayoutLibrary, delta: number): LayoutLibrary {
  const list = cycleList(lib);
  const at = currentIndex(lib);
  const next = at < 0 ? (delta > 0 ? 0 : list.length - 1) : (at + delta + list.length) % list.length;
  return applyEntry(lib, list[next]);
}

/** applyAt applies the n-th entry (0-based) of the cycle list; out of range is a no-op returning null. */
export function applyAt(lib: LayoutLibrary, n: number): LayoutLibrary | null {
  const list = cycleList(lib);
  return n >= 0 && n < list.length ? applyEntry(lib, list[n]) : null;
}

/** labelOf is what the layout pill says: the saved name, the preset's label, or Custom. */
export function labelOf(lib: LayoutLibrary): string {
  if (lib.active !== null && hasLayout(lib, lib.active) && sameProfile(lib.current, lib.profiles[lib.active])) return lib.active;
  const p = presetOf(lib.current);
  if (p !== null) return PRESET_LABELS[p];
  return lib.active !== null ? `${lib.active} (edited)` : 'Custom';
}
