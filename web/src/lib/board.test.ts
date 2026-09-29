import { describe, expect, it } from 'vitest';
import { attachedTo, everyVisibleCard, findCardAnywhere, groupBattlefield, RECENT_RESOLVE_WINDOW, recentlyMattered, stackFaces, stackIdentical, visibleHand } from './board';
import type { CardView, EventBody, PlayerView, View } from '../protocol';

const card = (id: number, types: string): CardView => ({ id, name: `c${id}`, types, tapped: false, power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false, printing: { name: `c${id}` }, token: `#${id}` });

// protocol.ts types PlayerView.hand as CardView[] (non-nullable), but the
// wire can and does send null (Spectator: Public hides every seat's hand as
// a Go nil slice, which JSON-encodes to null). The cast below reproduces
// that real shape for the test.
const player = (hand: CardView[] | null): PlayerView => ({
  seat: 0, name: 'p', life: 20, lost: false, library_size: 0, hand_size: 0, graveyard_size: 0,
  completed_dungeons: 0,
  hand: hand as CardView[], battlefield: [], graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
});

describe('board', () => {
  it('findCardAnywhere finds an id in each visible zone and the stack, and returns null for a hidden one', () => {
    const v = {
      viewer: 255, visibility: 'public', turn: 1, round: 1, step: 'main1', phase: 'main1', active: 0, priority: 0,
      over: false, draw: false, winner: null, pending: [],
      stack: [{ id: 900, kind: 'spell', name: 's', text: '', controller: 0, targets: [], card: card(900, 'Instant'), optional: false }],
      players: [player([]), { ...player([]), seat: 1, battlefield: [card(11, 'Creature')], graveyard: [card(12, 'Instant')], exile: [card(13, 'Creature')] }],
    } as unknown as View;
    expect(findCardAnywhere(v, 11)?.id).toBe(11);
    expect(findCardAnywhere(v, 12)?.id).toBe(12);
    expect(findCardAnywhere(v, 13)?.id).toBe(13);
    expect(findCardAnywhere(v, 900)?.id).toBe(900);
    expect(findCardAnywhere(v, 777)).toBeNull(); // library / nowhere: hidden or gone
  });
  it('groups lands, creatures and the rest, ordered by id', () => {
    const g = groupBattlefield([card(9, 'Creature Goblin'), card(3, 'Basic Land Mountain'), card(5, 'Artifact'), card(2, 'Creature Human'), card(7, 'Artifact Creature Golem')]);
    expect(g.lands.map((c) => c.id)).toEqual([3]);
    expect(g.creatures.map((c) => c.id)).toEqual([2, 7, 9]);
    expect(g.others.map((c) => c.id)).toEqual([5]);
  });
  it('finds the last resolved object', () => {
    const ev = (seq: number, kind: string, obj?: number): EventBody => ({ event: { seq, kind, player: 0, obj }, line: '' });
    expect(recentlyMattered([ev(1, 'stack_push', 4), ev(2, 'stack_resolve', 4), ev(3, 'tap', 9)])).toBe(4);
    expect(recentlyMattered([ev(1, 'tap', 9)])).toBeNull();
  });
  it('shows a fresh resolve that is inside the recency window', () => {
    const ev = (seq: number, kind: string, obj?: number): EventBody => ({ event: { seq, kind, player: 0, obj }, line: '' });
    // A resolve a few events back from the newest is still within the trailing
    // RECENT_RESOLVE_WINDOW, so the strip shows it.
    const arr = Array.from({ length: RECENT_RESOLVE_WINDOW + 1 }, (_, i) => ev(i, 'tap', i));
    arr[RECENT_RESOLVE_WINDOW - 5] = ev(RECENT_RESOLVE_WINDOW - 5, 'stack_resolve', 42);
    expect(recentlyMattered(arr)).toBe(42);
  });
  it('shows a resolve at the very newest position regardless of the window', () => {
    const ev = (seq: number, kind: string, obj?: number): EventBody => ({ event: { seq, kind, player: 0, obj }, line: '' });
    expect(recentlyMattered([ev(0, 'stack_resolve', 7)])).toBe(7);
  });
  it('clears a resolve that is further back than the window (shows NOTHING)', () => {
    const ev = (seq: number, kind: string, obj?: number): EventBody => ({ event: { seq, kind, player: 0, obj }, line: '' });
    // The only resolve is the OLDEST event of a buffer one longer than the
    // window, so it lies just outside the trailing RECENT_RESOLVE_WINDOW.
    const arr = Array.from({ length: RECENT_RESOLVE_WINDOW + 1 }, (_, i) => ev(i, 'tap', i));
    arr[0] = ev(0, 'stack_resolve', 99);
    expect(recentlyMattered(arr)).toBeNull();
  });
  it('treats the window boundary as inclusive: W events back still shows', () => {
    const ev = (seq: number, kind: string, obj?: number): EventBody => ({ event: { seq, kind, player: 0, obj }, line: '' });
    // Exactly RECENT_RESOLVE_WINDOW events, the resolve at index 0 is the
    // oldest of the trailing window -> still shown.
    const atBoundary = Array.from({ length: RECENT_RESOLVE_WINDOW }, (_, i) => ev(i, 'tap', i));
    atBoundary[0] = ev(0, 'stack_resolve', 11);
    expect(recentlyMattered(atBoundary)).toBe(11);
    // One event more, the same resolve at index 0 falls just outside the
    // trailing window -> cleared.
    const pastBoundary = Array.from({ length: RECENT_RESOLVE_WINDOW + 1 }, (_, i) => ev(i, 'tap', i));
    pastBoundary[0] = ev(0, 'stack_resolve', 11);
    expect(recentlyMattered(pastBoundary)).toBeNull();
  });
  it('clears when a turn or step boundary is newer than the resolve (fb-20260917T231516Z)', () => {
    const ev = (seq: number, kind: string, obj?: number): EventBody => ({ event: { seq, kind, player: 0, obj }, line: '' });
    // The resolved card belongs to the phase it resolved in: the newest turn
    // or step boundary wins over the most recent resolve.
    expect(recentlyMattered([ev(1, 'stack_resolve', 4), ev(2, 'step')])).toBeNull();
    expect(recentlyMattered([ev(1, 'stack_resolve', 4), ev(2, 'turn')])).toBeNull();
    expect(recentlyMattered([ev(1, 'stack_resolve', 4), ev(2, 'tap', 9), ev(3, 'step')])).toBeNull();
  });
  it('keeps showing a resolve that is newer than the newest turn/step boundary', () => {
    const ev = (seq: number, kind: string, obj?: number): EventBody => ({ event: { seq, kind, player: 0, obj }, line: '' });
    // Boundaries OLDER than the resolve do not clear it — only a boundary
    // between the resolve and the newest event does.
    expect(recentlyMattered([ev(1, 'turn'), ev(2, 'step'), ev(3, 'stack_resolve', 4), ev(4, 'tap', 9)])).toBe(4);
  });
  it('treats a wire-null hand as invisible, not an empty array', () => {
    expect(visibleHand(player(null))).toBeNull();
    expect(visibleHand(player([]))).toEqual([]);
    expect(visibleHand(player([card(1, 'Creature')]))).toEqual([card(1, 'Creature')]);
  });
});

