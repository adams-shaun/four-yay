package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// The quiet-seat proof's per-face facts (design
// docs/superpowers/specs/2026-10-08-quiet-seat-walk-skip-design.md, §2.4,
// §2.5, §3.1). They are compiled once per face, in the same constructor as
// the other walkFaceFacts, under the same currentFor guards, so a face whose
// ability or keyword list was replaced after the table was built reads as a
// miss.
//
// Every field over-approximates "may offer": a fact set too generously only
// costs coverage, never soundness. Nothing here computes an option; the
// proof built on it only ever shows that none can exist.

// quietZones is the number of zone summaries per face: the five zones the
// battlefield ability loop visits (legal_walk_battlefield.go:169, in its
// order).
const quietZones = 5

// quietZoneIndex maps a walk zone to its abQuiet slot, or -1 for a zone the
// ability loop does not visit.
func quietZoneIndex(z state.Zone) int {
	switch z {
	case state.ZBattlefield:
		return 0
	case state.ZStack:
		return 1
	case state.ZGraveyard:
		return 2
	case state.ZHand:
		return 3
	case state.ZExile:
		return 4
	}
	return -1
}

// abQuietZone summarizes one face's non-mana activated abilities whose
// ActivationZone$ admits one zone. Every admitting ability is bucketed by
// whether its cost needs {T}, and the bucket records the minimum mana floor
// and whether every member is sorcery-speed-only. Q3b: an admitting ability
// whose cost's non-mana parts the proof can bound is stored as ONE
// quietBoundedAbility group instead of only the nonMana bit, so the proof can
// evaluate the parts' necessary conditions instead of blocking outright.
type abQuietZone struct {
	any      bool // some ability admits this zone
	nonMana  bool // some admitting ability's cost has a part the bound cannot price
	hasAny   bool // floorAny is set
	hasTap   bool // floorTap is set
	floorAny int32
	floorTap int32
	sorcAny  bool // every no-{T} admitting ability is SorcerySpeed$ True
	sorcTap  bool // every {T} admitting ability is SorcerySpeed$ True
	// groups holds the part-carrying admitting abilities whose cost the
	// Q3b bound CAN price: mana-only abilities live in the floorAny/floorTap
	// aggregation above, an unpriceable cost sets nonMana, and one group per
	// priceable ability carries its own mana floor, {T} need, sorcery timing
	// and part list. Overflow (more abilities than quietMaxGroups, or more
	// parts than quietMaxParts) sets nonMana -- the ability then blocks
	// unconditionally, the same over-approximation as before Q3b.
	//
	// It is SPARSE: only the present groups are stored, because a quietPart
	// embeds a 16-byte string and a dense [quietZones][quietMaxGroups] array
	// would grow walkFaceFacts ~5x past its committed memory-shape bound
	// (TestWalkFaceFactsAltCostSidecarIsSparse) for EVERY face, part-carrying
	// or not. The overwhelming majority of faces carry no bounded part, so
	// they allocate nothing here; len(groups) is the group count. The same
	// sparse-sidecar discipline as walkFaceFacts.altCosts.
	groups []quietBoundedAbility
}

// quietMaxGroups and quietMaxParts bound the per-zone part storage. Three
// groups cover a planeswalker face's three loyalty abilities (each a
// SubCounter<N/LOYALTY> cost) beside an ordinary activated ability; three
// parts cover the corpus's multi-part ability costs. Anything beyond is
// nonMana (blocked outright), which can only cost coverage.
const (
	quietMaxGroups = 3
	quietMaxParts  = 3
)

// quietPartKind names the non-mana cost-part kinds the Q3b bound prices.
// Every other component (Phyrexian pips, X, AddCounter, Reveal, ExileFromTop,
// ...) stays in quietCostUnclassified and reads as nonMana.
type quietPartKind uint8

const (
	pqNone         quietPartKind = iota // zero value: the part needs no check
	pqSac                               // sacrifice N permanents from the payer's battlefield
	pqDiscard                           // discard N cards from the payer's hand
	pqSubCounter                        // remove N counters of a kind from the source
	pqLife                              // pay N life
	pqExileGrave                        // exile N cards from the payer's graveyard
	pqExileHand                         // exile N cards from the payer's hand
	pqTapPermanent                      // tap N untapped permanents from the payer's battlefield
)

// quietPart is one bounded non-mana cost part: the kind, the count, and the
// cheap precomputed shape the proof's count over-approximates the part's
// Forge filter spec with. incl is the type mask a candidate must intersect
// (0 = any type -- a subtype base such as "Goblin" has no cheap bit, and the
// bare count the brief calls the coarse version), excl the types a candidate
// must have none of (a "nonLand" predicate), exclSelf the ".Other" predicate
// (the source itself is not a candidate), and self a CARDNAME/NICKNAME spec
// (the source is the ONLY candidate). kindStr carries a SubCounter part's
// counter kind -- the same string the offer gate's pay.SubCounterAvailable
// compares, so gate and proof cannot disagree about which counters pay.
type quietPart struct {
	kind     quietPartKind
	n        int32
	incl     cards.TypeMask
	excl     cards.TypeMask
	exclSelf bool
	self     bool
	kindStr  string
}

// quietBoundedAbility is one admitting ability whose cost the Q3b bound
// prices: its own mana floor, {T} need, sorcery timing, and the bounded
// non-mana parts.
type quietBoundedAbility struct {
	tap    bool
	sorc   bool
	floor  int32
	nParts uint8
	parts  [quietMaxParts]quietPart
}

