// Package view projects one seat's view of a state.Game: a hidden zone
// contributes a count and nothing else unless the viewer owns it, another
// seat's decision is never attached, and everything that can or will hit
// the stack (the user's requirement R3) is described for every seat. It is
// the only package a client-facing layer needs to read game state through —
// nothing here leaks a rules concept the client would have to understand.
//
// RedactEvents (redact.go) does the same job for the event log: it is
// state-aware, not merely Secret-flag-aware, because an event's Player
// field does not always name the seat whose secret its payload is (a
// trigger's controller and the owner of the card it remembered can be two
// different seats) — see RedactEvents' own doc for the three rules. An
// events.Note is always public unless its own emitter marks it Secret
// (Ruling T23-w): it is the engine's explicit "tell everyone" channel.
package view

import (
	"cmp"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Chars is what the view asks the engine for: derived characteristics and
// the triggers that have matched but are not yet on the stack. *rules.Engine
// satisfies it (view_test.go pins that with a compile-time assertion);
// tests here use a flat stand-in instead of importing rules.
//
// Ruling F2: this is named Keywords, not Derived — Engine.Derived already
// returns a struct, and a method of that name could not also satisfy an
// interface expecting a slice.
type Chars interface {
	Power(state.ObjID) int32
	Toughness(state.ObjID) int32
	Keywords(state.ObjID) []string
	// PendingTriggers is R3: everything that WILL hit the stack, once its
	// controller has ordered it or its decider has accepted it, must be
	// observable too — not only what already has.
	PendingTriggers() []state.PendingTrigger
	// StackOptional is Ruling VW-1: an optional triggered ability that is on
	// the stack, awaiting its resolution-time yes/no (CR 603.5), reports that
	// it is optional and who answers it — the optionality lives on the stack
	// entry, not the queue, because under CR 603.5 the ability was pushed
	// unconditionally and the question is posed as it resolves. It reports
	// not-optional for a mandatory trigger, an activated ability, or a
	// trigger whose decider has left the game.
	StackOptional(id state.ObjID) (optional bool, decider state.PlayerID)
	// AvailableMana is the engine's answer to the seat-box line 3: the mana
	// this player could produce right now by activating the free-to-tap mana
	// abilities of untapped permanents it controls. It is derived from the
	// battlefield (a public zone), so unlike the floating Pool it may be
	// shown for every seat and every visibility, and it is what populates
	// that line in the ordinary case where the floating pool is empty.
	AvailableMana(state.PlayerID) state.Mana
	// AbilityCosts returns the current offer-time costs of id's non-mana
	// activated abilities for player p, in face ability order. A rules.Engine
	// applies the same RaiseCost/ReduceCost composition as legalActions, so a
	// client deciding whether tapping mana would unlock an activation cannot
	// disagree with the engine by pricing only the printed cost.
	AbilityCosts(state.PlayerID, state.ObjID) []string
	// MayLookAtLibraryTop is the Continuous MayLookAt grant's read (Oracle
	// of Mul Daya): whether p may look at the top card of their own library
	// right now. The view uses it to reveal that top card to p's own seat
	// only; a nil ch degrades to false, the same way it degrades every
	// other derived fact.
	MayLookAtLibraryTop(state.PlayerID) bool
	// PotentialActions is the seat's own "what could I still do after tapping
	// out" projection: the engine's legal-offer walk priced against the
	// hypothetical pool its untapped sources could produce (rules.
	// PotentialActions). Only real plays are carried (cast/ability/play_land,
	// never the mana tap), so a client's stop decision reads this instead of
	// re-deriving castability from printed costs -- which drifts from the
	// engine on every cost rule (live RaiseCost/ReduceCost, command-zone
	// casts, flashback, X at 0, indeterminate sources). The projection reads
	// the seat's own hidden zones, so the view attaches it to the viewer's
	// own seat only.
	PotentialActions(state.PlayerID) []decision.PotentialAction
	// OwnDeck is the viewer's own genesis deck manifest, or nil when the
	// implementation has none. It is a member of Chars (not an optional
	// capability probed by type assertion) so that a Chars WRAPPER -- such
	// as searchprobe's noPotentialChars -- forwards it to whatever it
	// embeds; an assertion against the wrapper sees only the embedded
	// interface's methods and would silently drop the manifest.
	OwnDeck(state.PlayerID) *deck.Manifest
}

// View is one seat's complete picture of the game: everything public, plus
// whatever is theirs alone (their hand, their mana pool, a decision asked of
// them).
type View struct {
	Viewer  state.PlayerID `json:"viewer"`
	OwnDeck *deck.Manifest `json:"own_deck,omitempty"`
	// Visibility names which rule set built this view: "seat", "public" or
	// "omniscient" (see Visibility).
	Visibility string `json:"visibility"`
	// Turn is the engine's per-player-turn counter (TurnChange events). It
	// increments once per seat's turn, so a four-seat table reads "Turn 22"
	// after five and a half rounds. Round is the exact round-trip count of the
	// players still alive, folded over the ordered event stream (RoundOf) by
	// host, which has the log in hand when it builds this view; a caller with
	// only a snapshot gets the roundOf approximation instead. Both Turn and
	// Round are sent because the raw engine counter is what the transcript
	// ("Turn N: <player>") refers to, and the round is what the board's
	// clock shows.
	Turn     int32          `json:"turn"`
	Round    int32          `json:"round"`
	Step     string         `json:"step"`
	Phase    string         `json:"phase"`
	Active   state.PlayerID `json:"active"`
	Priority state.PlayerID `json:"priority"`
	Over     bool           `json:"over"`
	Draw     bool           `json:"draw"`
	// Winner is nil unless Over && !Draw: PlayerID's zero value is seat 0, a
	// real seat, so a bare PlayerID field could never distinguish "seat 0
	// won" from "the game is still going" or "it was a draw" (Task 22
	// finding 5). A JSON null is unambiguous where a bare 0 would not be.
	Winner *state.PlayerID `json:"winner"`
	// Players, Stack and Pending (below) are every public list this type
	// carries. All of them are built non-nil even when empty (Ruling
	// T23-u): a client should never have to treat a bare JSON `null` and an
	// empty `[]` as the same "nothing here" case for one of these, the way
	// it legitimately must for Hand/Pool below.
	Players []PlayerView `json:"players"`
	// Stack keeps g.Stack's own order: index 0 is the bottom, the last
	// entry is the top. Public for every seat — R3.
	Stack []StackView `json:"stack"`
	// Pending is the trigger queue, in the order it will be placed on the
	// stack. Public for every seat — R3.
	Pending  []PendingView      `json:"pending"`
	Decision *decision.Decision `json:"decision,omitempty"`
}

// commanderViews builds a player's commander roster and its parallel
// command-zone cast counts in one pass, so a commander whose object is
// somehow absent (defensive -- a dangling roster id) is dropped from BOTH
// lists and the pair stays aligned. The roster CardViews project the
// commander's current zone's state like ordinary zone CardViews; the cast
// count is Player.CmdCasts's entry for that commander, whatever zone it
// currently occupies.
func (p *projector) commanderViews(buf []CardView, castBuf []int32, ids []state.ObjID, casts []int32) ([]CardView, []int32) {
	cmds := refill(buf, len(ids))
	cs := refill(castBuf, len(ids))
	for k, id := range ids {
		o := p.g.Obj(id)
		if o == nil || o.Face() == nil || o.Ephemeral() {
			continue
		}
		n := len(cmds)
		cmds = cmds[:n+1]
		p.cardView(&cmds[n], id)
		if k < len(casts) {
			cs = append(cs, casts[k])
		} else {
			cs = append(cs, 0)
		}
	}
	return cmds, cs
}

// DungeonView is the public projection of one active dungeon.
type DungeonView struct {
	Name string `json:"name"`
	Room string `json:"room"`
}

// dungeonRoomName resolves the marker's script key to the printed room label.
// A missing or malformed RoomName$ falls back to the key rather than hiding
// the marker; the venture logic owns validation of which key can be entered.
func dungeonRoomName(face *cards.Face, key string) string {
	if key == "" {
		return ""
	}
	for rest, more := face.SVars[key], true; more; {
		var field string
		field, rest, more = strings.Cut(rest, "|")
		name, value, ok := strings.Cut(strings.TrimSpace(field), "$")
		if ok && name == "RoomName" && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return key
}

// PlayerView is one seat's own public state, plus (only when this is the
// viewer's own seat) the private parts.
//
// ID's tag is "seat", not "id": state.PlayerID and state.ObjID are both
// small integers, and this package's own leak test proves the collision is
// real -- a four-seat board's low ObjIDs (the very first cards dealt) land
// in the same 0-3 range as every PlayerID, so an "id" tag here would make a
// CardView's object id and a PlayerView's seat number indistinguishable by
// key name alone.
type PlayerView struct {
	ID            state.PlayerID `json:"seat"`
	Name          string         `json:"name"`
	Life          int32          `json:"life"`
	Lost          bool           `json:"lost"`
	LibrarySize   int            `json:"library_size"`
	HandSize      int            `json:"hand_size"`
	PlanarDeck    []CardView     `json:"planar_deck,omitempty"`
	GraveyardSize int            `json:"graveyard_size"`
	// LibraryTop is the player's own library's top card, revealed only when
	// a live Continuous MayLookAt grant covers it (Oracle of Mul Daya's
	// "play with the top card of your library revealed") and only to that
	// player's own seat -- the CR 400.2 hidden-zone redaction the Hand field
	// documents applies to it in full. nil (an omitted JSON key) for every
	// other seat and whenever no grant is live.
	LibraryTop *CardView `json:"library_top,omitempty"`
	// Library is the viewer's own library as an UNORDERED list of cards:
	// the contents a human knows about their own deck, without the secret
	// order CR 400.2 hides from every player (including its owner, who may
	// not reorder or inspect it). It is sorted into a canonical order (by
	// card name, then object id) that is a pure function of the CONTENTS,
	// so it never preserves and never leaks the library's actual order.
	// Like Hand it is a CR 400.2 hidden zone and is filled only for the
	// viewer's own seat (widened by CR 720.4 to a seat the viewer controls);
	// for every other seat it is nil and omitted from the wire, and a
	// spectator never matches the gate. LibrarySize (an int) and LibraryTop
	// (one card, gated on a MayLookAt grant) are not it.
	Library []CardView `json:"library,omitempty"`
	// Hand is nil (marshalling to a literal JSON null, not an omitted key --
	// it deliberately carries no "omitempty" tag) for every seat but the
	// viewer's own, whose Hand is always non-nil even when empty ("[]").
	// omitempty cannot express "present but possibly empty": with it, the
	// viewer's own EMPTY hand would have marshalled identically to another
	// seat's HIDDEN one (the key simply missing either way), which is exactly
	// the ambiguity this type exists to avoid everywhere else (Winner's own
	// *PlayerID is the same shaped fix). null-vs-[] is what a client checks
	// instead. Hand is a hidden zone under CR 400.2 and is gated on "is this
	// the viewer's own seat", widened by CR 720.4 to a seat the viewer
	// controls (you may look at all cards that player could see), unlike Pool
	// (next field), which is public.
	Hand        []CardView `json:"hand"`
	Battlefield []CardView `json:"battlefield"`
	Graveyard   []CardView `json:"graveyard"`
	Exile       []CardView `json:"exile"`
	// Pool is the mana currently floating in this player's pool. It is
	// PUBLIC information under the CR: a mana pool is not one of the seven
	// zones in CR 400.1 and holds no cards, so CR 400.2's hidden-zone
	// framework (library and hand) has no purchase on it; instead CR 106.4a,
	// 106.4b, 117.3d and 118.3a all require a player to ANNOUNCE what is in
	// their pool, an obligation that is incoherent for information meant to
	// be hidden. So Pool is projected for every seat under every visibility
	// (seat, public, omniscient), like Available. It is always non-nil, even
	// when empty ("{}"), because there is no longer a hidden state to
	// distinguish: an empty pool is simply "{}". The field carries no
	// omitempty, so it is always present even when zero -- the one wire
	// distinction it keeps against Available (which carries omitempty and is
	// absent when nothing is available).
	Pool map[string]int32 `json:"pool"`
	// PoolRestrictions annotates Pool: one entry per batch of floating mana
	// whose producing ability carried a RestrictValid$ spend limit, naming
	// the colour, the amount and a human-readable spend text. It exists
	// because Pool alone renders a bare spendable-looking chip for mana a
	// cast may in fact refuse (Cavern of Souls' {B} that pays only for a
	// creature spell of the chosen type), leaving the seat unable to learn
	// why no cast was offered. Like Pool it is public information -- the
	// restriction is derived from a public battlefield permanent's own
	// ability (CR 106.4a/106.4b), so it is projected for every seat under
	// every visibility. It carries omitempty: a seat holding no restricted
	// mana is absent, never a JSON null, so every view without a restriction
	// serialises byte-identically to before this field existed (the
	// Available convention). A batch whose Valid is empty -- an
	// AddsNoCounter$-only batch, which imposes no spend limit -- is omitted:
	// this field names spend restrictions, and such a batch has none.
	PoolRestrictions []PoolRestrictionView `json:"pool_restrictions,omitempty"`
	// PotentialActions is this seat's "what could I still do after tapping
	// out" projection, filled ONLY for the viewer's own seat (the walk reads
	// the seat's own hand, command zone and graveyard -- a CR 400.2 hidden
	// zone, so another seat's would leak it). Empty/nil means the engine
	// would offer nothing the seat could pay for even after floating every
	// untapped source: the auto-pass stop decision reads this field and
	// nothing else, so it inherits the engine's own cost rules (live
	// RaiseCost/ReduceCost, commander tax, command-zone and flashback casts,
	// X priced at 0, indeterminate sources) instead of a client-side
	// re-derivation of them. It carries omitempty: a seat with no potential
	// actions is absent, like Available.
	PotentialActions []decision.PotentialAction `json:"potential_actions,omitempty"`
	// Available is what this player could produce right now by tapping
	// untapped permanents' free-to-tap mana abilities — the "free mana one
	// gains by tapping lands or other effects" half of the seat box's line
	// 3. It is derived from the battlefield (public), so it is filled for
	// every seat under every visibility (seat, public, omniscient), never
	// gated on "is this the viewer's own seat". It is always non-nil, even
	// when empty ("{}"), like Pool. It is separate from Pool on purpose — a
	// reader must never mistake mana that could be tapped for mana already
	// floating.
	// It carries omitempty (so an empty availability is absent, never a JSON
	// null): Available is a public quantity that is only ever present-when-
	// nonzero. Pool, in contrast, carries no omitempty and is always present
	// as an object. This mirrors the sibling CmdDamage field, another public
	// per-player map that is omitted when zero rather than sent as {}.
	Available map[string]int32 `json:"available,omitempty"`
	// Command is the command zone (CR 903.6): the player's commanders
	// currently sitting there, in zone order. A commander leaves it when it
	// is cast (the object itself moves; its id is stable), so Command is the
	// ever-shrinking subset of Commanders that can still be cast from the
	// command zone under the CR 903.8 tax. Public for every seat -- ZCommand
	// is not a hidden zone, and commander identity is open information.
	Command []CardView `json:"command"`
	// Commanders is the player's full commander roster -- the same list
	// m30's genesis built, in the same order, never shrunk as commanders
	// are cast or die. A roster CardView projects the commander's CURRENT
	// zone's state (a cast commander is a battlefield object, a dead one a
	// graveyard object), so the roster is what lets a client -- and the bot
	// policy's view-shaped half -- tell that a battlefield creature is a
	// commander (the CR 903.10 clock's subject) even when it has left the
	// command zone and not yet dealt damage. Public for every seat: the
	// identity of a player's commanders is the premise of the format.
	Commanders []CardView `json:"commanders"`
	// Dungeon is this player's active public dungeon and venture room.
	Dungeon *DungeonView `json:"dungeon,omitempty"`
	// CompletedDungeons is the number of dungeons this seat has completed.
	CompletedDungeons int32 `json:"completed_dungeons"`
	// Counters are the player's own counters by kind (poison, energy,
	// experience, ...; state.Player.Counters). Public like the life total;
	// omitted when the seat holds none, so a view without player counters
	// serialises byte-identically to before this field existed.
	Counters map[string]int32 `json:"counters,omitempty"`
	// LandDropSpent is true when the seat may not play another land this
	// turn by the land-drop count alone (lands played this turn has reached
	// the allowance, Exploration-style extras included): the negation of
	// MageZero's CanPlayLand, stated this way so the ordinary state -- a land
	// still playable -- is the omitted zero. Timing (whose turn, which step,
	// the stack) is deliberately not folded in: the fact is the count's.
	LandDropSpent bool `json:"land_drop_spent,omitempty"`
	// HasInitiative is the CR 726.1 initiative designation: true for the one
	// player who currently has it. Public for every seat -- like the monarch,
	// the designation is open information and drives attacking decisions. It
	// carries omitempty so a match with no initiative (the common case)
	// serialises byte-identically to before this field existed; an absent key
	// means "this seat does not have the initiative", the Available
	// convention.
	HasInitiative bool `json:"has_initiative,omitempty"`
	// CommanderCasts runs parallel to Commanders: entry k is how many times
	// Commanders[k] has been cast from the command zone, the CR 903.8 tax
	// base for its next command-zone cast (an additional {2} per prior
	// cast). Public for every seat -- the count is derived from public
	// events.
	CommanderCasts []int32 `json:"commander_casts"`
	// CmdDamage is the commander damage this player has taken (CR 903.10),
	// keyed by each commander's object id -- the 21-damage clock, public
	// for every seat like a life total. nil/absent when the player has
	// taken no commander damage (omitempty: absence is zero), so a
	// Constructed game never pays for a per-player empty map.
	CmdDamage map[state.ObjID]int32 `json:"cmd_damage,omitempty"`
	// Archetype is a posterior over this seat's deck archetype, inferred
	// from the cards REVEALED about it (its public battlefield, graveyard,
	// exile and command-zone lists). It is filled for every seat EXCEPT the
	// viewer's own -- the fact is an OPPONENT archetype posterior, and a
	// seat's own deck identity is the manifest's job (deck.File.Archetype),
	// not something to re-infer. It is nil when the viewer is a spectator
	// or nothing classifiable has been revealed yet, and it never reads a
	// hidden zone (a hidden hand is not a CardView at all) or a card name.
	// Like Available it carries omitempty so a view with no posterior
	// serialises byte-identically to before the field existed.
	Archetype *ArchetypePosterior `json:"archetype,omitempty"`
}

// PoolRestrictionView is one restricted floating-mana batch as the wire
// sees it: the produced symbol verbatim (a bare WUBRGC letter, an "S<colour>"
// snow unit or a "<Tag><colour>" typed unit, exactly ManaRestriction.Color),
// the unit count, and Text -- a sentence naming what the mana may be spent
// on, or the raw Valid$ string when the formatter does not recognise the
// shape (an honest floor).
type PoolRestrictionView struct {
	Color  string `json:"color"`
	Amount int32  `json:"amount"`
	Text   string `json:"text"`
}

// PendingView is a trigger that will hit the stack once its controller has
// ordered it / its decider has accepted it. R3.
type PendingView struct {
	Source     state.ObjID     `json:"source"`
	Controller state.PlayerID  `json:"controller"`
	Label      string          `json:"label"`
	Optional   bool            `json:"optional"`
	Decider    *state.PlayerID `json:"decider,omitempty"` // nil unless Optional
}

func displayName(p *state.Player) string {
	if p.PlayerName != "" {
		return p.PlayerName
	}
	return p.Name
}

// Project builds one seat's view. A hidden zone contributes a count and
// nothing else unless the viewer owns it, and a decision is attached only to
// the player it was asked of. Total: g == nil, ch == nil, and an
// out-of-range viewer all degrade rather than panic (supplement §7).
// Project is ProjectFor with Seat visibility.
func Project(g *state.Game, ch Chars, viewer state.PlayerID, d *decision.Decision) View {
	return ProjectFor(g, ch, viewer, Seat, d)
}

// ProjectForControlled is ProjectFor with CR 723.4's "also visible" widening:
// alsoVisible names seats whose hidden information the viewer may read because
// the viewer currently controls them. A player-controlling effect (CR 723.1)
// makes "information about an object ... visible to the player being
// controlled ... visible to both that player and the controller", so the
// viewer's own hand gate and the face-down face gate admit every seat in the
// set, and no others. The set is deliberately narrow -- hands and face-down
// faces only, exactly the CR 723.4 example; it is NOT the spectator
// Omniscient mode and must not be reused as one. Project and ProjectFor are
// this function with an empty set.
func ProjectForControlled(g *state.Game, ch Chars, viewer state.PlayerID, vis Visibility, alsoVisible []state.PlayerID, d *decision.Decision) View {
	return ProjectForControlledFor(g, ch, viewer, vis, alsoVisible, d)
}

func copyDecision(d *decision.Decision) *decision.Decision { return copyDecisionInto(nil, d, nil) }

// copyDecisionInto is copyDecision written into dst's storage when dst is
// non-nil (decision.CloneInto; ProjectInto's reuse), its label substitutions
// memoised in text when that is non-nil.
func copyDecisionInto(dst, d *decision.Decision, text *textMemo) *decision.Decision {
	if d == nil {
		return nil
	}
	if dst == nil {
		dst = new(decision.Decision)
	}
	d.CloneInto(dst)
	for i := range dst.Options {
		dst.Options[i].Label = text.labelText(dst.Options[i].Label)
	}
	return dst
}

// optionLabelText substitutes Forge's self-reference placeholders in an
// offer label of the "<name>: <description>" shape the engine builds for
// every activated ability (rules/legal.go: face name + ": " +
// SpellDescription$). The engine copies the raw SpellDescription$, so Mount
// Doom's damage ability read "Mount Doom: CARDNAME deals 1 damage to each
// opponent." on the seat panel and the card wheel (fb-20260923T033148Z).
// The name is the label's own prefix -- the one the engine already chose to
// show this seat -- so the substitution reads no game state and can reveal
// nothing the label did not. A label without a placeholder, or without the
// prefix, is returned unchanged.
func optionLabelText(label string) string {
	if !hasPlaceholder(label) {
		return label
	}
	i := strings.Index(label, ": ")
	if i <= 0 {
		return label
	}
	return label[:i+2] + substitutePlaceholders(label[i+2:], label[:i])
}

// librarySuppressor is the optional Chars capability that lets a caller skip
// paying for the own-library CONTENTS projection it does not use. The search
// observation (internal/searchprobe) strips the library from every frame -- it
// samples the unseen draws and the recorded/hypothetical engines allocate
// hidden library objects from different arenas -- so it declares this and
// unorderedLibrary is never built for it. A Chars that does not implement it
// (every ordinary caller, including rules.Engine) keeps the field, so the
// projection's allocation cost is paid only where the field is read.
type librarySuppressor interface{ SuppressOwnLibrary() bool }

func suppressesOwnLibrary(ch Chars) bool {
	s, ok := ch.(librarySuppressor)
	return ok && s.SuppressOwnLibrary()
}

// unorderedLibrary projects a seat's own library as a canonical, order-free
// list of CardViews: the same multiset of cards the library holds, arranged
// by card name then object id so the slice is a pure function of the
// CONTENTS and never of the secret order CR 400.2 keeps hidden. It is called
// only for a seat the viewer is entitled to read (their own, or one they
// control under CR 720.4), mirroring the Hand gate; a nil ch degrades to an
// empty projection like every other derived fact.
func (p *projector) unorderedLibrary(buf []CardView, ids []state.ObjID, abilityPlayer state.PlayerID, alsoVisible *seatSet) []CardView {
	cvs := p.cardViews(buf, ids, false, abilityPlayer, false, alsoVisible)
	// (Name, ID) is a total order over distinct objects, and one object
	// projects one CardView, so every correct sort leaves the same list.
	slices.SortFunc(cvs, func(a, b CardView) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return cvs
}

// projectMode is the rule set one projection applies (ProjectForControlledInto
// picks it from the Visibility). revealFaceDown is the omniscient
// projection's face-down reveal for the stack and for every hand (omniHands):
// it is the same flag cardViews' FaceDown redaction takes, so the two views
// (a face-down card in a hand and a face-down spell on the stack) cannot
// disagree about who may look at a face-down card's printed face.
// alsoVisible is the CR 723.4 widening set (see ProjectForControlled): seats
// whose hidden information the viewer may read because they control them. It
// is empty for every ordinary projection, so the pre-existing gates are
// unchanged unless a caller explicitly widens. ownLibrary admits the viewer's
// own (or controlled) library CONTENTS into the projection
// (own_library_list); it is true only for a real seat's Seat-mode view, so no
// spectator mode -- Public or Omniscient -- ever lists a library (spec D12).
// omniHands projects every seat's hand, face-down cards revealed and without
// ability costs, and anyDecision attaches d whoever it was asked of: the
// Omniscient spectator's view.
type projectMode struct {
	revealFaceDown bool
	alsoVisible    seatSet
	ownLibrary     bool
	omniHands      bool
	anyDecision    bool
	// omit is a lean projection's skipped parts (ProjectLeanInto); zero for
	// every other entry point.
	omit Omit
}

// project is projectInto on a fresh View with a map-shaped alsoVisible, for
// this package's own tests.
func project(g *state.Game, ch Chars, viewer state.PlayerID, d *decision.Decision, revealFaceDown bool, alsoVisible map[state.PlayerID]bool, ownLibrary bool) View {
	m := projectMode{revealFaceDown: revealFaceDown, ownLibrary: ownLibrary}
	for p, ok := range alsoVisible { // a set: order cannot matter
		if ok {
			m.alsoVisible.add(p)
		}
	}
	var v View
	projectInto(&v, g, ch, viewer, d, &m)
	return v
}

// projectInto is every projection's body: it overwrites *dst with viewer's
// view of g under m, reusing the storage dst already holds -- every list's
// backing array, and the structs, slices and maps its elements point to
// (refill, projector.cardView) -- so a caller that refills one View per
// call allocates nothing once its buffers have grown. A zero dst allocates
// exactly what a fresh View needs. Every field is rewritten: nothing of
// dst's previous contents survives in the result.
func projectInto(dst *View, g *state.Game, ch Chars, viewer state.PlayerID, d *decision.Decision, m *projectMode) {
	players, stack, pending, winner, dec := dst.Players, dst.Stack, dst.Pending, dst.Winner, dst.Decision
	*dst = View{Viewer: viewer}
	if g == nil {
		if m.anyDecision {
			dst.Decision = copyDecisionInto(dec, d, nil)
		}
		return
	}
	// A Chars that memoises derived characteristics for a pure read
	// (*rules.Engine's BeginDerivedReads, botpolicy's derivedReadScoper) gets
	// the whole projection as one read: every per-card mana, cost and
	// characteristic query then shares one board-statics walk instead of
	// rescanning per card. The projection copies every slice it keeps.
	if sc, ok := ch.(interface {
		BeginDerivedReads()
		EndDerivedReads()
	}); ok {
		sc.BeginDerivedReads()
		defer sc.EndDerivedReads()
	}
	v := dst
	v.Turn = g.Turn
	v.Round = roundOf(g)
	v.Step = g.Step.String()
	v.Phase = PhaseOf(g.Step)
	v.Active = g.Active
	v.Priority = g.Priority
	// The London mulligan round happens before the first TurnChange. Its
	// starting seat is nevertheless authoritative Game state now, so a
	// snapshot-only projection (without a live rules.Engine capability) keeps
	// the same play/draw seat after an opening effect changes it. Retain the
	// capability fallback for hand-built legacy state with no designation.
	if g.Turn == 0 && !g.Over {
		if g.HasStartingPlayer {
			v.Active = g.StartingPlayer
		} else if starter, ok := ch.(interface{ PregameStarter() (state.PlayerID, bool) }); ok {
			if p, ok := starter.PregameStarter(); ok {
				v.Active = p
			}
		}
	}
	// Terminal genesis (the game ended during its opening deal) never began
	// a turn, so it has no active seat -- the zero value would read as seat
	// 0 having the turn. A live pregame obtains its resolved starter through
	// the optional capability above.
	if g.Over && g.Turn == 0 {
		v.Active = NoSeat
	}
	v.Over = g.Over
	v.Draw = g.Draw
	if g.Over && !g.Draw && int(g.Winner) < len(g.Players) {
		if winner == nil {
			winner = new(state.PlayerID)
		}
		*winner = g.Winner
		v.Winner = winner
	}
	sc := scratchPool.Get().(*projScratch)
	pr := newProjector(g, ch, viewer, sc)
	if m.omit&OmitEffectiveCost != 0 {
		pr.effective = nil
	}
	seatCosts := m.omit&OmitAbilityCosts == 0
	noLists := m.omit&OmitCardLists != 0
	pr.noDerived = m.omit&OmitDerivedChars != 0
	v.Stack = pr.stackViews(stack, g.Stack, m.revealFaceDown)
	// Non-nil even when empty (Ruling T23-u), whether or not ch is nil.
	var pts []state.PendingTrigger
	if ch != nil {
		pts = ch.PendingTriggers()
	}
	v.Pending = pendingViews(pending, pts)

	// A viewer index that names no real seat is a spectator: everything
	// below that gates on "is this the viewer's own seat" naturally stays
	// closed for them, since no real p.ID will ever equal an out-of-range
	// viewer — but the decision check does not have that same natural
	// guard (a malformed Decision.Player could coincide with a malformed
	// viewer), so it is checked explicitly.
	spectator := int(viewer) >= len(g.Players)

	// Non-nil even when g.Players is empty (Ruling T23-u): an empty match
	// still marshals "players":[], never "players":null.
	players = refill(players, len(g.Players))
	for i := range g.Players {
		p := &g.Players[i]
		players = players[:i+1]
		pv := &players[i]
		prev := *pv // the slot's previous storage
		var roster []CardView
		var casts []int32
		if !noLists {
			roster, casts = pr.commanderViews(prev.Commanders, prev.CommanderCasts, p.Commanders, p.CmdCasts)
		}
		*pv = PlayerView{
			ID: p.ID, Name: displayName(p), Life: p.Life, Lost: p.Lost,
			LibrarySize:       len(g.Zone(state.ZLibrary, p.ID)),
			HandSize:          len(g.Zone(state.ZHand, p.ID)),
			PlanarDeck:        pr.listViews(noLists, prev.PlanarDeck, g.Zone(state.ZPlanarDeck, p.ID), false, p.ID, &m.alsoVisible),
			GraveyardSize:     len(g.Zone(state.ZGraveyard, p.ID)),
			Battlefield:       pr.listViews(noLists && p.ID != viewer, prev.Battlefield, g.Zone(state.ZBattlefield, p.ID), seatCosts, p.ID, &m.alsoVisible),
			Graveyard:         pr.listViews(noLists, prev.Graveyard, g.Zone(state.ZGraveyard, p.ID), false, p.ID, &m.alsoVisible),
			Exile:             pr.listViews(noLists, prev.Exile, g.Zone(state.ZExile, p.ID), false, p.ID, &m.alsoVisible),
			Command:           pr.listViews(noLists, prev.Command, g.Zone(state.ZCommand, p.ID), false, p.ID, &m.alsoVisible),
			Commanders:        roster,
			CompletedDungeons: p.CompletedDungeons,
			HasInitiative:     g.IsInitiative(p.ID),
			CommanderCasts:    casts,
		}
		if len(p.Counters) > 0 {
			ctr := prev.Counters
			if ctr == nil {
				ctr = make(map[string]int32, len(p.Counters))
			} else {
				clear(ctr)
			}
			for _, c := range p.Counters {
				ctr[c.Kind] = c.N
			}
			pv.Counters = ctr
		}
		pv.LandDropSpent = landDropSpent(ch, p)
		if dungeon := g.Obj(p.DungeonObj); dungeon != nil && dungeon.Zone == state.ZCommand && dungeon.Face() != nil {
			dv := prev.Dungeon
			if dv == nil {
				dv = new(DungeonView)
			}
			*dv = DungeonView{Name: dungeon.Face().Name, Room: dungeonRoomName(dungeon.Face(), p.DungeonRoom)}
			pv.Dungeon = dv
		}
		// Available is public (battlefield-derived) and so projected for
		// every seat under every visibility, like Pool; only Hand (a CR 400.2
		// hidden zone) is filled for the viewer's own seat. A nil ch
		// (supplement §7) degrades to an empty (zero) availability, the same
		// way it degrades every other derived characteristic.
		if m.omit&OmitAvailable == 0 {
			var avail state.Mana
			if ch != nil {
				avail = ch.AvailableMana(p.ID)
			}
			pv.Available = poolView(prev.Available, avail)
		}
		// ownSeat is "this is the viewer's own seat", widened by CR 723.4's
		// alsoVisible set and by CR 720.4 to a seat the viewer controls: the
		// one gate every hidden-information field below is filled behind.
		ownSeat := viewer == p.ID || m.alsoVisible.has(p.ID) || controlsSeat(g, viewer, p.ID)
		// The MayLookAt grant (Oracle of Mul Daya): the viewer's own seat sees
		// the top card of their own library when a live grant covers it, and
		// (CR 720.4) so does the controller of that seat -- the controller may
		// look at all cards the controlled player could see. Every other seat
		// sees nothing (the CR 400.2 hidden-zone rule the Hand field documents
		// applies in full). A nil ch degrades to no reveal, the way it degrades
		// every other derived fact.
		if ownSeat && ch != nil && ch.MayLookAtLibraryTop(p.ID) {
			if lib := g.Zone(state.ZLibrary, p.ID); len(lib) > 0 {
				top := prev.LibraryTop
				if top == nil {
					top = new(CardView)
				}
				if pr.zoneCard(top, lib[0], false, p.ID, false, &m.alsoVisible) {
					pv.LibraryTop = top
				}
			}
		}
		// The 21-damage clock: this player's cumulative commander damage,
		// keyed by the commander that dealt it -- re-keyed off the match-wide
		// dense order Player.CmdDamage is indexed by (every commander object,
		// player order then each player's Commanders order, as rules.New
		// assigns it at genesis). Only built when any tally is nonzero --
		// nil/absent means zero -- so no map is filled for a player (or a
		// game) with no commander damage.
		if len(p.CmdDamage) > 0 {
			pv.CmdDamage = cmdDamageView(prev.CmdDamage, g, p.CmdDamage)
		}
		// Pool is public (CR 106.4a/106.4b): a mana pool is not one of the
		// seven zones in CR 400.1 and holds no cards, so CR 400.2's hidden-
		// zone framework has no purchase on it; instead 106.4a, 106.4b,
		// 117.3d and 118.3a all require a player to ANNOUNCE what is in their
		// pool, an obligation incoherent for hidden information. So Pool is
		// projected for every seat under every visibility, like Available;
		// only Hand stays gated on "is this the viewer's own seat" (CR 400.2
		// names hand as a hidden zone).
		pv.Pool = poolView(prev.Pool, p.Pool)
		pv.PoolRestrictions = poolRestrictions(prev.PoolRestrictions, g, p.RestrictedMana, pr.text)
		switch {
		case m.omniHands:
			// The omniscient spectator sees every hand, face-down cards
			// included, without the seat-only ability costs.
			pv.Hand = pr.cardViews(prev.Hand, g.Zone(state.ZHand, p.ID), false, p.ID, true, &noSeats)
		case ownSeat && !noLists:
			pv.Hand = pr.cardViews(prev.Hand, g.Zone(state.ZHand, p.ID), seatCosts, p.ID, false, &m.alsoVisible)
		}
		// The library CONTENTS are the viewer's own seat only (CR 400.2),
		// and only in a real seat's own projection (ownLibrary): a spectator
		// mode -- Public or Omniscient -- never lists a library, preserving
		// D12's "library order is hidden" for every spectator. unorderedLibrary
		// canonicalises the order, so even the own seat learns the contents
		// and never the secret draw order.
		if ownSeat && m.ownLibrary && !suppressesOwnLibrary(ch) {
			pv.Library = pr.unorderedLibrary(prev.Library, g.Zone(state.ZLibrary, p.ID), p.ID, &m.alsoVisible)
		}
		// PotentialActions is the "what could I still do after tapping out"
		// projection (rules.PotentialActions): the engine's own legal-offer
		// walk priced against the hypothetical tapped-out pool. It is
		// projected for the viewer's own seat AND (CR 720.4) for a seat the
		// viewer controls -- the controller needs to know what the controlled
		// player could still do, and the walk reads that seat's hidden zones,
		// which the controller is entitled to see under the same rule. It is
		// never projected for any other seat (CR 400.2): a seat the view does
		// not belong to carries no field at all (nil), and a spectator
		// (viewer naming no real seat) never matches the gate above. A nil
		// ch degrades to an empty projection like every other derived fact.
		if ownSeat && ch != nil && m.omit&OmitPotential == 0 {
			pv.PotentialActions = ch.PotentialActions(p.ID)
			for i := range pv.PotentialActions {
				pv.PotentialActions[i].Label = pr.text.labelText(pv.PotentialActions[i].Label)
			}
		}
		// The opponent archetype posterior rides the public card lists this
		// projection already decided the viewer may see; it is never computed
		// for the viewer's own seat (whose archetype is the manifest's fact)
		// nor for a spectator (whose viewer index matches no real seat).
		if p.ID != viewer && !spectator && m.omit&(OmitArchetype|OmitCardLists|OmitDerivedChars) == 0 {
			pv.Archetype = inferArchetypePosteriorInto(prev.Archetype, pv.Battlefield, pv.Graveyard, pv.Exile, pv.Command)
		}
	}
	v.Players = players

	if m.anyDecision || !spectator && d != nil && d.Player == viewer && m.omit&OmitDecision == 0 {
		// A copy, never the engine's own pending pointer (supplement §10):
		// a Seat (Task 25) holds this View in-process and must not be able
		// to corrupt the live decision through it.
		v.Decision = copyDecisionInto(dec, d, pr.text)
		fillVisibleOptionLabels(v)
	}
	scratchPool.Put(sc)
}

// fillVisibleOptionLabels derives missing card-option labels only from card
// identities already admitted to this viewer's projection. In particular,
// hidden hands and libraries are never read here: their cards are absent from
// the corresponding PlayerView slices.
func fillVisibleOptionLabels(v *View) {
	if v == nil || v.Decision == nil {
		return
	}
	for i := range v.Decision.Options {
		o := &v.Decision.Options[i]
		if o.Kind != "card" || o.Label != "" || o.Obj == 0 {
			continue
		}
		name := visibleCardName(v, o.Obj)
		if name != "" {
			o.Label = name
		}
	}
}

func visibleCardName(v *View, id state.ObjID) string {
	for i := range v.Players {
		p := &v.Players[i]
		for _, cards := range [][]CardView{p.Battlefield, p.Graveyard, p.Exile, p.Command, p.Hand, p.Library} {
			for j := range cards {
				if cards[j].ID == id {
					return cards[j].Name
				}
			}
		}
		if p.LibraryTop != nil && p.LibraryTop.ID == id {
			return p.LibraryTop.Name
		}
	}
	for i := range v.Stack {
		s := &v.Stack[i]
		if s.ID == id {
			return s.Name
		}
		if s.Card != nil && s.Card.ID == id {
			return s.Card.Name
		}
	}
	return ""
}

// cmdDamageView is the CmdDamage map of one player's tallies (Player.CmdDamage,
// dense commander order), written into m's storage when m is non-nil; nil
// when every tally is zero.
func cmdDamageView(m map[state.ObjID]int32, g *state.Game, tallies []int32) map[state.ObjID]int32 {
	dense := 0
	for i := range g.Players {
		dense += len(g.Players[i].Commanders)
	}
	var out map[state.ObjID]int32
	j := 0
	for i := range g.Players {
		for _, id := range g.Players[i].Commanders {
			if j < len(tallies) && tallies[j] != 0 {
				if out == nil {
					if out = m; out == nil {
						out = make(map[state.ObjID]int32, dense)
					} else {
						clear(out)
					}
				}
				out[id] = tallies[j]
			}
			j++
		}
	}
	return out
}

// RoundOf is the EXACT round-trip count, folded over the ordered event
// stream rather than projected from a single state.Game snapshot. This is
// the honest fix for the round number: the log knows the order things
// happened in, which a snapshot cannot. It is a pure function of the game
// and the event log -- it reads g.Players (the seat count) and the
// events.PlayerLost / events.TurnChange stream, writes nothing, and cannot
// change a chain head -- so it is identical across a live game and a
// log-only reconstruction of the same game (replay reproduces the same
// events, and this fold is deterministic on them).
//
// The rule, stated precisely: the fold is ANCHORED ON THE GAME'S STARTING
// PLAYER -- the seat the first TurnChange hands the turn to (the CR 103.1
// toss winner, resolved over the survivors). Turn order is the cyclic order
// that begins at that anchor, not at seat 0 (before the toss it was seat 0,
// which is why the old rule read "lowest-index survivor" -- a board clock
// fed a game whose toss started seat 1 ran one round ahead all game, ~half
// of 2-seat and ~3/4 of 4-seat games). A round boundary is a TurnChange
// that hands the turn to the first still-living seat IN THAT CYCLIC ORDER --
// i.e. play has returned to the seat that opened the current pass. Round 1
// is the initial pass, so the very first TurnChange never counts as a
// boundary (the fold reads it as the anchor and skips the increment); each
// later return to the then-first survivor opens a new round. The fold
// tracks the alive set itself by folding PlayerLost events, so it knows the
// first survivor at every moment rather than only at the end -- the thing a
// snapshot cannot recover.
//
// The edge cases fall out of the one rule. First round before any turn: no
// TurnChange has been seen, so RoundOf is 1 and the anchor is unset; the
// first TurnChange establishes the anchor and never increments. A seat
// eliminated MID-ROUND (not the anchor): the first survivor is unchanged,
// so the survivors after the death simply get a shorter cycle and no
// boundary fires until play genuinely returns to that (unchanged) first
// survivor. A seat eliminated who WAS the anchor (the starting player): the
// anchor moves to the next living seat in turn order from the starter, and
// the next turn to reach that new anchor -- which the old anchor's death
// immediately makes imminent -- opens a new round. This is the case that
// breaks a naive "the active index fell" wrap rule: when the anchor dies
// during its own turn, the turn passes to the next seat (an index RISE when
// the starter was seat 0), yet play has returned to the first still-living
// seat in the anchor's order, so it IS a round boundary. The count is
// monotonic non-decreasing: it only ever increments, never repeats a value
// for a later state and never jumps backwards.
//
// Extra turns (a card granting a seat two turns inside one round-trip) are
// out of scope for this build: beginTurn is reached only from genesis and
// from rules/turn.go's NextAlive advance, and the effects registry
// registers no AddTurn primitive, so the engine never produces a same-seat
// repeat turn for the fold to misread; if AddTurn ever lands, this fold
// must be revisited.
//
// Guards: a nil game, an empty log (or one with no TurnChange) and a
// table whose seats have all been eliminated all fold to round 1.
//
// This is what the board clock should use wherever the ordered event stream
// is in hand (host builds views from the log); roundOf below is the
// snapshot-only approximation and the fallback for a consumer that has only
// a state.Game.
func RoundOf(g *state.Game, evs []events.Event) int32 {
	if g == nil {
		return 1
	}
	n := len(g.Players)
	if n <= 0 {
		return 1
	}
	var aliveBuf [16]bool // the alive set lives on the stack for any real table
	alive := aliveBuf[:0]
	if n <= len(aliveBuf) {
		alive = aliveBuf[:n]
	} else {
		alive = make([]bool, n)
	}
	for i := range alive {
		alive[i] = true
	}
	round := int32(1)
	start := -1
	for _, ev := range evs {
		switch ev.Kind {
		case events.PlayerLost:
			if int(ev.Player) < n {
				alive[ev.Player] = false
			}
		case events.TurnChange:
			cur := int(ev.Player)
			if start < 0 {
				// The first TurnChange IS the anchor: the game's starting
				// player (always alive at that moment -- beginTurn only ever
				// reaches a survivor). It opens round 1 and never counts as
				// a boundary.
				start = cur
				continue
			}
			// The anchor: the first still-living seat in the cyclic turn
			// order that begins at the starting player. If no seat is alive
			// (defensive -- a finished game whose winner has not been
			// flagged) there is no first survivor to return to, so no
			// boundary can fire.
			for i := 0; i < n; i++ {
				first := (start + i) % n
				if !alive[first] {
					continue
				}
				if cur == first {
					round++
				}
				break
			}
		}
	}
	return round
}

// roundOf projects the engine's per-player-turn counter (g.Turn) onto the
// number of round-trips the table has made. The rule, stated once: a round
// is one full pass around the table among the players still alive, and the
// projected number is 1 + (Turn-1)/AliveCount -- so round 1 spans the first
// complete pass of every surviving seat, round 2 the second, and the round
// length (AliveCount) shrinks as seats are eliminated.
//
// This is the SNAPSHOT-ONLY APPROXIMATION, kept as the fallback for a
// consumer that has only a state.Game (the cmd/* tools and this package's
// own tests are the ones that genuinely lack the log). It is exact only when
// the game's starting player is seat 0 (a snapshot cannot recover the CR
// 103.1 toss winner the round anchor needs) and before the first
// elimination; after either, it runs AHEAD of the true round-trip count --
// the exact one is view.RoundOf, folded over the ordered event stream,
// which is what host uses to build the board clock and what the AGENTS.md
// row names as the honest fix. Do not route a log-bearing caller
// through this function; it is here so a snapshot-only caller still gets a
// never-wrong-direction round rather than a panic, and because the log is
// genuinely unavailable in that shape.
//
// This is a projection of View's existing state and nothing else: it reads
// g.Turn and each seat's Lost flag, writes no event, and cannot change a
// chain head. It is computed from the single snapshot because neither view
// nor the client keeps a per-turn history; the consequence is that it
// cannot reconstruct WHEN a seat died, only the current alive set. Two
// truths follow and are worth writing down. First, before the first
// elimination it is exactly the round-trip count. Second, after an
// elimination it stays monotonic -- 1+(Turn-1)/AliveCount only ever grows
// as Turn grows and only grows faster when AliveCount shrinks -- so it
// never repeats a value for a later game state and never jumps backwards,
// which is the failure the board's clock must not exhibit; it may run ahead
// of the literal "when play returned to the round's anchored seat" count,
// because that count needs the death times (and the starting player) this
// snapshot does not carry.
//
// Extra turns (a card granting a seat two turns inside one round-trip) are
// taken care of by the engine's shape, not by this function: beginTurn is
// reached only from genesis and from rules/turn.go's NextAlive advance, and
// the effects registry registers no AddTurn primitive, so this build never
// produces a same-seat repeat turn for the projection to account for.
//
// Guards: a nil game, a Turn still at its pre-genesis 0, or a table with no
// surviving seats all project to round 1 rather than dividing by zero.
func roundOf(g *state.Game) int32 {
	if g == nil || g.Turn <= 0 {
		return 1
	}
	alive := int32(0)
	for _, p := range g.Players {
		if !p.Lost {
			alive++
		}
	}
	if alive <= 0 {
		return 1
	}
	return (g.Turn-1)/alive + 1
}

// PhaseOf groups a Step into the five phases a client shows: beginning,
// main1, combat, main2, ending; "" for an invalid Step.
func PhaseOf(s state.Step) string {
	switch s {
	case state.StepUntap, state.StepUpkeep, state.StepDraw:
		return "beginning"
	case state.StepMain1:
		return "main1"
	case state.StepBeginCombat, state.StepDeclareAttackers, state.StepDeclareBlockers,
		state.StepCombatDamage, state.StepEndCombat:
		return "combat"
	case state.StepMain2:
		return "main2"
	case state.StepEnd, state.StepCleanup:
		return "ending"
	default:
		return ""
	}
}

// substitutePlaceholders replaces Forge's self-reference placeholders in a
// rules-text string with the card's own display name. CARDNAME is Forge's
// token for the card's full name; NICKNAME is the same token narrowed to the
// name's first word (Forge's default nickname -- the corpus carries no
// Nickname$ or Nickname: line and the parser has no field for one, so the
// first word is the whole of what the engine can know). Both are matched on
// a word boundary -- a letter or digit on either side is not a match -- so a
// token inside a larger word ("CARDNAMES") is left alone, and the substituted
// name is written out and never re-scanned, so a name that legitimately
// contains the letters ("Forked Bolt") is inserted whole and a name that
// happened to be a placeholder token as a whole word would not be rewritten
// a second time.
//
// The set is what the corpus actually uses in the text this package renders:
// CARDNAME is pervasive in SpellDescription$/TriggerDescription$/
// StackDescription$ (and, like NICKNAME, is always the uppercase token in
// those fields), NICKNAME occurs in about a thousand of them, and the "~"
// shorthand does not occur in any of them (it appears only in Forge SVar
// arithmetic, which is never rendered) -- see the task report for the grep.
func substitutePlaceholders(text, name string) string {
	if text == "" || name == "" {
		return text
	}
	// No token anywhere: the scan below would rebuild text byte for byte.
	if !hasPlaceholder(text) {
		return text
	}
	nick := firstWord(name)
	var b strings.Builder
	b.Grow(len(text))
	i := 0
	for i < len(text) {
		if !isWordByte(text[i]) {
			b.WriteByte(text[i])
			i++
			continue
		}
		j := i
		for j < len(text) && isWordByte(text[j]) {
			j++
		}
		tok := text[i:j]
		switch tok {
		case "CARDNAME":
			b.WriteString(name)
		case "NICKNAME":
			b.WriteString(nick)
		default:
			b.WriteString(tok)
		}
		i = j
	}
	return b.String()
}

// hasPlaceholder reports whether text contains either placeholder token's
// letters at all; without them substitutePlaceholders returns text unchanged.
func hasPlaceholder(text string) bool {
	return strings.Contains(text, "CARDNAME") || strings.Contains(text, "NICKNAME")
}

// firstWord is the first whitespace-delimited word of a name -- Forge's
// NICKNAME default ("Forked Bolt" -> "Forked"). A trailing comma, the
// separator between a legendary title and its epithet ("Ambergris, Agent of
// Destruction" -> "Ambergris"), is dropped so the substitution does not
// render "Ambergris, deals ..."; a hyphen inside the first word is kept
// ("A-Alrund, God of the Cosmos" -> "A-Alrund").
func firstWord(s string) string {
	i := strings.IndexByte(s, ' ')
	if i < 0 {
		i = len(s)
	}
	return strings.TrimRight(s[:i], ",")
}

// isWordByte is the word-character edge the placeholder scan matches on:
// letters and digits only. A placeholder token at either end of the string,
// or adjacent to punctuation (an apostrophe, a comma, a period), is still a
// whole word; a placeholder inside a larger word is not.
func isWordByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// landDropSpent answers PlayerView.LandDropSpent: the engine's own land-drop
// allowance when ch offers it (the optional LandDropOpen capability, which
// folds in the "additional land" grants), else the plain one-drop count.
func landDropSpent(ch Chars, p *state.Player) bool {
	if d, ok := ch.(interface{ LandDropOpen(state.PlayerID) bool }); ok {
		return !d.LandDropOpen(p.ID)
	}
	return p.LandsPlayed >= 1
}
