import type { CardView, Decision, Option, PotentialAction } from '../protocol';
import { offersPotential } from './castable';

/**
 * cardoptions.ts is the ONE mechanism behind three symptoms (task ui21):
 * "abilities tied to a card should be viewable and actionable from options on
 * that card", "highlight cards with valid options", and "valid targets should
 * get highlighted". They are one fact — *the pending decision offers you
 * something involving this card* — and that fact is already on the wire:
 * every decision's Option carries `obj`, the board object the option
 * concerns, whether it is a cast, an activation, a target, an attacker or a
 * blocker. This module groups a decision's options by that `obj`.
 *
 * R-E4-2 (the client is rules-ignorant): the index grouping is a pure
 * re-grouping of options the server already sent. Nothing here, and nothing
 * the board does with it, decides what an ability is, what a legal target
 * is, or what a blocker is. A target option and a cast option are the same
 * shape on the wire, so ONE index serves both "highlight cards with valid
 * options" and "highlight valid targets". No `kind` is special-cased.
 *
 * R-E4-1 (never resolve an option by position): every function here keys
 * options by their own `index` (carried through from the decision). The tile
 * posts the option's own index — never a position in a list the client
 * rebuilt.
 *
 * Pure functions only: no Svelte, no DOM, no storage. `cardoptions.test.ts`
 * builds decisions by hand and asserts nothing about creatures or combat.
 */

/**
 * OptionTone is the board marking's register — the SAME two senses the seat
 * panel already paints with (toneOf in seatpanel.svelte.ts): `initiative`
 * when the game is BLOCKED on this decision (a target, a block, a mode — no
 * pass is possible), `offered` when a window is open and declining is a
 * legal answer (a cast/activate priority window that also carries a pass).
 * If the board marking uses the same distinction it will read correctly for
 * free; see Table.svelte, which resolves tone via toneOf and hands it in.
 */
export type OptionTone = 'initiative' | 'offered' | 'idle';

/**
 * CardOptions is what the board gets for the pending decision: the options
 * indexed by obj, the seat's picked indices (in click order — for a
 * permutation answer the ORDER is the answer), the tone the marking should
 * wear, and the post callback that hands one option index back to the seat
 * panel (the single answer path, R-E4-1).
 *
 * post's `holdPriority` (prio3) threads the Ctrl modifier from the tile
 * click into SeatPanelState.click's `{ holdPriority }`: Ctrl held while
 * submitting a cast/ability must not arm pass-after-acting for that one
 * action. post's `expectFollowUp` declares that a card-anchored post may
 * hand back a follow-up decision for the same object (fb-e079def5: a
 * multi-ability mana source's stage-1 ability pick, answered through the
 * wheel, must re-open the wheel for its stage-2 colour ask). The ARM itself
 * is SeatPanelState's (fb-20260923T050205Z): panel.click arms it for an
 * `activate` or `mana` answer only (seatpanel.svelte.ts followUpArm), so the
 * tile and the panel's own buttons behave identically. The route's own
 * boardOptions.post is what finally reaches panel.click, so this signature
 * is the contract every tile affordance (direct icon, radial wheel, long
 * list menu -- CardTile, CommanderTile, IdentityBar, HandFan) speaks.
 */
export interface CardOptions {
  /** Object the pending decision resolves for. When absent, ordinary
   * priority/options windows produce no preview relationships. */
  source?: number;
  byObj: Map<number, Option[]>;
  /** byPlayer indexes the same decision's player-target options (see
   *  optionsByPlayer) by the targeted seat, so a seat with nothing else on
   *  the board to target (an opponent, a spell that can only hit a player)
   *  still gets marked. Built alongside byObj from the same decision. */
  byPlayer: Map<number, Option[]>;
  picked: number[];
  tone: OptionTone;
  /** Object whose next local choice should open immediately after a direct
   *  card action posted the preceding decision. */
  autoOpenObj?: number;
  /** later indexes the seat's potential actions (laterByObj) by object: the
   *  abilities the engine WOULD offer on a card once mana floats — indexed
   *  for every permanent that carries one, whether or not the decision also
   *  offers that permanent a live option (fb-20260928T230741Z: an Equipment
   *  at a mana-taps-only window still gets its Equip badge). Absent when
   *  there are none. */
  later?: Map<number, PotentialAction[]>;
  post: (index: number, expectFollowUp?: boolean, holdPriority?: boolean) => void;
}

