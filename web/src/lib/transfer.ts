/**
 * transfer is export/import-to-file for the client-side libraries (UI rework
 * spec §1). A file is one JSON envelope {kind, version, items}. Import runs
 * every entry through the owning store's own validate, so a file can never
 * carry a configuration the model would reject; a bad entry is skipped and
 * counted, never fatal, and an existing profile is never overwritten.
 */
import { listProfiles, MAX_PROFILE_NAME, normaliseName, storeSave, type ProfileStore } from './profiles';
import { validate } from './playsettings';
import { overridesOf, validateKeymap, type Keymap } from './keymap';

type Kind = 'gorge-flow' | 'gorge-keymap';
const WHAT: Record<Kind, string> = { 'gorge-flow': 'flow profiles', 'gorge-keymap': 'keyboard shortcuts' };

function envelope(text: string, want: Kind): { items: unknown } | { error: string } {
  let v: unknown;
  try {
    v = JSON.parse(text);
  } catch {
    return { error: 'This file is not a gorge settings export.' };
  }
  if (typeof v !== 'object' || v === null) return { error: 'This file is not a gorge settings export.' };
  const o = v as Record<string, unknown>;
  if (o.kind !== 'gorge-flow' && o.kind !== 'gorge-keymap') return { error: 'This file is not a gorge settings export.' };
  if (o.kind !== want) return { error: `This file holds ${WHAT[o.kind]}, not ${WHAT[want]}.` };
  if (o.version !== 1) return { error: 'This export is from a newer version of gorge.' };
  return { items: o.items };
}

/** uniqueName returns name, or name suffixed " (2)", " (3)"… (truncated to fit MAX_PROFILE_NAME) when it is taken. */
export function uniqueName(name: string, taken: readonly string[]): string {
  if (!taken.includes(name)) return name;
  for (let n = 2; ; n++) {
    const suffix = ` (${n})`;
    const cand = name.slice(0, MAX_PROFILE_NAME - suffix.length) + suffix;
    if (!taken.includes(cand)) return cand;
  }
}

/** exportFlow is every saved flow profile, in order, as a gorge-flow file. */
export function exportFlow(store: ProfileStore): string {
  const items = listProfiles(store).map((name) => ({ name, settings: store.profiles[name] }));
  return JSON.stringify({ kind: 'gorge-flow', version: 1, items }, null, 2);
}

/** importFlow merges a gorge-flow file into store: clashes are renamed, bad entries skipped and counted. */
export function importFlow(text: string, store: ProfileStore): { store: ProfileStore; added: string[]; skipped: number } | { error: string } {
  const env = envelope(text, 'gorge-flow');
  if ('error' in env) return env;
  if (!Array.isArray(env.items)) return { error: 'This file is not a gorge settings export.' };
  let next = store;
  const added: string[] = [];
  let skipped = 0;
  for (const it of env.items) {
    const o = (typeof it === 'object' && it !== null ? it : {}) as Record<string, unknown>;
    const name = normaliseName(typeof o.name === 'string' ? o.name : '');
    const settings = validate(o.settings);
    if (name === null || settings === null) {
      skipped++;
      continue;
    }
    const final = uniqueName(name, listProfiles(next));
    next = storeSave(next, final, settings);
    added.push(final);
  }
  // storeSave marks each saved profile active; an import must not switch the player's active profile.
  return { store: { ...next, lastActive: store.lastActive }, added, skipped };
}

/** exportKeymap is the keymap's overrides (the difference from defaults) as a gorge-keymap file. */
export function exportKeymap(k: Keymap): string {
  return JSON.stringify({ kind: 'gorge-keymap', version: 1, items: overridesOf(k) }, null, 2);
}

/** importKeymap reads a gorge-keymap file onto the defaults through validateKeymap. */
export function importKeymap(text: string): { keymap: Keymap } | { error: string } {
  const env = envelope(text, 'gorge-keymap');
  if ('error' in env) return env;
  const k = validateKeymap({ version: 1, overrides: env.items });
  return k === null ? { error: 'This file is not a gorge settings export.' } : { keymap: k };
}
