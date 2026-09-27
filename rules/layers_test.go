package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// onBoard places a card straight onto the battlefield, bypassing events the
// same way every other direct test setup in this package does. Ruling T21-f
// (Task 21 fix round 1): it sets SummonSick, matching what a real entry
// (events.Move's ZBattlefield case) does -- previously it left this false,
// so every test built on it got an attack-ready creature for free, unlike a
// real game. A test that wants that must clear it explicitly (see
// combat_test.go's onBoardReady), the same way a real game needs a full turn
// to pass before a permanent loses summoning sickness.
func onBoard(t testing.TB, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	o.Zone = state.ZBattlefield
	o.SummonSick = true
	e.G.Clock++
	o.Timestamp = e.G.Clock
	e.G.SetZone(state.ZBattlefield, p, append(e.G.Zone(state.ZBattlefield, p), o.ID))
	// The placement is eventless, so it must stale the derived memos exactly
	// like an emitted event would: the static scan (rules/layers.go) and the
	// sorted effect list are keyed on the log head, and a board change that
	// never emits would otherwise stay invisible to every later Derived
	// (the static-grant work's trigger walk is what first made the memo
	// build during genesis, exposing the stale-window this bump closes).
	e.staticEpoch = -1
	e.activeEpoch = -1
	// The placement is eventless, so the layer-4 derived-type table
	// (layer4types.go) must not stay a stale cache hit for the ordinary filter
	// grammar either: refresh it now, exactly as an emitted board change
	// would. Cheap and gated, so the common test board pays one branch.
	e.typesEpoch = -1
	if e.layer4InPool {
		e.refreshDerivedTypes()
	}
	return o.ID
}

func layerEngine(t testing.TB) *Engine {
	t.Helper()
	// Seat 0 is the protagonist of every layer/synthetic-upkeep fixture (the
	// synthetic StepChange emits rely on G.Active reading seat 0);
	// seatZeroStart advances the seed until the CR 103.1 toss starts seat 0.
	return New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
}

func TestDerivedStartsFromPrintedCharacteristics(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Trample\nOracle:x\n")
	d := e.Derived(id)
	if d.Power != 2 || d.Toughness != 2 {
		t.Fatalf("P/T = %d/%d", d.Power, d.Toughness)
	}
	if !e.HasKeyword(id, "Trample") {
		t.Error("printed keyword missing")
	}
}

// TestDerivedSeedsTypesFromPrintedThenAddsGrantedTypes pins the layer-4 seed
// (rules/layers.go): Derived.Types must start from the object's printed type
// line, exactly as Keywords starts from the printed keyword line, and then
// accumulate any LType-granted types. This is the regression test for the
// fix-round-1 defect where the printed types were dropped (ty was reset but
// never seeded from f.Types), leaving the field holding only granted types --
// a change no consumer exercised, so no suite caught it. Removing the seed
// makes the printed type disappear from Derived.Types and fails this test by
// name.
func TestDerivedSeedsTypesFromPrintedThenAddsGrantedTypes(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if got := e.Derived(id).Types; len(got) != 2 || got[0] != "Creature" || got[1] != "Bear" {
		t.Fatalf("printed types missing from Derived.Types before any effect: %v", got)
	}
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LType,
		Affects: "Card.Self", AddTypes: []string{"Bird"}, UntilEOT: true})
	got := e.Derived(id).Types
	if len(got) != 3 || got[0] != "Creature" || got[1] != "Bear" || got[2] != "Bird" {
		t.Fatalf("Derived.Types = %v, want [Creature Bear Bird] (printed then granted)", got)
	}
}

// TestDerivedSeedsKeywordsFromPrintedThenAddsGrantedKeywords is the same pin
// for the layer-6 seed: Derived.Keywords must contain the printed keyword
// AND any LAbilities-granted keyword. It has the same shape as the types seed
// and, before this fix round, the same missing test, so it gets the same
// guard; removing the seed makes the printed keyword disappear and fails this
// test by name.
func TestDerivedSeedsKeywordsFromPrintedThenAddsGrantedKeywords(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Trample\nOracle:x\n")
	if got := e.Derived(id).Keywords; len(got) != 1 || got[0] != "Trample" {
		t.Fatalf("printed keyword missing from Derived.Keywords before any effect: %v", got)
	}
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LAbilities,
		Affects: "Card.Self", AddKeywords: []string{"Flying"}, UntilEOT: true})
	d := e.Derived(id)
	if len(d.Keywords) != 2 || d.Keywords[0] != "Trample" || d.Keywords[1] != "Flying" {
		t.Fatalf("Derived.Keywords = %v, want [Trample Flying] (printed then granted)", d.Keywords)
	}
}