/**
 * TileOptions is what ONE tile renders: the options its object carries, the
 * already-picked ordinals for that object (in click order — the panel's
 * `{pickedAt + 1}` idiom), the tone, and the post callback. It is null when
 * the pending decision offers this object nothing — which is exactly the
 * "no badge, no mark" state, so the tile never learns a rule to decide it.
 * A later-only tile (list empty, later rows present — fb-20260928T230741Z's
 * Equipment at a mana-taps-only window) is NOT null: it renders the count
 * badge, and opening it shows the not-yet-payable rows as disabled, so the
 * affordance is visible whatever the mana mode.
 */
export interface TileOptions {
  list: Option[];
  pickedOrder: number[];
  tone: OptionTone;
  autoOpen?: boolean;
  /** later is this tile's not-yet-payable abilities (laterByObj): shown in
   *  the picker as disabled rows, never posted. Absent or empty for almost
   *  every tile. */
  later?: PotentialAction[];
  post: (index: number, expectFollowUp?: boolean, holdPriority?: boolean) => void;
}

/**
 * LATER_KINDS are the projected kinds a tile shows as a later row: the
 * mana-costed activations and special actions ON a permanent — "ability", a
 * max-speed "granted" ability (Avishkar Raceway's "{3}, {T}, Discard a card:
 * Draw a card." beside its own mana tap is the Mount Doom shape again), a
 * Room "unlock", a morph "turn_face_up" and a "specialize"
 * (aph-web-manual-only-plays widened the projection to them). Hand casts are
 * handled separately for actual hand objects with no live cast option or
 * payment action; land drops and stations remain excluded. An unknown kind is
 * not indexed, so it can never produce a row.
 */
const LATER_KINDS: ReadonlySet<string> = new Set(['ability', 'granted', 'unlock', 'turn_face_up', 'specialize']);

/**
 * laterByObj indexes the seat's own potential actions (PlayerView.
 * potential_actions, rules.PotentialActions: the engine's own offer walk
 * priced against the mana the seat could float) by object, for every
 * permanent that carries a LATER_KINDS entry the decision does not already
 * offer live (castable.offersPotential), plus eligible plan-less hand casts.
 *
 * fb-20260928T230741Z widened the index from "the objects this decision
 * already offers something" to EVERY permanent with a float-gated
 * LATER_KINDS action. The reporter's ask — "equipment should have a cast
 * button, e.g. equip, in both manual and auto mana mode" — is exactly the
 * shape the old guard refused: at a mana-taps-only window an Equipment's
 * live options are none, so it rendered no affordance at all, and the only
 * way to discover the Equip was to float mana first and look again. The
 * boundary the old guard drew (pinned then in laterabilities.test.ts) was
 * drawn for Mount Doom — whose mana tap IS a live option — and for a
 * face-down morph / locked Room; this request supersedes it: the badge now
 * appears on the permanent with the disabled "tap other mana first" row,
 * whatever the mana mode (the row is a function of potential_actions, which
 * carries no mana-mode dependence). A cast, a land drop and a station stay
 * out (LATER_KINDS), while plan-less hand casts are indexed separately only
 * when they have no live option and no payment action.
 *
 * Why (fb-20260923T033148Z-877b8f8f, "mount doom -- can only play tap for
 * mana, not the other abilities"): the engine offers a mana-costed ability
 * only once its mana floats (the float-then-cast model), so an untapped
 * Mount Doom's live option list is its one mana activation -- and a
 * one-option card acts directly, so the click that was meant to find
 * "{1}{B}{R}, {T}: 1 damage to each opponent" TAPPED Mount Doom for mana,
 * spending the very {T} the damage ability needs. With this index the tile
 * sees that the card has more abilities than the window offers, opens its
 * picker instead of acting, and shows them as disabled "tap other mana
 * first" rows.
 *
 * R-E4-2 holds: nothing is derived here -- the entries are the server's own
 * offer labels and the index is a pure regrouping. They are display-only and
 * carry no wire index, so they can never be posted (R-E4-1). Only a
 * LATER_KINDS entry is indexed, plus hand casts with no live cast option or
 * payment action, so the float-gated plan-less cast is discoverable.
 *
 * fb-20261006T100405Z: a hand cast the ENGINE has proved unpayable
 * (PotentialAction.payable === false, projected by
 * rules.Engine.PotentialActions) is never
 * indexed -- its "tap other mana first" row promised a tapped-out cast no
 * amount of tapping could reach. The verdict is the server's own; the client
 * must not re-derive affordability from mana_cost (castable.ts).
 */
