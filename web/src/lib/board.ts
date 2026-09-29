import type { CardView, PlayerView, View } from '../protocol';

export type Group = 'lands' | 'creatures' | 'others';

/**
 * visibleHand reads a player's hand off the wire view. protocol.ts generates
 * PlayerView.hand as CardView[] (non-nullable), but a Go nil slice
 * serializes to JSON `null`, and any table configured Spectator: Public
 * sends exactly that for every seat: a null hand, not an omitted or
 * empty-array one. Everything that reads a hand should go through this
 * rather than `p.hand` directly, so the null case is handled once.
 */
export function visibleHand(p: PlayerView): CardView[] | null {
  return p.hand ?? null;
}

/**
 * everyVisibleCard flattens every card a view exposes, across every seat and
 * every zone, for callers that need the whole set rather than one zone (the
 * transcript's card-name/colour resolver is the only one today).
 *
 * It exists as a named, tested function rather than an inline flatMap because
 * the inline version shipped a crash: it spread `p.hand` directly, and on a
 * public spectator client that is a literal JSON null, so the whole table view
 * died with "hand is not iterable" and never left "Loading match N...".
 * protocol.ts types all six zones as non-nullable CardView[] and TypeScript
 * therefore cannot catch it -- see visibleHand above for the same trap. Every
 * zone is defended here, not only the two the server nils today, so a zone that
 * becomes hideable later cannot reopen the same hole.
 */
export function everyVisibleCard(players: readonly PlayerView[] | null | undefined): CardView[] {
  const out: CardView[] = [];
  for (const p of players ?? []) {
    if (!p) continue;
    for (const zone of [p.hand, p.battlefield, p.graveyard, p.exile, p.command, p.commanders]) {
      for (const c of zone ?? []) if (c) out.push(c);
    }
  }
  return out;
}

/**
 * cardById resolves the object a log line's `#<id>` tag names against the
 * current view's flattened cards (the same everyVisibleCard list the
 * transcript's colour resolver is built over). It is the id -> CardView half
 * of the card-name hover preview (fb-20260927T154603Z): the transcript's card
 * branch opens the detail panel for the object under the pointer, and the
 * only shared handle between a log line and the live view is the object id.
 *
 * It returns null — never an invented card — for an id that resolves to no
 * visible object (a destroyed permanent, a gone token, an exiled card) and
 * for the redacted id 0, which describe.go renders as "a card" and which is
 * not any single object. A caller that gets null keeps today's title-only
 * affordance; the honest direction is no panel rather than a made-up one.
 */
export function cardById(cards: readonly (CardView | null | undefined)[] | null | undefined, id: number | string): CardView | null {
  const n = typeof id === 'number' ? id : Number(id);
  if (!Number.isInteger(n) || n <= 0) return null;
  for (const c of cards ?? []) if (c && c.id === n) return c;
  return null;
}

/** groupBattlefield sorts a seat's permanents into the three rows a quadrant shows. Type words come from the view; nothing here decides what a card does. Attachments are excluded — attachedTo places them under their host instead. */
export function groupBattlefield(cards: CardView[]): Record<Group, CardView[]> {
  const out: Record<Group, CardView[]> = { lands: [], creatures: [], others: [] };
  const hosts = new Set(cards.map((c) => c.id));
  for (const c of [...cards].sort((a, b) => a.id - b.id)) {
    // An Aura or Equipment whose host is on this battlefield rides under that
    // host (see attachedTo) rather than occupying a slot of its own. One whose
    // host is NOT here — a stolen or otherwise off-board host — still has to
    // be drawn somewhere, so it falls through to its own type row rather than
    // vanishing.
    if (c.attached_to !== undefined && c.attached_to !== 0 && hosts.has(c.attached_to)) continue;
    const words = c.types.split(' ');
    if (words.includes('Creature')) out.creatures.push(c);
    else if (words.includes('Land')) out.lands.push(c);
    else out.others.push(c);
  }
  return out;
}

/** attachedTo returns the permanents attached to host, in id order. The wire carries CardView.attached_to (W1); nothing here decides what an attachment does. */
export function attachedTo(cards: CardView[], host: number): CardView[] {
  return cards.filter((c) => c.attached_to === host).sort((a, b) => a.id - b.id);
}