// quietFaceFacts are the spell-half and ability-half facts the quiet proof
// reads. They live on walkFaceFacts.quiet.
type quietFaceFacts struct {
	// Spell classifier (§2.5).
	isLand       bool
	instantSpeed bool
	castOpen     bool
	castFloor    int32

	// Graveyard recast routes a face can open (Flashback, Escape, Unearth,
	// Disturb, Jump-start, Retrace, Embalm, Eternalize, Scavenge, Encore,
	// Harmonize, Mayhem, Aftermath) -- any of these is a blocker for that
	// card in a graveyard. Q3a adds recastFloor/recastOpen to price the route
	// instead of blocking on the raw head.
	recastKW bool
	// recastFloor is the minimum mana floor over the printed graveyard-recast
	// routes the walk prices (Flashback's keyword cost, Mayhem's substituted
	// cost, a Warp the face may use from its graveyard); -1 means no printed
	// graveyard cast route is priced. recastOpen marks a printed route whose
	// cost the bound cannot price: Escape's exile part, Retrace's and
	// Jump-start's discard, Harmonize's creature-power reduction, a route
	// keyword the graveyard walk does not price as a cast (Unearth, Disturb,
	// Embalm, Eternalize, Scavenge, Encore, Aftermath), a bare parameterless
	// Mayhem, or a cost-modifier static that could rewrite the recast cost.
	// The Q3a blocker fires when the recast's timing is open and the route is
	// open or the floor is affordable.
	recastFloor int32
	recastOpen  bool
	// Exile recast routes a face can open (Warp, Foretell, Plot, Suspend).
	exileCastKW bool
	// downCast: the face prints Morph/Megamorph/Disguise (a {3} face-down
	// cast). Same three reads quietFaceHasDownCast makes, folded in once.
	downCast bool

	// manaMax is the most units ONE activation of any printed mana ability
	// yields (an over-count when a face prints several); manaIndeterminate
	// reports a printed mana ability whose amount the projection cannot
	// price, which makes the mana ceiling unbounded.
	manaMax           int32
	manaIndeterminate bool

	abQuiet [quietZones]abQuietZone
}

// quietGraveHeads are the keyword heads that open a cast from the graveyard.
var quietGraveHeads = [...]kwHead{
	kwhFlashback, kwhEscape, kwhRetrace, kwhJumpStart, kwhMayhem, kwhHarmonize, kwhWarp,
}

// quietGraveHeadNames are the same heads plus the parameterless/derived
// graveyard keywords read by altcast_modes and cast_altcost (Unearth,
// Disturb, Embalm, Eternalize, Scavenge, Encore, Aftermath). They are read
// as printed keyword heads; a face carrying any one is a graveyard blocker.
var quietGraveHeadNames = [...]string{
	"Unearth", "Disturb", "Embalm", "Eternalize", "Scavenge", "Encore", "Aftermath",
}

// quietExileHeads are the keyword heads that open a cast from exile.
var quietExileHeads = [...]kwHead{kwhWarp, kwhForetell}

// quietExileHeadNames are the parameterless exile-recast keywords read as
// printed heads.
var quietExileHeadNames = [...]string{"Plot", "Suspend"}

// quietHandActionHeads are the keyword-action heads the hand walk offers
// beyond a face's own cast timing, as precompiled heads for the derived
// precheck. Suspend, Plot and MayFlashCost have no keyword_heads.go literal.
var (
	kwhSuspend      = newKWHead("Suspend")
	kwhPlot         = newKWHead("Plot")
	kwhMayFlashCost = newKWHead("MayFlashCost")
)

// quietGraveDerivedHeads and quietExileDerivedHeads are the DERIVED-keyword
// twins of recastKW / exileCastKW: every graveyard- and exile-recast head,
// including the parameterless/activation ones, for the per-object derived
// precheck (quietObjectDerivedRoute). A layer-6 grant -- Snapcaster Mage's
// Flashback, Underworld Breach's Escape, a Warp/Foretell grant -- reaches an
// object whose printed face lacks the head, and the walk offers the recast, so
// the proof must read the derived list, not only the printed face.
var quietGraveDerivedHeads = func() []kwHead {
	out := make([]kwHead, 0, len(quietGraveHeads)+len(quietGraveHeadNames))
	out = append(out, quietGraveHeads[:]...)
	for _, n := range quietGraveHeadNames {
		out = append(out, newKWHead(n))
	}
	return out
}()

var quietExileDerivedHeads = func() []kwHead {
	out := make([]kwHead, 0, len(quietExileHeads)+len(quietExileHeadNames))
	out = append(out, quietExileHeads[:]...)
	for _, n := range quietExileHeadNames {
		out = append(out, newKWHead(n))
	}
	return out
}()

// computeQuietFaceFacts builds the quiet facts for f. It is a pure function
// of the face, like every other walkFaceFacts member. hasAltCosts reports
// whether the face carries a compiled alternative-cost keyword entry (the
// walkFaceFacts.altCosts family), which is castOpen for the same reason.
// castOpenReason names which castOpen source fired. coSelfReduceFloor marks
// the §2.5 case the pip floor prices instead of blocking: every cost static
// the face prints is a self-scoped generic-only ReduceCost, so the cast's
// true mana floor is the face's non-reducible pip count and castOpen is not
// needed.
type castOpenReason uint8

const (
	coNone castOpenReason = iota
	coAltCosts
	coKeyword
	coStatic
	coSelfReduceFloor
	coMayPlayStatic
	coPhyrexian
	coUnknown
	coNoManaCost
)

