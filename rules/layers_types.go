package rules

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// faceDownBasis is CR 708.5's synthetic printed face for a face-down
// battlefield permanent: a vanilla 2/2 creature. Only exported fields are
// read off it (derivedScalarFrom pins the 2/2 base itself, since Face's
// parsed P/T is unexported); nothing writes to it.
var faceDownBasis = &cards.Face{Types: []string{"Creature"}}

// faceDownPrintedHides is the one CR 708.8 gate every printed-face scan
// shares: while a battlefield object is face down, its printed abilities,
// triggers and statics do not exist. The ability-offer loop, the
// mana-ability collector, the trigger scan and both static scans all
// consult it, so no printed face of a manifested card can leak into any
// offer or queue while it is face down.
func (e *Engine) faceDownPrintedHides(o *state.Object) bool {
	return o != nil && o.FaceDown && o.Zone == state.ZBattlefield
}

// typeCharacteristics applies layer 4 before anything that tests a type. The
// accumulated types-so-far list passed into the shared effects filter is what
// lets a later effect select a creature made a Goblin by an earlier layer-4
// effect rather than looking back at its printed face. atStack is
// derivedWith's zone override for AffectedZone$ Stack grants; the zero value
// reads the object's live zone.
func (e *Engine) typeCharacteristics(id state.ObjID, atStack state.Zone) []string {
	return e.typeCharacteristicsActive(e.active(), id, atStack)
}

