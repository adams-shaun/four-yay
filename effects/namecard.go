package effects

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// NameChoices is the ONE card-name universe builder every NameCard path
// shares: the cast-time "as this enters" ask (rules.etbOptions), the
// mid-resolution DB$/SP$/AB$ NameCard ask (effNameCard) and the R-9 no-host
// fallback all read it, so the offered names, the recorded answer and a bot
// policy can never disagree about what a name choice ranges over.
//
// The universe is the compiled corpus the embedder supplied as
// state.Game.NameUniverse, so an unseen land (Pithing Needle) or an unseen
// opponent card is offered. Order is the sorted distinct primary-face name;
// a nil or face-less universe yields nil and each caller keeps its own R-9
// deterministic stand-in.
//
// spec is the SA's ValidCards$ filter, evaluated through the engine's own
// predicate grammar (MatchesObjectCtx) against each card's printed face, so
// `Card.nonLand` (Phyrexian Revoker, Cabal Therapy), `Card.Land+nonBasic`
// (Alpine Moon), `Card.Creature` (Wood Sage) and `Card.nonLand+nonArtifact`
// (Lost Legacy) all restrict exactly as their script says. An EMPTY spec is
// deliberately unrestricted -- Forge's NameCardEffect does not filter when
// ValidCards$ is absent (Pithing Needle names any card, lands included).
//
// description is the SA's ValidDescription$. Forge reads it as the rendered
// PROMPT text only: ChooseCardNameEffect.resolve filters the common-card set
// by ValidCards$ (defaulting to "Card") and uses ValidDescription$ just to
// word the "choose a specific card name" message, which is why
// rules/paramcensus_test.go's ignoredParamKeys cites it as UI text. When
// ValidCards$ is present it is therefore the ONLY filter. When ValidCards$
// is absent but a description this build can read is present, the description
// is mapped to its equivalent predicate as a SAFETY fallback (see
// descriptionSpec): it only ever narrows an otherwise-unrestricted offer,
// never widens one, so it cannot change real corpus behaviour (no corpus
// NameCard carries ValidDescription$ without ValidCards$). This keeps the
// brief's "ValidCards$/ValidDescription semantics" honest without
// re-implementing Forge's message localisation.
//
// A spec this build cannot evaluate for a printed face (a dynamic
// game-state comparator such as `Creature.cmcEQX`, or an object predicate
// with no resolving context) fails closed in the matcher, and a filter that
// would empty the ask falls back to the unrestricted universe. That is the
// totality rule (R-9): a name ask is never posted with zero options.
func NameChoices(g *state.Game, spec, description string) []string {
	return NameChoicesFromList(g, spec, description, "")
}

// NameChoicesFromList applies ChooseFromList$ after the ordinary card filter.
// The universe snapshot remains authoritative during replay; list order is
// normalized to the same sorted option order as other name choices.
//
// The returned list may be SHARED across games (namecard_cache.go): callers
// must treat it as read-only.
func NameChoicesFromList(g *state.Game, spec, description, chooseFromList string, strictFilter ...bool) []string {
	return NameChoicesFromListCtx(g, spec, description, chooseFromList, nil, strictFilter...)
}

