package rules

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Derived-transparent rebuilds.
//
// The Derived memo's cross-walk reuse (derivedmemo.go) used to key on
// activeBuildSeq, which every non-inert event moves. Measured on the search
// bench (az leg, 32 games at 100 sims), 78.6M of the 115.7M derivations the
// bot's board build asks for missed that key, and 77.7M of those misses
// recomputed exactly the value the stale entry held: a mana tap, a pool
// change or a step change rebuilt active() and so retired every entry,
// though no derivation can read what those events write.
//
// derivedSeq is the memo's own key. It moves with every active() rebuild
// EXCEPT one that is provably transparent to every derivation:
//
//  1. every event logged since the previous build (or layer-inert re-stamp)
//     is a layer-inert kind (layercache.go) or one of derivedQuietKinds --
//     Tap and Untap (they write o.Tapped and nothing else), ManaAdd and
//     ManaClear (they write a player's mana pool), StepChange (it writes
//     g.Step and the combat-mana bookkeeping);
//  2. the continuous registry version and the object arena are unchanged;
//  3. the rebuilt list is equal to the previous one in every field a
//     derivation reads (derivedEffectEqual), in the same order -- so a
//     duration expiring at a step boundary, a static gate the event flipped,
//     or any static scan output change is a content change and moves the key;
//  4. every effect the derivation reads is local (derivedEffectLocal) --
//     required even across a run of layer-inert events alone, because a
//     forced rebuild (cascade.go's scratch window) can change a non-event
//     input a Count$ amount reads: its Affected$ spec
//     uses only predicates that read the candidate's own characteristics,
//     counters and controller and the effect's source and remembered set
//     (none of which a quiet event writes -- in particular none reads
//     tapped/untapped), and its P/T amounts are integer literals (a Count$
//     amount could read a pool, the step or a tapped count).
//
// The derived object's own fields are the remaining input: Tap writes its
// Tapped flag, which no derivation step reads outside an Affected$ predicate
// (and no local predicate reads it), and an object whose face carries a
// characteristic-defining P/T static is never stamped reusable at all
// (derivedMemoizedAt), because cdaSetPT evaluates an arbitrary count.
//
// Everything else that moves activeBuildSeq -- a re-entrant build, an explicit
// retirement (retireCrossWalkMemo) -- moves derivedSeq too. In the rules test
// binary derivedMemoVerify recomputes every cross-walk hit and panics on a
// difference, which checks this argument over the whole suite.

// derivedQuietKinds are the non-layer-inert event kinds a transparent
// rebuild may span (condition 1 above).
func derivedQuietKind(k events.Kind) bool {
	switch k {
	case events.Tap, events.Untap, events.ManaAdd, events.ManaClear, events.StepChange:
		return true
	}
	return false
}

// derivedRebuildTransparent reports whether the list just built (fresh) can
// keep derivedSeq, given prev the list the previous build returned.
func (e *Engine) derivedRebuildTransparent(prev, fresh []ContinuousEffect) bool {
	if e.derivedSeq == 0 || e.derivedPrevEpoch <= 0 || e.derivedPrevEpoch > len(e.L.Events) ||
		e.derivedPrevVersion != e.continuousVersion || e.derivedPrevObjs != len(e.G.Objs) ||
		len(prev) != len(fresh) {
		return false
	}
	for _, ev := range e.L.Events[e.derivedPrevEpoch:] {
		switch ev.Kind {
		case events.DecisionAsk, events.DecisionMade, events.Priority:
		default:
			if !derivedQuietKind(ev.Kind) {
				return false
			}
		}
	}
	for i := range fresh {
		if !derivedEffectEqual(&prev[i], &fresh[i]) || !derivedEffectLocal(&fresh[i]) {
			return false
		}
	}
	return true
}

// derivedNoteBuild records the key the build or re-stamp that produced the
// current list was taken at.
func (e *Engine) derivedNoteBuild() {
	e.derivedPrevEpoch = len(e.L.Events)
	e.derivedPrevVersion = e.continuousVersion
	e.derivedPrevObjs = len(e.G.Objs)
}

// derivedEffectEqual compares every ContinuousEffect field the layer walk
// reads: derivedCompute, typeCharacteristicsActive, abilityDependencyOrder,
// derivedScalarFrom and the matchesWithCharsPT bind. SVars is left out: only
// a non-literal amount resolves through it, and such an effect is never
// local (derivedEffectLocal), so a transparent rebuild never reads it.
func derivedEffectEqual(a, b *ContinuousEffect) bool {
	return a.Source == b.Source && a.Layer == b.Layer && a.Sub == b.Sub && a.Timestamp == b.Timestamp &&
		a.Affects == b.Affects && a.Controller == b.Controller && a.AffectedZone == b.AffectedZone &&
		a.MayPlay == b.MayPlay &&
		a.AddPower == b.AddPower && a.AddToughness == b.AddToughness &&
		a.DoublePower == b.DoublePower && a.DoubleToughness == b.DoubleToughness &&
		a.SetPower == b.SetPower && a.SetToughness == b.SetToughness && a.HasSet == b.HasSet &&
		a.StaticSet == b.StaticSet && a.SetPowerPresent == b.SetPowerPresent && a.SetToughnessPresent == b.SetToughnessPresent &&
		a.AddPowerExpr == b.AddPowerExpr && a.AddToughnessExpr == b.AddToughnessExpr &&
		a.AddPowerAffected == b.AddPowerAffected && a.AddToughnessAffected == b.AddToughnessAffected &&
		a.SetPowerExpr == b.SetPowerExpr && a.SetToughnessExpr == b.SetToughnessExpr &&
		a.SetName == b.SetName && a.TextFrom == b.TextFrom && a.TextTo == b.TextTo &&
		a.TextSet == b.TextSet && a.TextSetSet == b.TextSetSet &&
		a.OverwriteColors == b.OverwriteColors && a.RemoveCreatureTypes == b.RemoveCreatureTypes &&
		a.RemoveSubTypes == b.RemoveSubTypes && a.SetCreatureTypes == b.SetCreatureTypes &&
		a.AddAllCreatureTypes == b.AddAllCreatureTypes && a.RemoveCardTypes == b.RemoveCardTypes &&
		a.RemoveLegendary == b.RemoveLegendary && a.RemoveAbilities == b.RemoveAbilities &&
		slices.Equal(a.AddKeywords, b.AddKeywords) && slices.Equal(a.AddTypes, b.AddTypes) &&
		slices.Equal(a.AddColors, b.AddColors) && slices.Equal(a.RemoveTypes, b.RemoveTypes) &&
		slices.Equal(a.RemoveKeywords, b.RemoveKeywords) && slices.Equal(a.CantHaveKeywords, b.CantHaveKeywords) &&
		slices.Equal(a.Remembered, b.Remembered)
}