describe('attachments', () => {
  const perm = (id: number, types: string, attachedTo?: number): CardView =>
    ({ id, name: `c${id}`, types, printing: { name: `c${id}` }, token: '', tapped: false,
       power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0,
       summon_sick: false, attached_to: attachedTo }) as CardView;

  it('an attachment rides under its host instead of taking a slot of its own', () => {
    const bf = [perm(1, 'Creature'), perm(2, 'Artifact Equipment', 1)];
    const groups = groupBattlefield(bf);
    expect(groups.creatures.map((c) => c.id)).toEqual([1]);
    expect(groups.others).toEqual([]);
    expect(attachedTo(bf, 1).map((c) => c.id)).toEqual([2]);
  });

  it('an attachment whose host is not on this battlefield still gets drawn', () => {
    // A stolen host, or one that left: the attachment must not vanish.
    const bf = [perm(2, 'Enchantment Aura', 99)];
    expect(groupBattlefield(bf).others.map((c) => c.id)).toEqual([2]);
  });

  it('an unattached permanent is unaffected', () => {
    const bf = [perm(1, 'Creature'), perm(2, 'Land')];
    const groups = groupBattlefield(bf);
    expect(groups.creatures.map((c) => c.id)).toEqual([1]);
    expect(groups.lands.map((c) => c.id)).toEqual([2]);
    expect(attachedTo(bf, 1)).toEqual([]);
  });
});