// typeCharacteristicsActive is typeCharacteristics with the layer walk's own
// active()-list supplied by the caller. The list IS what active() returns --
// the live registered effects plus the memoized static scan, layer/sub/
// timestamp sorted -- so a caller that has already proven no static can change
// a type (layer4types.go's staticsMayChangeTypes precheck) may pass just the
// live registered LType effects and skip rebuilding the static memo; the
// result is identical by that proof, and layer4PrecheckVerify compares every
// such fast build against the typeCharacteristics full walk.
func (e *Engine) typeCharacteristicsActive(act []ContinuousEffect, id state.ObjID, atStack state.Zone) []string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	// CR 708.5: a face-down battlefield permanent's type set is exactly
	// {Creature} -- its printed types do not exist while it is face down
	// (even a manifested land) -- unless a ChangeZone FaceDownSetType$
	// replaced the set (Yedora's face-down Forest). That set is the BASE the
	// layer-4 walk below then modifies like any other type set (CR 613.1c):
	// a Maskwood Nexus granting every creature type reaches a manifested
	// 2/2 exactly as it reaches a face-up creature, while the printed face
	// stays hidden (no printed word reappears merely from a type grant).
	base := o.Face().Types
	if o.FaceDown && o.Zone == state.ZBattlefield {
		base = o.FaceDownTypeWords()
	}
	if o.CopyNonLegendary {
		// CopySpellAbility NonLegendary$ True (The Sixth Doctor and the
		// corpus's six-carrier family): the copy's characteristic set has
		// the Legendary supertype removed. It is a layer-4 base strip in
		// exactly the sense CopyPermanent's RemoveLegendary$ is, and it
		// must reach the DERIVED list -- not just the printed face --
		// because CR 704.5j's legendGroups reads the derived types. An
		// instant/sorcery copy never reaches the battlefield, so this is
		// harmless there.
		stripped := make([]string, 0, len(base))
		for _, t := range base {
			if !strings.EqualFold(t, "Legendary") {
				stripped = append(stripped, t)
			}
		}
		base = stripped
	}
	zone := o.Zone
	if atStack != 0 {
		zone = atStack
	}
	// Fast path: with no layer-4 effect active anywhere the derived list IS
	// the printed list. Returning the face slice directly (every caller only
	// reads it) keeps the common game -- no Animate/type-granting static in
	// play -- allocation-free; the legal-actions pass reaches here through
	// HasKeyword's Derived read, and the cost/action-statics hotspot pins
	// measure that pass.
	anyLType := false
	for i := range act {
		if act[i].Layer == LType {
			anyLType = true
			break
		}
	}
	if !anyLType {
		return reconfigureTypeSwitch(o, bestowedTypeSwitch(o, base))
	}
	// Copy-on-write: the printed list is copied only once an effect actually
	// applies to this object (most objects are untouched by the layer-4
	// effects in play). Every modification below -- the in-place filters
	// and the appends -- runs on the owned copy, never on the face's array.
	ty := base
	owned := false
	for i := range act {
		ce := &act[i]
		if ce.Layer != LType || !e.matchesWithTypes(ce, id, ty, atStack) {
			continue
		}
		if ce.AffectedZone != "" && !ce.MayPlay {
			if zones, all, ok := effects.ParseZones(ce.AffectedZone); !ok || (!all && !slices.Contains(zones, zone)) {
				continue
			}
		}
		if !owned {
			ty = append([]string(nil), ty...)
			owned = true
		}
		if ce.RemoveCardTypes {
			// RemoveCardTypes$ keeps only the SUPERTYPES: a subtype is tied to
			// its card type (CR 205.2-family), so losing the card type loses
			// its subtypes, and the flat type list cannot attribute a subtype
			// word to a surviving type. Both flags together are therefore
			// "everything but supertypes" -- Darksteel Mutation's oracle.
			kept := ty[:0]
			for _, t := range ty {
				if isSupertype(t) {
					kept = append(kept, t)
				}
			}
			ty = kept
		}
		if ce.RemoveCreatureTypes || ce.RemoveSubTypes || ce.SetCreatureTypes {
			kept := ty[:0]
			for _, t := range ty {
				if ce.RemoveSubTypes {
					if isCardType(t) || isSupertype(t) {
						kept = append(kept, t)
					}
				} else if ce.SetCreatureTypes {
					if !effects.CreatureTypeWords(t) {
						kept = append(kept, t)
					}
				} else if !isCreatureSubtype(t) {
					kept = append(kept, t)
				}
			}
			ty = kept
		}
		if len(ce.RemoveTypes) > 0 {
			kept := ty[:0]
			for _, t := range ty {
				if !slices.ContainsFunc(ce.RemoveTypes, func(remove string) bool { return strings.EqualFold(t, remove) }) {
					kept = append(kept, t)
				}
			}
			ty = kept
		}
		if ce.RemoveLegendary {
			// NonLegendary$ True (CR 205.4's supertype): drop only the
			// Legendary word, leaving every other supertype (Basic, Snow,
			// World, Ongoing) in place -- distinct from RemoveCardTypes,
			// which keeps supertypes and drops everything else.
			kept := ty[:0]
			for _, t := range ty {
				if !strings.EqualFold(t, "Legendary") {
					kept = append(kept, t)
				}
			}
			ty = kept
		}
		ty = appendLandTypes(ty, ce.AddTypes, e.landTypeWords)
		if ce.AddAllCreatureTypes {
			ty = appendAllCreatureTypes(ty)
		}
	}
	if !owned && len(ty) == 0 {
		// The copy of an empty list was nil; keep that exact value.
		ty = nil
	}
	return reconfigureTypeSwitch(o, bestowedTypeSwitch(o, ty))
}

// landTypeWordsCache memoises corpusLandTypeWords per universe, keyed by the
// universe slice's identity (first element address + length). The universe
// is immutable by contract (state.Game.NameUniverse) and shared by every game
// an embedder starts from one registry, so the ~24k-card walk runs once per
// registry instead of once per game. The cached list is shared read-only:
// its one reader, appendLandTypes, only appends it into another slice.
// Bounded: dropped wholesale on overflow, which only costs a recomputation.
var landTypeWordsCache struct {
	mu sync.Mutex
	m  map[landTypeWordsKey][]string
}

type landTypeWordsKey struct {
	first **cards.Card
	n     int
}

