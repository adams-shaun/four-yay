/**
 * ids.ts inverts gorge's ManaBrew id mint (internal/manabrew/ids.go):
 * `player-<seat>`, `o<ObjID>` for cards, `s<ObjID>` for stack objects and
 * `h-<zone>-<owner>-<i>` for hidden entries. The opposite direction mints
 * the same shapes for answers.
 */

const num = (s: string | undefined, prefix: string): number | null => {
  if (s === undefined || !s.startsWith(prefix)) return null;
  const rest = s.slice(prefix.length);
  if (!/^\d+$/.test(rest)) return null;
  const n = Number(rest);
  return Number.isSafeInteger(n) ? n : null;
};

export const seatOfPlayerId = (id: string | null | undefined): number | null => num(id ?? undefined, 'player-');
export const objOfCardId = (id: string | null | undefined): number | null => num(id ?? undefined, 'o');
export const objOfStackId = (id: string | null | undefined): number | null => num(id ?? undefined, 's');

export const playerId = (seat: number) => `player-${seat}`;
export const cardId = (obj: number) => `o${obj}`;
export const stackId = (obj: number) => `s${obj}`;

const ZONE_SLOT: Record<string, number> = { hand: 1, library: 2, graveyard: 3, exile: 4, command: 5, battlefield: 6 };

/**
 * hiddenObj is the synthetic object id a hidden entry renders under: always
 * negative (a real ObjID is positive), and stable for a (zone, owner,
 * position) so two snapshots of an unchanged face-down pile diff as
 * unchanged.
 */
export function hiddenObj(zone: string, owner: number, index: number): number {
  return -(((ZONE_SLOT[zone] ?? 7) * 1000 + owner) * 10000 + index + 1);
}