export function laterByObj(
  decision: Decision | null,
  potential: readonly PotentialAction[] | undefined,
  handIds?: readonly number[],
): Map<number, PotentialAction[]> | undefined {
  if (decision === null || decision.kind !== 'priority' || !potential?.length) return undefined;
  const live = optionsByObj(decision);
  const hand = handIds === undefined ? undefined : new Set(handIds);
  let m: Map<number, PotentialAction[]> | undefined;
  for (const a of potential) {
    const handCast = a.kind === 'cast' && a.obj !== undefined && hand?.has(a.obj) === true;
    if ((!LATER_KINDS.has(a.kind) && !handCast) || a.obj === undefined) continue;
    if (handCast && a.payable === false) continue;
    if (handCast && decision.payment_actions?.some((action) => action.cast.object === a.obj)) continue;
    // fb-20260928T230741Z: an object with no live option is indexed too —
    // that is the Equipment case. An entry the decision already offers live
    // (its mana floated) is still not repeated (offersPotential).
    const offered = live.get(a.obj);
    if (offered !== undefined && offersPotential(offered, a)) continue;
    m ??= new Map();
    const list = m.get(a.obj);
    if (list === undefined) m.set(a.obj, [a]);
    else list.push(a);
  }
  return m;
}

/** laterLabel is a later row's text: the server's label plus why it is not
 *  selectable yet. */
export function laterLabel(a: Pick<PotentialAction, 'label'>): string {
  return `${a.label ?? 'Ability'} (tap other mana first)`;
}

/** hasLater reports whether the tile carries any not-yet-payable ability. */
export function hasLater(tile: Pick<TileOptions, 'later'>): boolean {
  return (tile.later?.length ?? 0) > 0;
}

/**
 * The badge's scenario icon, derived from the option's WIRE kind — a pure
 * wire-kind → glyph mapping (R-E4-2: display, not rules). A one-option card
 * acts directly instead of opening a one-row menu. The wire can distinguish a
 * cast, the dedicated mana activation (`activate`, whose server label is
 * "Tap … for mana"), a target candidate (`permanent` object / `player` seat),
 * an attacker declaration and a blocker declaration. A general `ability`
 * option does not carry its cost, so it stays neutral rather than guessing
 * whether that ability taps; every other kind (sacrifice, discard, x, name,
 * …) is equally neutral.
 */
export type SingleActionIcon = 'cast' | 'tap' | 'target' | 'attack' | 'block' | 'action';

/**
 * scenarioIconOf maps ONE option's wire kind to its scenario icon. Exactly the
 * four scenarios the reporter named plus the two pre-existing ones: cast, tap,
 * target (bullseye), attacker (sword), blocker (shield), neutral.
 */
export function scenarioIconOf(kind: string): SingleActionIcon {
  switch (kind) {
    case 'cast': return 'cast';
    // 'mana' is an announced select-mana window's per-ability option
    // (announce-then-pay): the board tile answers it by tapping the source.
    case 'activate': case 'mana': return 'tap';
    case 'permanent':
    case 'player': return 'target';
    case 'attacker': return 'attack';
    case 'block': return 'block';
    default: return 'action';
  }
}

export function singleActionIcon(option: Option): SingleActionIcon {
  return scenarioIconOf(option.kind);
}

