package effects

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// This file is the ONE home for the option lists a ChooseType Type$ category
// ranges over (task ct1). Before it, only "Creature" (and an absent Type$)
// had an option builder, so a resolution-time or as-enters ChooseType for a
// non-creature category emitted a loud Note and the nonsensical creature-type
// fallback. Here each enumerable category names its real, rules-defined list;
// the asking primitive (effects/choose.go) and the as-enters ask
// (rules/cast.go etbOptions) both build their options from these functions, so
// the two asks and the no-ask fallback can never disagree about what a
// category ranges over.
//
// Every list is static and sorted, or (for Shared and CreatureInTargetedDeck
// and the owner-scoped Creature list) derived from immutable game state in
// object/zone order and then sorted -- never from a map range -- so a choice
// is deterministic and replayable.

// chooseBasicLandTypes is CR 205.3i's basic land type list -- the option list
// a Type$ Basic Land choose ranges over (Convincing Mirage, Realmwright,
// Dream Thrush, Thran Portal). Wastes is deliberately absent: it is a basic
// land with NO basic land type, so it is never a legal "basic land type".
var chooseBasicLandTypes = []string{"Forest", "Island", "Mountain", "Plains", "Swamp"}

// chooseNonbasicLandTypes is the land-type vocabulary outside the five basic
// land types (CR 205.3i, including later Omenpath, Planet and Town): the
// option list a Type$ Nonbasic Land choose ranges over (March from Velis Vel).
// Keep this list shared with the hasANonBasicLandType filter predicate.
var chooseNonbasicLandTypes = []string{
	"Cave", "Desert", "Gate", "Lair", "Locus", "Mine", "Omenpath",
	"Planet", "Power-Plant", "Sphere", "Tower", "Town", "Urza's",
}

// chooseLandTypes is every land subtype -- the option list a Type$ Land choose
// ranges over (Vision Charm, Barbarian Guides, Illusionary Presence). It is
// the union of the basic and nonbasic lists, built once from those slices so
// the three can never drift, and already sorted (basic < nonbasic across the
// concatenation is not guaranteed, so sort explicitly).
var chooseLandTypes = func() []string {
	out := make([]string, 0, len(chooseBasicLandTypes)+len(chooseNonbasicLandTypes))
	out = append(out, chooseBasicLandTypes...)
	out = append(out, chooseNonbasicLandTypes...)
	sort.Strings(out)
	return out
}()

// chooseCardTypes is the CR 205.1 card-type vocabulary a Type$ Card choose
// ranges over, restricted to the types a game can actually contain (Blood
// Oath and Fertile Imagination list exactly these nine in their own reminder
// text). Kindred is the current spelling of the retired Tribal; the corpus
// prints Kindred (78 files), so offering Tribal would offer a word no card
// carries.
var chooseCardTypes = []string{
	"Artifact", "Battle", "Creature", "Enchantment", "Instant",
	"Kindred", "Land", "Planeswalker", "Sorcery",
}

// choosePlaneswalkerTypes is the planeswalker-subtype vocabulary a Type$
// Planeswalker choose ranges over (Deification, Leori Sparktouched Hunter).
// Derived from the committed corpus's own planeswalker type lines and checked
// as a sorted set: it includes the Un-set and Universes-Beyond subtypes
// (B.O.B., Duck, Monopoly, You, Dungeon Master's "Dungeon"/"Master") because
// those are real planeswalker subtypes the corpus prints.
var choosePlaneswalkerTypes = []string{
	"Ajani", "Aminatou", "Angrath", "Arlinn", "Arzakon", "Ashiok", "B.O.B.",
	"Bahamut", "Basri", "Bolas", "Calix", "Chandra", "Comet", "Dack",
	"Dakkon", "Daretti", "Davriel", "Deb", "Dellian", "Dihada", "Domri",
	"Dovin", "Duck", "Dungeon", "Dyfed", "Ellywick", "Elminster", "Elspeth",
	"Equipment", "Ersta", "Estrid", "Feroz", "Freyalise", "Garruk", "Gideon",
	"Greensleeves", "Grist", "Guff", "Huatli", "Inzerva", "Jace", "Jared",
	"Jaya", "Jeska", "Kaito", "Karn", "Kasmina", "Kaya", "Kiora", "Koth",
	"Liliana", "Lolth", "Lukka", "Master", "Minsc", "Monopoly",
	"Mordenkainen", "Nahiri", "Narset", "Niko", "Nissa", "Nixilis", "Oko",
	"Quintorius", "Ral", "Rowan", "Saheeli", "Samut", "Sarkhan", "Serra",
	"Sifa", "Sivitri", "Sorin", "Stone", "Szat", "Tamiyo", "Tasha", "Teferi",
	"Teyo", "Tezzeret", "Thomil", "Tibalt", "Tyvar", "Ugin", "Urza",
	"Venser", "Vivien", "Vraska", "Vronos", "Will", "Windgrace", "Worzel",
	"Wrenn", "Xenagos", "Yanggu", "Yanling", "You", "Zariel",
}

