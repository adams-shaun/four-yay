import type { View } from '../protocol';
import type { CardOptions } from './cardoptions';

/**
 * seatglow says why a seat's box or header bar glows (UI rework spec §3):
 * verdigris when the pending decision offers that PLAYER as a choice (a
 * target, a player to attack), ember while creatures are attacking them.
 * Both read facts already on the wire: the decision's own player-indexed
 * options (cardoptions.optionsByPlayer) and each creature's
 * attacking_player. Nothing here decides legality.
 */
export interface SeatGlow {
  target: boolean;
  attacked: boolean;
}

export function seatGlow(view: View, options: CardOptions | null, seat: number): SeatGlow {
  const attacked = view.players.some((p) => (p.battlefield ?? []).some((c) => c.attacking && c.attacking_player === seat));
  const target = options !== null && options.byPlayer.has(seat);
  return { target, attacked };
}