// Stacking fixtures: same-printing Zombie tokens with every key field present
// as a default, so a test can flip one field and watch the group split.
type StackOver = Partial<Pick<CardView, 'name' | 'types' | 'printing' | 'tapped' | 'summon_sick' | 'attacking' | 'damage' | 'power' | 'toughness' | 'counters' | 'keywords' | 'controller' | 'owner' | 'attacking_player' | 'blocked_by' | 'attached_to'>>;
const zombie = (id: number, o: StackOver = {}): CardView => ({
  id, name: 'Zombie', types: 'Creature Zombie',
  printing: { name: 'Zombie', set: 'TOK', number: '25' }, token: `#${id}`,
  tapped: false, power: 2, toughness: 2, damage: 0, attacking: false,
  controller: 0, owner: 0, summon_sick: false, ...o,
});

describe('stackIdentical', () => {
  it('four identical tokens collapse into one group of four, id-sorted', () => {
    const groups = stackIdentical([zombie(9), zombie(4), zombie(7), zombie(1)]);
    expect(groups).toHaveLength(1);
    expect(groups[0].cards.map((c) => c.id)).toEqual([1, 4, 7, 9]);
  });

  it('one tapped separates two otherwise identical permanents', () => {
    const groups = stackIdentical([zombie(1), zombie(2, { tapped: true }), zombie(3)]);
    expect(groups.map((g) => g.cards.map((c) => c.id))).toEqual([[1, 3], [2]]);
  });

  // fb-20260916T201423Z: the lands row merges regardless of tapped state —
  // a pile of Forests is a pile of Forests, and the readiness moves onto
  // the pile's tab instead of sharding the pile.
  it('ignoreTapped merges mixed tapped/untapped same-name cards into one pile, id-sorted', () => {
    // the report's exact shape: two tapped, one untapped
    const groups = stackIdentical([zombie(1, { tapped: true }), zombie(2, { tapped: true }), zombie(3)], { ignoreTapped: true });
    expect(groups.map((g) => g.cards.map((c) => c.id))).toEqual([[1, 2, 3]]);
    // member order stays id-sorted even when the untapped one arrives first
    const groups2 = stackIdentical([zombie(3), zombie(1, { tapped: true }), zombie(2, { tapped: true })], { ignoreTapped: true });
    expect(groups2.map((g) => g.cards.map((c) => c.id))).toEqual([[1, 2, 3]]);
  });

  it('ignoreTapped removes ONLY the tapped component: counters and every other key field still split', () => {
    const tapped = (id: number, o: StackOver = {}): CardView => zombie(id, { tapped: true, ...o });
    expect(stackIdentical([tapped(1), tapped(2, { counters: { p1p1: 1 } })], { ignoreTapped: true })).toHaveLength(2);
    expect(stackIdentical([tapped(1), tapped(2, { summon_sick: true })], { ignoreTapped: true })).toHaveLength(2);
    expect(stackIdentical([tapped(1), tapped(2, { keywords: ['flying'] })], { ignoreTapped: true })).toHaveLength(2);
    expect(stackIdentical([tapped(1), tapped(2, { damage: 2 })], { ignoreTapped: true })).toHaveLength(2);
    expect(stackIdentical([tapped(1), tapped(2, { controller: 1 })], { ignoreTapped: true })).toHaveLength(2);
  });

  // fb-20260917T004545Z: the lands row also drops summoning sickness from the
  // identity. The engine sets SummonSick on every battlefield entry and clears
  // it at the next turn boundary, so a land played THIS round carried a key
  // component its older name-mates lacked and sat in its own pile until then.
  // Fixture is the reported snapshot's exact shape: three Islands, all tapped,
  // the third (88) played during turn 6 and therefore still summon_sick.
  const island = (id: number, o: StackOver = {}): CardView => ({
    id, name: 'Island', types: 'Basic Land Island',
    printing: { name: 'Island', set: 'ZNR', number: '264' }, token: '',
    tapped: true, power: 0, toughness: 0, damage: 0, attacking: false,
    controller: 0, owner: 0, summon_sick: false, ...o,
  });
  const LANDS = { ignoreTapped: true, ignoreSummonSick: true } as const;

  it('a land played this turn joins its name-pile: the reported three-Islands snapshot is ONE pile', () => {
    // the snapshot: 81 and 89 played in earlier turns (sick=false), 88 played
    // during turn 6 (sick=true), all three tapped. With tapped and sickness
    // both out of the lands key they share an identity.
    const groups = stackIdentical([island(81), island(89), island(88, { summon_sick: true })], LANDS);
    expect(groups).toHaveLength(1);
    expect(groups[0].cards.map((c) => c.id)).toEqual([81, 88, 89]);
    expect(groups[0].render).toBe('r81');
  });

  it('the creature and others rows still split a sick member: the split IS gameplay information there', () => {
    // default options (what Quadrant passes creatures/others): sickness stays
    // in the key — a sick zombie cannot attack, so it is a separate pile.
    expect(stackIdentical([zombie(1), zombie(2, { summon_sick: true })])).toHaveLength(2);
    // ...and the lands option is lands-only by contract; nothing else passes it.
    expect(stackIdentical([island(1), island(2, { summon_sick: true })], LANDS)).toHaveLength(1);
  });

  it('the lands option still splits on counters: tapped and sickness are the ONLY components that leave the key', () => {
    expect(stackIdentical([island(1), island(2, { counters: { p1p1: 1 } })], LANDS)).toHaveLength(2);
    expect(stackIdentical([island(1), island(2, { damage: 2 })], LANDS)).toHaveLength(2);
    expect(stackIdentical([island(1), island(2, { controller: 1 })], LANDS)).toHaveLength(2);
    expect(stackIdentical([island(1), island(2, { keywords: ['flying'] })], LANDS)).toHaveLength(2);
    expect(stackIdentical([island(1), island(2, { printing: { name: 'Island', set: 'MH2', number: '1' } })], LANDS)).toHaveLength(2);
  });

  it('a lands pile\'s render key is stable across a member tap/untap AND across the sickness clearing at the turn boundary', () => {
    // the turn boundary: 88's summon_sick clears — membership and lead do not move
    const during = stackIdentical([island(81), island(88, { summon_sick: true })], LANDS)[0];
    expect(during.render).toBe('r81');
    const nextTurn = stackIdentical([island(81), island(88)], LANDS)[0];
    expect(nextTurn.render).toBe('r81');
    expect(nextTurn.key).toBe(during.key); // the pile IS the same pile across the boundary
    // a tap/untap inside the round likewise never churns the render key
    const tapped = stackIdentical([island(81), island(88, { tapped: false })], LANDS)[0];
    expect(tapped.render).toBe('r81');
    expect(tapped.key).toBe(during.key);
  });

  it('an ignoreTapped pile still never merges across a shared attachment host', () => {
    const groups = stackIdentical([zombie(1, { tapped: true }), zombie(2, { tapped: true, attached_to: 1 }), zombie(3, { tapped: true })], { ignoreTapped: true });
    expect(groups.map((g) => g.cards.map((c) => c.id))).toEqual([[1], [2], [3]]);
  });

  it('a mixed ignoreTapped pile keeps its render key stable across a member tap/untap (membership unchanged)', () => {
    // Lead is the lowest id (2), whether it is tapped or not: no reordering
    // to untapped-first, so the keyed-each lifecycle key never moves on a tap.
    const before = stackIdentical([zombie(2), zombie(3, { tapped: true }), zombie(4, { tapped: true })], { ignoreTapped: true })[0];
    expect(before.render).toBe('r2');
    const after = stackIdentical([zombie(2, { tapped: true }), zombie(3), zombie(4, { tapped: true })], { ignoreTapped: true })[0];
    expect(after.render).toBe('r2');
    // with tapped out of the identity, a tap does not move the equivalence
    // string either — the group IS the same pile before and after the tap.
    expect(after.key).toBe(before.key);
    expect(after.cards.map((c) => c.id)).toEqual([2, 3, 4]);
  });

  it('different counters separate two otherwise identical permanents', () => {
    const groups = stackIdentical([
      zombie(1, { counters: { 'p1p1': 2 } }),
      zombie(2, { counters: { 'p1p1': 1 } }),
      zombie(3, { counters: { 'p1p1': 2 } }),
    ]);
    expect(groups.map((g) => g.cards.map((c) => c.id))).toEqual([[1, 3], [2]]);
  });

  it('counter keys are compared as a set, not by object identity or wire order', () => {
    const groups = stackIdentical([
      zombie(1, { counters: { a: 1, b: 2 } }),
      zombie(2, { counters: { b: 2, a: 1 } }),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0].cards.map((c) => c.id)).toEqual([1, 2]);
  });

  it('different derived power separates two otherwise identical permanents', () => {
    // power on the wire is derived: an anthem on one of them showed up there.
    const groups = stackIdentical([
      zombie(1, { power: 3 }),
      zombie(2, { power: 2 }),
      zombie(3, { power: 3 }),
    ]);
    expect(groups.map((g) => g.cards.map((c) => c.id))).toEqual([[1, 3], [2]]);
  });

  it('a permanent with an attachment on it never joins a group', () => {
    const groups = stackIdentical([
      zombie(1),
      zombie(2, { attached_to: 1 }), // a rider on 1
      zombie(3),
    ]);
    // 1 hosts 2, so 1 is individual; 2 is itself attached, so it is too. Only
    // 3 is freely mergeable, but there is nothing left for it to merge with.
    expect(groups.map((g) => g.cards.map((c) => c.id))).toEqual([[1], [2], [3]]);
    expect(groups.every((g) => g.cards.length === 1)).toBe(true);
  });

  it('a permanent that is itself attached never joins a group', () => {
    const groups = stackIdentical([
      zombie(1),
      zombie(2, { attached_to: 9 }), // host 9 is off this row (stolen/elsewhere)
    ]);
    expect(groups.map((g) => g.cards.map((c) => c.id))).toEqual([[1], [2]]);
  });

  it('different controller separates two otherwise identical permanents', () => {
    const groups = stackIdentical([zombie(1, { controller: 0 }), zombie(2, { controller: 1 })]);
    expect(groups).toHaveLength(2);
  });

  it('a blocked attacker is not the same board object as an unblocked one', () => {
    const unblocked = stackIdentical([zombie(1), zombie(2, { attacking: true, attacking_player: 1 })]);
    expect(unblocked).toHaveLength(2);
    const blocked = stackIdentical([
      zombie(1, { attacking: true, attacking_player: 1 }),
      zombie(2, { attacking: true, attacking_player: 1, blocked_by: [5] }),
    ]);
    expect(blocked).toHaveLength(2);
    const sameBlocker = stackIdentical([
      zombie(1, { attacking: true, attacking_player: 1, blocked_by: [5] }),
      zombie(2, { attacking: true, attacking_player: 1, blocked_by: [5] }),
    ]);
    expect(sameBlocker).toHaveLength(1);
  });

  it('different keywords separate two otherwise identical permanents', () => {
    const groups = stackIdentical([zombie(1, { keywords: ['flying'] }), zombie(2, { keywords: ['flying', 'vigilance'] })]);
    expect(groups).toHaveLength(2);
    // same keyword set in a different wire order is still one group
    const same = stackIdentical([zombie(1, { keywords: ['flying', 'vigilance'] }), zombie(2, { keywords: ['vigilance', 'flying'] })]);
    expect(same).toHaveLength(1);
  });

  it('group and member order is stable and id-sorted from unsorted input', () => {
    const groups = stackIdentical([
      zombie(6), zombie(2), zombie(4, { tapped: true }), zombie(3), zombie(1), zombie(5),
    ]);
    expect(groups.map((g) => g.cards.map((c) => c.id))).toEqual([[1, 2, 3, 5, 6], [4]]);
  });

  it('a board with no duplicates produces one group of one per card, in id order', () => {
    const groups = stackIdentical([
      zombie(8, { printing: { name: 'A', set: 'S1', number: '1' } }),
      zombie(3, { printing: { name: 'B', set: 'S1', number: '1' } }),
      zombie(5, { printing: { name: 'A', set: 'S1', number: '2' } }),
    ]);
    expect(groups.map((g) => g.cards.map((c) => c.id))).toEqual([[3], [5], [8]]);
    expect(groups.every((g) => g.cards.length === 1)).toBe(true);
  });

  it('stackFaces: collapsed shows only the first card; expanded shows every member', () => {
    const g = stackIdentical([zombie(2), zombie(5), zombie(9)])[0];
    expect(stackFaces(g, false).map((c) => c.id)).toEqual([2]);
    expect(stackFaces(g, true).map((c) => c.id)).toEqual([2, 5, 9]);
  });

  // The render key (fb-20260915T182335Z): the keyed-each identity Quadrant
  // mounts a CardStack under. It must be the visible lead object's stable
  // identity — NOT the equivalence string — so a kept tile handed a newer
  // CardView for the same object survives the update with its local hover
  // state intact.
  it('the render key survives a state mutation that changes the equivalence key (same lead object)', () => {
    const before = stackIdentical([zombie(7)])[0];
    const after = stackIdentical([zombie(7, { tapped: true })])[0];
    expect(after.key).not.toBe(before.key); // the equivalence string moved: that is the grouping working
    expect(after.render).toBe(before.render); // the lifecycle key did not: the tile is kept
  });

  it('the render key is unique within a row across merged and individual groups', () => {
    const groups = stackIdentical([
      zombie(4), zombie(2), zombie(9, { tapped: true }),
      zombie(6, { attached_to: 5 }), // individual by attachment
    ]);
    const renders = groups.map((g) => g.render);
    expect(new Set(renders).size).toBe(renders.length);
    // groups come back ordered by their lowest member id: the {2,4} pile, then 6, then 9
    expect(renders).toEqual(['r2', 'r6', 'r9']); // each lead object's own id, prefix-isolated from g.key
  });

  it('the render key moves when a pile\'s visible lead changes (the old lead stops fronting that pile)', () => {
    // Two identical tokens, lead 2. Card 2 gains a counter: the pile
    // reshapes — 2 becomes its own individual tile and 3 leads what
    // remains. The render key set moves with it: the pile the reader sees is
    // now fronted by 3, and the tile the reader hovered is still on the
    // battlefield but under its own key.
    const before = stackIdentical([zombie(2), zombie(3), zombie(4)])[0];
    expect(before.render).toBe('r2');
    const after = stackIdentical([zombie(2, { counters: { p1p1: 1 } }), zombie(3), zombie(4)]);
    expect(after.map((g) => g.render)).toEqual(['r2', 'r3']);
    expect(after.find((g) => g.cards.length > 1)?.render).toBe('r3');
    expect(after.find((g) => g.cards.length > 1)?.cards.map((c) => c.id)).toEqual([3, 4]);
  });

  it('the render key disappears with its object: a mid-pile member surfacing as lead re-keys the pile', () => {
    // Pile {3, 4} fronts 3. Card 3 is destroyed: 4 surfaces as the new lead.
    // The old key 'r3' must leave the row (unmounting the tile the reader
    // hovered — the object is gone) and the pile re-keys to 'r4'.
    const before = stackIdentical([zombie(3), zombie(4)])[0];
    expect(before.render).toBe('r3');
    const after = stackIdentical([zombie(4)]);
    expect(after.map((g) => g.render)).toEqual(['r4']);
  });
});

