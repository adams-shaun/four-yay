package chars

import (
	"slices"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// faceDownBasis is CR 708.5's synthetic printed face for a face-down
// battlefield permanent: a vanilla 2/2 creature. Only exported fields are
// read off it (PT pins the 2/2 base itself, since Face's
// parsed P/T is unexported); nothing writes to it.
var faceDownBasis = &cards.Face{Types: []string{"Creature"}}

// Types is the layer-4 walk (CR 613.1d): the object's type list after every
// applicable type-changing effect in act, applied before anything that tests
// a type. The accumulated types-so-far list passed into the shared effects
// filter is what lets a later effect select a creature made a Goblin by an
// earlier layer-4 effect rather than looking back at its printed face.
// atStack is the derivation's zone override for AffectedZone$ Stack grants;
// the zero value reads the object's live zone.
//
// act IS what the engine's active() returns -- the live registered effects
// plus the memoized static scan, layer/sub/timestamp sorted -- so a caller
// that has already proven no static can change a type (rules/layer4types.go's
// staticsMayChangeTypes precheck) may pass just the live registered LType
// effects and skip rebuilding the static memo; the result is identical by
// that proof, and rules' layer4PrecheckVerify compares every such fast build
// against the full walk.
func Types(b Board, act []state.ContinuousEffect, id state.ObjID, atStack state.Zone) []string {
	ty, _ := TypesAndAllCreatureTypes(b, act, id, atStack)
	return ty
}

// TypesAndAllCreatureTypes is the layer-4 walk and its semantic all-types
// marker. The marker is captured at the same point as the expansion so
// snapshots need not infer rules state from today's subtype vocabulary.
func TypesAndAllCreatureTypes(b Board, act []state.ContinuousEffect, id state.ObjID, atStack state.Zone) ([]string, bool) {
	o := b.Game().Obj(id)
	if o == nil || o.Face() == nil {
		return nil, false
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
	faceDown := o.FaceDown && o.Zone == state.ZBattlefield
	if faceDown {
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
		if act[i].Layer == state.LType {
			anyLType = true
			break
		}
	}
	if !anyLType {
		return impendingTypeSwitch(o, reconfigureTypeSwitch(o, bestowedTypeSwitch(o, base))), !faceDown && !o.ImpendingDormant() && o.Face().AllCreatureTypesCDA()
	}
	// Copy-on-write: the printed list is copied only once an effect actually
	// applies to this object (most objects are untouched by the layer-4
	// effects in play). Every modification below -- the in-place filters
	// and the appends -- runs on the owned copy, never on the face's array.
	ty := base
	owned := false
	allCreatureTypes := !faceDown && o.Face().AllCreatureTypesCDA()
	for i := range act {
		ce := &act[i]
		if ce.Layer != state.LType || !matchesWithTypes(b, ce, id, ty, atStack) {
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
			allCreatureTypes = false
			kept := ty[:0]
			for _, t := range ty {
				if IsSupertype(t) {
					kept = append(kept, t)
				}
			}
			ty = kept
		}
		if ce.RemoveCreatureTypes || ce.RemoveSubTypes || ce.SetCreatureTypes {
			// The semantic marker follows the same timestamp-ordered type
			// changes as the materialized list. A later strip invalidates an
			// earlier all-types grant; a subsequent AddAllCreatureTypes below
			// can establish it again.
			allCreatureTypes = false
			kept := ty[:0]
			for _, t := range ty {
				if ce.RemoveSubTypes {
					if IsCardType(t) || IsSupertype(t) {
						kept = append(kept, t)
					}
				} else if ce.SetCreatureTypes {
					if !effects.CreatureTypeWords(t) {
						kept = append(kept, t)
					}
				} else if !IsCreatureSubtype(t) {
					kept = append(kept, t)
				}
			}
			ty = kept
		}
		if len(ce.RemoveTypes) > 0 {
			if allCreatureTypes && slices.ContainsFunc(ce.RemoveTypes, func(remove string) bool {
				return slices.ContainsFunc(ty, func(typ string) bool {
					return effects.CreatureTypeWords(typ) && strings.EqualFold(typ, remove)
				})
			}) {
				allCreatureTypes = false
			}
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
		ty = appendLandTypes(ty, ce.AddTypes, b.LandTypeWords())
		if ce.AddAllCreatureTypes {
			ty = appendAllCreatureTypes(ty)
			allCreatureTypes = true
		}
	}
	if !owned && len(ty) == 0 {
		// The copy of an empty list was nil; keep that exact value.
		ty = nil
	}
	if o.ImpendingDormant() {
		// The impending switch removes every creature subtype along with
		// Creature, so it also invalidates the semantic all-types marker.
		allCreatureTypes = false
	}
	return impendingTypeSwitch(o, reconfigureTypeSwitch(o, bestowedTypeSwitch(o, ty))), allCreatureTypes
}

// landTypeWordsCache memoises CorpusLandTypeWords per universe, keyed by the
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

// CorpusLandTypeWords derives the land-subtype vocabulary from the parsed
// compiled corpus supplied as the game's NameUniverse. Card and supertype
// words, plus creature subtypes printed on creature lands, are excluded.
// Sorting makes the derived layer list deterministic.
func CorpusLandTypeWords(universe []*cards.Card) []string {
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
				if strings.EqualFold(typ, "Land") || IsSupertype(typ) || IsCardType(typ) || effects.CreatureTypeWords(typ) {
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

func IsCardType(t string) bool {
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
	return withoutCreature(types, false)
}

// withoutCreature drops Creature from a derived type list and, with
// subtypes, every creature subtype too; other card types, their subtypes and
// the supertypes remain. The shared strip behind the reconfigure and
// impending switches.
func withoutCreature(types []string, subtypes bool) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		if t == "Creature" || (subtypes && effects.CreatureTypeWords(t)) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// impendingTypeSwitch applies CR 702.176a's switch to a DERIVED type list: a
// permanent cast for its impending cost is not a creature while it has a time
// counter, so its creature subtypes are removed along with Creature. Subtypes
// of retained card types (e.g. Equipment on an Artifact) remain. The positive
// creature-subtype vocabulary avoids treating every non-card-type word as a
// creature subtype (Saga, Forest, etc.). Other card types and supertypes remain.
// An object not impending-dormant -- printed without Impending, cast for its
// plain mana cost, or with its last time
// counter removed -- keeps the list unchanged, returning the SAME slice so the
// common game stays byte-identical and allocation-free. The dormancy is
// derived live (state.Object.ImpendingDormant), so every replay derives the
// switch identically.
func impendingTypeSwitch(o *state.Object, types []string) []string {
	if !o.ImpendingDormant() {
		return types
	}
	return withoutCreature(types, true)
}

// cardTypeWords are the card types; supertypeWords the supertypes. Every
// other type word on a face is a subtype, so RemoveCreatureTypes' strip is
// "drop what is neither" -- the same split Forge's own type vocabulary makes.
var (
	cardTypeWords  = []string{"Artifact", "Battle", "Creature", "Enchantment", "Instant", "Land", "Planeswalker", "Sorcery", "Tribal"}
	supertypeWords = []string{"Basic", "Legendary", "Ongoing", "Snow", "World"}
)

// IsSupertype reports whether t is a supertype word (the only thing a
// RemoveCardTypes strip keeps: card types and their subtypes go).
func IsSupertype(t string) bool {
	for _, w := range supertypeWords {
		if strings.EqualFold(w, t) {
			return true
		}
	}
	return false
}

// IsCreatureSubtype reports whether t is a subtype word (a creature type
// under RemoveCreatureTypes' reading): not a card type and not a supertype.
func IsCreatureSubtype(t string) bool {
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

// SupertypeWords is the supertype vocabulary IsSupertype tests against. The
// slice is shared: callers range it and never write to it.
func SupertypeWords() []string { return supertypeWords }

// matchesWithTypes is matchesWithChars with no keyword list bound: the
// layer-4 walk's own applicability test.
func matchesWithTypes(b Board, ce *state.ContinuousEffect, id state.ObjID, types []string, atStack state.Zone) bool {
	return matchesWithChars(b, ce, id, types, nil, atStack)
}

// matchesWithChars is the engine's Affected$ match (Board.Matches) with the
// walk's types-so-far and keywords-so-far lists bound and no in-progress
// layer-7 value.
func matchesWithChars(b Board, ce *state.ContinuousEffect, id state.ObjID, types, keywords []string, atStack state.Zone) bool {
	return b.Matches(ce, id, types, keywords, atStack, PTBind{})
}
