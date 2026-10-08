package mzenc

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// idsFor is the test helper: rebuild a Features tree with the SAME public
// operations ProcessState must perform, and return its id set, so a test can
// assert the walker emitted exactly the expected families. Built per test;
// no corpus, no game.
func idsFor(build func(f *Node)) map[int32]struct{} {
	e := NewEncoder(DefaultTableSize)
	build(e.Root())
	return e.IDs()
}

func TestProcessStateGlobals(t *testing.T) {
	v := view.View{Viewer: 0, Step: "main1", Phase: "main1"}
	got := ProcessState(v, nil, 0, 0, "priority")

	// upstream processState (StateEncoder.java:634-641): a step feature, the
	// decisionType name, and the cleaned decisionsText, all at the root.
	want := idsFor(func(f *Node) {
		f.AddFeature("PRECOMBAT_MAIN") // see Step: the ActionType/step name mapping below
		f.AddFeature("PRIORITY")
		f.AddFeature("priority")
	})
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing global id %d (want %v got %v)", id, want, got)
		}
	}
}

func TestProcessStatePlayerScalars(t *testing.T) {
	v := view.View{
		Players: []view.PlayerView{
			{ID: 0, Life: 20, LibrarySize: 53, HandSize: 7, Pool: map[string]int32{"W": 2}},
			{ID: 1, Life: 18, LibrarySize: 60, HandSize: 5},
		},
	}
	got := ProcessState(v, nil, 0, 0, "x")

	want := idsFor(func(f *Node) {
		me := f.SubFeatures("Player", true)
		me.AddNumericFeature("LifeTotal", 20, true)
		me.AddNumericFeature("LibraryCount", 53, true)
		// No CardsInHand: perfectInfo (the always-omniscient public entry)
		// walks every Hand instead (StateEncoder.java:601), Task 4.
		me.AddFeature("IsActivePlayer")
		me.AddFeature("IsDecisionPlayer")
		mp := me.SubFeatures("ManaPool", false)
		mp.AddNumericFeature("WhiteMana", 2, true)
		opp := f.SubFeatures("Opponent", true)
		opp.AddNumericFeature("LifeTotal", 18, true)
		opp.AddNumericFeature("LibraryCount", 60, true)
	})
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing player id %d", id)
		}
	}
}

func TestProcessStateBattlefieldDeterministicOrder(t *testing.T) {
	mk := func(id state.ObjID, name string, tapped bool, power int32) view.CardView {
		return view.CardView{ID: id, Name: name, Types: "Creature", Tapped: tapped,
			Power: power, Toughness: 3, Keywords: []string{"Flying"}}
	}
	// Two battlefields. Within one, TWO permanents share the name "Alpha" but
	// differ in Tapped/Power, so the sorted walk is the only thing that pins
	// which permanent gets the "#1" vs "#2" occurrence key. Distinct names
	// alone would always land on their own name#1 child and never exercise the
	// ordering; the duplicate name is what makes this test guard the sort.
	v := view.View{Players: []view.PlayerView{
		{ID: 0, Battlefield: []view.CardView{
			mk(10, "Alpha", false, 3),
			mk(11, "Alpha", true, 2),
			mk(12, "Grizzly Bears", false, 2),
		}},
		{ID: 1, Battlefield: nil},
	}}
	a := ProcessState(v, nil, 0, 0, "x")
	// swap the two SAME-NAME permanents; with a sorted walk the id set is
	// unchanged, without it the Alpha#1/#2 occurrence keys move and the id set
	// changes. (Sorted order is by (Name, ID), so id 10 stays Alpha#1.)
	v.Players[0].Battlefield[0], v.Players[0].Battlefield[1] = v.Players[0].Battlefield[1], v.Players[0].Battlefield[0]
	b := ProcessState(v, nil, 0, 0, "x")
	if len(a) != len(b) {
		t.Fatalf("order-dependent id set: %d vs %d", len(a), len(b))
	}
	for id := range a {
		if _, ok := b[id]; !ok {
			t.Fatalf("order-dependent id set: id %d only in first", id)
		}
	}
	// The exact subtree the walk must emit. The battlefield nests under the
	// player's subtree (StateEncoder processPlayer:592), so `want` hangs it
	// under "Player". Sorted by ID: id 10 is Alpha#1 (Power 3), id 11 is
	// Alpha#2 (Tapped, Power 2); the Features engine numbers the two same-name
	// SubFeatures calls #1 then #2.
	want := idsFor(func(f *Node) {
		me := f.SubFeatures("Player", true)
		bf := me.SubFeatures("Battlefield", true)
		a1 := bf.SubFeatures("Alpha", true)
		a1.AddFeature("creature") // type word, lowercased
		a1.AddNumericFeature("Power", 3, true)
		a1.AddNumericFeature("Toughness", 3, true)
		a2 := bf.SubFeatures("Alpha", true)
		a2.AddFeature("Tapped")
		a2.AddFeature("creature")
		a2.AddNumericFeature("Power", 2, true)
		a2.AddNumericFeature("Toughness", 3, true)
		pg := bf.SubFeatures("Grizzly Bears", true)
		pg.AddNumericFeature("Power", 2, true)
	})
	for id := range want {
		if _, ok := a[id]; !ok {
			t.Fatalf("missing battlefield id %d", id)
		}
	}
}