describe('everyVisibleCard — the public-spectator null zones', () => {
  const seat = (over: Partial<PlayerView>): PlayerView =>
    ({
      seat: 0, name: 'Player 1', life: 40, lost: false,
      library_size: 90, hand_size: 7, graveyard_size: 0,
      completed_dungeons: 0,
      hand: [], battlefield: [], graveyard: [], exile: [],
      pool: {}, command: [], commanders: [], commander_casts: [],
      ...over,
    }) as PlayerView;
  const named = (id: number, name: string) => ({ id, name }) as CardView;

  it('survives the null hand and null pool a public spectator really receives', () => {
    // Exactly the wire shape captured from a live `-spectator public` server:
    // hand and pool are literal JSON nulls, every other zone is an array.
    // Spreading that hand crashed the whole table view.
    const players = [
      seat({ hand: null as unknown as CardView[], pool: null as unknown as Record<string, number>, battlefield: [named(66, 'Mountain')] }),
      seat({ seat: 1, hand: null as unknown as CardView[], graveyard: [named(31, 'Hordeling Outburst')] }),
    ];
    const names = everyVisibleCard(players).map((c) => c.name);
    expect(names).toEqual(['Mountain', 'Hordeling Outburst']);
  });

  it('collects every zone for an omniscient view', () => {
    const p = seat({
      hand: [named(1, 'Ponder')], battlefield: [named(2, 'Island')], graveyard: [named(3, 'Brainstorm')],
      exile: [named(4, 'Delve')], command: [named(5, 'Lathliss, Dragon Queen')], commanders: [named(5, 'Lathliss, Dragon Queen')],
    });
    expect(everyVisibleCard([p]).map((c) => c.name)).toEqual([
      'Ponder', 'Island', 'Brainstorm', 'Delve', 'Lathliss, Dragon Queen', 'Lathliss, Dragon Queen',
    ]);
  });

  it('treats a missing players list as empty rather than throwing', () => {
    expect(everyVisibleCard(null)).toEqual([]);
    expect(everyVisibleCard(undefined)).toEqual([]);
  });
});