/**
 * isSelectionOption keeps the picker open while a MULTI-PICK decision is
 * being answered.
 *
 * An attacker declaration (CR 508.1, decision kind `attackers` with option
 * kind `attacker`) is a SELECTION, not a submission: SeatPanelState.click
 * toggles a non-single decision into `picked` and posts nothing, and the
 * player commits the whole declaration afterwards. Closing the picker on the
 * click therefore interrupts exactly the flow it is meant to serve — the
 * reported fb-20260923T020152Z bug, where each attacker had to be re-opened
 * from the badge.
 *
 * The test is on the OPTION's own wire kind, in one place, so the next
 * selection interaction that shares the `attacker` wire shape cannot miss it.
 * It is deliberately narrow: an ordinary immediate action (`cast`,
 * `activate`, an ability) posts and resolves that action, and the picker
 * closes for it as before. Do not widen this to every multi-pick kind
 * without an interaction report for that kind.
 */
export function isSelectionOption(option: Pick<Option, 'kind'>): boolean {
  return option.kind === 'attacker';
}

/**
 * ACTION_GLYPHS is the icon → text-glyph table the badges render. Plain text
 * glyphs in the existing register (↻ ✦ ›), not colour emoji: the two new
 * pictographs carry an explicit U+FE0E text-presentation selector so a font
 * that owns both a text and an emoji face (U+2694 ⚔ is in Noto Color Emoji)
 * keeps the monochrome one. Verified against the fonts this deployment's
 * browsers fall back to (IBM Plex Mono lacks all three; DejaVu Sans Mono has
 * ◎ and ⚔, Noto Sans Symbols/FreeSans have ⛨) — see the task report.
 */
export const ACTION_GLYPHS: Record<SingleActionIcon, string> = {
  cast: '✦',
  tap: '↻',
  target: '◎',
  attack: '⚔\uFE0E',
  block: '⛨\uFE0E',
  action: '›',
};

/**
 * actionAccessibleLabel is the accessible name + title the DIRECT-action
 * badge wears (the glyph itself is aria-hidden, so this string is all a
 * screen-reader user or a hover ever gets). The wire label already names its
 * scenario for every kind except a target candidate: cast options are
 * "Cast <name>", activations "Tap <name> for mana", attackers "Attack with
 * <name> at <defender>", blockers "<name> blocks <defender>" (rules/legal.go,
 * rules/combat.go, rules/cast.go). A target option's label is only the
 * candidate's own name — "Ari" or "Wasteland (Ari)" (rules/stack.go
 * targetOptionLabel) — so it is prefixed with the scenario verb here, in ONE
 * place, rather than re-phrased per renderer. The target prefix never
 * collides with a raw "Target …" wire label: targetOptionLabel never emits
 * one (it is the candidate's face name, optionally " (<controller>)").
 */
export function actionAccessibleLabel(option: Pick<Option, 'kind' | 'label'>): string {
  return scenarioIconOf(option.kind) === 'target' ? `Target ${option.label}` : option.label;
}

/** The count-badge noun for each icon: "3 targets for Ari", "2 blocks for
 *  Bear" — wording that names the scenario, not just "actions". */
const SCENARIO_NOUN: Record<SingleActionIcon, string> = {
  cast: 'plays',
  tap: 'activations',
  target: 'targets',
  attack: 'attacks',
  block: 'blocks',
  action: 'actions',
};

export interface TileScenario {
  icon: SingleActionIcon;
  /** Count-badge noun, e.g. "targets" for a target list. */
  noun: string;
}

/**
 * tileScenario derives the scenario a TILE's option list names, for the count
 * badge: the scenario only when EVERY option in the list maps to the same
 * icon — a list that genuinely mixes scenarios (a cast next to an ability,
 * a target next to a block) gets null and the badge falls back to the bare
 * neutral count, because a mixed icon would claim a scenario the list does
 * not have. The rule never lies and never needs to know a decision kind.
 */
export function tileScenario(tile: Pick<TileOptions, 'list'>): TileScenario | null {
  if (tile.list.length === 0) return null;
  const first = scenarioIconOf(tile.list[0].kind);
  for (const option of tile.list) {
    if (scenarioIconOf(option.kind) !== first) return null;
  }
  return { icon: first, noun: SCENARIO_NOUN[first] };
}

