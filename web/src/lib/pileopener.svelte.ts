import type { CardView, PlayerView } from '../protocol';
import { visibleHand } from './board';
import { zonesFor, type ZoneName } from './zones';

/**
 * pileopener.svelte.ts is the ONE shared pile-opener state (task
 * fb-20260916T225802Z): the rail's SeatTable pile buttons and the identity
 * bar's new graveyard/exile icons both open the same PileModal, through one
 * store, so a pile cannot open twice at once. The store holds only WHICH
 * pile is open (seat, zone, the trigger element for focus return); the
 * single modal instance that renders it lives in PileHost.svelte, mounted
 * once by Table.svelte, so exactly one dialog can exist in the DOM no
 * matter how many affordances open one. Same shape as layoutsettings's
 * LayoutStore: a class with $state fields, exported as a module singleton.
 *
 * PileZone is ZoneName (graveyard/exile — the two card-list zones
 * zonesFor projects) plus 'hand', the third pile the rail's buttons open;
 * the wire never exposes library cards, so library is not a pile anywhere.
 */
export type PileZone = ZoneName | 'hand';

/** OpenPile is the one pile currently open, with the button that opened it
 *  (PileModal returns focus to it on close — but only when the pile was
 *  opened from the KEYBOARD: a pointer click releases the opener's focus
 *  (lib/pointer.ts pointerRelease), and giving focus back to a control the
 *  pointer never meant to park the keyboard on would re-steal Space/Enter
 *  from the table hotkeys the moment the modal closes. hotkey-focus2.
 *  byPointer is the open click's e.detail > 0; a keyboard opener (Enter on
 *  a focused pile pill, or ViewHotkeys' open-grave/open-exile chords) keeps
 *  the accessibility focus return. */
export interface OpenPile {
  seat: number;
  zone: PileZone;
  trigger: HTMLElement;
  byPointer: boolean;
}

export class PileOpener {
  /** current is the open pile, or null when none is. */
  current = $state<OpenPile | null>(null);

  open(seat: number, zone: PileZone, trigger: HTMLElement, byPointer: boolean): void {
    this.current = { seat, zone, trigger, byPointer };
  }

  close(): void {
    this.current = null;
  }
}

/** The module singleton every affordance and the host read. */
export const pileOpener = new PileOpener();

/**
 * pileCards projects one pile's cards off the wire view — the same read
 * SeatTable's local cardsFor was: the hand only when the view carries it
 * (a hidden hand is null, visibleHand's null-guard), graveyard/exile from
 * zonesFor (whose null-guard absorbs a Go nil slice serialised as JSON
 * null — a redacted zone renders empty, never crashes).
 */
export function pileCards(player: PlayerView, zone: PileZone): CardView[] {
  if (zone === 'hand') return visibleHand(player) ?? [];
  return zonesFor(player).find((summary) => summary.zone === zone)?.cards ?? [];
}

/**
 * pileTitle is the modal's title, in SeatTable's established possessive
 * style ("<Name>'s graveyard"; the player's own name reads "You" and takes
 * "Your"). The caller resolves the display name (the seat's registered
 * name falling back to the wire player name, then the Seat N placeholder).
 */
export function pileTitle(name: string, zone: PileZone): string {
  const who = name === 'You' ? 'Your' : `${name}'s`;
  return `${who} ${zone}`;
}

/**
 * pileLabel is the accessible label in SeatTable's pileLabel style, now
 * shared by both affordances so the two never drift.
 */
export function pileLabel(name: string, zone: PileZone, count: number): string {
  return `View ${name === 'You' ? 'Your' : `${name}'s`} ${zone} (${count} ${count === 1 ? 'card' : 'cards'})`;
}