func TestProcessStateHandAndGraveyard(t *testing.T) {
	v := view.View{Players: []view.PlayerView{
		{ID: 0,
			Hand:      []view.CardView{{ID: 20, Name: "Counterspell", Types: "Instant", ManaCost: "U U"}},
			Graveyard: []view.CardView{{ID: 21, Name: "Bolt", Types: "Instant", ManaCost: "R"}},
		},
		{ID: 1, Hand: []view.CardView{{ID: 22, Name: "Forest", Types: "Land"}}},
	}}
	got := ProcessState(v, nil, 0, 0, "x")

	want := idsFor(func(f *Node) {
		me := f.SubFeatures("Player", true)
		h := me.SubFeatures("Hand", true)
		hc := h.SubFeatures("Counterspell", true)
		hc.AddFeature("Card")
		hc.AddFeature("instant")
		hc.AddNumericFeature("ManaValue", 2, true)
		gy := me.SubFeatures("Graveyard", true)
		gc := gy.SubFeatures("Bolt", true)
		gc.AddFeature("Card")
		gc.AddFeature("instant")
		// perfectInfo: the opponent's hand is walked too.
		oh := f.SubFeatures("Opponent", true).SubFeatures("Hand", true)
		oc := oh.SubFeatures("Forest", true)
		oc.AddFeature("Card")
		oc.AddFeature("land")
	})
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing hand/graveyard id %d", id)
		}
	}
}

func TestProcessStateBlockedByRegistersBattlefieldName(t *testing.T) {
	// The attacker is the seat's; its blocker sits on the OPPONENT's
	// battlefield. processPerm reads w.names for the BlockedBy id, so the
	// blocker's name must already be registered before the walk.
	attacker := view.CardView{ID: 30, Name: "Hill Giant", Types: "Creature",
		Attacking: true, BlockedBy: []state.ObjID{10}, Power: 3, Toughness: 3}
	blocker := view.CardView{ID: 10, Name: "Grizzly Bears", Types: "Creature",
		Power: 2, Toughness: 2}
	v := view.View{Players: []view.PlayerView{
		{ID: 0, Battlefield: []view.CardView{attacker}},
		{ID: 1, Battlefield: []view.CardView{blocker}},
	}}
	got := ProcessState(v, nil, 0, 0, "x")

	want := idsFor(func(f *Node) {
		me := f.SubFeatures("Player", true)
		bf := me.SubFeatures("Battlefield", true)
		atk := bf.SubFeatures("Hill Giant", true)
		atk.AddFeature("Grizzly Bears Blocking")
	})
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing Blocking id %d", id)
		}
	}
}

