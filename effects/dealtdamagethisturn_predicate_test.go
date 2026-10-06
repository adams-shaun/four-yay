package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDealtDamageThisTurnPredicate is the effects leaf for Forge's
// dealtDamageThisTurn -- the SOURCE side of combat damage, distinct from
// wasDealtDamageThisTurn (the recipient side). It reads
// state.Object.DamageDealtThisTurn, the record events.Apply's
// DamageProvenance fold appends to. Corpus carriers: Avenging Arrow,
// Executioner's Swing, Restore the Peace, Red Guardian, Super Soldier and
// Treacherous Greed's sacrifice cost. Asserting the precondition (the two
// candidate objects actually differ in the record) keeps the test from
// passing vacuously if the predicate matched nothing.
func TestDealtDamageThisTurnPredicate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})

	dealt := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	dealt.DamageDealtThisTurn = append(dealt.DamageDealtThisTurn, state.DamageDealtRecord{Amount: 2})
	silent := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	if len(dealt.DamageDealtThisTurn) == len(silent.DamageDealtThisTurn) {
		t.Fatal("precondition failed: the two candidates must differ in DamageDealtThisTurn")
	}

	if !MatchesObjectCtx(g, "Creature.dealtDamageThisTurn", dealt, SpecContext{You: 0}) {
		t.Error("Creature.dealtDamageThisTurn must match a creature that dealt damage this turn")
	}
	if MatchesObjectCtx(g, "Creature.dealtDamageThisTurn", silent, SpecContext{You: 0}) {
		t.Error("Creature.dealtDamageThisTurn must not match a creature that dealt no damage this turn")
	}
	// The census and the matcher share the one recogniser: an unknown-word
	// regression here would silently make every carrier's filter inert.
	if got := UnknownPredicates("Creature.dealtDamageThisTurn"); len(got) != 0 {
		t.Errorf("UnknownPredicates(Creature.dealtDamageThisTurn) = %v, want empty", got)
	}
	// The recipient-side sibling stays a separate predicate (a creature that
	// was dealt damage is not thereby one that dealt damage).
	if MatchesObjectCtx(g, "Creature.wasDealtDamageThisTurn", silent, SpecContext{You: 0}) {
		t.Error("dealtDamageThisTurn must not widen wasDealtDamageThisTurn")
	}
}
