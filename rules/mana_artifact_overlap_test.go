package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A Treasure is simultaneously an Artifact. Paying with it must retain both
// facts without double-counting its one unit in the pool.
func TestArtifactTreasureManaPaysCastWithBothProvenances(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Marut"), corpusAlternativeCard(t, "Shadow the Hedgehog"))
	marut, shadow := e.G.Zone(state.ZHand, 0)[0], e.G.Zone(state.ZHand, 0)[1]
	placeOnBattlefield(t, e, shadow)
	if e.G.Obj(shadow).Zone != state.ZBattlefield {
		t.Fatal("precondition: Shadow must be on battlefield")
	}
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "c_a_treasure_sac"})
	var tok state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && faceHasType(o, "Treasure") {
			tok = id
		}
	}
	if tok == 0 || !faceHasType(e.G.Obj(tok), "Artifact") || e.G.Obj(tok).Zone != state.ZBattlefield {
		t.Fatal("precondition: real corpus Treasure token is not an Artifact on the battlefield")
	}
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision")
	}
	act := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == tok {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatalf("Treasure mana ability not offered: %+v", d.Options)
	}
	submitChoices(t, e, act)
	if cd := e.Pending(); cd != nil && cd.Kind == decision.KChoose {
		submitChoices(t, e, 0)
	}
	if got := e.G.Players[0].ArtifactTyped[state.TypedTreasure].Total(); got != 1 {
		t.Fatalf("artifact+Treasure provenance = %d, want 1", got)
	}
	if got := e.G.Players[0].TypedMana[state.TypedTreasure].Total(); got != 1 {
		t.Fatalf("historical Treasure tally = %d, want unchanged at 1", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool = %d, want one unit, not two", got)
	}
	// Marut costs {8}; this one unit plus seven plain units all get spent.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 7})
	if e.G.Players[0].Pool.Total() != 8 {
		t.Fatal("precondition: eight units must be payable")
	}
	castMode(t, e, marut, "")
	o := e.G.Obj(marut)
	if o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: Marut must be cast onto stack, got %+v", o)
	}
	if o.ManaTreasureSpent != 1 || o.ManaArtifactSpent != 1 || o.ManaSpent != 8 {
		t.Fatalf("pay-time provenance total/Treasure/Artifact = %d/%d/%d, want 8/1/1", o.ManaSpent, o.ManaTreasureSpent, o.ManaArtifactSpent)
	}
	if _, ok := e.castSaAdmits("Card.CastSa Spell.ManaFromArtifact", marut); !ok {
		t.Fatal("Artifact Treasure spend failed CastSa predicate")
	}
	if _, ok := e.castSaAdmits("Card.CastSa Spell.ManaFromTreasure", marut); !ok {
		t.Fatal("Treasure tag lost on Artifact mana")
	}
	if got := e.spellsCastThisTurnMatching(0, "Card.CastSa Spell.ManaFromArtifact", 0); len(got) != 1 || got[0] != marut {
		t.Fatalf("cast count = %v, want Marut", got)
	}
	if !hasSplitSecond(e.derivedWith(marut, state.ZStack).Keywords) {
		t.Fatal("Shadow did not grant Split second to Treasure-paid Marut")
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after payment = %d, want 0", got)
	}
}