func TestCleanStringStripsUUIDTagsAndAngleBrackets(t *testing.T) {
	cases := map[string]string{
		"Lightning Bolt [1a2b3c]": "Lightning Bolt",
		"<b>Flying</b>":           "Flying",
		"plain":                   "plain",
		"":                        "",
	}
	for in, want := range cases {
		if got := cleanString(in); got != want {
			t.Errorf("cleanString(%q)=%q want %q", in, got, want)
		}
	}
}

// TestStepNameTable pins every mapped step to its expected upstream
// TurnStepType name, one row per state.Step, so the spelling that drives a
// hashed feature id is locked and cannot shift if state.Step.String() changes.
// The exact spellings are an assumption validated against XMage's enum
// (deferred to a live oracle, mzenc design §9).
func TestStepNameTable(t *testing.T) {
	cases := []struct {
		step state.Step
		name string
	}{
		{state.StepUntap, "UNTAP"},
		{state.StepUpkeep, "UPKEEP"},
		{state.StepDraw, "DRAW"},
		{state.StepMain1, "PRECOMBAT_MAIN"},
		{state.StepBeginCombat, "BEGIN_COMBAT"},
		{state.StepDeclareAttackers, "DECLARE_ATTACKERS"},
		{state.StepDeclareBlockers, "DECLARE_BLOCKERS"},
		{state.StepCombatDamage, "COMBAT_DAMAGE"},
		{state.StepEndCombat, "END_COMBAT"},
		{state.StepMain2, "POSTCOMBAT_MAIN"},
		{state.StepEnd, "END_TURN"},
		{state.StepCleanup, "CLEANUP"},
	}
	for _, c := range cases {
		got, ok := stepName[c.step.String()]
		if !ok {
			t.Errorf("stepName missing key %q", c.step.String())
			continue
		}
		if got != c.name {
			t.Errorf("stepName[%q]=%q want %q", c.step.String(), got, c.name)
		}
	}
}

// TestStepNameCoversAllSteps holds stepName to state.Step's own String()
// spellings: every defined Step must have a key, and that key must be exactly
// what Step.String() produces, so the table cannot drift from the state
// package's single home of the step names.
func TestStepNameCoversAllSteps(t *testing.T) {
	for _, s := range state.AllSteps().Steps() {
		name, ok := stepName[s.String()]
		if !ok {
			t.Errorf("stepName missing key %q (state.Step %d)", s.String(), s)
			continue
		}
		if name == "" {
			t.Errorf("stepName[%q] is empty", s.String())
		}
	}
}

// TestProcessStateStackDepth pins the stack skeleton's traversal: view.View.Stack
// is bottom-to-top (index 0 the bottom), each object's subtree is keyed by its
// cleaned name and carries Depth 1,2,... bottom-to-top (upstream processStack,
// StateEncoder.java:413-424).
func TestProcessStateStackDepth(t *testing.T) {
	v := view.View{Stack: []view.StackView{
		{ID: 100, Name: "Lightning Bolt"},
		{ID: 101, Name: "Counterspell"},
	}}
	got := ProcessState(v, nil, 0, 0, "x")
	want := idsFor(func(f *Node) {
		st := f.SubFeatures("Stack", false)
		a := st.SubFeatures("Lightning Bolt", true)
		a.AddNumericFeature("Depth", 1, false)
		b := st.SubFeatures("Counterspell", true)
		b.AddNumericFeature("Depth", 2, false)
	})
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing stack id %d", id)
		}
	}
}

// TestProcessStateWatchersFromFields is the honest gate for the static/global
// remainder families Task 7 owns: GlobalWatchers, DayNight and CanPlayLand.
// view.PlayerView / view.View carry NO watcher counters, NO day/night state and
// NO land-drop-availability flag (confirmed by reading view/view.go), so the
// walker CANNOT emit any of them. The assertion is deliberately positive in
// BOTH directions: each family MUST be in the register (a silent drop would
// hide the gap) AND the walker must NOT emit it (an emission would be a false
// claim of support). The brief's original `emitted[fam] && !unsupported[fam]`
// form passes trivially when the family is absent from the register entirely,
// so it is not sufficient as a gate.
func TestProcessStateWatchersFromFields(t *testing.T) {
	emitted, unsupported := ProcessStateReport(coverageView(), nil, 0, 0, "x")
	for _, fam := range []string{"GlobalWatchers", "DayNight", "CanPlayLand"} {
		if !unsupported[fam] {
			t.Errorf("family %q not registered unsupported: a view cannot carry it, so a missing register entry is a silent claim of support", fam)
		}
		if emitted[fam] {
			t.Errorf("family %q reported emitted, but no view field carries it", fam)
		}
	}
}