func TestLayer7dCountersAddToPowerAndToughness(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(id).AddCounter("P1P1", 3)
	if got := e.Power(id); got != 5 {
		t.Fatalf("power = %d, want 5", got)
	}
	if got := e.Toughness(id); got != 5 {
		t.Fatalf("toughness = %d, want 5", got)
	}
}

func TestLayer7cModificationsStack(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", Controller: 0, AddPower: 3, AddToughness: 3, UntilEOT: true})
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 2, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", Controller: 0, AddPower: 1, AddToughness: 0, UntilEOT: true})
	if got := e.Power(id); got != 6 {
		t.Fatalf("power = %d, want 6", got)
	}
	if got := e.Toughness(id); got != 5 {
		t.Fatalf("toughness = %d, want 5", got)
	}
}

// Layer order is the whole point: a set-P/T effect must be overwritten by a
// later set effect, and both must be applied before modifications and counters.
func TestSetBeforeModifyRegardlessOfTimestamp(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// The +2/+2 has the EARLIER timestamp; the 1/1 set has the later one.
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", AddPower: 2, AddToughness: 2, UntilEOT: true})
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 2, Layer: LPT, Sub: SubSet,
		Affects: "Card.Self", SetPower: 1, SetToughness: 1, HasSet: true, UntilEOT: true})
	e.G.Obj(id).AddCounter("P1P1", 1)
	// 1/1 set, then +2/+2 modification, then +1/+1 counter.
	if got, want := e.Power(id), int32(4); got != want {
		t.Fatalf("power = %d, want %d", got, want)
	}
}

func TestLayer6GrantsKeywords(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.HasKeyword(id, "Flying") {
		t.Fatal("bear should not start with flying")
	}
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LAbilities,
		Affects: "Card.Self", AddKeywords: []string{"Flying"}, UntilEOT: true})
	if !e.HasKeyword(id, "Flying") {
		t.Fatal("granted keyword missing")
	}
}

func TestWithKeywordPredicateSeesLayer6Grant(t *testing.T) {
	e := layerEngine(t)
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	obj := e.G.Obj(bear)
	if obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("bear precondition: object=%v zone is not battlefield", obj)
	}
	if obj.Face() == nil || obj.Face().HasKeyword("Menace") {
		t.Fatal("bear must not print Menace")
	}
	sc := e.specCtx(0, 0)
	if e.matchesSpec("Creature.YouCtrl+withMenace", bear, sc) {
		t.Fatal("ungranted bear matched withMenace")
	}
	if !e.matchesSpec("Creature.YouCtrl+withoutMenace", bear, sc) {
		t.Fatal("ungranted bear did not match withoutMenace")
	}

	e.AddContinuous(ContinuousEffect{Source: bear, Timestamp: 1, Layer: LAbilities,
		Affects: "Creature.YouCtrl", Controller: 0, AddKeywords: []string{"Menace"}, UntilEOT: true})
	if !e.HasKeyword(bear, "Menace") {
		t.Fatal("layer-6 Menace grant is not derived")
	}
	sc = e.specCtx(0, 0)
	with := e.matchesSpec("Creature.YouCtrl+withMenace", bear, sc)
	without := e.matchesSpec("Creature.YouCtrl+withoutMenace", bear, sc)
	if !with || without {
		t.Fatalf("derived keyword filter = with %v, without %v; want true, false", with, without)
	}

	e.EndOfTurnCleanup()
	if e.HasKeyword(bear, "Menace") {
		t.Fatal("expired Menace grant remains derived")
	}
	sc = e.specCtx(0, 0)
	with = e.matchesSpec("Creature.YouCtrl+withMenace", bear, sc)
	without = e.matchesSpec("Creature.YouCtrl+withoutMenace", bear, sc)
	if with || !without {
		t.Fatalf("expired keyword filter = with %v, without %v; want false, true", with, without)
	}
}