/** A stack is one group of interchangeable permanents: everything a player would act on matches, so one tile with a count stands in for all of them. `key` is the deterministic identity the group was built from; cards are the members, id-sorted. A group of one is still a group — the caller renders it exactly like a single permanent. */
export interface CardStackGroup {
  key: string;
  /** render is the keyed-each LIFECYCLE key for the group's tile — the stable identity of the group's visible lead object (its first member, cards[0]) — deliberately distinct from `key`, the stacking-equivalence string. Two names on purpose: `key` must change whenever any visible state changes (that is how unlike permanents stay in separate piles), but a DOM key that changes unmounts the tile and destroys its local hover state, so a permanent that merely untapped or stopped attacking lost its open inspector (fb-20260915T182335Z). Object ids are unique across a battlefield, so the lead id is unique within its row; it changes exactly when the described object stops being rendered (it leaves the battlefield, hides behind a new pile lead, or is replaced), which is where closing the inspector is correct. */
  render: string;
  cards: CardView[];
}

/** attachedToId returns the id of the permanent c is attached to, or null when it is unattached. The wire sends 0 for "no host" the same way it omits the field (view.go), so both mean unattached here. */
function attachedToId(c: CardView): number | null {
  return c.attached_to !== undefined && c.attached_to !== 0 ? c.attached_to : null;
}

/** StackOptions tunes what leaves a group's identity. Today only lands use it: a pile of Forests is a pile of Forests whether some of its members tapped for mana or not (fb-20260916T201423Z) — the readiness a tapped split used to carry moves onto the pile's tab (CardStack) instead of sharding the pile. Creatures and every other row keep the strict key, because there a tapped member really cannot do what an untapped one can (attack, block), and the split IS the gameplay information. */
export interface StackOptions {
  /** ignoreTapped removes the tapped component from the stack identity, so same-printing permanents merge into one pile regardless of tapped state. Counters, damage, controller, keywords, attachment state and printing identity all still split. */
  ignoreTapped?: boolean;
  /** ignoreSummonSick removes the summoning-sickness component as well (fb-20260917T004545Z). The ENGINE sets SummonSick on EVERY battlefield entry, lands included, and clears it at the next turn boundary — so a land played this turn carries a key component its older name-mates lack until then and never joins its pile (the reported third Island). For a NONCREATURE land the flag is invisible to every player action: a sick land can still tap for mana (the engine's tap gate checks the CREATURE type explicitly), so two Islands differing only in summon_sick are interchangeable and merging them hides no gameplay information. LANDS-ONLY by contract — Quadrant passes this only for the lands row: a sick CREATURE genuinely cannot attack, and there the split is the gameplay information. */
  ignoreSummonSick?: boolean;
}

