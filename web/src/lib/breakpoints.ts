import type { View } from '../protocol';
import type { Breakpoints } from './playsettings';

/**
 * breakpoints is the pure "pause on X" check (UI rework spec §1). It answers
 * one question for decide(): does a rule the player set want this priority
 * window shown to them? It never decides a pass. Firing is keyed: each hit
 * names the object or turn it fired for, and the caller remembers the
 * decision seq it fired at, so a hit stops that window on every re-check and
 * never re-stops later windows for the same thing.
 */

export type BreakpointKind = 'targets-me' | 'watchlist' | 'attacked' | 'stack-depth';

export interface BreakpointHit {
  kind: BreakpointKind;
  /** what it fired for: targets:<id> | watch:<id> | attacked:<turn> | depth:<id> */
  key: string;
  /** plain words for the auto note, no identifiers */
  detail: string;
}

interface Known {
  controller: number;
  name: string;
}

/**
 * index maps every visible object id to its current controller and name. It
 * reads every public zone the View exposes plus the stack:
 *
 * - every CardView[] zone on view.players[*] — battlefield, graveyard,
 *   exile, and hand (present only for the viewer's own seat, a CR 400.2
 *   hidden zone) — found structurally by walking the player object's array
 *   fields and admitting only entries shaped like a CardView (numeric id +
 *   numeric controller), so a zone the view gains later is covered without
 *   this function changing, and a non-card array field can never contribute
 *   a bogus id. view/view.go's cardView() sets Controller for every zone,
 *   so graveyard/exile targets ("target card in a graveyard") count;
 * - view.stack, where a spell or ability object's controller is the seat
 *   that cast/activated it — a counterspell at MY spell on the stack must
 *   count as "targeting me", not just one at my creature.
 *
 * Every entry is the object's CURRENT controller, so an object that changed
 * zones or controllers resolves to whoever holds it now.
 */
function index(view: View): Map<number, Known> {
  const out = new Map<number, Known>();
  for (const p of view.players ?? []) {
    for (const zone of Object.values(p)) {
      if (!Array.isArray(zone)) continue;
      for (const c of zone) {
        if (c !== null && typeof c === 'object' && typeof c.id === 'number' && typeof c.controller === 'number') {
          out.set(c.id, { controller: c.controller, name: typeof c.name === 'string' ? c.name : '' });
        }
      }
    }
  }
  for (const s of view.stack ?? []) out.set(s.id, { controller: s.controller, name: s.name });
  return out;
}

/**
 * targetsSeat reports whether any target is the seat itself or an object the
 * seat currently controls. Every entry is read at its CURRENT controller, so
 * an object that changed hands resolves to whoever holds it now.
 */
export function targetsSeat(
  view: View,
  seat: number,
  top: { targets?: { obj?: number; player: number; is_player: boolean }[] },
): boolean {
  const known = index(view);
  return (top.targets ?? []).some((t) =>
    t.is_player ? t.player === seat : t.obj !== undefined && known.get(t.obj)?.controller === seat,
  );
}

/** attackersOn counts battlefield creatures, controlled by someone else, attacking the seat. */
function attackersOn(view: View, seat: number): number {
  let n = 0;
  for (const p of view.players ?? []) {
    for (const c of p.battlefield ?? []) {
      if (c.attacking && c.attacking_player === seat && c.controller !== seat) n++;
    }
  }
  return n;
}

export function checkBreakpoints(a: {
  view: View;
  seat: number;
  bp: Breakpoints;
  fired: ReadonlyMap<string, number>;
  seq: number;
  /** the caller's yield/Resolve-All baseline says the top object is already consented to: skip the top-object rules */
  skipTop: boolean;
}): BreakpointHit | null {
  const { view, seat, bp, fired, seq, skipTop } = a;
  const live = (hit: BreakpointHit): BreakpointHit | null => {
    const at = fired.get(hit.key);
    return at === undefined || at === seq ? hit : null;
  };
  const stack = view.stack ?? [];
  const top = stack.length > 0 ? stack[stack.length - 1] : null;
  const theirs = top !== null && top.controller !== seat && !skipTop;

  if (bp.targetsMe && theirs && top !== null) {
    const player = (top.targets ?? []).find((t) => t.is_player && t.player === seat);
    const known = index(view);
    const obj = (top.targets ?? []).find((t) => !t.is_player && t.obj !== undefined && known.get(t.obj)?.controller === seat);
    if (player !== undefined || obj !== undefined) {
      const what = player !== undefined ? 'you' : known.get(obj!.obj!)?.name || 'your permanent';
      const hit = live({ kind: 'targets-me', key: `targets:${top.id}`, detail: `${top.name} targets ${what}` });
      if (hit) return hit;
    }
  }

  if (bp.watchlist.length > 0 && theirs && top !== null) {
    const names = [top.name, top.card?.name].filter((n): n is string => typeof n === 'string').map((n) => n.toLowerCase());
    const match = bp.watchlist.find((w) => names.includes(w.toLowerCase()));
    if (match !== undefined) {
      const shown = top.card?.name && top.card.name.toLowerCase() === match.toLowerCase() ? top.card.name : top.name;
      const hit = live({ kind: 'watchlist', key: `watch:${top.id}`, detail: `${shown} is on your watchlist` });
      if (hit) return hit;
    }
  }

  if (bp.attacked) {
    const n = attackersOn(view, seat);
    if (n > 0) {
      const hit = live({ kind: 'attacked', key: `attacked:${view.turn}`, detail: `${n} ${n === 1 ? 'creature is' : 'creatures are'} attacking you` });
      if (hit) return hit;
    }
  }

  if (bp.stackDepth > 0 && stack.length >= bp.stackDepth && top !== null && !skipTop) {
    const hit = live({ kind: 'stack-depth', key: `depth:${top.id}`, detail: `${stack.length} objects are on the stack` });
    if (hit) return hit;
  }
  return null;
}
