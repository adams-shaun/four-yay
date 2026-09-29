import type { CardView, Decision, Option, View } from '../../protocol';
import { findCardAnywhere } from '../board';

/**
 * combat.ts is the attackers/blockers prompts' pure half (UI rework spec §4:
 * "attackers and blockers are assigned by clicking the board, and the dock
 * summarises the assignments"). It reads the offered options and the view;
 * it decides no legality — the server validates every declaration — and the
 * quick answers it builds (attack with all, no blocks) are ordinary picks
 * the player still commits.
 */

function nameOf(view: View, obj: number | undefined): string {
  if (obj === undefined) return 'a creature';
  return findCardAnywhere(view, obj)?.name ?? `Object ${obj}`;
}

function seatName(view: View, seat: number): string {
  if (seat === view.viewer) return 'you';
  return view.players.find((p) => p.seat === seat)?.name ?? `Seat ${seat}`;
}

/** pickedOptions resolves picked wire indexes to their options, in pick order. */
function pickedOptions(d: Decision, picked: readonly number[]): Option[] {
  return picked.flatMap((i) => d.options.filter((o) => o.index === i));
}

/** attackTarget names what an attacker option attacks: the battle/planeswalker by name, else the defending player. */
function attackTarget(view: View, o: Option): string {
  if (o.battle !== undefined && o.battle !== 0) return nameOf(view, o.battle);
  return seatName(view, o.player);
}

/** attackLines summarises a declaration in progress: "Grizzly Bears → Bob", one line per picked attacker. */
export function attackLines(d: Decision, view: View, picked: readonly number[]): string[] {
  return pickedOptions(d, picked).map((o) => `${nameOf(view, o.obj)} → ${attackTarget(view, o)}`);
}

/** attackOptionText is one attacker row's words, from the option's own objects rather than the engine label. */
export function attackOptionText(d: Decision, view: View, o: Option): string {
  if (o.kind !== 'attacker') return o.label;
  return `${nameOf(view, o.obj)} → ${attackTarget(view, o)}${o.required ? ' (must attack)' : ''}`;
}

/**
 * attackAllPicks is "attack with all": one option per creature that can
 * attack — its first offered defender, or its required pairing when one is
 * marked — capped at the decision's max. Required creatures are placed first
 * so the cap never drops one. It only SELECTS; the player confirms.
 */
export function attackAllPicks(d: Decision): number[] {
  if (d.kind !== 'attackers') return [];
  const chosen = new Map<number, Option>();
  for (const o of d.options) {
    if (o.kind !== 'attacker' || o.obj === undefined) continue;
    const had = chosen.get(o.obj);
    if (had === undefined || (o.required && !had.required)) chosen.set(o.obj, o);
  }
  const all = [...chosen.values()];
  const ordered = [...all.filter((o) => o.required), ...all.filter((o) => !o.required)];
  return ordered.slice(0, d.max).map((o) => o.index);
}

/** noBlocksAllowed reports whether the empty declaration is a legal answer the client may send: min 0 and no forced block. */
export function noBlocksAllowed(d: Decision): boolean {
  return d.kind === 'blockers' && d.min === 0 && !d.options.some((o) => o.required);
}

/** noAttackAllowed is noBlocksAllowed's attackers twin: min 0 and no creature that must attack. */
export function noAttackAllowed(d: Decision): boolean {
  return d.kind === 'attackers' && d.min === 0 && !d.options.some((o) => o.required);
}

export interface BlockLine {
  attacker: number;
  /** "Thragtusk 5/3 attacking you". */
  head: string;
  /** "Blocked by Dark Confidant" or "Unblocked". */
  detail: string;
  blocked: boolean;
}

/** attackersAt lists the creatures attacking the viewer (or a permanent of theirs), plus any attacker a block option names. */
function attackersFacing(d: Decision, view: View): CardView[] {
  const out = new Map<number, CardView>();
  for (const p of view.players) {
    for (const c of p.battlefield) {
      if (c.attacking && c.attacking_player === view.viewer) out.set(c.id, c);
    }
  }
  for (const o of d.options) {
    if (o.attacker === undefined || out.has(o.attacker)) continue;
    const c = findCardAnywhere(view, o.attacker);
    if (c) out.set(c.id, c);
  }
  return [...out.values()].sort((a, b) => a.id - b.id);
}

/** blockLines summarises a block declaration in progress, one line per attacker facing the viewer. */
export function blockLines(d: Decision, view: View, picked: readonly number[]): BlockLine[] {
  const chosen = pickedOptions(d, picked);
  return attackersFacing(d, view).map((a) => {
    const by = chosen.filter((o) => o.attacker === a.id).map((o) => nameOf(view, o.obj));
    const target = a.attacking_player === view.viewer ? 'you' : a.attacking_player != null ? seatName(view, a.attacking_player) : 'you';
    return {
      attacker: a.id,
      head: `${a.name} ${a.power}/${a.toughness} attacking ${target}`,
      detail: by.length > 0 ? `Blocked by ${by.join(' and ')}` : 'Unblocked',
      blocked: by.length > 0,
    };
  });
}

/** blockOptionText is one block row's words: "Dark Confidant blocks Thragtusk". */
export function blockOptionText(d: Decision, view: View, o: Option): string {
  if (o.kind !== 'block' || o.attacker === undefined) return o.label;
  return `${nameOf(view, o.obj)} blocks ${nameOf(view, o.attacker)}${o.required ? ' (must block)' : ''}`;
}

/**
 * unblockedDamage is the footer's "you'd take N (17 → 12)": the printed
 * power of every attacker aimed at the viewer that no picked block covers.
 * It is an estimate from power alone (no trample, first strike or
 * prevention), which the footer says by rounding nothing and claiming no
 * more than "you'd take". Null when nothing unblocked is aimed at the viewer.
 */
export function unblockedDamage(d: Decision, view: View, picked: readonly number[]): { damage: number; life: number } | null {
  const chosen = pickedOptions(d, picked);
  const me = view.players.find((p) => p.seat === view.viewer);
  if (!me) return null;
  let damage = 0;
  for (const a of attackersFacing(d, view)) {
    if (a.attacking_player !== view.viewer) continue;
    if (chosen.some((o) => o.attacker === a.id)) continue;
    damage += Math.max(0, a.power);
  }
  return damage > 0 ? { damage, life: me.life } : null;
}

/** damageText renders unblockedDamage in the spec's words. */
export function damageText(x: { damage: number; life: number } | null): string | null {
  return x === null ? null : `you'd take ${x.damage} (${x.life} → ${x.life - x.damage})`;
}
