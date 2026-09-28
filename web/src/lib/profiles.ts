import { validate, type PlaySettings } from './playsettings';

/**
 * profiles is the pure settings-store sibling of playsettings.ts: named,
 * player-authored configurations of the auto-pass settings that can be
 * saved, re-applied and deleted. It is the feature the table asked for
 * ("ability to modify casual/no tells/full profiles (or even create our own)
 * and be able to switch between them") delivered WITHOUT touching the shipped
 * presets: PRESETS stays immutable, and "modify casual" is save-my-edits-
 * as-a-profile.
 *
 * Persistence is its OWN localStorage key, `gorge.playsettings.profiles.v1`,
 * deliberately NOT a field in the v2 settings blob under
 * `gorge.playsettings.v1`: that blob's format is pinned field-for-field by
 * playsettings.test.ts, and adding a list to it would orphan every existing
 * player's saved settings on the next load. A profile entry is a full
 * PlaySettings validated by playsettings.ts's own validate (exported for
 * exactly this reuse), so a profile can never encode a configuration the
 * model itself would reject.
 *
 * The module is pure TypeScript (no Svelte runes, no I/O beyond the two
 * storage functions) so its rules are testable without a browser; the
 * reactive shell that makes the panel re-render lives in seatpanel.svelte.ts.
 */

/** KEY is the profile store's own localStorage key (see module doc). */
export const PROFILES_KEY = 'gorge.playsettings.profiles.v1';

/** MAX_PROFILE_NAME is the longest accepted trimmed name (brief: ~24 chars). */
export const MAX_PROFILE_NAME = 24;

/**
 * ProfileStore is the persisted blob: the profiles map, an explicit `order`
 * array (object-key iteration order is not a contract, so the list order is
 * pinned here) and `lastActive` — the name last applied, or null. The active
 * identity lives HERE, not in a PlaySettings field, precisely so the v2
 * settings blob's format does not move.
 */
export interface ProfileStore {
  version: 1;
  profiles: Record<string, PlaySettings>;
  order: string[];
  lastActive: string | null;
}

