package effects

import (
	"math/bits"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// evalCountBodyZone evaluates the Valid/ValidAll/ValidZone zone-scan arm,
// including the $<Property> suffix folds, the extreme reductions and the
// distinct-set properties. Its fallthrough (below) is the dispatcher's.
func evalCountBodyZone(h Host, c *Ctx, g *state.Game, head, arg string, depth int) (int32, bool, bool) {
	// Valid / ValidZone forms count objects in a zone matching a filter (the
	// ValidAll all-zones head is the one exception -- see the branch itself).
	// A `$<Property>` suffix sums that numeric property over the matches
	// instead of counting them -- Mosswort Bridge's gate
	// `Count$Valid Creature.YouCtrl$CardPower` ("creatures you control have
	// total power 10 or greater") is the corpus shape (62 raw lines over 61
	// files: CardPower 42, CardManaCost 13, CardToughness 5). CardTypes and
	// Colors are DISTINCT-set counts over the same matches, not sums:
	// Colors counts the distinct colours among the matched permanents
	// ("the number of colors among permanents you control", Shimmercreep's
	// Vivid et al., 31 raw corpus lines, bounded by five), read through
	// ColorMaskOf so an explicit Colors: line and Devoid's colourless
	// treatment agree with the colour predicates; its single corpus op
	// suffix /LimitMax.<n> (happily_ever_after) clamps the result. An
	// unrecognised property keeps the whole token as the spec -- the
	// pre-existing fail-closed behaviour, since such a token never matched
	// anyway. The Different* distinct family IS read since diffcount1
	// (differentPropertyKindOf's case above): distinct powers/names/mana
	// values among the matches, the Augur of Autumn Coven gate's shape.
	// CreatureType and CardTypesPermanent (task diffcount2) are the same
	// distinct-set shape over a narrower vocabulary. The four extreme
	// reductions (GreatestCardPower 64 files,
	// GreatestCardManaCost 62, GreatestCardToughness 12, LeastCardPower 1;
	// 136 files total) are read: the max (or min, for Least) of the
	// property over the matches, with zero matches yielding 0 rather than a
	// sentinel. The bounded distinct-set properties' /LimitMax.<n> op
	// suffix (Colors one, CreatureType two) is honoured at
	// evalCountExprOK's generic /Op site (countDistinctLimitMax).
	isAll := head == "ValidAll"
	multi := multiCountZones(head)
	if zone, ok := countZone(head); ok || isAll || multi != nil {
		spec, prop, hasProp := strings.Cut(arg, "$")
		if !hasProp {
			spec, prop = arg, ""
		} else {
			prop = strings.TrimSpace(prop)
			switch {
			case prop == "CardPower" || prop == "CardToughness" || prop == "CardManaCost" ||
				prop == "CardTypes" || prop == "CardTypesPermanent" || prop == "Colors" ||
				prop == "CreatureType" || strings.HasPrefix(prop, "CardCounters."):
			case isExtremeProperty(prop):
			case differentPropertyKindOf(prop) != diffNone:
			default:
				// Not a recognised property (DifferentNames,
				// Different*, ...): keep the old whole-token spec read.
				spec, prop = arg, ""
			}
		}
		// CardTypes is Tarmogoyf's distinct-card-type form, not a filter:
		// count each real card type (CR 205.1) represented among the
		// selected cards once. The map is read only through len, so its
		// iteration order never reaches an event or a view. The two
		// siblings (task diffcount2) are the same distinct-set shape over a
		// narrower vocabulary: CreatureType counts distinct creature
		// subtypes (Valiant Changeling's per-type reduction),
		// CardTypesPermanent the six CR 205.2 permanent types (Korvold,
		// Gleeful Glutton's combat-damage trigger; Matzalantli's transform
		// gate, whose oracle names the six).
		var seenCardTypes map[string]bool
		if prop == "CardTypes" || prop == "CardTypesPermanent" {
			seenCardTypes = make(map[string]bool)
		}
		var seenCreatureTypes map[string]bool
		if prop == "CreatureType" {
			seenCreatureTypes = make(map[string]bool)
		}
		// The bare wasCastFromYourHand qualifier (task castprov3, Approach of
		// the Second Sun's Count$ValidStack Card.wasCastFromYourHand+Self):
		// not a filter predicate — split out per CANDIDATE object through the
		// Host's log read before the ordinary match (the ByYou family never
		// needed this here because it had no Valid* carrier; a ByYou spec
		// still routes to its own helper's absence and fails closed as
		// before, unchanged).
		hasBareHand := !strings.Contains(spec, "wasCastFromYourHandByYou") && strings.Contains(spec, "wasCastFromYourHand")
		// token$DifferentCardNames (Sandsteppe War Riders' "bolster X, where X
		// is the number of differently named artifact tokens you control";
		// also Gimbal Gremlin Prodigy, Audience with Trostani, Neriv Crackling
		// Vanguard -- 4 raw corpus lines): a SET-level qualifier the
		// per-object filter cannot express -- the count is the number of
		// DISTINCT face names among the matching tokens, not the number of
		// tokens. Stripped here and rewritten to the plain `token` predicate
		// for the per-object match; the distinctness is a seen-names set at
		// this count site (the CardTypes/Colors distinct-count precedent).
		// The filter's own read (matchPositive's token$DifferentCardNames
		// case) is the per-object half -- "is a token" -- so a non-count read
		// of the qualifier admits every matching token and narrows nothing.
		var seenTokenNames map[string]bool
		if strings.Contains(spec, "token$DifferentCardNames") {
			spec = strings.ReplaceAll(spec, "token$DifferentCardNames", "token")
			seenTokenNames = make(map[string]bool)
		}
		// An extreme property (Greatest*/Least*) folds a max/min over the
		// matches instead of a sum, so it needs its own accumulator plus a
		// seen flag -- zero matches must read 0, never an int-min/max
		// sentinel.
		extreme := isExtremeProperty(prop)
		// The fold scans candidates IN PLACE -- no materialised candidate
		// slice: Count$Valid is on the hottest condition path
		// (effects.CheckSVarHolds intervening-ifs, static gates, SVarCompare)
		// and the single-zone scan must stay allocation-free
		// (TestEvalCountValidZoneScanIsAllocationFree holds the line). ValidAll (6 corpus Count$ValidAll carriers: Cactus Preserve
		// and Tangleweave Armor's greatest-commander-mana-value, Kefka's
		// imprinted card, Mangara/Tomik's attacking-LKI count, You Will Know
		// True Suffering's commander mana value) extends the scan to EVERY
		// card zone -- countAllZones per seat plus the ONE stack pass --
		// because a commander sits in the command zone and an imprinted card
		// in exile; a battlefield-only scan can never see them. Each
		// candidate is matched against ITS OWN zone (the way Forge evaluates
		// a ValidAll spec against the card's actual zone), so a battlefield
		// candidate keeps the whole-spec MatchesObjectCtx read and a
		// command-zone or exile candidate the per-alternative in-zone read.
		// (ValidAll also occurs outside the count head -- `Defined$ ValidAll`
		// 3 files, `ImprintCards$ ValidAll` 2; 11 carrier files total -- and
		// those two paths are still unhandled: a `Defined$ ValidAll` spec
		// fails closed in effects/context.go, an `ImprintCards$ ValidAll`
		// imprint remembers nothing. Recorded in the report.)
		// specCtx is a LOCAL, never a struct field: storing the
		// SpecContext(...)-built value in the fold struct made escape
		// analysis summarise evalCountBody's *Ctx param as leaking (the
		// struct escapes through the pointer receiver), which heap-
		// allocated EVERY caller-built Ctx on the hot layer-walk path
		// (rules/layers.go's cdaSetPT Ctx) -- exactly the allocation class
		// rules' Derived pin holds the line on.
		// Built once here and passed to visit as a parameter instead.
		specCtx := c.SpecContext(c.Controller)
		f := zoneCountFold{h: h, g: g, spec: spec,
			prop: prop, extreme: extreme, isLeast: isLeastProperty(prop),
			hasBareHand: hasBareHand, readsPT: SpecReadsPT(spec),
			seenTokenNames: seenTokenNames, seenCardTypes: seenCardTypes,
			seenCreatureTypes: seenCreatureTypes}
		// The Different* distinct-set property family (task diffcount1):
		// DifferentCardManaCost / DifferentCardPower / DifferentCardNames /
		// DifferentColorPair count the DISTINCT values among the matching
		// cards, not the cards themselves. Numeric values fold into a set
		// keyed by the value; names into a string set. Both are read only
		// through len, so no map ordering ever reaches an event or a view.
		if dk := differentPropertyKindOf(prop); dk != diffNone {
			f.diffKind = dk
			if dk == diffName {
				f.seenDiffNames = make(map[string]bool)
			} else {
				f.seenDiffValues = make(map[int32]bool)
			}
		}
		if multi != nil {
			// A comma-joined zone list (Count$ValidGraveyard,Exile,
			// ValidBattlefield,Graveyard, ValidBattlefield,Command --
			// fuzz-cov3): the single-zone scan over each named zone in turn,
			// the shared stack once. Each candidate is matched against its
			// own zone, the ValidAll convention.
			for _, z := range multi {
				if z == state.ZStack {
					for _, id := range g.Stack {
						f.visit(id, z, specCtx)
					}
					continue
				}
				for _, p := range g.AliveFrom(0) {
					for _, id := range g.Zone(z, p) {
						f.visit(id, z, specCtx)
					}
				}
			}
		} else if isAll {
			for _, p := range g.AliveFrom(0) {
				for _, z := range countAllZones {
					for _, id := range g.Zone(z, p) {
						f.visit(id, z, specCtx)
					}
				}
			}
			for _, id := range g.Stack {
				f.visit(id, state.ZStack, specCtx)
			}
		} else {
			// The stack is ONE shared list (state.Game.Zone returns g.Stack
			// for every seat), so a single-zone stack scan must run exactly
			// once: without this guard an N-seat table counts every stack
			// object N times -- Mindbreak Trap's MaxTgts bound and Display
			// of Power's copy count both read on the caster's own spell(s).
			// Scanned under the first alive seat, the same convention
			// rules/statics.go and rules/trigger_match.go use for the
			// shared stack. The ValidAll branch above is exempt: its stack
			// pass sits outside the seat loop already.
			for si, p := range g.AliveFrom(0) {
				if zone == state.ZStack && si > 0 {
					continue
				}
				for _, id := range g.Zone(zone, p) {
					f.visit(id, zone, specCtx)
				}
			}
		}
		if extreme {
			// A matched set with no members has no extreme: 0, per the
			// seen guard, never an int-min/max sentinel.
			if !f.seen {
				return 0, true, true
			}
			return f.best, true, true
		}
		if prop == "CardTypes" || prop == "CardTypesPermanent" {
			return int32(len(seenCardTypes)), true, true
		}
		if prop == "CreatureType" {
			return int32(len(seenCreatureTypes)), true, true
		}
		if seenTokenNames != nil {
			return int32(len(seenTokenNames)), true, true
		}
		if prop == "Colors" {
			return int32(bits.OnesCount8(uint8(f.colorsSeen))), true, true
		}
		if f.diffKind != diffNone {
			if f.seenDiffNames != nil {
				return int32(len(f.seenDiffNames)), true, true
			}
			return int32(len(f.seenDiffValues)), true, true
		}
		return f.n, true, true
	}
	return 0, false, false
}
