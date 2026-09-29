import type { Decision, Option, View } from '../protocol';
import { triggerPromptLabel } from './remembered';

export type StickyKind = 'target' | 'choose' | 'modes' | 'trigger_optional' | 'trigger_order';

export interface StickyRule {
  key: string;
  kind: StickyKind;
  label: string;
  answer?: { label: string; controller: number | null };
  order?: string[];
}

const KINDS: readonly string[] = ['target', 'choose', 'modes', 'trigger_optional', 'trigger_order'];
const memory = new Map<string, Map<string, StickyRule>>();

/** Read only visible objects, including stack abilities and every projected card zone. */
function objectInView(view: View, id: number | undefined): { name: string; controller: number } | null {
  if (!id) return null;
  for (const p of view.players) {
    for (const field of Object.values(p)) {
      for (const c of Array.isArray(field) ? field : [field]) {
        if (c && typeof c === 'object' && 'id' in c && c.id === id &&
          'name' in c && typeof c.name === 'string' &&
          'controller' in c && typeof c.controller === 'number') return c;
      }
    }
  }
  return view.stack.find((s) => s.id === id) ?? null;
}

/** triggerLabel on the wire is "<name>: <text>" (or a keyword's full label). */
function triggerEntry(o: Option, view: View): string {
  const name = objectInView(view, o.obj)?.name ?? '';
  const text = name && o.label.startsWith(`${name}: `) ? o.label.slice(name.length + 2) : o.label;
  return `${name}\u0000${text}`;
}

/** A colour produced by a mana ability, not a mana payment/allocation window. */
export function isManaColourChoice(d: Decision): boolean {
  return d.kind === 'choose' && d.min === 1 && d.max === 1 && !d.mana_payment
    && d.options.length > 0 && d.options.every((o) => o.kind === 'mana' && o.amount === undefined);
}

export function stickyKey(d: Decision, view: View): string | null {
  if (!KINDS.includes(d.kind) || d.mana_payment || d.options.some((o) =>
    o.kind === 'autofill' || o.kind === 'activate')) return null;
  if (d.options.some((o) => o.kind === 'mana') && !isManaColourChoice(d)) return null;
  if (d.kind === 'choose' && (d.max > 1 || d.options.some((o) =>
    o.kind === 'x' || o.kind === 'amount' || o.amount !== undefined))) return null;
  if (d.kind === 'trigger_order') {
    return `${d.kind}\u0000${d.options.map((o) => triggerEntry(o, view)).sort().join('\u0000')}`;
  }
  return `${d.kind}\u0000${objectInView(view, d.source)?.name ?? ''}\u0000${d.prompt}`;
}

/** Label patterns, never stored wire indices. Duplicate matches use wire order. */
export function stickyAnswer(d: Decision, view: View, rules: ReadonlyMap<string, StickyRule>): number[] | null {
  const key = stickyKey(d, view);
  const rule = key === null ? undefined : rules.get(key);
  if (!rule || rule.kind !== d.kind) return null;
  if (d.kind === 'trigger_order') {
    if (!rule.order || rule.order.length !== d.options.length) return null;
    const entries = d.options.map((o) => triggerEntry(o, view));
    const used = new Set<number>();
    const choices: number[] = [];
    for (const entry of rule.order) {
      const at = entries.findIndex((e, i) => e === entry && !used.has(i));
      if (at < 0) return null;
      used.add(at);
      choices.push(d.options[at].index);
    }
    return choices;
  }
  if (!rule.answer || d.min > 1 || d.max < 1) return null;
  const answer = rule.answer;
  const option = d.options.find((o) => o.label === answer.label &&
    (d.kind !== 'target' || answer.controller === null ||
      objectInView(view, o.obj)?.controller === answer.controller));
  return option ? [option.index] : null;
}

export function ruleFromAnswer(d: Decision, view: View, chosen: number[]): StickyRule | null {
  const key = stickyKey(d, view);
  if (key === null) return null;
  const name = objectInView(view, d.source)?.name ?? '';
  const tail = triggerPromptLabel(d.prompt);
  const rule: StickyRule = {
    key, kind: d.kind as StickyKind, label: name ? `${name}: ${tail}` : tail,
  };
  const options = chosen.map((index) => d.options.find((o) => o.index === index));
  if (options.some((o) => !o)) return null;
  if (d.kind === 'trigger_order') {
    if (chosen.length !== d.options.length || new Set(chosen).size !== chosen.length) return null;
    rule.order = options.map((o) => triggerEntry(o!, view));
  } else {
    if (chosen.length !== 1 || d.min > 1 || d.max < 1) return null;
    const option = options[0]!;
    const controller = d.kind === 'target' && option.obj ? objectInView(view, option.obj)?.controller : null;
    // A hidden/missing object is not a player target: never turn it into a wildcard.
    if (controller === undefined) return null;
    rule.answer = { label: option.label, controller };
  }
  return rule;
}

/** Names encoded by the key (or each trigger-order entry), never label substrings. */
export function stickySourceNames(rules: readonly StickyRule[]): string[] {
  return [...new Set(rules.flatMap((rule) => rule.kind === 'trigger_order'
    ? (rule.order ?? []).map((entry) => entry.split('\u0000')[0])
    : [rule.key.split('\u0000')[1] ?? '']).filter(Boolean))];
}

export function stickyStorageKey(table: string, match: number): string {
  return `gorge.sticky.${table}:${match}`;
}

/** Corruption empties the whole store; no partially understood auto-answers. */
function parse(raw: string | null): Map<string, StickyRule> {
  if (raw === null) return new Map();
  const value: unknown = JSON.parse(raw);
  if (!Array.isArray(value)) return new Map();
  const rules = new Map<string, StickyRule>();
  for (const r of value) {
    if (!r || typeof r !== 'object' || Array.isArray(r) ||
      typeof r.key !== 'string' || !KINDS.includes(r.kind) ||
      !r.key.startsWith(`${r.kind}\u0000`) || typeof r.label !== 'string' || rules.has(r.key)) return new Map();
    if (r.kind === 'trigger_order') {
      if (r.answer !== undefined || !Array.isArray(r.order) ||
        !r.order.every((e: unknown) => typeof e === 'string' && e.includes('\u0000'))) return new Map();
    } else if (r.order !== undefined || !r.answer || typeof r.answer.label !== 'string' ||
      !(r.answer.controller === null || (Number.isInteger(r.answer.controller) && r.answer.controller >= 0))) return new Map();
    rules.set(r.key, r as StickyRule);
  }
  return rules;
}

/** Memory is authoritative within a tab; sessionStorage restores the same game on reload. */
export function loadSticky(table: string, match: number, storage: Storage | null): ReadonlyMap<string, StickyRule> {
  const scope = `${table}:${match}`;
  const hit = memory.get(scope);
  if (hit) return hit;
  let rules: Map<string, StickyRule>;
  try {
    rules = parse(storage?.getItem(stickyStorageKey(table, match)) ?? null);
  } catch {
    rules = new Map();
  }
  memory.set(scope, rules);
  return rules;
}

export function saveSticky(table: string, match: number, rules: ReadonlyMap<string, StickyRule>, storage: Storage | null): void {
  memory.set(`${table}:${match}`, new Map(rules));
  try {
    storage?.setItem(stickyStorageKey(table, match), JSON.stringify([...rules.values()]));
  } catch {
    // Private mode / quota: the in-memory rules still work.
  }
}

/** Clear only this game, in both layers. */
export function clear(table: string, match: number, storage: Storage | null): void {
  saveSticky(table, match, new Map(), storage);
}