func TestLayer6KeywordFilterRestrictsStatic(t *testing.T) {
	e := layerEngine(t)
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 0, "Name:Wind Wall\nTypes:Enchantment\nOracle:x\nS:Mode$ CantAttack | ValidCard$ Creature.withFlying\n")
	e.AddContinuous(ContinuousEffect{Source: bear, Timestamp: 1, Layer: LAbilities,
		Affects: "Card.Self", AddKeywords: []string{"Flying"}, UntilEOT: true})
	if !e.HasKeyword(bear, "Flying") {
		t.Fatal("layer-6 Flying grant is not derived")
	}
	if !e.attackBlocked(bear, 1, 0) {
		t.Fatal("CantAttack withFlying did not see derived Flying")
	}
	e.EndOfTurnCleanup()
	if e.attackBlocked(bear, 1, 0) {
		t.Fatal("expired Flying still satisfies withFlying")
	}
}

func TestAffectsFilterScopesTheEffect(t *testing.T) {
	e := layerEngine(t)
	mine := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	theirs := onBoard(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: mine, Timestamp: 1, Layer: LPT, Sub: SubModify,
		Affects: "Creature.YouCtrl", Controller: 0, AddPower: 1, AddToughness: 1})
	if e.Power(mine) != 3 {
		t.Errorf("own creature power = %d, want 3", e.Power(mine))
	}
	if e.Power(theirs) != 2 {
		t.Errorf("opponent creature power = %d, want 2", e.Power(theirs))
	}
}

func TestEndOfTurnCleanupDropsUntilEOTEffects(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", AddPower: 3, UntilEOT: true})
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 2, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", AddPower: 1})
	if e.Power(id) != 6 {
		t.Fatalf("power = %d, want 6", e.Power(id))
	}
	e.EndOfTurnCleanup()
	if e.Power(id) != 3 {
		t.Fatalf("power after cleanup = %d, want 3", e.Power(id))
	}
}

func BenchmarkDerivedWithContinuousEffects(b *testing.B) {
	e := layerEngine(b)
	id := onBoard(b, e, 0, "Name:Layered bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Trample\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LType,
		Affects: "Card.Self", AddTypes: []string{"Warrior"}})
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 2, Layer: LColor,
		Affects: "Card.Self", AddColors: []string{"U"}})
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 3, Layer: LAbilities,
		Affects: "Card.Self", AddKeywords: []string{"Vigilance"}})
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 4, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", AddPower: 2, AddToughness: 1})
	want := Derived{Power: 4, Toughness: 3, Keywords: []string{"Trample", "Vigilance"},
		Types: []string{"Creature", "Bear", "Warrior"}, Colors: "UG"}
	b.ReportAllocs()
	b.ResetTimer()
	var got Derived
	for range b.N {
		got = e.Derived(id)
	}
	b.StopTimer()
	if got.Power != want.Power || got.Toughness != want.Toughness || got.Colors != want.Colors ||
		!slices.Equal(got.Keywords, want.Keywords) || !slices.Equal(got.Types, want.Types) {
		b.Fatalf("Derived = %+v, want %+v", got, want)
	}
}

func TestDerivedWithContinuousEffectsDoesNotAllocate(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Layered bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: id, Layer: LColor,
		Affects: "Card.Self", AddColors: []string{"U"}})
	e.Derived(id)
	if allocs := testing.AllocsPerRun(1000, func() { e.Derived(id) }); allocs != 0 {
		t.Fatalf("warm Derived allocated %.2f objects/call, want zero", allocs)
	}
}