/**
 * A collapsed pile of interchangeable mana sources can act like one physical
 * button when every offered action has identical object-independent wire
 * semantics. The option with the lowest wire index wins; member/list order is
 * not option identity (R-E4-1). An announced select-mana window's per-source
 * `mana` options (announce-then-pay) collapse the same way: a pile of basic
 * Swamps offering "Add B" each is one tap, while a dual land's two colours
 * differ in wire semantics and keep the picker.
 */
export function singleTapOptionOf(tile: TileOptions): Option | null {
  if (hasLater(tile)) return null;
  if (tile.list.length === 0 || tile.list.some((o) => o.kind !== 'activate' && o.kind !== 'mana')) return null;

  const semantics = (option: Option) => JSON.stringify(
    Object.entries(option)
      .filter(([key]) => key !== 'index' && key !== 'obj')
      .sort(([a], [b]) => a.localeCompare(b)),
  );
  const firstShape = semantics(tile.list[0]);
  if (tile.list.some((o) => semantics(o) !== firstShape)) return null;

  return tile.list.reduce((first, option) => option.index < first.index ? option : first);
}

/** Post one displayed option by its WIRE index (R-E4-1), never list position. `holdPriority` (Ctrl held) skips pass-after-acting for this one action.
 *
 * Every picker post declares a possible card follow-up (task fb-e079def5;
 * the arm is SeatPanelState.followUpArm's, `activate`/`mana` answers only):
 * every option a picker posts is card-anchored (it comes from optionsByObj /
 * optionsByPlayer, so its `obj` names the card the choice was made on), and a
 * card-anchored choice can hand the server a follow-up decision for the SAME
 * object -- a multi-ability mana source's stage-1 ability pick is answered
 * through the wheel, and its stage-2 colour ask must re-open that wheel, not
 * fall back to the seat panel's generic option list. The arm is
 * self-disarming: Table.svelte's $effect decodes it through
 * resolveCardFollowUp, which re-opens the MANA colour wheel only -- the next
 * decision must carry 2-6 options on the expected object AND every option
 * must be of kind 'mana' (fb-20260917T233137Z: without the kind gate a
 * shock land's "pay 2 life?" election -- 2 'mode' options on the land --
 * auto-opened the radial wheel and presented as the mana bubble). */
export function postTileOption(tile: TileOptions, option: Option, holdPriority = false): void {
  tile.post(option.index, true, holdPriority);
}

/**
 * resolveCardFollowUp is the ONE decoder of an armed card-follow-up
 * expectation (task fb-e079def5, generalising the ui24 single-action arm to
 * every card-anchored post). After a card action is posted, the next
 * decision for the seat re-opens that card's picker exactly when it carries
 * 2-6 options on the expected object AND every option is kind 'mana' --
 * the stage-2 colour wheel's shape (server side: rules/mana_activation.go
 * mints every wheel option with Kind "mana" labelled "Add <colour>").
 * Underground Sea's activate → Add U / Add B, a Talisman's stage-1 ability
 * pick → its stage-2 colour wheel.
 *
 * The kind gate (fb-20260917T233137Z) keeps every OTHER card-anchored
 * follow-up off the radial picker: a shock land's pay-life election, a
 * Charm's mode ask, a counter's "pay to save" -- all arrive as 'mode'
 * options on the played card and present through the seat panel and the
 * tile badge instead of popping the mana wheel. Anything else also returns
 * null, so an armed expectation cannot open a wrong picker: a target ask
 * (its options carry the CANDIDATES' objects, never the actor's), a
 * tapped-out source with nothing left to offer (0 options on the object), a
 * one-option follow-up (a direct badge, not a picker), and a >6-option
 * list-menu follow-up all disarm. The caller (Table.svelte's $effect) keeps
 * the sequencing guard -- same-seq decisions and the decision-less frames
 * leave the expectation armed so a follow-up that arrives later still
 * decodes.
 */
export function resolveCardFollowUp(
  expected: { seq: number; obj: number } | null,
  d: Decision | null,
): { seq: number; obj: number } | null {
  if (d === null || expected === null || d.seq === expected.seq) return null;
  if (d.options.length === 0 || !d.options.every((option) => option.kind === 'mana')) return null;
  const count = d.options.filter((option) => option.obj === expected.obj).length;
  return count >= 2 && count <= 6 ? { seq: d.seq, obj: expected.obj } : null;
}

