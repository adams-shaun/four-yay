/**
 * patch.ts applies a ManaBrew `stateDelta` patch to the last full
 * `gameView` (spec Appendix A.2: `$v` replace, `$d` remove, `$k` keyed
 * array edits whose element key is `id` — `"<zone>/<ownerId>"` for zones —
 * and `$o` the full key order). The upstream grammar is terse, so this
 * reads it as a structural merge: a plain object recurses, a `$`-object is
 * an edit, anything else is a replacement (`null` included).
 *
 * gorge's server does not send patches (scope spec Q7); this exists because
 * conforming clients should accept them. Anything it cannot apply throws
 * PatchError, and the caller resynchronises from `GET …/state`.
 */

export class PatchError extends Error {}

type Json = null | boolean | number | string | Json[] | { [k: string]: Json };
type Obj = { [k: string]: Json };

const isObj = (v: unknown): v is Obj => v !== null && typeof v === 'object' && !Array.isArray(v);
const isEdit = (v: unknown): v is Obj => isObj(v) && Object.keys(v).some((k) => k.startsWith('$'));

/** elementKey is an array element's identity for `$k` edits. */
export function elementKey(el: Json): string | null {
  if (!isObj(el)) return null;
  if (typeof el.zone === 'string' && typeof el.ownerId === 'string') return `${el.zone}/${el.ownerId}`;
  if (typeof el.id === 'string' || typeof el.id === 'number') return String(el.id);
  return null;
}

function applyKeyed(arr: Json, edits: Obj, order: Json | undefined): Json[] {
  if (!Array.isArray(arr)) throw new PatchError('$k on a non-array');
  const byKey = new Map<string, Json>();
  const keys: string[] = [];
  for (const el of arr) {
    const k = elementKey(el);
    if (k === null) throw new PatchError('$k over an array without element keys');
    byKey.set(k, el);
    keys.push(k);
  }
  for (const [k, e] of Object.entries(edits)) {
    if (isObj(e) && e.$d !== undefined) {
      byKey.delete(k);
      continue;
    }
    const cur = byKey.get(k);
    const next = cur === undefined ? (isObj(e) && '$v' in e ? e.$v : e) : applyValue(cur, e);
    if (!byKey.has(k)) keys.push(k);
    byKey.set(k, next);
  }
  const final = Array.isArray(order) ? order.map(String) : keys.filter((k) => byKey.has(k));
  return final.map((k) => {
    const el = byKey.get(k);
    if (el === undefined) throw new PatchError(`$o names unknown element ${k}`);
    return el;
  });
}

function applyValue(target: Json, patch: Json): Json {
  if (isEdit(patch)) {
    if ('$v' in patch) return patch.$v;
    if ('$k' in patch) {
      if (!isObj(patch.$k)) throw new PatchError('$k must be an object');
      return applyKeyed(target, patch.$k, patch.$o);
    }
    if ('$o' in patch && Array.isArray(target)) return applyKeyed(target, {}, patch.$o);
    // An object-level $o only reorders keys, which no reader depends on;
    // apply the non-$ keys as an ordinary merge.
    const rest: Obj = {};
    for (const [k, v] of Object.entries(patch)) if (!k.startsWith('$')) rest[k] = v;
    return applyValue(target, rest);
  }
  if (isObj(patch) && isObj(target)) {
    const out: Obj = { ...target };
    for (const [k, v] of Object.entries(patch)) {
      if (isObj(v) && '$d' in v) delete out[k];
      else out[k] = k in out ? applyValue(out[k], v) : isEdit(v) && '$v' in v ? v.$v : v;
    }
    return out;
  }
  return patch;
}

/** applyPatch returns the patched copy of `base`; the input is not mutated. */
export function applyPatch<T>(base: T, patch: unknown): T {
  if (!isObj(patch)) throw new PatchError('a patch must be an object');
  return applyValue(base as Json, patch as Json) as T;
}
