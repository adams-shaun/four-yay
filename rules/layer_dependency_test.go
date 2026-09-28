package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestLayer6DependencyOrderCavalryMasterOlderThanSidewinder pins the CR
// 613.6 half of the kw:Flanking row's layer-walk narrowing: Cavalry Master's
// `Affected$ Creature.Other+withFlanking+YouCtrl` lord that entered the
// battlefield BEFORE a Sidewinder Sliver must still give the Sliver a second
// flanking instance (CR 702.25b), because the lord's effect DEPENDS on the
// Sliver's own grant -- applying the grant changes what the lord applies to
// -- so CR 613.6 applies the lord after the grant instead of in raw
// timestamp order. Before the fix the walk evaluated the lord's gate against
// the pre-grant keyword list, `withFlanking` failed, and the Sliver sat on
// one instance.
func TestLayer6DependencyOrderCavalryMasterOlderThanSidewinder(t *testing.T) {
	t.Parallel()
	master := mshCorpusCard(t, "Cavalry Master")
	sliverCard := mshCorpusCard(t, "Sidewinder Sliver")
	e := combatEngine(t)
	// Cavalry Master FIRST: its lord effect is the OLDER one, so raw
	// timestamp order evaluates its gate before Sidewinder's own grant is in
	// the walk's keyword list.
	masterID := onBoardCard(t, e, 0, master)
	sliver := onBoardCard(t, e, 0, sliverCard)

	// Preconditions: both permanents are on the battlefield the walk reads,
	// the Sliver prints no flanking of its own (the second instance must come
	// from the lord), the Sliver's own grant is live (one instance without
	// the lord), and the lord's layer-6 grant is registered.
	if e.G.Obj(masterID) == nil || e.G.Obj(masterID).Zone != state.ZBattlefield {
		t.Fatalf("Cavalry Master zone = %v, want battlefield", e.G.Obj(masterID).Zone)
	}
	if e.G.Obj(sliver) == nil || e.G.Obj(sliver).Zone != state.ZBattlefield {
		t.Fatalf("Sidewinder Sliver zone = %v, want battlefield", e.G.Obj(sliver).Zone)
	}
	if e.G.Obj(sliver).Face().HasKeyword("Flanking") {
		t.Fatal("Sidewinder Sliver prints K:Flanking; the dependency case is not exercised")
	}
	if e.G.Obj(masterID).Timestamp >= e.G.Obj(sliver).Timestamp {
		t.Fatalf("timestamps %d >= %d: Cavalry Master is not the older effect, setup is not the CR 613.6 case",
			e.G.Obj(masterID).Timestamp, e.G.Obj(sliver).Timestamp)
	}
	masterGrant := false
	for _, ce := range e.active() {
		if ce.Source == masterID && ce.Layer == LAbilities && len(ce.AddKeywords) > 0 {
			masterGrant = true
		}
	}
	if !masterGrant {
		t.Fatal("Cavalry Master's layer-6 flanking grant is not active; setup is vacuous")
	}
	if got := e.flankingInstances(sliver); got != 2 {
		t.Fatalf("flanking instances = %d, want 2 (Sidewinder's own grant + Cavalry Master's lord applied after it, CR 613.6)", got)
	}
}

// TestLayer6NegativeKeywordDependencyAppliesDependentAfter pins the negative
// direction of the same CR 613.6 modelling: an older `without<Keyword>`-gated
// lord DEPENDS on a newer layer-6 grant that turns the gate off, so the lord
// applies after the grant and stops matching the object -- the Muraganda
// Petroglyphs ruling's reading. Before the fix raw timestamp order applied
// the older lord first, while the gate still matched, and its grant stood.
func TestLayer6NegativeKeywordDependencyAppliesDependentAfter(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	grantor := onBoard(t, e, 0, "Name:Grantor\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	gated := onBoard(t, e, 0, "Name:Gated\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bear := onBoard(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// OLDER (timestamp 1): a `withoutFlying`-gated lord granting deathtouch.
	// NEWER (timestamp 2): a plain grant of flying. Sources are live
	// battlefield permanents so active() keeps both registrations.
	e.AddContinuous(state.ContinuousEffect{
		Source: state.ObjID(gated), Timestamp: 1, Layer: LAbilities,
		Affects: "Creature.Other+withoutFlying", Controller: 0,
		AddKeywords: []string{"Deathtouch"},
	})
	e.AddContinuous(state.ContinuousEffect{
		Source: state.ObjID(grantor), Timestamp: 2, Layer: LAbilities,
		Affects: "Creature.YouCtrl", Controller: 0,
		AddKeywords: []string{"Flying"},
	})

	// Preconditions: both registrations are live in the sorted active list,
	// and without the fix's ordering the gate pair is exactly the
	// order-sensitive shape (the lord matches the bear pre-grant).
	var older, newer *ContinuousEffect
	for i := range e.active() {
		ce := &e.active()[i]
		if ce.Layer != LAbilities {
			continue
		}
		if ce.Timestamp == 1 {
			older = ce
		}
		if ce.Timestamp == 2 {
			newer = ce
		}
	}
	if older == nil || newer == nil {
		t.Fatalf("registered layer-6 effects missing from active(): older=%v newer=%v", older != nil, newer != nil)
	}
	if !e.HasKeyword(bear, "Flying") {
		t.Fatal("the flying grant did not reach the bear; setup is vacuous")
	}
	if e.HasKeyword(bear, "Deathtouch") {
		t.Fatal("the withoutFlying lord's grant stood after the flying grant; the dependent-after direction did not bind")
	}
}

// TestLayer6KeywordDependencyChainResolvesTransitively pins the transitive
// half: a lord gated on a keyword that is itself granted by a gate on a
// keyword granted by a third effect, with the three timestamps in the
// reverse order, must still land all three grants (the dependency chain
// reorders to A1, A2, B). Before the fix only the innermost, ungated grant
// survived.
func TestLayer6KeywordDependencyChainResolvesTransitively(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bear := onBoard(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e.AddContinuous(state.ContinuousEffect{ // oldest: gated on withFlying
		Source: state.ObjID(src), Timestamp: 1, Layer: LAbilities,
		Affects: "Creature.Other+withFlying", Controller: 0,
		AddKeywords: []string{"Deathtouch"},
	})
	e.AddContinuous(state.ContinuousEffect{ // middle: gated on withReach
		Source: state.ObjID(src), Timestamp: 2, Layer: LAbilities,
		Affects: "Creature.Other+withReach", Controller: 0,
		AddKeywords: []string{"Flying"},
	})
	e.AddContinuous(state.ContinuousEffect{ // newest: ungated
		Source: state.ObjID(src), Timestamp: 3, Layer: LAbilities,
		Affects: "Creature.Other", Controller: 0,
		AddKeywords: []string{"Reach"},
	})

	// Preconditions: the ungated grant and both chain gates resolve onto the
	// bear -- the chain reaches every keyword.
	if !e.HasKeyword(bear, "Reach") || !e.HasKeyword(bear, "Flying") || !e.HasKeyword(bear, "Deathtouch") {
		t.Fatalf("chained keyword grants incomplete: reach=%v flying=%v deathtouch=%v",
			e.HasKeyword(bear, "Reach"), e.HasKeyword(bear, "Flying"), e.HasKeyword(bear, "Deathtouch"))
	}
}