func computeQuietFaceFacts(f *cards.Face, hasAltCosts bool) quietFaceFacts {
	var q quietFaceFacts
	q.isLand = f.IsLand()
	q.castFloor = f.Cmc()
	q.instantSpeed = f.IsInstant() || f.HasKeyword("Flash") || mayFlashSacFace(f) || faceHasCastWithFlash(f)
	if hasAltCosts {
		q.castOpen = true
	} else if r, open := quietCastOpen(f); open {
		q.castOpen = true
		if r == coStatic {
			// §2.5's self-ReduceCost refinement: a face whose only cost
			// statics are self-scoped generic-only reductions is priced at
			// its pip floor, not blocked. The floor is a true lower bound of
			// the real mana cost (a generic-only reduction can never reduce
			// a coloured or hybrid pip), so claiming quiet below it stays
			// sound; when the refinement does not hold, r stays coStatic and
			// the face stays castOpen.
			if floor, ok := quietSelfReducePipFloor(f); ok {
				q.castOpen, q.castFloor = false, floor
			}
		}
	}
	if len(f.Keywords) > 0 {
		for _, h := range quietGraveHeads {
			if f.KeywordLinesHaveHead(h.S, h.ID) {
				q.recastKW = true
			}
		}
		for _, h := range quietGraveHeadNames {
			if hd := kwHeadOf(h); f.KeywordLinesHaveHead(hd.S, hd.ID) {
				q.recastKW = true
			}
		}
		for _, h := range quietExileHeads {
			if f.KeywordLinesHaveHead(h.S, h.ID) {
				q.exileCastKW = true
			}
		}
		for _, h := range quietExileHeadNames {
			if hd := kwHeadOf(h); f.KeywordLinesHaveHead(hd.S, hd.ID) {
				q.exileCastKW = true
			}
		}
	}
	q.downCast = quietFaceHasDownCast(f)
	quietRecastFacts(f, &q)
	quietManaFacts(f, &q)
	quietAbilityFacts(f, &q)
	return q
}

// quietCastOpen reports the §2.5 castOpen conditions: any shape whose real
// cost could be lower than the printed mana value, or that could replace the
// mana cost. When in doubt it answers true. It also returns which source
// fired, so computeQuietFaceFacts can apply the self-ReduceCost pip floor to
// the coStatic case.
func quietCastOpen(f *cards.Face) (castOpenReason, bool) {
	if f == nil {
		return coUnknown, true
	}
	if isNoManaCost(f.ManaCost) {
		return coNoManaCost, true
	}
	c := ParseCost(f.ManaCost)
	if len(c.Phyrexian) > 0 || len(c.HybridPhyrexian) > 0 {
		return coPhyrexian, true
	}
	if len(c.Unknown) > 0 || len(c.Withheld) > 0 {
		return coUnknown, true
	}
	if len(f.Keywords) > 0 {
		for _, h := range quietCastOpenHeads {
			if f.HasKeyword(h) {
				return coKeyword, true
			}
		}
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		switch st.Mode {
		case "AlternativeCost", "SetCost", "ReduceCost":
			return coStatic, true
		}
		// A self-carried may-play static (Omniscience, Conspiracy Unraveler)
		// is the second source alternativeCosts reads while the card is still
		// in hand: it may replace the mana cost with a cheaper or free one.
		// Any of the may-play cost keys is enough; when in doubt, castOpen.
		if quietStaticMayPlay(st) {
			return coMayPlayStatic, true
		}
	}
	return coNone, false
}

// quietSelfReducePipFloor prices the §2.5 self-ReduceCost refinement: when
// EVERY cost static the face prints is a ReduceCost scoped to the face itself
// (ValidCard$ exactly "Card.Self" -- the same predicate costStaticSelfOnly
// applies at pricing time) and names no Color$ (so it reduces the generic
// part only; a colour reduction takes pips and a numeric token reduces
// generic too, so both fail closed), the real mana cost is the printed cost
// minus non-negative generic reductions. Its true lower bound is therefore
// the face's NON-REDUCIBLE pip count: the coloured pips (WUBRG only -- the
// colourless slot and Snow are generic units a reduction may eat) plus the
// two-colour hybrid pips (payable only as one of their colours, never with
// generic). Twobrid pips have a generic-paying face and are excluded; a
// Phyrexian or hybrid-Phyrexian face never reaches here (castOpen). A
// RaiseCost static only raises, so it does not lower the floor; a
// SetCost/AlternativeCost static or any may-play cost key can replace the
// cost with a cheaper one and fails closed.
func quietSelfReducePipFloor(f *cards.Face) (int32, bool) {
	if f == nil || isNoManaCost(f.ManaCost) {
		return 0, false
	}
	seen := false
	for i := range f.Statics {
		st := &f.Statics[i]
		switch st.Mode {
		case "ReduceCost":
			if validCardSpecIsSelf(st.ParamStr(cards.PKValidCard)) && strings.TrimSpace(st.ParamStr(cards.PKColor)) == "" {
				seen = true
				continue
			}
			return 0, false
		case "SetCost", "AlternativeCost":
			return 0, false
		default:
			if quietStaticMayPlay(st) {
				return 0, false
			}
		}
	}
	if !seen {
		return 0, false
	}
	c := ParseCost(f.ManaCost)
	var pips int32
	for i := 0; i < 5; i++ {
		pips += c.Colored[i]
	}
	return pips + int32(len(c.Hybrid)), true
}

// quietRecastOfferCreditHeads are the keyword heads the offer gate credits
// onto the recast cost through offerCastableUsing itself, independent of the
// base the caller passes. Delve is the one such head: offerCastableUsing
// reads hasKeywordH(id, kwhDelve) and subtracts one generic per graveyard card
// on EVERY cast scope (rules/mana.go), including the flashback/mayhem/warp
// recasts whose loops pass the raw cost without castOfferBase -- so the
// printed recast floor is not a lower bound of what the walk can offer.
// Convoke and Improvise are deliberately absent: their credit is composed by
// castOfferBase, which those loops do not call, so it does not reach the
// priced route. A future offer-time credit added to offerCastableUsing must be
// listed here. A granted Delve is a board-wide blocker via
// quietActiveKWGrantBlocker's quietCastOpenHeads scan.
var quietRecastOfferCreditHeads = [...]string{"Delve"}