/** Post a direct action by its WIRE index (R-E4-1), never list position. `holdPriority` (Ctrl held) skips pass-after-acting for this one action. */
export function postSingleAction(tile: TileOptions, expectFollowUp = false, holdPriority = false): void {
  if (hasLater(tile)) return;
  const option = tile.list.length === 1 ? tile.list[0] : singleTapOptionOf(tile);
  if (option === undefined || option === null) return;
  if (expectFollowUp) tile.post(option.index, true, holdPriority);
  else tile.post(option.index, false, holdPriority);
}

/**
 * optionsByObj indexes a decision's options by Option.obj, preserving wire
 * order within each object. Options with no `obj` — pass, concede, a mode
 * with no source — deliberately belong to no card and are NOT indexed: they
 * stay reachable only from the seat panel, never lost, just not card-anchored.
 * A null decision (no pending ask for this seat, or a spectator) yields an
 * empty map.
 */
export function optionsByObj(decision: Decision | null): Map<number, Option[]> {
  const m = new Map<number, Option[]>();
  if (decision === null) return m;
  for (const o of decision.options) {
    if (o.obj === undefined) continue;
    const list = m.get(o.obj);
    if (list === undefined) m.set(o.obj, [o]);
    else list.push(o);
  }
  return m;
}

/**
 * optionsByPlayer indexes a decision's options by Option.player, but ONLY
 * for options that target a PLAYER rather than an object — kind `"player"`,
 * the server's own label for a target candidate with no `obj` (rules/stack.go
 * legalTargetCandidates: a player candidate carries `obj: 0`, an object
 * candidate carries `kind: "permanent"`). This is deliberately narrower than
 * "every option missing obj": pass/concede/a sourceless mode also carry no
 * obj but their `player` field names the ACTING seat, not something offered
 * to be targeted — indexing those by player would wrongly mark whoever's
 * turn it is as a legal target. A spell whose only legal target is an
 * opponent (no creatures on board to target) previously marked NOTHING on
 * the board at all, because optionsByObj alone has no way to represent a
 * targetable player — this is the index that closes that gap.
 */
export function optionsByPlayer(decision: Decision | null): Map<number, Option[]> {
  const m = new Map<number, Option[]>();
  if (decision === null) return m;
  for (const o of decision.options) {
    if (o.kind !== 'player') continue;
    const list = m.get(o.player);
    if (list === undefined) m.set(o.player, [o]);
    else list.push(o);
  }
  return m;
}

/**
 * pileTone is the tone a WHOLE pile affordance wears (task fb-20260916T225802Z):
 * when the pending decision offers something to ANY card in the pile — a
 * flashback/escape/warp cast option on a graveyard card, a warp-recast or a
 * may-play land on an exile card, a target option on a card in an
 * OPPONENT's graveyard — the pile's affordance (the identity bar's
 * graveyard/exile icons, the rail's pile buttons) wears the SAME
 * initiative/offered ring the card tiles wear, resolved from the same
 * bundle.tone the tiles read, so "there is something to do in that pile"
 * is the same fact everywhere. A null bundle (spectator, nothing pending)
 * or a pile the decision does not touch reads idle — no ring, no claim.
 * Do not gate by pile owner: a target option on an opponent's graveyard
 * card must glow there too; the engine validates every posted option.
 */
export function pileTone(bundle: CardOptions | null, cards: readonly Pick<CardView, 'id'>[]): OptionTone {
  if (bundle === null) return 'idle';
  for (const card of cards) {
    if (bundle.byObj.has(card.id)) return bundle.tone;
  }
  return 'idle';
}

/** cardOptions returns the options a decision offers concerning `obj`, in
 *  wire order, or null when it offers none on this object. */
export function cardOptions(map: ReadonlyMap<number, Option[]>, obj: number): Option[] | null {
  return map.get(obj) ?? null;
}