// derivedEffectLocal reports whether a derivation's read of ce depends only
// on the candidate's own characteristics, counters and controller and on
// ce's own fields and source (condition 4 above). An effect outside the
// layers a derivation reads is trivially local.
func derivedEffectLocal(ce *ContinuousEffect) bool {
	switch ce.Layer {
	case LText, LType, LColor, LAbilities, LPT:
	default:
		if len(ce.CantHaveKeywords) == 0 {
			return true
		}
	}
	if !literalAmount(ce.AddPowerExpr) || !literalAmount(ce.AddToughnessExpr) ||
		!literalAmount(ce.SetPowerExpr) || !literalAmount(ce.SetToughnessExpr) {
		return false
	}
	return specLocal(ce.Affects)
}

// literalAmount reports whether a P/T expression is empty or an optionally
// signed decimal integer -- an amount no game state can move.
func literalAmount(s string) bool {
	if s == "" {
		return true
	}
	if s[0] == '+' || s[0] == '-' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// specLocalMemo caches specLocal per spec text (a pure function of it):
// a direct-mapped table of immutable entries, safe to share across engines.
var specLocalMemo [256]atomic.Pointer[specLocalEntry]

type specLocalEntry struct {
	spec string
	ok   bool
}

func specLocal(spec string) bool {
	h := uint32(2166136261)
	for i := 0; i < len(spec); i++ {
		h = (h ^ uint32(spec[i])) * 16777619
	}
	slot := &specLocalMemo[h%uint32(len(specLocalMemo))]
	if en := slot.Load(); en != nil && en.spec == spec {
		return en.ok
	}
	ok := specLocalParse(spec)
	slot.Store(&specLocalEntry{spec: spec, ok: ok})
	return ok
}

// specLocalParse is the whitelist: every comma alternative is a base word
// (a type word, optionally non-prefixed, or Card/Permanent) followed by
// '+'-joined predicates from localSpecPredicates, a counters_<CMP><n>_<KIND>
// comparison with a literal count, or a with<Keyword>/without<Keyword> test.
// Anything else -- a numeric predicate with an SVar right-hand side, a
// tapped/untapped/attacking test, a cast-provenance token, a name test, a
// player qualifier -- is not local.
func specLocalParse(spec string) bool {
	if spec == "" {
		return true
	}
	for alt := range strings.SplitSeq(spec, ",") {
		base, preds, _ := strings.Cut(alt, ".")
		if !letterWord(base) {
			return false
		}
		if preds == "" {
			continue
		}
		for p := range strings.SplitSeq(preds, "+") {
			if !localPredicate(p) {
				return false
			}
		}
	}
	return true
}

func letterWord(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

func localPredicate(p string) bool {
	switch p {
	case "Self", "Other", "YouCtrl", "OppCtrl", "YouOwn", "OppOwn", "EnchantedBy", "EquippedBy",
		"ChosenColor", "IsRemembered", "ChosenCard", "token", "nonToken":
		return true
	}
	if rest, ok := strings.CutPrefix(p, "counters_"); ok {
		if len(rest) < 4 {
			return false
		}
		switch rest[:2] {
		case "LT", "LE", "GT", "GE", "EQ", "NE":
		default:
			return false
		}
		num, kind, ok := strings.Cut(rest[2:], "_")
		return ok && literalAmount(num) && num != "" && num[0] != '+' && num[0] != '-' && letterWordDigits(kind)
	}
	if kw, ok := strings.CutPrefix(p, "without"); ok {
		return letterWord(kw)
	}
	if kw, ok := strings.CutPrefix(p, "with"); ok {
		return letterWord(kw)
	}
	return false
}

func letterWordDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// faceHasCDAStatic reports whether o's face carries a characteristic-
// defining Continuous static -- cdaSetPT's own test -- whose amount may read
// any game state, so the object's derivation is never reused across events.
func faceHasCDAStatic(o *state.Object) bool {
	if o == nil {
		return false
	}
	f := o.Face()
	if f == nil {
		return false
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		if st.Mode == "Continuous" && strings.TrimSpace(st.Params["CharacteristicDefining"]) != "" {
			return true
		}
	}
	return false
}