// TypeChoiceLabels returns the deterministic, sorted label list a ChooseType
// Type$ category ranges over for the categories whose list does not depend on
// the resolving effect's own context: Basic Land, Land, Nonbasic Land, Card
// and Planeswalker. It returns nil for every other category -- Creature (an
// owner-scoped list rules builds), Shared and CreatureInTargetedDeck (both
// context-scoped; see SharedTypeLabels and CreatureInTargetedDeckLabels), and
// any category this build still cannot name. A nil result tells the asking
// caller to take its documented fallback, so a list is never silently empty.
//
// validTypes/invalidTypes carry the SA's comma-separated category filters.
// They are applied to the category vocabulary before it is offered, including
// Basic Land's Roots of Life exclusion list. Card additionally expands the
// corpus's Nonland pseudo-type over the card-type vocabulary.
func TypeChoiceLabels(category, validTypes, invalidTypes string) []string {
	var vocabulary []string
	switch typeChoiceLabelsCodes.Code(string(strings.ToLower(strings.TrimSpace(category)))) {
	case typeChoiceLabelsBasicLand:
		vocabulary = chooseBasicLandTypes
	case typeChoiceLabelsLand:
		vocabulary = chooseLandTypes
	case typeChoiceLabelsNonbasicLand:
		vocabulary = chooseNonbasicLandTypes
	case typeChoiceLabelsCard:
		vocabulary = chooseCardTypes
	case typeChoiceLabelsPlaneswalker:
		vocabulary = choosePlaneswalkerTypes
	default:
		return nil
	}
	return filterTypeLabels(vocabulary, validTypes, invalidTypes, strings.EqualFold(strings.TrimSpace(category), "card"))
}

