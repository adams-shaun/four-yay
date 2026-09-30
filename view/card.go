package view

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Printing is the identity a client resolves an image by: the exact face
// name today. Set and Number stay empty until a printing table exists
// (roadmap open question 1); the fields are here so the wire shape does
// not change when it does.
type Printing struct {
	Name   string `json:"name"`
	Set    string `json:"set,omitempty"`
	Number string `json:"number,omitempty"`
}

// CardView is one object's public face: printed identity plus its current,
// derived characteristics. Nothing here is read from a hidden zone unless
// the viewer owns it — cardViews is only ever called with a zone list the
// caller has already decided is visible.
type CardView struct {
	ID       state.ObjID `json:"id"`
	Name     string      `json:"name"`
	FaceDown bool        `json:"face_down,omitempty"`
	Types    string      `json:"types"`
	// ManaCost is the printed cost in Forge's notation ("1 W", "R", "X G").
	// Hand lists render it as symbols.
	ManaCost string `json:"mana_cost,omitempty"`
	// EffectiveManaCost is the offer-time cost to cast this card when the
	// engine can determine a non-X own-cost composition that differs from
	// the printed ManaCost. Empty means use ManaCost.
	EffectiveManaCost string `json:"effective_mana_cost,omitempty"`
	// SpellAPI is the API of the card's primary cast-shape ability (its
	// SP$ line -- "Counter" for Counterspell, "DealDamage" for Lightning
	// Bolt, "" for a card with no spell ability, which is every creature
	// and every activated-ability permanent). It is a printed card fact,
	// projected for exactly the cards the projection already carries the
	// printed ManaCost and Types of -- a visible card's own text is open
	// information -- so a seat that can read a card's cost can read what
	// the cast does at the same grain. The bot policy's casting rule reads
	// it (carried into botpolicy.Card.Counter) to tell a counter spell
	// from any other cast. A hidden hand's cards are never projected as
	// CardViews at all, so this never leaks hidden information.
	SpellAPI string `json:"spell_api,omitempty"`
	// Printing is what an image lookup keys on; Token ("#12") tells two
	// copies of one card apart in the stack, the log and an arrow.
	Printing Printing `json:"printing"`
	Token    string   `json:"token"`
	// IsToken and IsCopy are the object's own state.Object flags projected
	// for every visible card: a battlefield token (CR 111.7) and a copy
	// (CR 707.10; a battlefield token copy carries BOTH) are public identity
	// facts like the name they sit beside, and a client wire that mints its
	// own identity DTO reads them rather than re-deriving one from Token --
	// which is the display tag every card carries, never a token flag.
	// cardViews' face-down replacement rebuilds the CardView from a fresh
	// literal without them, so a card hidden behind its face reveals no more
	// than that projection already did; and since a token exists only on the
	// battlefield and a copy only on the stack or the battlefield, neither
	// flag can name a card in a hidden zone.
	IsToken bool `json:"is_token,omitempty"`
	IsCopy  bool `json:"is_copy,omitempty"`
	// AttackingPlayer is the seat this creature is attacking while
	// Attacking is true, nil otherwise; BlockedBy lists the creatures
	// blocking it. Both exist for the arrow overlay (PL-17) and come
	// straight from the object's combat fields, which EndCombatReset clears.
	AttackingPlayer *state.PlayerID  `json:"attacking_player,omitempty"`
	BlockedBy       []state.ObjID    `json:"blocked_by,omitempty"`
	Tapped          bool             `json:"tapped"`
	Power           int32            `json:"power"`
	Toughness       int32            `json:"toughness"`
	Damage          int32            `json:"damage"`
	Attacking       bool             `json:"attacking"`
	Counters        map[string]int32 `json:"counters,omitempty"`
	Keywords        []string         `json:"keywords,omitempty"`
	// Controller and Owner can differ (a stolen permanent, a stack object
	// created for someone else's turn); a client needs both. Battlefield
	// zone lists are keyed by controller, every hidden/graveyard/exile list
	// by owner (events/apply.go's zoneOwner), so a CardView carries both
	// regardless of which zone list it came from.
	Controller state.PlayerID `json:"controller"`
	Owner      state.PlayerID `json:"owner"`
	SummonSick bool           `json:"summon_sick"`
	// AttachedTo is the permanent this Aura or Equipment is currently
	// attached to, 0 meaning unattached -- the same zero-value convention
	// Obj uses, so omitempty keeps an unattached permanent (or a non-
	// permanent: a spell, a card in a hand) from emitting a field. It comes
	// straight from state.Object.AttachedTo, which the engine's Attach event
	// sets and clears; without it a client could not render an attachment
	// beneath the permanent it modifies -- could not tell what is attached
	// to what at all.
	AttachedTo state.ObjID `json:"attached_to,omitempty"`
	// ActivatedThisTurn is how many non-mana activated abilities of this
	// object were activated this turn (events.Apply's AbilityPush census on
	// state.Object.ActivatedThisTurn). Public fact, like the tap state it
	// rides beside; the bot policy's repeatable-ability budget (A5) reads
	// it to bound its own loop-shaped activations (Basalt Monolith's untap
	// re-enabling its own tap) without ever gating a human seat, for whom
	// unlimited activations stay legal and offered.
	ActivatedThisTurn int32 `json:"activated_this_turn,omitempty"`
	// AbilityCosts is the current offer-time Forge-notation cost of each
	// non-mana activated ability, in face ability order. Applicable
	// RaiseCost/ReduceCost statics have already been composed exactly as the
	// engine's legal-action gate composes them; this is deliberately not just
	// the printed Cost$. It is projected only for battlefield cards and the
	// viewer's own hand: those are the cards an interactive seat can use to
	// decide whether floating mana would unlock an ability. Other zones leave
	// it nil, rather than turning this into a general rules-text projection.
	AbilityCosts []string `json:"ability_costs,omitempty"`
	// Produces is what this card's mana abilities add to the pool when a
	// tap-for-mana activation runs them, derived from the compiled abilities
	// (cards.Face.ManaProduction) rather than land subtypes: a basic land's
	// intrinsic {W}, a dual's {W}{U}, an "add any colour" source's five
	// colour alternatives (one unit each, flagged Any -- the colour is chosen
	// when it is tapped, CR 106.1b), a colourless rock's {C}{C}. nil when the card has no mana
	// ability at all, so a creature or a spell never pays for the six-entry
	// array on the wire. It is a projected characteristic like ManaCost and
	// Keywords -- a mana ability's production is a card fact every seat sees,
	// and it is projected for the same zones the card itself is visible in.
	// This is what the bot policy's colour-aware tap and land heuristics read
	// (carried into botpolicy.Card as plain data, never the view type).
	Produces *cards.ManaProduction `json:"produces,omitempty"`
}