// quietRecastFacts prices a face's printed graveyard-recast routes for the
// Q3a blocker: recastFloor is the minimum mana floor over the routes the
// graveyard walk prices, and recastOpen marks a route whose cost the bound
// cannot price. It reads only the printed keyword parameters and the face's
// own statics -- the same compiled reads the walk's graveyard section makes
// (e.flashbackCost / mayhemCastCost / keywordAltCost / warpGraveyardAllowed),
// never a Params map at proof time. A granted recast route is not read here:
// the board grant blocker fails the window closed on any active AddKeyword$
// recast head, and the per-object extra-route check covers intrinsic keywords
// and counters.
func quietRecastFacts(f *cards.Face, q *quietFaceFacts) {
	if !q.recastKW {
		q.recastFloor = -1
		return
	}
	floor := int32(-1)
	open := false
	// An offer-time keyword credit (Delve) lowers the real recast floor below
	// the printed one, so the route cannot be priced. Read the printed face;
	// a granted instance is the board grant blocker's.
	for _, name := range quietRecastOfferCreditHeads {
		if f.HasKeyword(name) {
			open = true
		}
	}
	add := func(c Cost) {
		fl, _, nonMana := quietCostFloor(&c)
		if nonMana {
			open = true
			return
		}
		if floor < 0 || fl < floor {
			floor = fl
		}
	}
	// A cost-modifier or may-play static the face prints can rewrite the
	// recast's cost (offerCastable composes them), so the printed route is
	// unpriced. Board-scoped statics are the board cost blocker's; a
	// self-scoped one is not, so it is read here.
	for i := range f.Statics {
		st := &f.Statics[i]
		switch st.Mode {
		case "ReduceCost", "SetCost", "AlternativeCost":
			open = true
		default:
			if quietStaticMayPlay(st) {
				open = true
			}
		}
	}
	if f.KeywordLinesHaveHead(kwhFlashback.S, kwhFlashback.ID) {
		if quietSpellExtrasNonMana(f) {
			open = true
		} else {
			add(ParseCost(quietFlashbackRaw(f)))
		}
	}
	if f.KeywordLinesHaveHead(kwhEscape.S, kwhEscape.ID) {
		open = true // CR 702.42a: the ExileFromGrave part cannot be bounded.
	}
	if f.KeywordLinesHaveHead(kwhRetrace.S, kwhRetrace.ID) {
		open = true // Discard<1/Land> additional cost.
	}
	if f.KeywordLinesHaveHead(kwhJumpStart.S, kwhJumpStart.ID) {
		open = true // Discard<1/Card> additional cost.
	}
	if f.KeywordLinesHaveHead(kwhMayhem.S, kwhMayhem.ID) {
		if raw, ok := f.KeywordParam("Mayhem"); ok && strings.TrimSpace(raw) != "" {
			add(ParseCost(raw))
		} else {
			open = true // bare K:Mayhem is the land play, not a priced cast
		}
	}
	if f.KeywordLinesHaveHead(kwhHarmonize.S, kwhHarmonize.ID) {
		open = true // harmonizePayment reduces the generic by creature power.
	}
	if f.KeywordLinesHaveHead(kwhWarp.S, kwhWarp.ID) && warpGraveyardAllowed(f) {
		if c, ok := keywordAltCost(f, "Warp"); ok {
			add(c)
		} else {
			open = true
		}
	}
	// The parameterless/activation heads the graveyard walk does not price as
	// a cast here (Unearth is the ability loop's; the rest are unimplemented
	// routes the proof must still fail closed on).
	for _, name := range quietGraveHeadNames {
		if hd := kwHeadOf(name); hd.S != "" && f.KeywordLinesHaveHead(hd.S, hd.ID) {
			open = true
		}
	}
	if open {
		q.recastOpen = true
		q.recastFloor = -1
		return
	}
	q.recastFloor = floor
}

// quietFlashbackRaw is the raw cost string e.flashbackCost parses for the
// printed face: the K:Flashback cost field, or the card's own mana cost when
// the keyword is parameterless.
func quietFlashbackRaw(f *cards.Face) string {
	if s, ok := f.KeywordCostParam("Flashback"); ok {
		return s
	}
	return f.ManaCost
}

// quietSpellExtrasNonMana reports whether the face's own SpellAbility Cost$
// carries a non-mana part. pay.WithSpellAbilityExtras folds those parts onto
// every recast the graveyard walk offers, so a face with one cannot be priced
// at its mana floor. The mana part of a Cost$ restates the printed cost and is
// deliberately ignored, exactly as foldAdditionalCost ignores it.
func quietSpellExtrasNonMana(f *cards.Face) bool {
	sa := f.SpellAbility()
	if sa == nil {
		return false
	}
	sc := strings.TrimSpace(sa.ParamStr(cards.PKCost))
	if sc == "" {
		return false
	}
	c := ParseCost(sc)
	_, _, nonMana := quietCostFloor(&c)
	return nonMana
}

