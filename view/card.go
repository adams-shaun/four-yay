package view

import (
	"strconv"
	"strings"
	"sync"

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

// projector is one projection's read-only context: the game, its Chars, the
// viewer, and the optional Chars capabilities, probed once per projection
// instead of once per card. It lives on the projecting call's stack.
type projector struct {
	g      *state.Game
	ch     Chars
	viewer state.PlayerID
	// effective and namer are ch's optional SpellEffectiveCost and layer-3
	// Name capabilities (nil when ch lacks them); noTokens is a
	// tokenSuppressor's answer.
	effective interface {
		SpellEffectiveCost(state.PlayerID, state.ObjID) string
	}
	namer interface{ Name(state.ObjID) string }
	// vchars is ch's optional one-call Name/Keywords/Power/Toughness
	// (rules.Engine.ViewCharacteristics), nil when ch lacks it.
	vchars interface {
		ViewCharacteristics(state.ObjID) (string, []string, int32, int32)
	}
	noTokens bool
	// types and text are the projection's type-line cache and display-text
	// memo (projScratch).
	types *typeCache
	text  *textMemo
}

func newProjector(g *state.Game, ch Chars, viewer state.PlayerID, sc *projScratch) projector {
	p := projector{g: g, ch: ch, viewer: viewer, types: &sc.types, text: &sc.text}
	p.effective, _ = ch.(interface {
		SpellEffectiveCost(state.PlayerID, state.ObjID) string
	})
	p.namer, _ = ch.(interface{ Name(state.ObjID) string })
	p.vchars, _ = ch.(interface {
		ViewCharacteristics(state.ObjID) (string, []string, int32, int32)
	})
	if s, ok := ch.(tokenSuppressor); ok && s.SuppressCardTokens() {
		p.noTokens = true
	}
	return p
}

// seatSet is a set of seats (CR 723.4's alsoVisible widening), a bitset over
// every PlayerID value so a membership test allocates nothing.
type seatSet [4]uint64

// noSeats is the empty set, shared read-only.
var noSeats seatSet

func (s *seatSet) add(p state.PlayerID)      { s[p>>6] |= 1 << (p & 63) }
func (s *seatSet) has(p state.PlayerID) bool { return s[p>>6]&(1<<(p&63)) != 0 }

// refill returns buf emptied for refilling with up to n elements, reusing its
// backing array -- and, through the elements' own reusable fields (cardView),
// the storage each slot still holds -- when it is large enough. A short buf
// is replaced by one exactly n long when it had no storage (a fresh
// projection allocates what Project always has), or by one with doubled
// capacity whose slots carry buf's old ones. Never nil, even for n == 0, so a
// refilled list marshals "[]" the way a fresh one does (Ruling T23-u).
func refill[T any](buf []T, n int) []T {
	if cap(buf) >= n && buf != nil {
		return buf[:0]
	}
	c := n
	if cap(buf) > 0 {
		c = max(n, 2*cap(buf))
	}
	grown := make([]T, cap(buf), c)
	copy(grown, buf[:cap(buf)])
	return grown[:0]
}

// cardViews maps a zone's object ids to CardViews, in zone order, into buf's
// storage (refill). An id whose object no longer exists (a dangling entry, or
// a defensively tampered list) is skipped rather than producing a zero
// CardView or panicking (supplement §7). Always non-nil (Ruling T23-u), even
// for an empty or all-dangling ids: this is what lets the viewer's own
// genuinely empty Hand marshal "[]" rather than the same "null" a hidden hand
// would.
//
// Ephemeral objects (copies, tokens off the battlefield) have ceased to
// exist -- state.Object.Ephemeral is the single definition of that, consulted
// here rather than re-spelled inline so it cannot drift from any other call
// site -- and an ability object (Card == nil, so Face() == nil too) never
// legitimately sits in a card zone at all. Both are parked in exile by the
// engine and are skipped here (Task 4).
func (p *projector) cardViews(buf []CardView, ids []state.ObjID, includeAbilityCosts bool, abilityPlayer state.PlayerID, revealFaceDown bool, alsoVisible *seatSet) []CardView {
	out := refill(buf, len(ids))
	for _, id := range ids {
		n := len(out)
		out = out[:n+1]
		if !p.zoneCard(&out[n], id, includeAbilityCosts, abilityPlayer, revealFaceDown, alsoVisible) {
			out = out[:n]
		}
	}
	return out
}

// zoneCard writes id's zone CardView into cv (cardViews' per-card body) and
// reports whether id projects at all; a skipped id leaves cv untouched.
func (p *projector) zoneCard(cv *CardView, id state.ObjID, includeAbilityCosts bool, abilityPlayer state.PlayerID, revealFaceDown bool, alsoVisible *seatSet) bool {
	o := p.g.Obj(id)
	// An ability object (no Face) is engine bookkeeping, not a card in
	// this zone; Ephemeral covers copies and tokens off the battlefield.
	if o == nil || o.Face() == nil || o.Ephemeral() {
		return false
	}
	if o.PhasedOut {
		// CR 702.25b: a phased-out permanent is treated as though it does
		// not exist, so it is absent from every projection of its zone.
		// PhasedOut is only ever set on a battlefield permanent (the
		// PhaseOut fold gates on it and the Move fold clears it), so this
		// cannot hide a card in a hidden zone.
		return false
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
	//
	// The redacted card is decided first and never built: none of the
	// derived facts the full card would read survives the blanking, so asking
	// Chars for them would only spend storage the blank card then drops.
	hidden := false
	if o.FaceDown {
		looker := o.Controller
		if o.HasMayLook {
			looker = o.MayLookPlayer
		}
		// A face-down planar-deck card is unknown to every seat, including
		// its owner. Only the face-up current plane is public.
		hidden = o.Zone == state.ZPlanarDeck || (!revealFaceDown && p.viewer != looker && !alsoVisible.has(looker))
	}
	if hidden {
		*cv = CardView{ID: id, FaceDown: true, Token: p.token(id),
			Controller: o.Controller, Owner: o.Owner}
	} else {
		p.cardView(cv, id)
		if p.effective != nil {
			if cost := p.effective.SpellEffectiveCost(abilityPlayer, id); cost != "" && cost != cv.ManaCost {
				cv.EffectiveManaCost = cost
			}
		}
		cv.FaceDown = o.FaceDown
	}
	if includeAbilityCosts {
		if p.ch != nil {
			cv.AbilityCosts = p.ch.AbilityCosts(abilityPlayer, id)
		} else {
			cv.AbilityCosts = printedNonManaAbilityCosts(o.Face())
		}
	}
	return true
}

// tokenSuppressor is a Chars whose caller discards every CardView.Token it
// projects (searchprobe's observation collector blanks them all before it
// compares or hashes a board), so cardView leaves the field empty instead of
// formatting a string per card. A Chars that does not implement it -- every
// ordinary caller, rules.Engine included -- gets the token.
type tokenSuppressor interface{ SuppressCardTokens() bool }

// token is id's display token ("#12"), or "" for a tokenSuppressor.
func (p *projector) token(id state.ObjID) string {
	if p.noTokens {
		return ""
	}
	return cardToken(id)
}

// tokenTableSize bounds the display tokens precomputed at init: "#0" through
// "#8191" live in one immutable string (tokenText, tokenEnds[i] the end of
// "#i"), so a projection slices its tokens out of it instead of formatting
// one string per card. Larger ids are formatted.
const tokenTableSize = 1 << 13

var tokenText, tokenEnds = buildTokenTable()

func buildTokenTable() (string, []uint32) {
	b := make([]byte, 0, tokenTableSize*6)
	ends := make([]uint32, tokenTableSize)
	for i := range ends {
		b = append(b, '#')
		b = strconv.AppendUint(b, uint64(i), 10)
		ends[i] = uint32(len(b))
	}
	return string(b), ends
}

// cardToken is id's display token, "#" and its decimal id.
func cardToken(id state.ObjID) string {
	if id < tokenTableSize {
		start := uint32(0)
		if id > 0 {
			start = tokenEnds[id-1]
		}
		return tokenText[start:tokenEnds[id]]
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

// cardView writes one object's public face into cv. ch is read through
// Power/Toughness/Keywords rather than any printed field directly — the view
// asks the engine for derived characteristics, never the card's own text —
// and a nil ch (supplement §7) degrades to the zero P/T with no keywords.
//
// Every field of cv is rewritten. The storage cv already holds -- its
// Produces and AttackingPlayer structs, its BlockedBy and Keywords arrays,
// its Counters map -- is reused for the new values (ProjectInto's reuse), so
// it must be storage a previous projection made, never a slice or pointer
// anything else holds. A fresh (zero) cv allocates exactly what a new
// CardView needs.
func (p *projector) cardView(cv *CardView, id state.ObjID) {
	o := p.g.Obj(id)
	prod, atk, ctr, types := cv.Produces, cv.AttackingPlayer, cv.Counters, cv.Types
	blk, kws := cv.BlockedBy[:0], cv.Keywords[:0]
	*cv = CardView{
		ID: id, Tapped: o.Tapped, Damage: o.Damage, Attacking: o.IsAttacking,
		Controller: o.Controller, Owner: o.Owner, SummonSick: o.SummonSick,
		AttachedTo: o.AttachedTo, ActivatedThisTurn: o.ActivatedThisTurn,
	}
	cv.Token = p.token(id)
	cv.IsToken = o.IsToken
	cv.IsCopy = o.IsCopy
	// vchars answers the four derived facts below in one call; without it
	// each is asked for separately.
	var dName string
	var dKw []string
	if p.vchars != nil {
		dName, dKw, cv.Power, cv.Toughness = p.vchars.ViewCharacteristics(id)
	}
	if f := o.Face(); f != nil {
		cv.Name = f.Name
		// Name is a layer-3 characteristic. Keep the optional method so
		// lightweight Chars test doubles remain source-compatible while the
		// real rules engine exposes SetName$ results to clients.
		if p.vchars != nil {
			if dName != "" {
				cv.Name = dName
			}
		} else if p.namer != nil {
			if name := p.namer.Name(id); name != "" {
				cv.Name = name
			}
		}
		cv.Types = p.typeLine(types, f.Types)
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
		// with no mana ability off the wire; the struct is this CardView's own
		// (fresh, or the one it held before), so the address is never aliased
		// across object views.
		if mp := f.ManaProduction(); !mp.IsZero() {
			if prod == nil {
				prod = new(cards.ManaProduction)
			}
			*prod = mp
			cv.Produces = prod
		}
	}
	if o.IsAttacking {
		if atk == nil {
			atk = new(state.PlayerID)
		}
		*atk = o.Attacking
		cv.AttackingPlayer = atk
	}
	if len(o.BlockedBy) > 0 {
		cv.BlockedBy = append(blk, o.BlockedBy...)
	}
	if p.vchars != nil {
		if len(dKw) > 0 {
			cv.Keywords = append(kws, dKw...)
		}
	} else if p.ch != nil {
		cv.Power = p.ch.Power(id)
		cv.Toughness = p.ch.Toughness(id)
		// A copy: Chars is an interface, and nothing guarantees an
		// implementation hands back a slice nobody else holds a reference
		// to (supplement §10's no-aliasing rule).
		if kw := p.ch.Keywords(id); len(kw) > 0 {
			cv.Keywords = append(kws, kw...)
		}
	}
	if len(o.Counters) > 0 {
		if ctr == nil {
			ctr = make(map[string]int32, len(o.Counters))
		} else {
			clear(ctr)
		}
		for _, c := range o.Counters {
			ctr[c.Kind] = c.N
		}
		cv.Counters = ctr
	}
}

// typeLine is strings.Join(parts, " "), the CardView.Types line, without a
// fresh string per card: prev (the slot's previous line) when it already
// reads the same, else the projection's type-line cache.
func (p *projector) typeLine(prev string, parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	if joinedIs(prev, parts) {
		return prev
	}
	if p.types != nil {
		return p.types.join(parts)
	}
	return strings.Join(parts, " ")
}

// joinedIs reports whether s == strings.Join(parts, " "), allocation-free.
func joinedIs(s string, parts []string) bool {
	for i, part := range parts {
		if i > 0 {
			if len(s) == 0 || s[0] != ' ' {
				return false
			}
			s = s[1:]
		}
		if !strings.HasPrefix(s, part) {
			return false
		}
		s = s[len(part):]
	}
	return len(s) == 0
}

// typeCache is a small direct-mapped cache of joined type lines, keyed by a
// hash of the parts and verified against them on every hit, so a hit is
// always exactly strings.Join(parts, " ").
type typeCache struct{ slots [256]string }

func (c *typeCache) join(parts []string) string {
	h := uint32(2166136261)
	for _, s := range parts {
		for i := 0; i < len(s); i++ {
			h = (h ^ uint32(s[i])) * 16777619
		}
		h = (h ^ ' ') * 16777619
	}
	slot := &c.slots[(h^h>>16)&255]
	if !joinedIs(*slot, parts) {
		*slot = strings.Join(parts, " ")
	}
	return *slot
}

// projScratch is the per-projection scratch that is not part of any View:
// only caches whose hits are verified, so which scratch a projection draws
// changes no output. Pooled, so a steady stream of projections reuses it.
type projScratch struct {
	types typeCache
	text  textMemo
}

// textMemo is a small direct-mapped memo of derived display text (a
// placeholder substitution, a restriction sentence), keyed by the two
// strings the text is a pure function of and verified against both on every
// hit, so a hit is always exactly what computing it would return.
type textMemo struct{ slots [64]textMemoEntry }

type textMemoEntry struct {
	k1, k2, out string
	kind        memoKind
}

// memoKind names which derivation an entry memoises; the zero value is an
// empty slot.
type memoKind uint8

const (
	memoSubstitute memoKind = iota + 1 // substitutePlaceholders(k1, k2)
	memoLabel                          // optionLabelText(k1)
	memoRestrict                       // the restriction sentence of Valid$ k1, chosen type k2
)

// lookup returns the memoised kind text for (k1, k2) and the slot it lives
// in; ok is false on a miss, and the caller stores the computed text through
// the slot's store.
func (m *textMemo) lookup(kind memoKind, k1, k2 string) (slot *textMemoEntry, out string, ok bool) {
	h := uint32(2166136261) ^ uint32(kind)
	for i := 0; i < len(k1); i++ {
		h = (h ^ uint32(k1[i])) * 16777619
	}
	h = (h ^ 0xff) * 16777619
	for i := 0; i < len(k2); i++ {
		h = (h ^ uint32(k2[i])) * 16777619
	}
	slot = &m.slots[(h^h>>16)&63]
	if slot.kind == kind && slot.k1 == k1 && slot.k2 == k2 {
		return slot, slot.out, true
	}
	return slot, "", false
}

func (e *textMemoEntry) store(kind memoKind, k1, k2, out string) string {
	*e = textMemoEntry{k1: k1, k2: k2, out: out, kind: kind}
	return out
}

// substitute is substitutePlaceholders(text, name), memoised in m when m is
// non-nil.
func (m *textMemo) substitute(text, name string) string {
	if m == nil || !hasPlaceholder(text) || name == "" {
		return substitutePlaceholders(text, name)
	}
	slot, out, ok := m.lookup(memoSubstitute, text, name)
	if ok {
		return out
	}
	return slot.store(memoSubstitute, text, name, substitutePlaceholders(text, name))
}

// labelText is optionLabelText(label), memoised in m when m is non-nil.
func (m *textMemo) labelText(label string) string {
	if m == nil || !hasPlaceholder(label) {
		return optionLabelText(label)
	}
	slot, out, ok := m.lookup(memoLabel, label, "")
	if ok {
		return out
	}
	return slot.store(memoLabel, label, "", optionLabelText(label))
}

var scratchPool = sync.Pool{New: func() any { return new(projScratch) }}