// cardViews maps a zone's object ids to CardViews, in zone order. An id
// whose object no longer exists (a dangling entry, or a defensively
// tampered list) is skipped rather than producing a zero CardView or
// panicking (supplement §7). Always non-nil (Ruling T23-u), even for an
// empty or all-dangling ids: this is what lets the viewer's own genuinely
// empty Hand marshal "[]" rather than the same "null" a hidden hand would.
//
// Ephemeral objects (copies, tokens off the battlefield) have ceased to
// exist -- state.Object.Ephemeral is the single definition of that, consulted
// here rather than re-spelled inline so it cannot drift from any other call
// site -- and an ability object (Card == nil, so Face() == nil too) never
// legitimately sits in a card zone at all. Both are parked in exile by the
// engine and are skipped here (Task 4).
func cardViews(g *state.Game, ch Chars, ids []state.ObjID, includeAbilityCosts bool, abilityPlayer, viewer state.PlayerID, revealFaceDown bool, alsoVisible map[state.PlayerID]bool) []CardView {
	out := make([]CardView, 0, len(ids))
	for _, id := range ids {
		o := g.Obj(id)
		// An ability object (no Face) is engine bookkeeping, not a card in
		// this zone; Ephemeral covers copies and tokens off the battlefield.
		if o == nil || o.Face() == nil || o.Ephemeral() {
			continue
		}
		if o.PhasedOut {
			// CR 702.25b: a phased-out permanent is treated as though it does
			// not exist, so it is absent from every projection of its zone.
			// PhasedOut is only ever set on a battlefield permanent (the
			// PhaseOut fold gates on it and the Move fold clears it), so this
			// cannot hide a card in a hidden zone.
			continue
		}
		cv := cardView(g, ch, id)
		if effective, ok := ch.(interface {
			SpellEffectiveCost(state.PlayerID, state.ObjID) string
		}); ok {
			if cost := effective.SpellEffectiveCost(abilityPlayer, id); cost != "" && cost != cv.ManaCost {
				cv.EffectiveManaCost = cost
			}
		}
		// A face-down exiled card is public as a distinct object but its face is
		// visible only to its controller (or an omniscient projection). Keep
		// every printed field blank for other viewers; Secret on the original
		// MoveZone was only event redaction and cannot carry this lasting fact.
		//
		// A WithMayLook$ True face-down exile (Ixhel, Scion of Atraxa) names the
		// exiling effect's controller as the ONE player who may look, so it
		// REPLACES the controller default: the card's owner may not read a face
		// the owner was never granted, and the looker may. o.MayLookPlayer is
		// the single read of that permission here, so this projection and any
		// later one cannot disagree about it.
		if o.FaceDown {
			cv.FaceDown = true
			looker := o.Controller
			if o.HasMayLook {
				looker = o.MayLookPlayer
			}
			// A face-down planar-deck card is unknown to every seat, including
			// its owner. Only the face-up current plane is public.
			if o.Zone == state.ZPlanarDeck || (!revealFaceDown && viewer != looker && !alsoVisible[looker]) {
				cv = CardView{ID: id, FaceDown: true, Token: cardToken(ch, id),
					Controller: o.Controller, Owner: o.Owner}
			}
		}
		if includeAbilityCosts {
			if ch != nil {
				cv.AbilityCosts = ch.AbilityCosts(abilityPlayer, id)
			} else {
				cv.AbilityCosts = printedNonManaAbilityCosts(o.Face())
			}
		}
		out = append(out, cv)
	}
	return out
}