// quietStaticMayPlay reports whether ONE static line carries any may-play
// cost parameter (MayPlay$, MayPlayAltManaCost$, MayPlayWithoutManaCost$) --
// any of which can substitute or remove a cast's mana cost while the card
// sits where the face is. It is the quiet-seat proof's direct scan over a
// face's Statics slice, the same shape mayPlayKinds and mayPlayAltCosts
// read, so its reads are family-attributed to Continuous.MayPlay like
// theirs: a MayPlay-carrying static's keys are checked against the family
// set, and these keys never mask a plain Continuous static's genuinely
// unread keys.
func quietStaticMayPlay(st *cards.Static) bool {
	return st.HasParam(cards.PKMayPlay) ||
		st.HasParam(cards.PKMayPlayAltManaCost) ||
		st.HasParam(cards.PKMayPlayWithoutManaCost)
}

// quietCastOpenHeads are keyword heads whose presence can open a cheaper or
// substituted HAND cast, so the proof treats the face as unpriced. It follows
// the design's §2.5 list; anything else is when-in-doubt castOpen. The
// graveyard/exile recast family (Flashback, Escape, Unearth, Disturb, Embalm,
// Eternalize, Scavenge, Encore, Aftermath, Retrace, Jump-start, Mayhem,
// Harmonize, Warp, Suspend, Foretell, Plot) is NOT here: those keywords open a
// cast from another zone with its own blocker (recastKW/exileCastKW), not a
// cheaper hand cast, and marking them castOpen wrongly blocked every hand card
// that prints one.
//
// Kicker is not here either (design §2.5: "only when a kicker could reduce or
// replace the cost"). A kicker is an OPTIONAL ADDITIONAL cost: the offer path
// adds it to the printed cost (rules/cast_begin.go's castModeKicked branch,
// cost.Plus(kickerCost)), so a kicker cast is never cheaper than the plain
// cast and the printed castFloor already bounds every kicker variant. Two-part
// and/or kickers add their parts the same way. A kicker with a non-mana part
// still pays the printed mana, so the floor test governs it too.
var quietCastOpenHeads = [...]string{
	"Convoke", "Delve", "Improvise", "Affinity", "Emerge", "Evoke", "Surge",
	"Spectacle", "Prowl", "Madness", "Ninjutsu", "Dash", "Bargain", "Offspring",
	"Blitz", "Sneak", "Web-slinging",
	// Conditional flash / cost-permission riders: the face is castable on a
	// timing or cost the plain classifier cannot price.
	"Teamwork", "MayFlashSac", "MayFlashCost",
}

// quietManaFacts fills manaMax / manaIndeterminate from the face's printed
// mana abilities.
func quietManaFacts(f *cards.Face, q *quietFaceFacts) {
	mp := f.ManaProduction()
	var sum int32
	for i := range mp.Colour {
		sum += mp.Colour[i]
	}
	// The colourless slot above already includes the one-unit
	// unmodelled-colour convention; an "Any" with an empty Colour vector
	// still yields at least one unit through that slot. Guard the corner
	// where the fold produced nothing at all.
	if sum == 0 && mp.Any {
		sum = 1
	}
	q.manaMax = sum
	q.manaIndeterminate = mp.Indeterminate
}

// quietAbilityFacts fills abQuiet over the face's non-mana activated
// abilities. A mana ability is excluded exactly as the ability loop excludes
// it (cards.IsManaAbilitySA && !loyalty); a loyalty-marked mana ability IS
// offered by the loop, so it stays in the summary.
func quietAbilityFacts(f *cards.Face, q *quietFaceFacts) {
	for _, ab := range f.Abilities {
		if ab == nil || ab.Kind != "AB" {
			continue
		}
		if cards.IsManaAbilitySA(ab) && !loyaltyAbilityText(ab) {
			continue
		}
		mask := abilityZoneMask(ab)
		c := ParseCost(ab.ParamStr(cards.PKCost))
		// The offer's ability-cost substitutions, fail closed: every one can
		// make the real payable price cheaper than the printed floor, so the
		// bound cannot price the ability and the summary marks it nonMana.
		// It is the same set offerFloorRefuses declines to floor-test
		// (rules/legal_walk.go: the ability's own ReduceCost$, an announced X
		// -- quietCostFloor's X arm -- and TapCreaturesForMana) plus the two
		// further compositions the printed offer runs: powerUpReduceCost's
		// PowerUp$ rider and a minted attach-cost SA's AlternateCost$ rider
		// (abilityAlternateCost prices the rider independently of the printed
		// cost, so the cheaper of the two is what can be offered). Heirloom
		// Epic's TapCreaturesForMana made the first arm real: three creature
		// taps pay three of its {4} with one floating mana, so the printed
		// floorTap of 4 mis-called a payable ability unaffordable and the
		// proof called the window quiet while the walk offered it.
		open := pay.TapCreaturesForMana(ab) ||
			strings.TrimSpace(ab.ParamStr(cards.PKReduceCost)) != "" ||
			strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKPowerUp)), "True") ||
			(isAttachCostSA(ab) && strings.TrimSpace(ab.ParamStr(cards.PKAlternateCost)) != "")
		sorc := strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKSorcerySpeed)), "True")
		var floor int32
		var tap bool
		var parts []quietPart
		if !open {
			floor, tap, parts, open = quietAbilityCost(&c)
		}
		for z := 0; z < quietZones; z++ {
			if mask&(1<<uint(quietZoneBit(z))) == 0 {
				continue
			}
			aq := &q.abQuiet[z]
			aq.any = true
			if open {
				// The bound cannot price the cost: the ability blocks
				// outright, the pre-Q3b behaviour (and it no longer
				// contributes its floor to the mana-only aggregation, whose
				// arms are exact only for abilities with no parts).
				aq.nonMana = true
				continue
			}
			if len(parts) == 0 {
				if tap {
					if !aq.hasTap {
						aq.floorTap, aq.hasTap, aq.sorcTap = floor, true, sorc
					} else {
						if floor < aq.floorTap {
							aq.floorTap = floor
						}
						aq.sorcTap = aq.sorcTap && sorc
					}
				} else {
					if !aq.hasAny {
						aq.floorAny, aq.hasAny, aq.sorcAny = floor, true, sorc
					} else {
						if floor < aq.floorAny {
							aq.floorAny = floor
						}
						aq.sorcAny = aq.sorcAny && sorc
					}
				}
				continue
			}
			// A priceable part-carrying ability: one group, its own floor.
			if len(aq.groups) >= quietMaxGroups || len(parts) > quietMaxParts {
				aq.nonMana = true
				continue
			}
			g := quietBoundedAbility{tap: tap, sorc: sorc, floor: floor, nParts: uint8(len(parts))}
			copy(g.parts[:len(parts)], parts)
			aq.groups = append(aq.groups, g)
		}
	}
}