// corpusLandTypeWords derives the land-subtype vocabulary from the parsed
// compiled corpus supplied as the game's NameUniverse. Card and supertype
// words, plus creature subtypes printed on creature lands, are excluded.
// Sorting makes the derived layer list deterministic.
func corpusLandTypeWords(universe []*cards.Card) []string {
	if len(universe) == 0 {
		return buildCorpusLandTypeWords(universe)
	}
	k := landTypeWordsKey{first: &universe[0], n: len(universe)}
	landTypeWordsCache.mu.Lock()
	if out, ok := landTypeWordsCache.m[k]; ok {
		landTypeWordsCache.mu.Unlock()
		return out
	}
	landTypeWordsCache.mu.Unlock()
	out := buildCorpusLandTypeWords(universe)
	out = out[:len(out):len(out)]
	landTypeWordsCache.mu.Lock()
	defer landTypeWordsCache.mu.Unlock()
	if prev, ok := landTypeWordsCache.m[k]; ok {
		return prev
	}
	if len(landTypeWordsCache.m) >= 64 {
		landTypeWordsCache.m = nil
	}
	if landTypeWordsCache.m == nil {
		landTypeWordsCache.m = make(map[landTypeWordsKey][]string)
	}
	landTypeWordsCache.m[k] = out
	return out
}