/** emptyStore is the absent/corrupt-blob value: no profiles, no active. */
export function emptyStore(): ProfileStore {
  return { version: 1, profiles: {}, order: [], lastActive: null };
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/**
 * normaliseName trims a candidate profile name and returns it, or null when
 * it is empty or longer than MAX_PROFILE_NAME. The rules are the UI's too:
 * a name that normaliseName rejects must never reach the store.
 */
export function normaliseName(raw: string): string | null {
  if (typeof raw !== 'string') return null;
  const name = raw.trim();
  if (name.length === 0 || name.length > MAX_PROFILE_NAME) return null;
  return name;
}

/**
 * validateStore returns a ProfileStore from a parsed value, or an empty store
 * when the blob is corrupt. Each profile entry is run through playsettings'
 * validate — a bad entry drops only that entry, never the whole blob and
 * never the primary settings blob. order is rebuilt to contain exactly the
 * names that survived, in blob order then any surviving names the order
 * omitted, so a hand-edited or partial order cannot lose a profile.
 */
export function validateStore(v: unknown): ProfileStore {
  const out = emptyStore();
  if (!isPlainObject(v)) return out;
  if (v.version !== 1) return out;
  if (!isPlainObject(v.profiles)) return out;
  for (const [name, raw] of Object.entries(v.profiles)) {
    const clean = normaliseName(name);
    if (clean === null) continue;
    const s = validate(raw);
    if (s === null) continue;
    out.profiles[clean] = s;
  }
  const seen = new Set<string>();
  if (Array.isArray(v.order)) {
    for (const name of v.order) {
      if (typeof name !== 'string') continue;
      const clean = normaliseName(name);
      if (clean !== null && clean in out.profiles && !seen.has(clean)) {
        out.order.push(clean);
        seen.add(clean);
      }
    }
  }
  for (const name of Object.keys(out.profiles)) {
    if (!seen.has(name)) out.order.push(name);
  }
  out.lastActive =
    typeof v.lastActive === 'string' && v.lastActive in out.profiles ? v.lastActive : null;
  return out;
}

/** deepClone returns an independent copy of settings (steps, pacing and breakpoints — with its watchlist array — are the only nested objects). */
function deepClone(s: PlaySettings): PlaySettings {
  return {
    ...s,
    steps: { yours: { ...s.steps.yours }, opponents: { ...s.steps.opponents } },
    pacing: { ...s.pacing },
    breakpoints: { ...s.breakpoints, watchlist: [...s.breakpoints.watchlist] },
  };
}

/**
 * loadProfiles reads the profile key. Absent, corrupt, wrong-version or a
 * throwing storage all yield the empty store; a throw must never escape —
 * private-mode browsers throw on getItem too. Same pattern as loadSettings.
 * It NEVER reads or writes the primary settings key.
 */
export function loadProfiles(storage: Storage | null): ProfileStore {
  try {
    const raw = storage?.getItem(PROFILES_KEY) ?? null;
    if (raw === null) return emptyStore();
    return validateStore(JSON.parse(raw));
  } catch {
    return emptyStore();
  }
}

/** saveProfiles writes the profile key. A throw (private mode, quota) is swallowed: the caller's in-memory copy is the source of truth until a later save lands. */
export function saveProfiles(storage: Storage | null, store: ProfileStore): void {
  try {
    storage?.setItem(PROFILES_KEY, JSON.stringify(store));
  } catch {
    /* private mode or quota: keep the in-memory copy */
  }
}

/** listProfiles returns the profile names in `order`, never map-iteration order. */
export function listProfiles(store: ProfileStore): string[] {
  return [...store.order];
}

/**
 * storeSave writes one profile (deep-cloned, so later edits to the caller's
 * settings cannot mutate the stored copy) and marks it lastActive. A name
 * that normaliseName rejects is a no-op returning the store unchanged.
 * Saving an existing name overwrites it (explicit Save semantics).
 */
export function storeSave(store: ProfileStore, name: string, settings: PlaySettings): ProfileStore {
  const clean = normaliseName(name);
  if (clean === null) return store;
  const next: ProfileStore = {
    version: 1,
    profiles: { ...store.profiles, [clean]: deepClone(settings) },
    order: store.order.includes(clean) ? [...store.order] : [...store.order, clean],
    lastActive: clean,
  };
  return next;
}

/**
 * storeApply returns an independent clone of a profile's settings, or null
 * when no such profile exists. The clone's `preset` label is whatever the
 * stored configuration earns (normally 'custom'), so withChange's relabel
 * logic keeps working on top of an applied profile.
 */
export function storeApply(store: ProfileStore, name: string): PlaySettings | null {
  const s = store.profiles[name];
  return s === undefined ? null : deepClone(s);
}

/** storeDelete removes a profile and its order entry; deleting the active profile clears lastActive. */
export function storeDelete(store: ProfileStore, name: string): ProfileStore {
  if (!(name in store.profiles)) return store;
  const profiles = { ...store.profiles };
  delete profiles[name];
  return {
    version: 1,
    profiles,
    order: store.order.filter((n) => n !== name),
    lastActive: store.lastActive === name ? null : store.lastActive,
  };
}

/**
 * storeRename renames a profile in place, preserving its order position. A
 * rejected new name, an absent old name, or a new name already taken is a
 * no-op returning the store unchanged (the caller reports the collision).
 */
export function storeRename(store: ProfileStore, oldName: string, newName: string): ProfileStore {
  if (!(oldName in store.profiles)) return store;
  const clean = normaliseName(newName);
  if (clean === null || clean === oldName || clean in store.profiles) return store;
  const profiles = { ...store.profiles };
  profiles[clean] = profiles[oldName];
  delete profiles[oldName];
  return {
    version: 1,
    profiles,
    order: store.order.map((n) => (n === oldName ? clean : n)),
    lastActive: store.lastActive === oldName ? clean : store.lastActive,
  };
}

/** storeSetActive marks which profile is currently applied (or null for none). */
export function storeSetActive(store: ProfileStore, name: string | null): ProfileStore {
  return { ...store, lastActive: name !== null && name in store.profiles ? name : null };
}
