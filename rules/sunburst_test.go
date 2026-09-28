package rules

// kw:Sunburst (CR 702.47; task agent-20260919T055500Z-a4cd7643): "This object
// enters with a +1/+1 counter on it for each color of mana spent to cast it.
// If it isn't a creature, it enters with that many charge counters instead."
//
// A PRINTED K:Sunburst line is expanded cards-side (cards/kw_sunburst.go)
// into the entry Repl whose body puts Count$Converge counters with the kind
// decided on the printed face's types; the layer-6 `DB$ Animate | Keywords$
// Sunburst` grant shape (Solar Array, Lux Artillery), whose printed face
// never carries the line, is covered rules-side by sunburstEntryMatch, which
// skips printed carriers so one entry counts once either way. The count is
// the existing CR 107.4f converge head fed by the pay-time FlagConverged
// CastInfo (rules/cast.go's faceWantsConverge gate, widened to cover sunburst
// faces; sunburstGrantOut is the additional arm for the grant-delivered
// case). Every corpus carrier here is loaded from the REAL registry -- the
// scripts are GPL and never committed.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// sunburstEngine builds a two-seat fixture game whose seat 0 deck is the named
// REAL corpus cards (looked up in the registry) padded with Mountains, moves
// the first one into seat 0's hand and drives to Main 1 with a fresh priority
// ask. Returns the engine, its config and the moved card's id.
func sunburstEngine(t *testing.T, reg *cards.Registry, seed uint64, names ...string) (*Engine, Config, state.ObjID) {
	t.Helper()
	deck := make([]*cards.Card, 0, len(names))
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus fixture: %s missing", name)
		}
		deck = append(deck, c)
	}
	deck = append(deck, mountainDeck(t, 40-len(deck))...)
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, names[0], state.ZHand)
	e.pending = nil
	e.priorityRound()
	return e, cfg, id
}