// filterTypeLabels applies ValidTypes$/InvalidTypes$ to a category's
// vocabulary. An empty ValidTypes$ means the whole vocabulary. For Card only,
// the corpus's Land,Nonland form expands Nonland to every card type except
// Land. Unknown-only ValidTypes$ and filters removing every value fail closed
// with nil, allowing callers to take their documented no-list fallback rather
// than offer illegal choices.
func filterTypeLabels(vocabulary []string, validTypes, invalidTypes string, cardTypes bool) []string {
	var allowed []string
	if strings.TrimSpace(validTypes) == "" {
		allowed = append(allowed, vocabulary...)
	} else {
		for _, v := range splitTypeList(validTypes) {
			if cardTypes && strings.EqualFold(v, "Nonland") {
				for _, c := range vocabulary {
					if !strings.EqualFold(c, "Land") {
						allowed = append(allowed, c)
					}
				}
				continue
			}
			for _, c := range vocabulary {
				if strings.EqualFold(c, v) {
					allowed = append(allowed, c)
				}
			}
		}
	}
	invalid := map[string]bool{}
	for _, v := range splitTypeList(invalidTypes) {
		invalid[strings.ToLower(v)] = true
	}
	out := make([]string, 0, len(allowed))
	seen := map[string]bool{}
	for _, c := range allowed {
		key := strings.ToLower(c)
		if invalid[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// splitTypeList splits a comma-separated Forge type list, trimming each
// entry and dropping empties, preserving order.
func splitTypeList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SharedTypeLabels returns the sorted CARD types shared by every object
// exiled with source -- the option list a Type$ Shared | TypesFromDefined$
// ExiledWith choose ranges over (Eye of Ojer Taq's Apex Observatory: "choose
// a card type shared among two exiled cards used to craft it"). The craft
// cost guarantees two exiled objects that already share a type, and the
// restriction is on a CARD type (withSharedCardType), so the intersection is
// restricted to the card-type vocabulary -- a shared supertype like Legendary
// is never offered. A source with fewer than two exiled objects, or an empty
// card-type intersection, yields nil and the caller falls back. The returned
// words are the objects' printed Types tokens (the same tokens hasType
// matches), so a chosen label resolves through Card.ChosenType.
func SharedTypeLabels(g *state.Game, source state.ObjID) []string {
	if g == nil || source == 0 {
		return nil
	}
	var shared map[string]bool
	n := 0
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.ExiledWith != source || o.Face() == nil {
			continue
		}
		n++
		set := map[string]bool{}
		for _, t := range o.Face().Types {
			if isChooseCardType(t) {
				set[t] = true
			}
		}
		if shared == nil {
			shared = set
			continue
		}
		for t := range shared {
			if !set[t] {
				delete(shared, t)
			}
		}
	}
	if n < 2 || len(shared) == 0 {
		return nil
	}
	out := make([]string, 0, len(shared))
	for t := range shared {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// isChooseCardType reports whether a Type token is one of the card types the
// Type$ Card category offers, so the Shared intersection and the Card list
// agree on what "a card type" means.
func isChooseCardType(t string) bool {
	for _, c := range chooseCardTypes {
		if strings.EqualFold(c, t) {
			return true
		}
	}
	return false
}

// CreatureInTargetedDeckLabels returns the sorted creature subtypes present
// in a targeted opponent's library -- the option list a Type$
// CreatureInTargetedDeck choose ranges over (Aswan Jaguar: "choose a random
// creature type from those in target opponent's deck"). The player targets
// come from the resolving effect's own target list, in target order; the
// first player target with a readable library wins. A trigger with no player
// target, or a deck naming no creature subtype, yields nil and the caller
// falls back.
func CreatureInTargetedDeckLabels(g *state.Game, targets []state.Target) []string {
	if g == nil {
		return nil
	}
	for _, t := range targets {
		if !t.IsPlayer {
			continue
		}
		seen := map[string]bool{}
		for _, id := range g.Zone(state.ZLibrary, t.Player) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil || !hasType(o, "Creature") {
				continue
			}
			for _, ty := range o.Face().Types {
				if creatureSubtypeWords[ty] {
					seen[ty] = true
				}
			}
		}
		if len(seen) == 0 {
			continue
		}
		out := make([]string, 0, len(seen))
		for ty := range seen {
			out = append(out, ty)
		}
		sort.Strings(out)
		return out
	}
	return nil
}

type typeChoiceLabelsCode uint16

const (
	typeChoiceLabelsBasicLand typeChoiceLabelsCode = iota + 1
	typeChoiceLabelsLand
	typeChoiceLabelsNonbasicLand
	typeChoiceLabelsCard
	typeChoiceLabelsPlaneswalker
)

var typeChoiceLabelsCodes = state.NewStrCodes(
	state.StrEntry[typeChoiceLabelsCode]{Key: "basic land", Val: typeChoiceLabelsBasicLand},
	state.StrEntry[typeChoiceLabelsCode]{Key: "land", Val: typeChoiceLabelsLand},
	state.StrEntry[typeChoiceLabelsCode]{Key: "nonbasic land", Val: typeChoiceLabelsNonbasicLand},
	state.StrEntry[typeChoiceLabelsCode]{Key: "card", Val: typeChoiceLabelsCard},
	state.StrEntry[typeChoiceLabelsCode]{Key: "planeswalker", Val: typeChoiceLabelsPlaneswalker},
)
