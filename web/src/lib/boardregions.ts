import type { CardView } from '../protocol';
import { groupBattlefield, stackIdentical, type CardStackGroup } from './board';
import { REGION_KEYS, type Anchor, type LayoutProfile, type RegionKey, type RegionOrder } from './layoutprofile';

/**
 * boardregions projects one seat's battlefield onto the layout profile's
 * board template (UI rework spec §2): each region (creatures, lands, other
 * permanents) sits on a row with a width weight, an anchor to fill from and
 * an order. It decides nothing about the rules — which region a permanent
 * belongs to is groupBattlefield's type-word reading, and what stacks is
 * stackIdentical's strict key, with the shipped lands exception (a pile of
 * lands ignores tapped and summoning sickness, fb-20260916T201423Z and
 * fb-20260917T004545Z).
 *
 * ORDER. Every order breaks ties by ENTRY order, so equal keys never
 * reshuffle; entry order is the wire's battlefield list order (the engine
 * appends a permanent on entry — the plan's ruling, the wire carries no
 * timestamp), with the object id as the final tie-break. Only entry order
 * never moves existing cards: a name or power order can slot a newcomer
 * between two old ones, which the drawer says.
 *
 * An empty region is still returned, so a row keeps its weights and a
 * region that fills from the centre does not jump when a neighbour empties.
 */

export interface RegionSlot {
  key: RegionKey;
  weight: number;
  anchor: Anchor;
  order: RegionOrder;
  groups: CardStackGroup[];
}

export type BoardRow = RegionSlot[];

export interface RegionOptions {
  /** stack identical permanents (the profile's cards.stacking) */
  stacking: boolean;
  /** a focus side strip draws only the creatures, on one row */
  creaturesOnly?: boolean;
}

function singles(cards: CardView[]): CardStackGroup[] {
  return cards.map((c) => ({ key: '#' + c.id, render: 'r' + c.id, cards: [c] }));
}

/** orderGroups sorts groups by the region's order, ties by entry (then id). */
export function orderGroups(groups: CardStackGroup[], order: RegionOrder, entry: ReadonlyMap<number, number>): CardStackGroup[] {
  const first = (g: CardStackGroup) => Math.min(...g.cards.map((c) => entry.get(c.id) ?? Number.MAX_SAFE_INTEGER));
  const lead = (g: CardStackGroup) => g.cards[0];
  const byEntry = (a: CardStackGroup, b: CardStackGroup) => first(a) - first(b) || lead(a).id - lead(b).id;
  const sorted = [...groups];
  if (order === 'name') {
    sorted.sort((a, b) => (lead(a).name < lead(b).name ? -1 : lead(a).name > lead(b).name ? 1 : 0) || byEntry(a, b));
  } else if (order === 'power') {
    sorted.sort((a, b) => lead(b).power - lead(a).power || byEntry(a, b));
  } else {
    sorted.sort(byEntry);
  }
  return sorted;
}

/** regionRows lays a battlefield out as the template's rows, nearest-the-centre row first. */
export function regionRows(battlefield: CardView[], profile: LayoutProfile, opts: RegionOptions): BoardRow[] {
  const entry = new Map<number, number>();
  battlefield.forEach((c, i) => entry.set(c.id, i));
  const grouped = groupBattlefield(battlefield);
  const groupsFor = (k: RegionKey): CardStackGroup[] => {
    const cards = grouped[k];
    const groups = !opts.stacking
      ? singles(cards)
      : k === 'lands'
        ? stackIdentical(cards, { ignoreTapped: true, ignoreSummonSick: true })
        : stackIdentical(cards);
    return orderGroups(groups, profile.regions[k].order, entry);
  };
  if (opts.creaturesOnly) {
    const r = profile.regions.creatures;
    return [[{ key: 'creatures', weight: 1, anchor: r.anchor, order: r.order, groups: groupsFor('creatures') }]];
  }
  const rows: BoardRow[] = [];
  for (const k of REGION_KEYS) {
    const r = profile.regions[k];
    while (rows.length <= r.row) rows.push([]);
    rows[r.row].push({ key: k, weight: r.weight, anchor: r.anchor, order: r.order, groups: groupsFor(k) });
  }
  // Within a row, regions keep the template's fixed key order (creatures,
  // lands, others) so the drawer's table and the board agree.
  return rows.filter((row) => row.length > 0);
}

/** slotUnits is a region's width in card widths: a tapped face is 88/63 wide. */
export function slotUnits(slot: RegionSlot): number {
  return slot.groups.reduce((n, g) => n + (g.cards.every((c) => c.tapped) ? 88 / 63 : 1), 0);
}