/** stackKey is the identity two permanents must share to be interchangeable. Everything a player could act on is included; power/toughness are DERIVED on the wire, so two cards under different anthems already differ here. The string is deterministic: counter keys and the keyword set are sorted before joining, and array/list fields never depend on wire order. With ignoreTapped the 't=' component is omitted and nothing else changes — every other key field still separates. With ignoreSummonSick the 'ss=' component is omitted the same way; the two are the ONLY components any caller may drop. */
function stackKey(c: CardView, ignoreTapped: boolean, ignoreSummonSick: boolean): string {
  const blocked = [...(c.blocked_by ?? [])].sort((a, b) => a - b);
  const counters = Object.entries(c.counters ?? {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  const keywords = [...(c.keywords ?? [])].sort();
  return [
    'p=' + c.printing.name,
    's=' + (c.printing.set ?? ''),
    'n=' + (c.printing.number ?? ''),
    // tapped leaves the identity ONLY when the caller says so; member order
    // stays id-sorted either way, so a tap/untap never moves the group's lead
    // id and the render key below never churns on a tap (fb-20260915T182335Z).
    ...(ignoreTapped ? [] : ['t=' + c.tapped]),
    // summoning sickness leaves the identity ONLY when the caller says so
    // (lands, fb-20260917T004545Z); for a creature the split is gameplay info.
    ...(ignoreSummonSick ? [] : ['ss=' + c.summon_sick]),
    'a=' + c.attacking,
    'd=' + c.damage,
    'pw=' + c.power,
    'tu=' + c.toughness,
    'ap=' + (c.attacking_player ?? ''),
    'bb=' + blocked.join(','),
    'ctr=' + c.controller,
    'ct=' + counters.map(([k, v]) => k + '=' + v).join(','),
    'kw=' + keywords.join(','),
  ].join('|');
}

/**
 * stackIdentical merges same-printing permanents into one group per identity. Merging too aggressively silently hides state, so the merge key is deliberately strict and attachment state wins outright: a permanent that is itself attached, or that another permanent is attached to, is individual by definition — its tile shows riders a group could not compose. When in doubt, do not merge. Groups come back ordered by their lowest member id, members id-sorted, so the caller's layout is stable whatever order the wire delivered.
 */
export function stackIdentical(cards: CardView[], opts?: StackOptions): CardStackGroup[] {
  const ignoreTapped = opts?.ignoreTapped === true;
  const ignoreSummonSick = opts?.ignoreSummonSick === true;
  const hosts = new Set<number>();
  for (const c of cards) {
    const host = attachedToId(c);
    if (host !== null) hosts.add(host);
  }
  const mergeable = (c: CardView) => attachedToId(c) === null && !hosts.has(c.id);

  const byKey = new Map<string, CardView[]>();
  for (const c of cards) {
    if (!mergeable(c)) continue;
    const key = stackKey(c, ignoreTapped, ignoreSummonSick);
    const group = byKey.get(key);
    if (group === undefined) byKey.set(key, [c]);
    else group.push(c);
  }
  const groups: CardStackGroup[] = [];
  for (const [key, cs] of byKey) {
    cs.sort((a, b) => a.id - b.id);
    groups.push({ key, render: 'r' + cs[0].id, cards: cs });
  }
  // A permanent that may not merge stays its own group of one, keyed by its
  // id so it can never collide with an identity key.
  for (const c of cards) {
    if (mergeable(c)) continue;
    groups.push({ key: '#' + c.id, render: 'r' + c.id, cards: [c] });
  }
  groups.sort((a, b) => a.cards[0].id - b.cards[0].id);
  return groups;
}

/** stackFaces picks the faces a stack group renders in a given expansion state: collapsed shows only the first card (the fan stands in for the rest), expanded shows every member. Keeping the mapping here lets the collapsed/expanded contract be tested without a DOM. */
export function stackFaces(group: CardStackGroup, expanded: boolean): CardView[] {
  return expanded ? group.cards : [group.cards[0]];
}

/**
 * RECENT_RESOLVE_WINDOW bounds how far back the resolved-card spotlight looks.
 * Measured against live games on the demo server: ~166-203 events arrive per
 * player turn; consecutive resolves in an active burst are 10-40 events apart;
 * the dominant "one resolve per turn" cluster is 161-230 events; and multi-turn
 * quiet periods are 230+. 100 sits comfortably above the burst cluster (so the
 * strip never flickers during active play) and above the ~85-events-per-action
 * bookkeeping density, yet below the one-per-turn cluster (so a card no longer
 * pedals the spotlight from turn to turn and instead clears within roughly half
 * a player turn). Bounded by event distance, not wall clock, so the module stays
 * pure and its tests stay deterministic.
 */
export const RECENT_RESOLVE_WINDOW = 100;

/** recentlyMattered is the object id of the most recent stack_resolve, for the rail stack section's resolved-card display (the old board overlay's data path, kept), but only if that resolution is the newest interesting event in the trailing RECENT_RESOLVE_WINDOW: a turn or step boundary newer than it clears the display (fb-20260917T231516Z — a resolved card belongs to the phase it resolved in, so the row must not pedal forward across turn/phase changes), and a resolve further back than the window also returns null. The parameter is structural — the display's caller (Rail) carries a widened event shape — so any object whose `event` names a kind (and optionally an obj) works. */
export function recentlyMattered(events: { event: { kind: string; obj?: number } }[]): number | null {
  const lo = Math.max(0, events.length - RECENT_RESOLVE_WINDOW);
  for (let i = events.length - 1; i >= lo; i--) {
    const e = events[i].event;
    if (e.kind === 'stack_resolve' && e.obj) return e.obj;
    // 'turn' and 'step' are the server's own boundary kinds (events/event.go
    // kindNames; logfilter.ts keys on 'step' too), so no extra wiring is
    // needed — every EventBody in the DVR list carries them.
    if (e.kind === 'turn' || e.kind === 'step') return null;
  }
  return null;
}

/** findCardAnywhere locates an object id across every visible zone of the
 *  view: each seat's battlefield, graveyard, exile and visible hand, then
 *  the stack. It exists for the resolved-card display — a resolved object
 *  has already LEFT the stack by the time the view renders, so the id is
 *  looked up where it landed. A card moved somewhere hidden (the library)
 *  returns null, and the display simply shows nothing. Previously an
 *  inline helper of the deleted RecentStrip; hoisted here so the display
 *  and its tests share one implementation. */
export function findCardAnywhere(v: View, obj: number): CardView | null {
  for (const p of v.players) {
    for (const list of [p.battlefield, p.graveyard, p.exile, visibleHand(p) ?? []]) {
      const c = list.find((x) => x.id === obj);
      if (c) return c;
    }
  }
  for (const s of v.stack) if (s.card?.id === obj) return s.card;
  return null;
}