// NameChoicesFromListCtx is NameChoicesFromList with the resolving context a
// mid-resolution NameCard ask owns, so a ValidCards$ whose numeric right-hand
// side is a resolution value (`Creature.cmcEQX` paid-X, `Creature.cmcEQM`
// published by a StoreSVar chain) evaluates with the SAME resolver the rest of
// the engine's resolving filters use. sc nil keeps the printed-face-only walk
// the cast-time ask has always had. Build it with (*Ctx).SpecContext, never a
// hand-rolled resolver: that keeps the numeric-RHS grammar's ONE legal-value
// home in effects/filter.go.
func NameChoicesFromListCtx(g *state.Game, spec, description, chooseFromList string, sc *SpecContext, strictFilter ...bool) []string {
	if g == nil {
		return nil
	}
	if spec == "" {
		spec = descriptionSpec(description)
	}
	strict := len(strictFilter) > 0 && strictFilter[0]
	// A resolution-state-bound filter's eligible set is a function of that
	// state, which the filter memo's key (a spec STRING) does not capture, so
	// compute directly. A context with no resolution state answers exactly
	// what the resolver-free walk would and may be memoised.
	if !pureNameSpec(spec) {
		return nameChoicesFiltered(g, spec, chooseFromList, strict, sc)
	}
	if sc.ResolutionStateBound() && !pureNameSpecIgnoresContext(sc) {
		return nameChoicesFiltered(g, spec, chooseFromList, strict, sc)
	}
	key := nameFilterKey{
		universe:       cardsKey(g.NameUniverse),
		snapshot:       namesKey(g.NameUniverseNames),
		spec:           spec,
		chooseFromList: chooseFromList,
		strict:         strict,
	}
	if spec == "" {
		if chooseFromList == "" {
			// The snapshot (or the memoised universe list) is itself
			// immutable and shared; hand it out directly.
			return nameUniverseSnapshot(g.NameUniverse, g.NameUniverseNames)
		}
		return cachedNameChoices(key, func() []string {
			return filterNameList(nameUniverseSnapshot(g.NameUniverse, g.NameUniverseNames), chooseFromList)
		})
	}
	out := cachedNameChoices(key, func() []string {
		return nameChoicesFiltered(g, spec, chooseFromList, strict, sc)
	})
	if nameChoicesMemoVerify && sc.ResolutionStateBound() {
		if fresh := nameChoicesFiltered(g, spec, chooseFromList, strict, sc); !slices.Equal(fresh, out) {
			panic(fmt.Sprintf("effects: name-choice memo for pure spec %q served %d names under a resolving context, recomputation gives %d", spec, len(out), len(fresh)))
		}
	}
	return out
}

// nameChoicesMemoVerify is set by the effects test binary: every memo hit a
// RESOLVING context takes (pureNameSpecIgnoresContext) is recomputed with
// that context and a difference panics.
var nameChoicesMemoVerify = false

// pureNameSpecIgnoresContext reports whether a pure printed-type spec
// (pureNameSpec) answers the same under sc as under no context at all, so a
// resolving NameCard ask (Cabal Therapy's Card.nonLand, which always carries
// Resolving) may share the process-wide filter memo. A pure spec reads only
// type words; the context reaches a type word only through ExtraTypes (read
// for every object) or an ID-keyed derived table, and every universe card is
// matched as a scratch object with ID 0, which no live binding names.
func pureNameSpecIgnoresContext(sc *SpecContext) bool {
	if sc == nil {
		return true
	}
	if sc.ExtraTypes != nil {
		return false
	}
	for _, d := range sc.Layers.DerivedTypes {
		if d.ID == 0 {
			return false
		}
	}
	for _, n := range sc.Layers.EffectiveNames {
		if n.ID == 0 {
			return false
		}
	}
	return true
}

// nameChoicesFiltered is the uncached ValidCards$ filter walk. sc carries the
// resolving context's numeric-RHS resolver (nil = printed face only; see
// NameChoicesFromListCtx).
func nameChoicesFiltered(g *state.Game, spec, chooseFromList string, strict bool, sc *SpecContext) []string {
	if spec == "" {
		return filterNameList(nameUniverseSnapshot(g.NameUniverse, g.NameUniverseNames), chooseFromList)
	}
	allowed := nameSet(g.NameUniverseNames)
	seen := make(map[string]bool)
	filtered := make([]string, 0, len(g.NameUniverse))
	// One scratch object serves every card: the matcher reads it and keeps
	// no reference, and a fresh ~1 KB Object per universe card was the
	// dominant allocation of a NameCard ask.
	o := new(state.Object)
	for _, c := range g.NameUniverse {
		if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
			continue
		}
		*o = state.Object{Card: c}
		if !MatchesObjectCtx(g, spec, o, specCtx(sc)) {
			continue
		}
		name := c.Faces[0].Name
		if allowed != nil && !allowed[name] {
			continue
		}
		if name != "" && !seen[name] {
			seen[name] = true
			filtered = append(filtered, name)
		}
	}
	if len(filtered) == 0 {
		// Ordinary asks retain the totality fallback. Random selection must
		// never turn an unevaluable/empty filter into an unrestricted lottery.
		if chooseFromList != "" || strict {
			return nil
		}
		return nameUniverseSnapshot(g.NameUniverse, g.NameUniverseNames)
	}
	filtered = filterNameList(filtered, chooseFromList)
	sort.Strings(filtered)
	return filtered
}