/** hasCardOptions is the cheap "does this object have options" a tile asks
 *  every render — the highlight mark and the affordance gate. */
export function hasCardOptions(map: ReadonlyMap<number, Option[]>, obj: number): boolean {
  const list = map.get(obj);
  return list !== undefined && list.length > 0;
}

/**
 * pickedOrdinalsOf is this object's picked options as their CLICK-ORDER
 * ordinals (1-based, the seat panel's `{pickedAt + 1}` idiom): for each pick
 * in the seat's picked list that belongs to one of `ids`, the position it
 * was picked at. A tile shows these so a player sees what they have already
 * picked and in which order — the same number the panel paints. This is
 * display data only; the option's own index is what gets posted, and that is
 * carried by `list`, never recovered from an ordinal.
 */
function pickedOrdinalsOf(ids: ReadonlySet<number>, picked: readonly number[]): number[] {
  const out: number[] = [];
  for (let i = 0; i < picked.length; i++) {
    if (ids.has(picked[i])) out.push(i + 1);
  }
  return out;
}

/**
 * optionSetFor is the pure per-object reduction: this object's options and
 * the picked ordinals among them (in click order). When the decision offers
 * this object nothing it returns null, so a tile that is not involved in the
 * pending decision carries no affordance and no mark.
 */
export function optionSetFor(
  byObj: ReadonlyMap<number, Option[]>,
  obj: number,
  picked: readonly number[],
): { list: Option[]; pickedOrder: number[] } | null {
  const list = byObj.get(obj);
  if (list === undefined || list.length === 0) return null;
  const ids = new Set(list.map((o) => o.index));
  return { list, pickedOrder: pickedOrdinalsOf(ids, picked) };
}

/**
 * optionSetForMany folds a whole stack group (several interchangeable
 * permanents) into ONE option set, in member order then wire order. A stack
 * collapses to one tile, so the tile speaks for every member: if any member
 * is offered anything, the pile is. Because stack members are
 * interchangeable (same identity and state — see stackIdentical), the
 * concatenation is the pile's options, and each option still carries its own
 * index and obj, so posting one is R-E4-1-correct whichever member it came
 * from.
 */
export function optionSetForMany(
  byObj: ReadonlyMap<number, Option[]>,
  objs: readonly number[],
  picked: readonly number[],
): { list: Option[]; pickedOrder: number[] } | null {
  const list: Option[] = [];
  for (const obj of objs) {
    const l = byObj.get(obj);
    if (l !== undefined) list.push(...l);
  }
  if (list.length === 0) return null;
  const ids = new Set(list.map((o) => o.index));
  return { list, pickedOrder: pickedOrdinalsOf(ids, picked) };
}

/** tileOptions hands a single tile its rendered option set plus the tone and
 *  the post callback — or null when the decision offers this object nothing.
 *  A later-only object (fb-20260928T230741Z: an Equipment whose Equip is
 *  gated behind floating mana, with no live option at this window) is NOT
 *  null: `list: []` / `pickedOrder: []` is a valid shape — OptionPicker's
 *  direct is false with an empty list, total counts the later rows, and
 *  hasLater already suppresses direct actions — so the tile renders the
 *  count badge and the picker opens to the disabled rows. */
export function tileOptions(bundle: CardOptions, obj: number): TileOptions | null {
  const set = optionSetFor(bundle.byObj, obj, bundle.picked);
  const later = bundle.later?.get(obj);
  if (set === null && !(later && later.length > 0)) return null;
  return {
    list: set?.list ?? [],
    pickedOrder: set?.pickedOrder ?? [],
    tone: bundle.tone,
    autoOpen: bundle.autoOpenObj === obj,
    ...(later ? { later } : {}),
    post: bundle.post,
  };
}

/** tileOptionsMany hands a collapsed-stack tile the pile's combined option
 *  set — or null when no member is offered anything and no member carries a
 *  later row (fb-20260928T230741Z: a pile of float-gated Equipment still
 *  gets its badge, as a later-only tile). */