// TestActiveCacheInvalidatesWhenEffectsEnterAndLeave is the Task c5 cache
// invalidation regression test. active() (layers.go) caches its sorted effect
// list on the pair (log head, continuousVersion) so Derived stops rebuilding
// it once per object; the enforced point is that the cache is invalidated
// whenever a continuous effect ENTERS (AddContinuous) or LEAVES
// (EndOfTurnCleanup) the live set. The leave direction is the load-bearing
// one: EndOfTurnCleanup rewrites e.continuous in place and emits NO event, so
// only the continuousVersion bump can ever invalidate the cache there. If
// that bump is removed (or the key widened to the log head alone), the cached
// list keeps the dropped UntilEOT pump and the engine reports a dead pump's
// P/T -- a stale derived characteristic, which is a wrong game, not a slow
// one.
//
// The test deliberately primes the cache by reading Derived before each
// mutation and re-primes before the leave, so it fails exactly when
// invalidation is missing rather than when the cache happens not to be built.
func TestActiveCacheInvalidatesWhenEffectsEnterAndLeave(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// Prime the cache on the bare board.
	if got := e.Power(id); got != 2 {
		t.Fatalf("prime: power = %d, want 2", got)
	}

	// Enter: an UntilEOT +3/+3 pump. The ClockTick and the version both move,
	// so the cached list must rebuild and the pump must be visible.
	e.AddContinuous(ContinuousEffect{Source: id, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", AddPower: 3, AddToughness: 3, UntilEOT: true})
	if got := e.Power(id); got != 5 {
		t.Fatalf("after enter: power = %d, want 5 (pump must appear)", got)
	}

	// Re-prime on the pumped board so the leave below is the only possible
	// source of staleness.
	if got := e.Power(id); got != 5 {
		t.Fatalf("re-prime: power = %d, want 5", got)
	}

	// Leave: EndOfTurnCleanup drops the UntilEOT pump with no log event. Only
	// the continuousVersion bump invalidates the cache here; without it the
	// dead pump would leak into derived P/T.
	e.EndOfTurnCleanup()
	if got := e.Power(id); got != 2 {
		t.Fatalf("after leave: power = %d, want 2 (cleaned-up pump must vanish)", got)
	}
}

func TestEffectsFromObjectsThatLeftAreDropped(t *testing.T) {
	e := layerEngine(t)
	lord := onBoard(t, e, 0, "Name:Lord\nManaCost:2 W\nTypes:Creature Human\nPT:2/2\nOracle:x\n")
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: lord, Timestamp: 1, Layer: LPT, Sub: SubModify,
		Affects: "Creature.YouCtrl+Other", Controller: 0, AddPower: 1, AddToughness: 1})
	if e.Power(bear) != 3 {
		t.Fatalf("power = %d, want 3", e.Power(bear))
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: lord,
		From: state.ZBattlefield, To: state.ZGraveyard})
	if e.Power(bear) != 2 {
		t.Fatalf("power after the lord left = %d, want 2", e.Power(bear))
	}
}

// TestAddContinuousClockAdvanceIsReplayable is the Ruling T19-a regression
// test: AddContinuous's Timestamp auto-stamp must advance Game.Clock only
// through a logged ClockTick event, never a direct field write, so folding
// the events these two calls produced into a snapshot taken just before
// them -- through events.Apply alone, no Engine, no rules code involved --
// reaches the same Clock the live game did. This is the same invariant
// fix1_test.go's TestReplayReconstructsPassesAndPriority locks in for
// Passes/Priority.
//
// The snapshot is taken immediately before the calls under test, rather than
// replaying the whole log from genesis, because onBoard (this file's own
// test helper, above) stamps Object.Timestamp with a direct e.G.Clock++ of
// its own -- test code is allowed to bypass events, but comparing from
// genesis would conflate that unrelated bypass with the one this test
// exists to catch.
func TestAddContinuousClockAdvanceIsReplayable(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	snapshot := e.G.Clone()
	firstNewEvent := len(e.L.Events)

	e.AddContinuous(ContinuousEffect{Source: id, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", AddPower: 1})
	e.AddContinuous(ContinuousEffect{Source: id, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", AddPower: 1})

	wantClock := e.G.Clock
	if wantClock != snapshot.Clock+2 {
		t.Fatalf("live Clock advanced by %d across two AddContinuous calls, want 2", wantClock-snapshot.Clock)
	}

	for _, ev := range e.L.Events[firstNewEvent:] {
		events.Apply(snapshot, ev)
	}
	if snapshot.Clock != wantClock {
		t.Fatalf("Clock reconstructed from the log alone = %d, want %d (live)", snapshot.Clock, wantClock)
	}
}

// TestHasKeywordIsCaseInsensitive is the Ruling T19-b regression test:
// Engine.HasKeyword must match case-insensitively like its sibling
// cards.Face.HasKeyword, for both a printed keyword and one granted by a
// continuous effect.
func TestHasKeywordIsCaseInsensitive(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Trample\nOracle:x\n")
	if !e.HasKeyword(id, "trample") {
		t.Error("HasKeyword should match a printed keyword case-insensitively (lowercase query)")
	}
	if !e.HasKeyword(id, "TRAMPLE") {
		t.Error("HasKeyword should match a printed keyword case-insensitively (uppercase query)")
	}

	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LAbilities,
		Affects: "Card.Self", AddKeywords: []string{"flying"}, UntilEOT: true})
	if !e.HasKeyword(id, "Flying") {
		t.Error("HasKeyword should match a granted keyword case-insensitively even when the grant itself is lowercase")
	}
}