// quietZoneBit maps an abQuiet slot back to the state.Zone bit it summarizes.
func quietZoneBit(slot int) state.Zone {
	switch slot {
	case 0:
		return state.ZBattlefield
	case 1:
		return state.ZStack
	case 2:
		return state.ZGraveyard
	case 3:
		return state.ZHand
	case 4:
		return state.ZExile
	}
	return state.ZBattlefield
}

// quietManaFloor is the §2.4 mana half of the cost classifier: the mana floor
// with X = 0 and whether the cost needs {T}.
func quietManaFloor(c *Cost) (floor int32, tap bool) {
	if c == nil {
		return 0, false
	}
	floor = c.Generic + int32(len(c.Hybrid)) + int32(len(c.Twobrid)) + c.Snow
	for i := 0; i < len(c.Colored); i++ {
		floor += c.Colored[i]
	}
	return floor, c.Tap
}

// quietCostUnclassified reports the components quietCostFloor cannot bound
// at all -- every cost.Cost field outside the six kinds the Q3b bound prices
// (Sac, Discard, SubCounter, Life, Exile, TapPermanent) plus the shapes of
// those kinds no bound covers. Any field of cost.Cost not explicitly
// classified here means true -- TestQuietCostClassifierCoversCostFields
// fails the build when the struct gains a field this condition does not
// name, so a new cost component cannot silently read as free.
func quietCostUnclassified(c *Cost) bool {
	return len(c.Phyrexian) > 0 || c.XMin != 0 || c.Waterbend != 0 || c.WaterbendX ||
		c.Untap || len(c.AddCounter) > 0 || len(c.ExileFromTop) > 0 ||
		len(c.Reveal) > 0 || len(c.RevealOrChoose) > 0 || len(c.RevealChosen) > 0 ||
		len(c.Behold) > 0 || len(c.UntapPermanent) > 0 ||
		len(c.Blight) > 0 || len(c.Exert) > 0 || c.Forage || len(c.Draw) > 0 ||
		len(c.Energy) > 0 || len(c.LifeX) > 0 || c.LifeHalfUp || len(c.DamageYou) > 0 ||
		len(c.GainLife) > 0 || len(c.Return) > 0 || len(c.PutToLib) > 0 ||
		len(c.MoveToGrave) > 0 || len(c.Mill) > 0 || len(c.Evidence) > 0 ||
		len(c.RollDice) > 0 || len(c.Withheld) > 0 || len(c.Unknown) > 0
}

// quietCostFloor is the §2.4 cost classifier over a compiled cost: the mana
// floor with X = 0, whether the cost needs {T}, and whether any part cannot
// be priced (nonMana). It is the CONSERVATIVE classifier: the Q3a recast
// pricing (quietRecastFacts) uses it whole, where any non-mana part makes
// the route unpriceable. The Q3b ability path (quietAbilityCost) instead
// prices the six bounded kinds and falls back to this answer for everything
// else.
func quietCostFloor(c *Cost) (floor int32, tap, nonMana bool) {
	floor, tap = quietManaFloor(c)
	nonMana = c.X != 0 || c.Life != 0 || len(c.Sac) > 0 || len(c.Discard) > 0 ||
		len(c.SubCounter) > 0 || len(c.Exile) > 0 || len(c.TapPermanent) > 0 ||
		quietCostUnclassified(c)
	return floor, tap, nonMana
}

// quietAbilityCost classifies one compiled ABILITY cost for the Q3b part
// bounds: the mana floor with X = 0, the {T} need, the bounded non-mana
// parts, and whether some part cannot be bounded (open -- the caller then
// keeps the ability a plain nonMana blocker, the pre-Q3b behaviour). An
// announced part is trivial: its count is a legal-zero announcement
// (DiscardCostPayable's reading), so it can never withhold an offer and the
// bound skips it.
func quietAbilityCost(c *Cost) (floor int32, tap bool, parts []quietPart, open bool) {
	if c == nil {
		return 0, false, nil, false
	}
	floor, tap = quietManaFloor(c)
	if c.X != 0 {
		// X is announced separately; a cost that needs an X has no fixed
		// floor, so the bound cannot price it.
		return floor, tap, nil, true
	}
	if quietCostUnclassified(c) {
		return floor, tap, nil, true
	}
	add := func(p quietPart, ok bool) bool {
		if !ok {
			open = true
			return false
		}
		if p.kind != pqNone {
			parts = append(parts, p)
		}
		return true
	}
	for i := range c.Sac {
		if !add(quietSacPart(&c.Sac[i])) {
			return floor, tap, nil, true
		}
	}
	for i := range c.Discard {
		if !add(quietDiscardPart(&c.Discard[i])) {
			return floor, tap, nil, true
		}
	}
	for i := range c.SubCounter {
		if !add(quietSubCounterPart(&c.SubCounter[i])) {
			return floor, tap, nil, true
		}
	}
	if c.Life != 0 {
		parts = append(parts, quietPart{kind: pqLife, n: c.Life})
	}
	for i := range c.Exile {
		if !add(quietExilePart(&c.Exile[i])) {
			return floor, tap, nil, true
		}
	}
	for i := range c.TapPermanent {
		if !add(quietTapPermanentPart(&c.TapPermanent[i])) {
			return floor, tap, nil, true
		}
	}
	if len(parts) > quietMaxParts {
		return floor, tap, nil, true
	}
	return floor, tap, parts, false
}