// TestSunburstEtchedOracleEntersWithP1P1PerColour pins the creature half on
// the real corpus carrier Etched Oracle (Artifact Creature, {4}): {4} paid
// with two colours (RRGG) enters with exactly 2 +1/+1 counters; the SAME cast
// paid one colour (RRRR) enters with exactly 1. Two distinct values on
// purpose -- the defect is a silent zero and a single-value test can pass by
// coincidence. A colourless-only cast (CCCC) enters with none, proving the
// count is colours, not mana.
func TestSunburstEtchedOracleEntersWithP1P1PerColour(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	t.Run("two colours of mana", func(t *testing.T) {
		e, cfg, id := sunburstEngine(t, reg, seedTossSeat0(151), "Etched Oracle")
		// Precondition: the real carrier is a creature with the keyword, or the
		// P1P1 assertion below would not exercise the creature branch at all.
		o := e.G.Obj(id)
		if !o.Face().IsCreature() || !o.Face().HasKeyword("Sunburst") {
			t.Fatalf("precondition: Etched Oracle is creature=%v sunburst=%v, want both true",
				o.Face().IsCreature(), o.Face().HasKeyword("Sunburst"))
		}
		addMana(t, e, 0, "RRGG") // {4} paid 2 R + 2 G
		castFirst(t, e, "cast")
		passUntilStackEmpty(t, e, 20)
		o = e.G.Obj(id)
		if o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Oracle zone=%s, want battlefield", o.Zone)
		}
		if got := o.Counter("P1P1"); got != 2 {
			t.Fatalf("Oracle P1P1=%d, want 2 (two colours spent)", got)
		}
		if got := o.Counter("CHARGE"); got != 0 {
			t.Fatalf("Oracle CHARGE=%d, want 0 (a creature gets +1/+1, not charge)", got)
		}
		if o.ConvergeColours != 2 {
			t.Fatalf("Oracle ConvergeColours=%d, want 2", o.ConvergeColours)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("one colour of mana", func(t *testing.T) {
		e, cfg, id := sunburstEngine(t, reg, seedTossSeat0(157), "Etched Oracle")
		addMana(t, e, 0, "RRRR") // {4} paid 4 R
		castFirst(t, e, "cast")
		passUntilStackEmpty(t, e, 20)
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Oracle zone=%s, want battlefield", o.Zone)
		}
		if got := o.Counter("P1P1"); got != 1 {
			t.Fatalf("Oracle P1P1=%d, want 1 (one colour spent)", got)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("colourless mana places none", func(t *testing.T) {
		e, cfg, id := sunburstEngine(t, reg, seedTossSeat0(163), "Pentad Prism")
		addMana(t, e, 0, "CC") // {2} paid entirely as {C}
		castFirst(t, e, "cast")
		passUntilStackEmpty(t, e, 20)
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Prism zone=%s, want battlefield", o.Zone)
		}
		if got := o.Counter("CHARGE"); got != 0 {
			t.Fatalf("Prism CHARGE=%d, want 0 (colourless is not a colour)", got)
		}
		replayCheck(t, e, cfg)
	})
}

// TestSunburstPentadPrismEntersWithChargePerColour pins the noncreature half
// on the real corpus carrier Pentad Prism (Artifact, {2}): it is NOT a
// creature, so the SAME colour count arrives as CHARGE counters, not +1/+1 --
// the branch distinction a single-card test could not see.
func TestSunburstPentadPrismEntersWithChargePerColour(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, id := sunburstEngine(t, reg, seedTossSeat0(167), "Pentad Prism")
	o := e.G.Obj(id)
	if o.Face().IsCreature() {
		t.Fatalf("precondition: Pentad Prism is a creature, want noncreature (the charge branch)")
	}
	if !o.Face().HasKeyword("Sunburst") {
		t.Fatal("precondition: Pentad Prism lacks the printed Sunburst keyword")
	}
	addMana(t, e, 0, "RG") // {2} paid one R + one G
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 20)
	o = e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Prism zone=%s, want battlefield", o.Zone)
	}
	if got := o.Counter("CHARGE"); got != 2 {
		t.Fatalf("Prism CHARGE=%d, want 2 (two colours spent)", got)
	}
	if got := o.Counter("P1P1"); got != 0 {
		t.Fatalf("Prism P1P1=%d, want 0 (a noncreature gets charge counters)", got)
	}
	replayCheck(t, e, cfg)
}

// TestSunburstAnimateGrantOnSolarArray pins the grant-delivered shape: Solar
// Array's "{T}: Add one mana of any color. When you next cast an artifact
// spell this turn, that spell gains sunburst." is the real corpus script. The
// Animate grant lands on the spell AFTER payment, so the printed-keyword arm
// cannot see it at pay time -- sunburstGrantOut is what arms the capture.
// Cast a real Ornithopter of Paradise ({2}, Artifact Creature, 0/2, NO printed
// Sunburst) with two colours; the ONLY source of the keyword is Solar Array's
// grant, so this test fails vacuously if the grant path does not run.
func TestSunburstAnimateGrantOnSolarArray(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, thopterID := sunburstEngine(t, reg, seedTossSeat0(173), "Ornithopter of Paradise", "Solar Array")
	to := e.G.Obj(thopterID)
	if to.Face().HasKeyword("Sunburst") {
		t.Fatal("precondition: Ornithopter has printed Sunburst; the test would not exercise the grant")
	}
	// Put Solar Array on the battlefield, then advance to seat 0's NEXT turn so
	// the {T} mana ability is no longer summoning-sick.
	solarID := moveByName(t, e, 0, "Solar Array", state.ZBattlefield)
	if solarID == 0 {
		t.Fatal("precondition: Solar Array not found")
	}
	driveToStep(t, e, e.G.Turn+2, 0, state.StepMain1)
	toMain1(t, e)
	// Activate "{T}: Add one mana of any color" -- this also sets up the
	// one-shot "next artifact spell gains sunburst" Effect.
	submitChoices(t, e, activateOption(t, e, solarID))
	// Pin the produced colour to green so the later payment is deterministic:
	// the pool is one G from Solar Array plus R added below = exactly two
	// colours spent on the {2} cast.
	submitChoices(t, e, manaOption(t, e.Pending(), "G"))
	passUntilStackEmpty(t, e, 20)
	// Now cast the artifact spell with two colours: one G from Solar Array plus
	// one R.
	addMana(t, e, 0, "R")
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(thopterID)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Ornithopter zone=%s, want battlefield", o.Zone)
	}
	if o.Counter("P1P1") != 2 {
		t.Fatalf("Ornithopter P1P1=%d, want 2 (sunburst granted by Solar Array, two colours spent)", o.Counter("P1P1"))
	}
	if o.ConvergeColours != 2 {
		t.Fatalf("Ornithopter ConvergeColours=%d, want 2 (sunburstGrantOut armed the capture)", o.ConvergeColours)
	}
	replayCheck(t, e, cfg)
}

// answerManaAutomatically is no longer needed.
