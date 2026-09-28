import type { CardView } from '../protocol';

/**
 * sicktap is the display-side mirror of the engine's summoning-sickness tap
 * gate: rules/mana_activation.go's tapFlagsSick withholds the tap-for-mana
 * option from a creature that started the turn under its controller's
 * control (CR 302.6) unless it has haste. When the engine withholds the
 * option, the client has nothing to badge (and per R-E4-2 must not invent
 * one) — so the Elvish Mystic you just cast shows NO tap affordance and no
 * reason why (fb-20260928T161557Z-2d23d432). This module is the muted
 * affordance's one predicate: on such a creature the tile renders the tap
 * glyph DISABLED with a reason tooltip. It is a CARD-STATE ANNOTATION, not
 * a decision fact: it never posts, never carries an option index, and is
 * never indexed into optionsByObj/TileOptions.
 *
 * The mirror contract — keep these two in step or change them together:
 *
 *   func (e *Engine) tapFlagsSick(source state.ObjID, tap, untap bool) bool {
 *     o := e.G.Obj(source)
 *     if o == nil || (!tap && !untap) || o.Zone != state.ZBattlefield ||
 *        !o.SummonSick {
 *       return false
 *     }
 *     return slices.Contains(e.Derived(source).Types, "Creature") &&
 *            !e.hasKeywordH(source, kwhHaste)
 *   }
 *
 * Every input below is already on the wire, projected for every visible
 * card (view/view.go: SummonSick on every CardView, Produces set from
 * f.ManaProduction() whenever non-zero, Keywords derived — so a granted
 * haste reads here exactly like a printed one, the same fact CardTile's own
 * MARKS map assumes). A sick LAND is never affected: the engine's gate is
 * creature-only, and the existing sick dim + detail chip already make that
 * same distinction (fb-20260917T004545Z).
 */

/** The reason string the muted glyph's tooltip and accessible name carry. */
export const SICK_TAP_REASON = 'summoning sick — untappable until your next turn';

/**
 * sickUntappableManaSource is true for a card the player would try to tap
 * for mana but cannot: an untapped, summoning-sick, non-haste CREATURE that
 * carries a mana ability (card.produces non-null). All five conditions
 * mirror tapFlagsSick plus the "would otherwise be a mana source" gate the
 * engine expresses by simply not offering the option.
 */
export function sickUntappableManaSource(card: CardView): boolean {
  if (!card.types.includes('Creature')) return false; // tapFlagsSick's creature gate
  if (!card.summon_sick) return false; // tapFlagsSick's SummonSick gate
  if (card.tapped) return false; // nothing to tap: the glyph would lie
  if (card.produces == null) return false; // no mana ability: not a mana source
  // tapFlagsSick's haste escape (kwhHaste). Keywords are derived on the
  // wire; compare the head case-insensitively the way MARKS does, so a
  // parameterised or differently-cased entry cannot hide a real haste.
  const haste = (card.keywords ?? []).some((k) => k.toLowerCase().split(' ')[0] === 'haste');
  return !haste;
}