// quietSelfN1 guards the self-reference parts: the only candidate is the
// source itself, so a count above 1 cannot be paid and the bound is open.
func quietSelfN1(n int32) bool { return n <= 1 }

// quietSacPart bounds one Sac cost part. An announced Sac<X/Spec> is trivial
// (X = 0 is a legal announcement, SacrificeCostAssignable's reading); a
// bound referent is a granted-ability shape the printed face never carries,
// and the bound fails closed on it.
func quietSacPart(part *CostPart) (quietPart, bool) {
	if part.Announced || part.N <= 0 {
		return quietPart{}, true
	}
	if part.Referent != 0 {
		return quietPart{}, false
	}
	if spec := sacrificeMatchSpec(part.Spec); spec == "CARDNAME" {
		if !quietSelfN1(part.N) {
			return quietPart{}, false
		}
		return quietPart{kind: pqSac, n: part.N, self: true}, true
	}
	incl, excl, exclSelf, self, ok := quietSpecMask(sacrificeMatchSpec(part.Spec))
	if !ok || self {
		// A mask-classified Sac part must be a real type filter; a
		// self-shaped spec that survived the CARDNAME check is unboundable.
		return quietPart{}, false
	}
	return quietPart{kind: pqSac, n: part.N, incl: incl, excl: excl, exclSelf: exclSelf}, true
}

// quietDiscardPart bounds one Discard cost part. "Hand" is Forge's
// discard-your-whole-hand shape whose count the payment ignores (payable
// empty), so it is trivial; "Random" is any card; "LastDrawn" is a
// history-keyed slot the proof cannot see and fails closed on.
func quietDiscardPart(part *CostPart) (quietPart, bool) {
	if part.Announced || part.N <= 0 {
		return quietPart{}, true
	}
	switch sacrificeMatchSpec(part.Spec) {
	case "CARDNAME":
		if !quietSelfN1(part.N) {
			return quietPart{}, false
		}
		return quietPart{kind: pqDiscard, n: part.N, self: true}, true
	case "Hand":
		// The whole-hand shape never withholds an offer (DiscardCostPayable
		// reserves every candidate and checks no count).
		return quietPart{}, true
	case "LastDrawn":
		return quietPart{}, false
	case "Random":
		return quietPart{kind: pqDiscard, n: part.N}, true
	}
	if part.Referent != 0 {
		return quietPart{}, false
	}
	incl, excl, exclSelf, self, ok := quietSpecMask(part.Spec)
	if !ok || self {
		return quietPart{}, false
	}
	return quietPart{kind: pqDiscard, n: part.N, incl: incl, excl: excl, exclSelf: exclSelf}, true
}

// quietSubCounterPart bounds one SubCounter cost part. The bound prices only
// the source-anchored shapes (subCounterTargetsSource): a part whose
// removal-target field names another permanent needs a battlefield
// candidate the printed-face facts cannot see, so it fails closed. The
// counter kind is the part's own Spec string, the exact string the offer
// gate's pay.SubCounterAvailable compares ("Any" = the object's whole
// counter count).
func quietSubCounterPart(part *CostPart) (quietPart, bool) {
	if part.Announced || part.N <= 0 {
		return quietPart{}, true
	}
	if !subCounterTargetsSource(part.Target) {
		return quietPart{}, false
	}
	return quietPart{kind: pqSubCounter, n: part.N, kindStr: part.Spec}, true
}

// quietExilePart bounds one Exile cost part. Only the hand and graveyard
// zones are priced: a ZoneSet part (ExileCtrlOrGrave) names two zones at
// once and a bound Referent names a grantor, and both fail closed.
func quietExilePart(part *CostPart) (quietPart, bool) {
	if part.Announced || part.N <= 0 {
		return quietPart{}, true
	}
	if part.ZoneSet != 0 || part.Referent != 0 {
		return quietPart{}, false
	}
	var kind quietPartKind
	switch part.Zone {
	case 0:
		kind = pqExileHand
	case state.ZGraveyard:
		kind = pqExileGrave
	default:
		return quietPart{}, false
	}
	switch sacrificeMatchSpec(part.Spec) {
	case "CARDNAME":
		if !quietSelfN1(part.N) {
			return quietPart{}, false
		}
		return quietPart{kind: kind, n: part.N, self: true}, true
	case "All":
		// The whole-zone shape: the token still demands part.N cards
		// (IsWholeZoneExileSpec), priced as the plain zone count.
		return quietPart{kind: kind, n: part.N}, true
	}
	incl, excl, exclSelf, self, ok := quietSpecMask(part.Spec)
	if !ok || self {
		return quietPart{}, false
	}
	return quietPart{kind: kind, n: part.N, incl: incl, excl: excl, exclSelf: exclSelf}, true
}