export function tileOptionsMany(bundle: CardOptions, objs: readonly number[]): TileOptions | null {
  const set = optionSetForMany(bundle.byObj, objs, bundle.picked);
  // Interchangeable members carry identical later rows; show each once.
  const seen = new Set<string>();
  const later = objs.flatMap((obj) => bundle.later?.get(obj) ?? []).filter((a) => {
    const key = `${a.ability ?? 0}\u0000${a.label ?? ''}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
  if (set === null && later.length === 0) return null;
  return {
    list: set?.list ?? [],
    pickedOrder: set?.pickedOrder ?? [],
    tone: bundle.tone,
    autoOpen: bundle.autoOpenObj !== undefined && objs.includes(bundle.autoOpenObj),
    ...(later.length > 0 ? { later } : {}),
    post: bundle.post,
  };
}

/** playerOptions hands a seat's identity box its rendered option set — the
 *  same shape a card tile gets, so IdentityBar can reuse CardTile's own
 *  single-action/menu affordance — or null when the decision offers this
 *  seat nothing to be targeted by. */
export function playerOptions(bundle: CardOptions, seat: number): TileOptions | null {
  const set = optionSetFor(bundle.byPlayer, seat, bundle.picked);
  if (set === null) return null;
  return { list: set.list, pickedOrder: set.pickedOrder, tone: bundle.tone, autoOpen: false, post: bundle.post };
}

const LOYALTY_COST = /\b(Add|Sub)Counter<(\d+)\/LOYALTY>/;

/**
 * wheelFace is the short text on one radial-wheel button. It must tell a
 * card's options apart at 30px: every "ability" option's label is
 * "<card name>: <description>", so a first-word face read the card name on
 * every button (Jace, the Mind Sculptor's four abilities all showed "Jace,").
 * A planeswalker ability shows its signed loyalty cost, read from the
 * option's Forge-notation cost (+2, 0, −1, −12). Anything else shows the
 * first word of its text with the card-name prefix dropped, cut to fit. The
 * full label stays in the button's help bubble and aria-label.
 */
export function wheelFace(option: Pick<Option, 'kind' | 'label'> & { cost?: string }): string {
  const loyalty = option.cost?.match(LOYALTY_COST);
  if (loyalty) {
    const n = Number(loyalty[2]);
    if (n === 0) return '0';
    return `${loyalty[1] === 'Add' ? '+' : '−'}${n}`;
  }
  if (option.kind === 'mana') return manaWheelFace(option.label);
  const colon = option.label.indexOf(': ');
  const text = colon >= 0 ? option.label.slice(colon + 2) : option.label;
  const word = (text.trim().split(/\s+/)[0] ?? text).replace(/[.,;:]+$/, '');
  return word.length > 6 ? `${word.slice(0, 5)}…` : word;
}

const MANA_PIPS = /^[WUBRGC]+$/;
const MANA_ALTERNATIVES = /^[WUBRGC](, [WUBRGC])* or [WUBRGC]$/;

/**
 * manaWheelFace is a "mana" option's face (the stage-1 "choose a mana
 * ability" wheel, rules/mana_activation.go manaAbilityLabel). Those labels
 * are "Add <production>", optionally behind the ability's extra cost --
 * Phyrexian Tower's paid ability is "Sacrifice 1 creature: Add BB"
 * (fb-20260924T180813Z) -- so the generic first-word face read "Add" on
 * every button. The face is the production (pips as-is, otherwise its first
 * word; "B or R" reads "B/R"), prefixed with the cost's first three letters
 * when there is one: Tower's wheel reads "C" and "Sac BB".
 */
function manaWheelFace(label: string): string {
  const add = label.lastIndexOf('Add ');
  if (add < 0) return label.length > 6 ? `${label.slice(0, 5)}…` : label;
  const produced = label.slice(add + 4).trim();
  const prod = MANA_PIPS.test(produced)
    ? produced
    : MANA_ALTERNATIVES.test(produced)
      ? produced.split(/,\s*|\s+or\s+/).join('/')
      : (produced.split(/\s+/)[0] ?? produced);
  const colon = label.indexOf(': ');
  const face = colon >= 0 && colon < add ? `${label.slice(0, Math.min(colon, 3))} ${prod}` : prod;
  return face.length > 6 ? `${face.slice(0, 5)}…` : face;
}