// tokenSuppressor is a Chars whose caller discards every CardView.Token it
// projects (searchprobe's observation collector blanks them all before it
// compares or hashes a board), so cardView leaves the field empty instead of
// formatting a string per card. A Chars that does not implement it -- every
// ordinary caller, rules.Engine included -- gets the token.
type tokenSuppressor interface{ SuppressCardTokens() bool }

// cardToken is id's display token ("#12"), or "" for a tokenSuppressor.
func cardToken(ch Chars, id state.ObjID) string {
	if s, ok := ch.(tokenSuppressor); ok && s.SuppressCardTokens() {
		return ""
	}
	return "#" + strconv.FormatUint(uint64(id), 10)
}

// printedNonManaAbilityCosts is the no-rules fallback: it preserves the
// face's ability order while exposing only activated abilities that are not
// mana abilities. An absent Cost$ remains an empty string. A real Engine Chars
// supplies effective costs instead, including live cost modifiers.
func printedNonManaAbilityCosts(f *cards.Face) []string {
	var out []string
	for _, a := range f.Abilities {
		if a.Kind == "AB" && a.API != "Mana" {
			out = append(out, a.Params["Cost"])
		}
	}
	return out
}

// cardView builds one object's public face. ch is read through Power/
// Toughness/Keywords rather than any printed field directly — the view asks
// the engine for derived characteristics, never the card's own text — and a
// nil ch (supplement §7) degrades to the zero P/T with no keywords.
func cardView(g *state.Game, ch Chars, id state.ObjID) CardView {
	o := g.Obj(id)
	cv := CardView{
		ID: id, Tapped: o.Tapped, Damage: o.Damage, Attacking: o.IsAttacking,
		Controller: o.Controller, Owner: o.Owner, SummonSick: o.SummonSick,
		AttachedTo: o.AttachedTo, ActivatedThisTurn: o.ActivatedThisTurn,
	}
	cv.Token = cardToken(ch, id)
	cv.IsToken = o.IsToken
	cv.IsCopy = o.IsCopy
	if f := o.Face(); f != nil {
		cv.Name = f.Name
		// Name is a layer-3 characteristic. Keep the optional method so
		// lightweight Chars test doubles remain source-compatible while the
		// real rules engine exposes SetName$ results to clients.
		if named, ok := ch.(interface{ Name(state.ObjID) string }); ok {
			if name := named.Name(id); name != "" {
				cv.Name = name
			}
		}
		cv.Types = strings.Join(f.Types, " ")
		cv.ManaCost = f.ManaCost
		cv.Printing = Printing{Name: f.Name}
		// The spell-ability API projection: the card's own SP$ line, the
		// same fact boardFromGame reads for the casting census. Empty when
		// the card has no spell ability (a creature, a land, a permanent
		// with only activated abilities).
		if sa := f.SpellAbility(); sa != nil {
			cv.SpellAPI = sa.API
		}
		// The mana-production projection (Task dp2): what tapping this card
		// puts in the pool, from its own abilities. A nil pointer keeps a card
		// with no mana ability off the wire; p is a fresh value per call, so
		// the address is never aliased across object views.
		if p := f.ManaProduction(); !p.IsZero() {
			cv.Produces = &p
		}
	}
	if o.IsAttacking {
		p := o.Attacking
		cv.AttackingPlayer = &p
	}
	if len(o.BlockedBy) > 0 {
		cv.BlockedBy = append([]state.ObjID(nil), o.BlockedBy...)
	}
	if ch != nil {
		cv.Power = ch.Power(id)
		cv.Toughness = ch.Toughness(id)
		// A defensive copy: Chars is an interface, and nothing guarantees
		// an implementation hands back a slice nobody else holds a
		// reference to (supplement §10's no-aliasing rule).
		if kw := ch.Keywords(id); len(kw) > 0 {
			cv.Keywords = append([]string(nil), kw...)
		}
	}
	if len(o.Counters) > 0 {
		cv.Counters = make(map[string]int32, len(o.Counters))
		for _, c := range o.Counters {
			cv.Counters[c.Kind] = c.N
		}
	}
	return cv
}
