package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
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
// and whether every member is sorcery-speed-only.
type abQuietZone struct {
	any      bool // some ability admits this zone
	nonMana  bool // some admitting ability's cost has a part the bound cannot price
	hasAny   bool // floorAny is set
	hasTap   bool // floorTap is set
	floorAny int32
	floorTap int32
	sorcAny  bool // every no-{T} admitting ability is SorcerySpeed$ True
	sorcTap  bool // every {T} admitting ability is SorcerySpeed$ True
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
	// card in a graveyard.
	recastKW bool
	// Exile recast routes a face can open (Warp, Foretell, Plot, Suspend).
	exileCastKW bool

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
// it (cards.IsManaAbilityAPI && !loyalty); a loyalty-marked mana ability IS
// offered by the loop, so it stays in the summary.
func quietAbilityFacts(f *cards.Face, q *quietFaceFacts) {
	for _, ab := range f.Abilities {
		if ab == nil || ab.Kind != "AB" {
			continue
		}
		if cards.IsManaAbilityAPI(ab.API) && !loyaltyAbilityText(ab) {
			continue
		}
		mask := abilityZoneMask(ab)
		c := ParseCost(ab.ParamStr(cards.PKCost))
		floor, tap, nonMana := quietCostFloor(&c)
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
		if pay.TapCreaturesForMana(ab) ||
			strings.TrimSpace(ab.ParamStr(cards.PKReduceCost)) != "" ||
			strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKPowerUp)), "True") ||
			(isAttachCostSA(ab) && strings.TrimSpace(ab.ParamStr(cards.PKAlternateCost)) != "") {
			nonMana = true
		}
		sorc := strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKSorcerySpeed)), "True")
		for z := 0; z < quietZones; z++ {
			if mask&(1<<uint(quietZoneBit(z))) == 0 {
				continue
			}
			aq := &q.abQuiet[z]
			aq.any = true
			if nonMana {
				aq.nonMana = true
			}
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

// quietCostFloor is the §2.4 cost classifier over a compiled cost: the mana
// floor with X = 0, whether the cost needs {T}, and whether any part cannot
// be priced (nonMana). Any field of cost.Cost not explicitly classified here
// means nonMana -- TestQuietCostClassifierCoversCostFields fails the build
// when the struct gains a field this switch does not name.
func quietCostFloor(c *Cost) (floor int32, tap, nonMana bool) {
	if c == nil {
		return 0, false, false
	}
	floor = c.Generic + int32(len(c.Hybrid)) + int32(len(c.Twobrid)) + c.Snow
	for i := 0; i < len(c.Colored); i++ {
		floor += c.Colored[i]
	}
	tap = c.Tap
	if c.Life != 0 || len(c.Phyrexian) > 0 || c.XMin != 0 || c.Waterbend != 0 || c.WaterbendX ||
		c.Untap || len(c.Sac) > 0 || len(c.Discard) > 0 || len(c.SubCounter) > 0 ||
		len(c.AddCounter) > 0 || len(c.Exile) > 0 || len(c.ExileFromTop) > 0 ||
		len(c.Reveal) > 0 || len(c.RevealOrChoose) > 0 || len(c.RevealChosen) > 0 ||
		len(c.Behold) > 0 || len(c.TapPermanent) > 0 || len(c.UntapPermanent) > 0 ||
		len(c.Blight) > 0 || len(c.Exert) > 0 || c.Forage || len(c.Draw) > 0 ||
		len(c.Energy) > 0 || len(c.LifeX) > 0 || c.LifeHalfUp || len(c.DamageYou) > 0 ||
		len(c.GainLife) > 0 || len(c.Return) > 0 || len(c.PutToLib) > 0 ||
		len(c.MoveToGrave) > 0 || len(c.Mill) > 0 || len(c.Evidence) > 0 ||
		len(c.RollDice) > 0 || len(c.Withheld) > 0 || len(c.Unknown) > 0 {
		nonMana = true
	}
	// X is announced separately; a cost that needs an X has no fixed floor.
	if c.X != 0 {
		nonMana = true
	}
	return floor, tap, nonMana
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