// quietTapPermanentPart bounds one tapXType cost part. Only the literal
// count is priced: the X and Any forms announce their count, MinPower is a
// SET-level power floor the per-object matcher cannot evaluate, and a bound
// Referent names a grantor -- all fail closed.
func quietTapPermanentPart(part *CostPart) (quietPart, bool) {
	if part.Announced || part.N <= 0 {
		return quietPart{}, true
	}
	if part.Dyn != "" || part.MinPower != 0 || part.Referent != 0 {
		return quietPart{}, false
	}
	if spec := sacrificeMatchSpec(part.Spec); spec == "CARDNAME" {
		if !quietSelfN1(part.N) {
			return quietPart{}, false
		}
		return quietPart{kind: pqTapPermanent, n: part.N, self: true}, true
	}
	incl, excl, exclSelf, self, ok := quietSpecMask(part.Spec)
	if !ok || self {
		return quietPart{}, false
	}
	return quietPart{kind: pqTapPermanent, n: part.N, incl: incl, excl: excl, exclSelf: exclSelf}, true
}

// sacrificeMatchSpec is SacrificeMatchSpec (rules/pay): NICKNAME reads as
// CARDNAME, everything else passes through. The part builders here classify
// the same specs the payment gates see.
func sacrificeMatchSpec(spec string) string {
	if strings.EqualFold(spec, "NICKNAME") {
		return "CARDNAME"
	}
	return spec
}

// quietSpecMask over-approximates one Forge filter spec with the cheap type
// masks the proof's zone counts match: incl is the type set a candidate must
// intersect (0 = any type), excl the type set it must avoid, exclSelf the
// ".Other" predicate (the source is not a candidate), self a
// CARDNAME/NICKNAME base (the source is the only candidate -- the callers
// above handle it before reaching here). ok is false when the spec cannot be
// over-approximated at all.
//
// The over-approximation is sound because every clause of a Forge filter is
// a conjunctive restriction on the candidate: a base word that is not a type
// line word (a subtype such as "Goblin", a marker such as "TopOfLibrary")
// reads as the wildcard (any type), and a predicate word that is not
// recognised here (a colour, "cmcEQX", "TriggeredSource", ...) is ignored --
// both can only widen the counted set. The one widening the proof must not
// ignore is a negated predicate ("!Creature" on a Card base): it is still a
// conjunctive restriction of the same base, so it too is ignored -- the
// wildcard base already admits everything. An exclusion predicate ("nonLand")
// is the only shape that needs a bit of its own: with a wildcard base,
// "wildcard minus the excluded type" is exactly the filter's match set.
func quietSpecMask(spec string) (incl, excl cards.TypeMask, exclSelf, self bool, ok bool) {
	spec = strings.TrimSpace(spec)
	if strings.EqualFold(spec, "CARDNAME") || strings.EqualFold(spec, "NICKNAME") {
		return 0, 0, false, true, true
	}
	sawWild := false
	first := true
	for alt := range effects.FilterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, rest, _ := strings.Cut(alt, ".")
		altIncl := cards.TypeMaskOfName(base)
		altExcl := cards.TypeMask(0)
		altExclSelf := false
		if altIncl == 0 {
			if strings.EqualFold(base, "CARDNAME") || strings.EqualFold(base, "NICKNAME") {
				// A mixed spec with a self alternative -- no corpus shape;
				// fail closed rather than reason about the union.
				return 0, 0, false, false, false
			}
			sawWild = true
		}
		for p := range strings.SplitSeq(rest, "+") {
			switch {
			case p == "":
			case p == "Other":
				altExclSelf = true
			case p == "YouCtrl" || p == "YouOwn":
				// The proof counts the payer's own zone only, so a
				// possession predicate is satisfied by construction.
			case p == "token" || p == "Token" || p == "Tapped":
				// A narrowing predicate: ignoring it over-counts (safe).
			case strings.HasPrefix(p, "non"):
				bit := cards.TypeMaskOfName(p[len("non"):])
				if bit == 0 {
					return 0, 0, false, false, false
				}
				altExcl |= bit
			default:
				// Any other predicate (a colour, cmcEQX, TriggeredSource,
				// ...) narrows within the base; ignoring it over-counts.
			}
		}
		if first {
			incl, excl, exclSelf = altIncl, altExcl, altExclSelf
			first = false
			continue
		}
		incl |= altIncl
		excl &= altExcl
		exclSelf = exclSelf && altExclSelf
	}
	if sawWild {
		// A wildcard alternative admits every type (subject to its own
		// exclusions), so the union's type requirement is the wildcard; the
		// intersected exclusions above stay the over-set's only restriction.
		incl = 0
	}
	if first {
		// Every alternative was blank: the spec matched nothing at parse.
		// Treat it as the wildcard rather than reason about an empty spec.
		return 0, 0, false, false, true
	}
	return incl, excl, exclSelf, false, true
}

// quietCostFieldNames is the explicit allowlist of cost.Cost fields the
// classifier reads. The classify test reflects over cost.Cost and fails when
// a field is not named here (so a new cost component cannot silently read as
// free).
var quietCostFieldNames = map[string]bool{
	"Colored": true, "Generic": true, "Life": true, "X": true, "XMin": true,
	"Hybrid": true, "Phyrexian": true, "Twobrid": true, "HybridPhyrexian": true,
	"Snow": true, "Waterbend": true, "WaterbendX": true, "Tap": true, "Untap": true,
	"Sac": true, "Discard": true, "SubCounter": true, "AddCounter": true,
	"Exile": true, "ExileFromTop": true, "Reveal": true, "RevealOrChoose": true,
	"RevealChosen": true, "Behold": true, "TapPermanent": true, "UntapPermanent": true,
	"Blight": true, "Exert": true, "Forage": true, "Draw": true, "Energy": true,
	"LifeX": true, "LifeHalfUp": true, "DamageYou": true, "GainLife": true,
	"Return": true, "PutToLib": true, "MoveToGrave": true, "Mill": true,
	"Evidence": true, "RollDice": true, "Withheld": true, "Unknown": true,
}
