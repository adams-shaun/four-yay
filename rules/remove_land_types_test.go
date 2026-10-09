package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// landTypesEngine is corpusEngine with the corpus universe bound as the
// Config's NameUniverse: the RemoveLandTypes strip's vocabulary is the
// corpus land-subtype list (chars.CorpusLandTypeWords over cfg.NameUniverse),
// so an engine without a universe has an EMPTY vocabulary and the strip --
// and any land-word AddTypes expansion -- cannot be exercised honestly.
func landTypesEngine(t *testing.T, reg *cards.Registry, extras0, extras1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	fill := func(n int) []*cards.Card {
		m, ok := reg.Lookup("Mountain")
		if !ok {
			t.Fatal("corpus fixture: Mountain missing")
		}
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = m
		}
		return out
	}
	cfg := Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		NameUniverse: reg.Universe(),
		Decks: [][]*cards.Card{append(append([]*cards.Card{}, extras0...), fill(40-len(extras0))...),
			append(append([]*cards.Card{}, extras1...), fill(40-len(extras1))...)}}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// TestZhaoMoonSlayerStripsLandTypes pins the ticket's own Level-B row:
// Zhao, the Moon Slayer's third static (AddType$ Mountain |
// RemoveLandTypes$ True | IsPresent$ Card.Self+counters_GE1_CONQUEROR)
// makes an opponent's nonbasic Dryad Arbor a Mountain AND strips its
// printed Forest land subtype (CR 613.1d -- the strip runs before the same
// effect's AddTypes), while Land, Creature and Dryad survive. Before the
// conqueror counter the IsPresent$ gate is closed and the Arbor still reads
// Forest with no Mountain.
func TestZhaoMoonSlayerStripsLandTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := landTypesEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Zhao, the Moon Slayer")},
		[]*cards.Card{lookup(t, reg, "Dryad Arbor")})
	zhao := moveByName(t, e, 0, "Zhao, the Moon Slayer", state.ZBattlefield)
	if zhao == 0 {
		t.Fatal("Zhao, the Moon Slayer not placed")
	}
	arbor := moveByName(t, e, 1, "Dryad Arbor", state.ZBattlefield)
	if arbor == 0 {
		t.Fatal("Dryad Arbor not placed")
	}

	// Precondition: with no conqueror counter the gate is closed and the
	// printed land type is still there.
	before := e.Derived(arbor).Types
	if !slices.Contains(before, "Forest") {
		t.Fatalf("pre-counter Dryad Arbor types = %v, want Forest present (gate closed)", before)
	}
	if slices.Contains(before, "Mountain") {
		t.Fatalf("pre-counter Dryad Arbor types = %v, want no Mountain (gate closed)", before)
	}

	e.emit(events.Event{Kind: events.CounterChange, Obj: zhao, Counter: "CONQUEROR", Amount: 1})

	after := e.Derived(arbor).Types
	if !slices.Contains(after, "Mountain") {
		t.Fatalf("post-counter Dryad Arbor types = %v, want Mountain present", after)
	}
	if slices.Contains(after, "Forest") {
		t.Fatalf("post-counter Dryad Arbor types = %v, want Forest stripped (RemoveLandTypes)", after)
	}
	for _, want := range []string{"Land", "Creature", "Dryad"} {
		if !slices.Contains(after, want) {
			t.Fatalf("post-counter Dryad Arbor types = %v, want %s to survive the strip", after, want)
		}
	}
	replayCheck(t, e, cfg)
}

// TestRemoveLandTypesStripsOnlyLandSubtypes is the strip's boundary, the
// mirror of TestContinuousRemoveCardTypesKeepsOnlySupertypes: a
// RemoveLandTypes-only layer effect removes every word in the board's
// land-type vocabulary and keeps the Land card type and the Basic
// supertype (CR 205.2/613.1d) -- no special-casing, only the vocabulary
// match.
func TestRemoveLandTypesStripsOnlyLandSubtypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := landTypesEngine(t, reg, []*cards.Card{lookup(t, reg, "Forest")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Forest", state.ZBattlefield)
	if id == 0 {
		t.Fatal("Forest not placed")
	}
	before := e.Derived(id).Types
	if !slices.Contains(before, "Basic") || !slices.Contains(before, "Land") || !slices.Contains(before, "Forest") {
		t.Fatalf("printed Forest types = %v, want [Basic Land Forest]", before)
	}
	e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LType,
		RemoveLandTypes: true})
	after := e.Derived(id).Types
	if slices.Contains(after, "Forest") {
		t.Fatalf("after RemoveLandTypes Forest types = %v, want Forest stripped", after)
	}
	for _, want := range []string{"Basic", "Land"} {
		if !slices.Contains(after, want) {
			t.Fatalf("after RemoveLandTypes Forest types = %v, want %s to survive (card type/supertype)", after, want)
		}
	}
}

// TestBloodMoonStripsLandTypes pins the family's simplest no-gate carrier:
// Blood Moon turns every nonbasic land into a Mountain and strips its
// printed land types, while a basic land in the same board keeps its types
// (Affected$ Land.nonBasic).
func TestBloodMoonStripsLandTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := landTypesEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Blood Moon")},
		[]*cards.Card{lookup(t, reg, "Dryad Arbor"), lookup(t, reg, "Forest")})
	arbor := moveByName(t, e, 1, "Dryad Arbor", state.ZBattlefield)
	if arbor == 0 {
		t.Fatal("Dryad Arbor not placed")
	}
	basic := moveByName(t, e, 1, "Forest", state.ZBattlefield)
	if basic == 0 {
		t.Fatal("basic Forest not placed")
	}

	// Precondition: both lands entered with their printed types, read
	// BEFORE Blood Moon enters the battlefield.
	preArbor := e.Derived(arbor).Types
	if !slices.Contains(preArbor, "Forest") || slices.Contains(preArbor, "Mountain") {
		t.Fatalf("pre-Blood Moon Dryad Arbor types = %v, want printed Forest, no Mountain", preArbor)
	}
	preBasic := e.Derived(basic).Types
	if !slices.Contains(preBasic, "Forest") {
		t.Fatalf("pre-Blood Moon basic Forest types = %v, want Forest present", preBasic)
	}

	if moveByName(t, e, 0, "Blood Moon", state.ZBattlefield) == 0 {
		t.Fatal("Blood Moon not placed")
	}

	afterArbor := e.Derived(arbor).Types
	if !slices.Contains(afterArbor, "Mountain") || slices.Contains(afterArbor, "Forest") {
		t.Fatalf("post-Blood Moon Dryad Arbor types = %v, want Mountain present, Forest stripped", afterArbor)
	}
	afterBasic := e.Derived(basic).Types
	if !slices.Contains(afterBasic, "Forest") || slices.Contains(afterBasic, "Mountain") {
		t.Fatalf("post-Blood Moon basic Forest types = %v, want unchanged (basic is not Affected$ Land.nonBasic)", afterBasic)
	}
	replayCheck(t, e, cfg)
}