// descriptionSpec maps a NameCard ValidDescription$ to the predicate
// equivalent of the description forms the corpus uses, so a description-only
// script is offered a safely-narrowed list rather than the whole universe.
// It is a fallback only: a present ValidCards$ is the filter and this is
// never consulted (Forge semantics; see NameChoices). An unrecognised
// description returns "" (unrestricted), which matches Forge's default and
// keeps the offer total.
func filterNameList(names []string, chooseFromList string) []string {
	if chooseFromList == "" {
		return names
	}
	listed := make(map[string]bool)
	for name := range strings.SplitSeq(chooseFromList, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			listed[name] = true
		}
	}
	selected := make([]string, 0, len(names))
	for _, name := range names {
		if listed[name] {
			selected = append(selected, name)
		}
	}
	return selected
}

func descriptionSpec(description string) string {
	if v, ok := descriptionSpecTab1.Get(strings.ToLower(strings.TrimSpace(description))); ok {
		return v
	}
	return ""
}

// buildNameUniverseNames computes the sorted, distinct primary-face-name
// list NameUniverseNames memoises.
func buildNameUniverseNames(universe []*cards.Card) []string {
	seen := make(map[string]bool, len(universe))
	out := make([]string, 0, len(universe))
	for _, c := range universe {
		if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
			continue
		}
		name := c.Faces[0].Name
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// nameUniverseSnapshot prefers a persisted match's immutable list over the
// current corpus so a later corpus update cannot renumber an answer. Both
// lists are immutable, so the result is returned without a copy and is
// read-only for the caller.
func nameUniverseSnapshot(universe []*cards.Card, snapshot []string) []string {
	if len(snapshot) > 0 {
		return snapshot[:len(snapshot):len(snapshot)]
	}
	return NameUniverseNames(universe)
}

// specCtx dereferences a caller's optional SpecContext, yielding the layout
// (and zero resolver) the printed-face-only walk has always built.
func specCtx(sc *SpecContext) SpecContext {
	if sc == nil {
		return NewSpecContext(0, 0)
	}
	return *sc
}

func nameSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}

var descriptionSpecTab1 = cards.NewStrTable[string](
	cards.StrEntry[string]{Key: "nonland", Val: "Card.nonLand"},
	cards.StrEntry[string]{Key: "creature", Val: "Card.Creature"},
	cards.StrEntry[string]{Key: "creature card", Val: "Card.Creature"},
	cards.StrEntry[string]{Key: "artifact", Val: "Card.Artifact"},
	cards.StrEntry[string]{Key: "artifact card", Val: "Card.Artifact"},
	cards.StrEntry[string]{Key: "land", Val: "Card.Land"},
	cards.StrEntry[string]{Key: "land card", Val: "Card.Land"},
	cards.StrEntry[string]{Key: "nonbasic land", Val: "Card.Land+nonBasic"},
	cards.StrEntry[string]{Key: "card other than a basic land", Val: "Card.Land+nonBasic"},
	cards.StrEntry[string]{Key: "nonartifact, nonland", Val: "Card.nonLand+nonArtifact"},
	cards.StrEntry[string]{Key: "noncreature, nonland", Val: "Card.nonLand+nonCreature"},
)