func buildCorpusLandTypeWords(universe []*cards.Card) []string {
	words := make(map[string]struct{})
	for _, card := range universe {
		if card == nil {
			continue
		}
		for _, face := range card.Faces {
			if face == nil || !slices.ContainsFunc(face.Types, func(t string) bool { return strings.EqualFold(t, "Land") }) {
				continue
			}
			for _, typ := range face.Types {
				if strings.EqualFold(typ, "Land") || isSupertype(typ) || isCardType(typ) || effects.CreatureTypeWords(typ) {
					continue
				}
				words[typ] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(words))
	for word := range words {
		out = append(out, word)
	}
	slices.Sort(out)
	return out
}

func isCardType(t string) bool {
	for _, word := range cardTypeWords {
		if strings.EqualFold(t, word) {
			return true
		}
	}
	return false
}

// appendLandTypes expands Forge's all-land-type tokens against the parsed
// corpus vocabulary. Additive types remain in their original order; the
// vocabulary itself is sorted to keep derived characteristics deterministic.
// A word the list already carries is not appended again (CR 205.1b: an
// object has a type or it does not; CR 613.1d's "in addition to its other
// types" adds only what is missing) -- Puppet Crafting's AddType$ Creature
// over an artifact creature derived "Creature" twice before this. Every
// layer-4 type grant, printed static or effect-registered (api:Animate's
// Types$), funnels through here, so the set semantics hold for all of them.
func appendLandTypes(types, grants, nonBasic []string) []string {
	for _, grant := range grants {
		switch {
		case strings.EqualFold(grant, "AllBasicLandType"):
			for _, w := range [...]string{"Plains", "Island", "Swamp", "Mountain", "Forest"} {
				types = appendTypeOnce(types, w)
			}
		case strings.EqualFold(grant, "AllNonBasicLandType"):
			for _, w := range nonBasic {
				types = appendTypeOnce(types, w)
			}
		default:
			types = appendTypeOnce(types, grant)
		}
	}
	return types
}

// appendTypeOnce appends word unless types already carries it
// (case-insensitively, the comparison every type reader uses).
func appendTypeOnce(types []string, word string) []string {
	for _, t := range types {
		if strings.EqualFold(t, word) {
			return types
		}
	}
	return append(types, word)
}

// appendAllCreatureTypes materialises the layer-4 "all creature types"
// grant (CR 613.1c alongside AddTypes) into the walk's type list: every
// creature-subtype word the shared CreatureTypeWords vocabulary knows, in
// sorted (deterministic) order. A word the printed face or an earlier effect
// already carries is skipped (CR 205.1b, the appendTypeOnce rule): the
// vocabulary itself has no repeats, so only the creature subtypes present
// BEFORE this grant need checking, and those are gathered once up front --
// the common case (a printed Thopter, a granted Elf) compares each
// vocabulary word against one or two words, not the whole growing list.
// The effects filter's type predicates (hasTypeCtx) answer every
// creature-subtype predicate and base from this list through ExtraTypes,
// exactly as Changeling's intrinsic CDA is answered through hasType.
func appendAllCreatureTypes(types []string) []string {
	var buf [8]string
	had := buf[:0]
	for _, t := range types {
		if effects.CreatureTypeWords(t) {
			had = append(had, t)
		}
	}
	for _, w := range effects.CreatureTypeWordList() {
		if len(had) > 0 && slices.ContainsFunc(had, func(t string) bool { return strings.EqualFold(t, w) }) {
			continue
		}
		types = append(types, w)
	}
	return types
}

// bestowedTypeSwitch applies CR 702.114c/e's type switch to a DERIVED type
// list: a bestowed card is an Aura, not a creature -- while attached to a
// creature (CR 702.114e, state.Object.BestowedAttached) AND while it is a
// bestowed spell on the stack (CR 702.114c, state.Object.BestowedAuraSpell).
// In both cases the printed "Enchantment Creature" pair loses its Creature
// half and gains Aura -- and an object that is neither keeps the list
// unchanged, returning the SAME slice so the common game stays byte-identical
// and allocation-free. Derived live state, never a stored marker, so every
// replay derives the switch identically. Creature SUBTYPES deliberately stay:
// the subtype words are inert on an Aura in every filter this engine
// evaluates (no Aura filter reads "Archon"), and stripping them would widen
// the diff into every subtype-affected static.
func bestowedTypeSwitch(o *state.Object, types []string) []string {
	if !o.BestowedAttached() && !o.BestowedAuraSpell() {
		return types
	}
	out := make([]string, 0, len(types)+1)
	for _, t := range types {
		if t == "Creature" {
			continue
		}
		out = append(out, t)
	}
	return append(out, "Aura")
}

// reconfigureTypeSwitch applies CR 702.150c's switch to a DERIVED type
// list: a Reconfigure card attached to a creature is not a creature -- the
// printed "Artifact Creature Equipment <subtype>" list loses only its
// Creature half and keeps Equipment/Artifact and the subtypes (the same
// deliberate keep-subtypes narrowing bestowedTypeSwitch practises: the
// subtype words are inert on a non-creature in every filter this engine
// evaluates, and stripping them would widen the diff into every
// subtype-affected static). An unattached reconfigure card (or anything
// not printed with the keyword) keeps the list unchanged, returning the
// SAME slice so the common game stays byte-identical and allocation-free.
// A face-down battlefield permanent keeps its CR 708.5 set: its printed
// face (and with it the Reconfigure keyword the switch keys on) does not
// exist while face down, so the switch must not strip Creature from a
// manifested reconfigure card's vanilla 2/2.
func reconfigureTypeSwitch(o *state.Object, types []string) []string {
	if !o.ReconfiguredAttached() || (o.FaceDown && o.Zone == state.ZBattlefield) {
		return types
	}
	out := make([]string, 0, len(types))
	for _, t := range types {
		if t == "Creature" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (e *Engine) matchesWithTypes(ce *ContinuousEffect, id state.ObjID, types []string, atStack state.Zone) bool {
	return e.matchesWithChars(ce, id, types, nil, atStack)
}

// matchesWithChars is matchesWithTypes with the walk's KEYWORDS-so-far list
// bound as well. It is the one seam a `with<Keyword>`/`without<Keyword>`
// predicate in an `Affected$` spec is answered through, for the same reason
// ExtraTypes exists: the effects filter's keyword predicates read the object
// alone (printed face plus marker counters) and cannot see a layer-6
// AddKeyword$ grant, so a lord that selects on a granted keyword would never
// match. Cavalry Master's `Creature.Other+withFlanking+YouCtrl` over a
// Sidewinder Sliver whose own static granted the flanking is the measured
// case (CR 702.25b: each instance triggers separately).
//
// The list is keywords-SO-FAR in the walk's own layer/timestamp order, which
// is the same reading ExtraTypes gives: a grant whose effect is applied
// earlier is visible, a later one is not. Within layer 6 the sequence itself
// is CR 613.6 dependency order (abilityDependencyOrder below), so a lord
// whose gate reads a keyword another layer-6 effect grants is applied after
// that grant regardless of timestamps -- the Cavalry Master-over-a-Sidewinder
// Sliver case. The layer-7 P/T walk binds the SAME finished list (CR 613
// orders layer 6 strictly before layer 7, so the walk's final keyword list
// is what a layer-7 applicability gate reads): Windstorm Drake's
// `Creature.withFlying+Other+YouCtrl` +1/+0 over a creature an earlier
// layer-6 effect granted flying is the measured case.
func (e *Engine) matchesWithChars(ce *ContinuousEffect, id state.ObjID, types, keywords []string, atStack state.Zone) bool {
	return e.matchesWithCharsPT(ce, id, types, keywords, atStack, 0, 0, 0, 0, false)
}

// matchesWithCharsPT binds the layer-7 walk's in-progress P/T values when an
// Affected$ predicate is evaluated during that walk. Calling Derived here
// would recurse through the same active layer scan.
func (e *Engine) matchesWithCharsPT(ce *ContinuousEffect, id state.ObjID, types, keywords []string, atStack state.Zone, power, toughness, basePower, baseToughness int32, hasPT bool) bool {
	// CR 702.25b: a phased-out permanent is treated as though it does not
	// exist, so NO continuous effect applies to it -- a lord's pump, a
	// keyword grant, a type change. This is the one applicability gate every
	// layer walk goes through, so the exclusion cannot be missed by a layer
	// the way a per-layer check could. PhasedOut is only ever true on a
	// battlefield permanent.
	if o := e.G.Obj(id); o != nil && o.PhasedOut {
		return false
	}
	// A spec whose Affects is exactly Card.Self names only the effect's own
	// source (effects' "Self" predicate is o.ID == src), so a different id can
	// never match it -- no later gate can rescue the match. Rejecting here,
	// before the cast-provenance gate and specCtx/MatchesSpecCtx, keeps a
	// board with several self-only type effects from paying the full match
	// per candidate: Clown Car's four crewed self type effects against tens
	// of thousands of goblin tokens was the measured case (seed
	// 6181111140895991800). This is a pure early-out -- identical result -- and
	// it lives on the one seam every layer's match shares, so a layer-6/7
	// self-only effect is covered too.
	if ce.Affects == "Card.Self" && id != ce.Source {
		if layer4PrecheckVerify {
			selfRejectVerify.Add(1)
			// Recompute through the full match and confirm the early-out
			// agreed: a non-source Card.Self must never match. This is the
			// empirical proof the shortcut is result-preserving.
			if e.matchesWithCharsPTSlow(ce, id, types, keywords, atStack, power, toughness, basePower, baseToughness, hasPT) {
				panic(fmt.Sprintf("rules: Card.Self early reject for id %d != source %d but the full match admitted it", id, ce.Source))
			}
		}
		return false
	}
	return e.matchesWithCharsPTSlow(ce, id, types, keywords, atStack, power, toughness, basePower, baseToughness, hasPT)
}

// selfRejectVerify counts the Card.Self early rejections the shortcut made
// under verify mode; a test asserts it advances so the shortcut cannot be
// silently removed. It is written only under the layer4PrecheckVerify branch,
// so production pays one predictable branch and no store. It is atomic
// because the test binary turns verify mode on and runs engines on parallel
// test goroutines (TestInvariantsUnderSeedFuzz's seed subtests), which all
// bump this one counter.
var selfRejectVerify atomic.Int64

// matchesWithCharsPTSlow is matchesWithCharsPT with the Card.Self early
// rejection removed. Verify mode calls it to prove the shortcut agrees with
// the full match; production never does.
func (e *Engine) matchesWithCharsPTSlow(ce *ContinuousEffect, id state.ObjID, types, keywords []string, atStack state.Zone, power, toughness, basePower, baseToughness int32, hasPT bool) bool {
	// The cast-provenance qualifiers (castprov1/2/3 — the_twelfth_doctor's
	// `Affected$ Card.YouCtrl+!wasCastFromYourHand`, quandrix_the_proof's
	// `Instant.wasCastByYou+wasCastFromYourHand`) are split out before the
	// filter match, through the combined entry point; its one-probe
	// specProvenanceGate is the early-out, so every Affected$ spec
	// without the tokens costs one cached lookup on this shared hot path.
	affects, ok := e.castProvenanceAdmitsWindow(ce.Affects, id, ce.Controller, atStack != 0)
	if !ok {
		return false
	}
	// ExtraTypes is the walk's types-so-far list for THIS object: a later
	// layer-4 effect selects a creature an earlier one made a Goblin, and a
	// layer-7 lord's Affected$ sees the derived type. A value slice, not a
	// callable, keeps the context stack-allocated on this hot path.
	sc := e.specCtx(ce.Source, ce.Controller)
	sc.AsStack = atStack != 0
	sc.ExtraTypes = types
	sc.ExtraKeywords = keywords
	if hasPT {
		sc.DerivedPower, sc.DerivedToughness, sc.HasDerivedPT = power, toughness, true
		sc.BasePower, sc.BaseToughness, sc.HasBasePT = basePower, baseToughness, true
	}
	// The walk's types-so-far list above is authoritative for this match, so
	// the published layer-4 table (layer4types.go's DerivedTypes, bound by
	// specCtx) must not be consulted as a fallback: it may carry a type a
	// LATER effect grants, which would break the walk's own layer/timestamp
	// ordering. This is the same deferral the PredicatePrograms clear below
	// practises, and it leaves the walk's printed-face/Changeling fallback
	// (hasTypeCtx -> hasType) exactly as it was.
	sc.DerivedTypes = nil
	// An Effect-delivered grant's Affected$ spec may name the objects the
	// Effect remembered (`Affected$ Permanent.IsRemembered`, energybending's
	// "lands you control gain all basic land types"). The restriction walk
	// (restrictionApplies) already binds the registered set; the layer walk
	// must too, or such an Affected$ would match nobody. Printed statics carry
	// no Remembered, so this is a no-op for them.
	for _, r := range ce.Remembered {
		sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
	}
	// The compiled predicate sidecar is ExtraTypes-aware: its type predicates
	// are answered through hasTypeCtx (effects/compiled_predicate.go), the same
	// helper the textual oracle uses, so it sees this bind's types-so-far list
	// exactly as the text path does. It therefore stays installed -- clearing it
	// would discard the immutable optimization across the whole derived walk.
	// A face-down candidate's colour-bearing programs still take the textual
	// fallback inside evaluate (CR 708.5), so no new printed-colour claim is
	// introduced for a context whose characteristics do not exist.
	return effects.MatchesSpecCtxPtr(e.G, affects, id, &sc)
}

// cardTypeWords are the card types; supertypeWords the supertypes. Every
// other type word on a face is a subtype, so RemoveCreatureTypes' strip is
// "drop what is neither" -- the same split Forge's own type vocabulary makes.
var (
	cardTypeWords  = []string{"Artifact", "Battle", "Creature", "Enchantment", "Instant", "Land", "Planeswalker", "Sorcery", "Tribal"}
	supertypeWords = []string{"Basic", "Legendary", "Ongoing", "Snow", "World"}
)

// isSupertype reports whether t is a supertype word (the only thing a
// RemoveCardTypes strip keeps: card types and their subtypes go).
func isSupertype(t string) bool {
	for _, w := range supertypeWords {
		if strings.EqualFold(w, t) {
			return true
		}
	}
	return false
}

// isCreatureSubtype reports whether t is a subtype word (a creature type
// under RemoveCreatureTypes' reading): not a card type and not a supertype.
func isCreatureSubtype(t string) bool {
	for _, w := range cardTypeWords {
		if strings.EqualFold(w, t) {
			return false
		}
	}
	for _, w := range supertypeWords {
		if strings.EqualFold(w, t) {
			return false
		}
	}
	return true
}

// containsKeywordHead reports whether the keyword k matches any name in
// names by keyword HEAD (cards.KeywordHead strips a parameter tail), so
// RemoveKeywords$ Protection removes a printed "Protection:..." grant and
// RemoveKeywords$ Soulbond removes the bare keyword.
func containsKeywordHead(names []string, k string) bool {
	head := cards.KeywordHead(k)
	for _, n := range names {
		if strings.EqualFold(cards.KeywordHead(n), head) {
			return true
		}
	}
	return false
}