// TestProcessStateIsDeterministic pins that ProcessState is a pure function of
// the View: two identical walks must produce the identical id set. It is a
// regression trap for any walk that ranges a Go map to build ids — map
// iteration order is randomised per range, so such a walk would build a
// different feature ORDER (and, for occurrence-keyed siblings, a different id
// set) across runs. The sorted walks make this hold today; if this ever fails,
// find the map range that reached an id and fix the WALK, never the test.
func TestProcessStateIsDeterministic(t *testing.T) {
	v := coverageView()
	a := ProcessState(v, nil, 0, 0, "x")
	b := ProcessState(v, nil, 0, 0, "x")
	if len(a) != len(b) {
		t.Fatalf("nondeterministic: %d vs %d", len(a), len(b))
	}
	for id := range a {
		if _, ok := b[id]; !ok {
			t.Fatalf("nondeterministic: id %d", id)
		}
	}
}

// TestProcessStateNoMapRange is the practical no-map-range guard. Go randomises
// map iteration order on every range, so a walk that ranged a map-valued View
// field (e.g. PlayerView.Pool) to BUILD ids would return a different id set
// across iterations and flake the determinism gate. ProcessState does not: the
// mana pool is read in the fixed manaPoolKeys order, and no other map-valued
// field reaches an id. This calls the walker 50 times over a view whose Pool
// maps are populated (coverageView sets {"W":2,"U":1} for the seat) and asserts
// the id set is byte-identical every time, which a map-backed walk cannot
// reliably pass. It is a behavioural guard, not a source-scanning AST test.
func TestProcessStateNoMapRange(t *testing.T) {
	v := coverageView()
	want := ProcessState(v, nil, 0, 0, "x")
	for i := 0; i < 50; i++ {
		got := ProcessState(v, nil, 0, 0, "x")
		if len(got) != len(want) {
			t.Fatalf("iteration %d: id-set size varies %d vs %d — a map range reached an id", i, len(got), len(want))
		}
		for id := range want {
			if _, ok := got[id]; !ok {
				t.Fatalf("iteration %d: id %d missing — a map range reached an id", i, id)
			}
		}
	}
}

// TestProcessStateExileZones pins the flat-view exile walk. Upstream
// processExile/processExileZone (StateEncoder.java:425-437) nests each zone's
// cards under the zone's name; view.PlayerView.Exile is one flat list with no
// per-zone name, so the port cannot reproduce that nesting. It emits ONE
// deterministic "ExileZone" wrapper under the root "Exile" subtree and records
// the per-zone-name gap as the ExileZoneNames caveat, then walks each exiled
// card via processCardInZone under its cleaned name.
func TestProcessStateExileZones(t *testing.T) {
	v := view.View{Players: []view.PlayerView{
		{ID: 0, Exile: []view.CardView{{ID: 30, Name: "Exiled Card", Types: "Sorcery"}}},
	}}
	got := ProcessState(v, nil, 0, 0, "x")
	want := idsFor(func(f *Node) {
		ex := f.SubFeatures("Exile", true)
		// upstream nests by exile-zone name; the view exposes a flat list, so
		// the zone name is not available -- record unsupported, emit the card
		// directly under a fixed "ExileZone" subfeature.
		z := ex.SubFeatures("ExileZone", true)
		c := z.SubFeatures("Exiled Card", true)
		c.AddFeature("Card")
		c.AddFeature("sorcery")
	})
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing exile id %d", id)
		}
	}
}
