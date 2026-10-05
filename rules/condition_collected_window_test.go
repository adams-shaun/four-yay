package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCollectEvidenceWindowChannel pins the Engine half of the
// ConditionDefined$ Collected group's channel (task condition-collected):
// after a real CollectEvidence<X> payment, Engine.CostMovesInWindow with
// events.CostMoveEvidence must return the exiled evidence card, and the
// Discarded/Returned kinds must NOT (the kind selects the cost marker, not a
// blanket window read). It mirrors the Urgent Necropsy fixture: the spell's
// evidence is owed on the target union, so the artifact target sizes X.
func TestCollectEvidenceWindowChannel(t *testing.T) {
	t.Parallel()
	necropsySrc := alltargetedCorpusText(t, "u/urgent_necropsy.txt")
	artifactSrc := "Name:Gold Myr\nManaCost:2\nTypes:Artifact Creature Myr\nPT:1/1\nOracle:x\n"
	grave3Src := "Name:Big Bones\nManaCost:3\nTypes:Artifact\nOracle:x\n"
	grave2Src := "Name:Small Bones\nManaCost:2\nTypes:Artifact\nOracle:x\n"

	e, _, necro := newFixtureDeck(t, 76, necropsySrc, artifactSrc, grave3Src, grave2Src)
	artifact := putCreature(t, e, 0, artifactSrc)
	mv3 := moveSeeded(t, e, 0, grave3Src, state.ZGraveyard)
	moveSeeded(t, e, 0, grave2Src, state.ZGraveyard)
	addMana(t, e, 0, "BBBG")
	e.Advance()
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == necro {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("precondition: Urgent Necropsy not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)
	// Root artifact target.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind == "cast_sub" {
		t.Fatalf("precondition: root artifact ask = %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == artifact {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("precondition: artifact not offered: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	// Creature chain sub (Min 0): elect zero.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "cast_sub" {
		t.Fatalf("precondition: chain sub ask = %+v", d)
	}
	submitChoices(t, e)
	// Evidence ask: elect the MV-3 card.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "evidence" {
		t.Fatalf("precondition: evidence ask = %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)

	// Preconditions: the elected card really left the graveyard for exile, and
	// the resolving spell is still on the stack so its activation window is
	// intact for the read below.
	o := e.G.Obj(mv3)
	if o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: evidence card %d zone = %v, want Exile", mv3, o)
	}
	if so := e.G.Obj(necro); so == nil || so.Zone != state.ZStack {
		t.Fatalf("precondition: the resolving spell %d is not on the stack: %+v", necro, so)
	}

	got := e.CostMovesInWindow(necro, events.CostMoveEvidence)
	found := false
	for _, id := range got {
		if id == mv3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("CostMovesInWindow(CostMoveEvidence) = %v, want it to contain the exiled evidence card %d", got, mv3)
	}
	// The kind selects the marker: the same window holds no cost discard or
	// cost return, so those kinds stay empty rather than echoing the evidence.
	if d := e.CostMovesInWindow(necro, events.CostMoveDiscard); len(d) != 0 {
		t.Fatalf("CostMovesInWindow(CostMoveDiscard) = %v, want empty (kind must select the marker)", d)
	}
	if r := e.CostMovesInWindow(necro, events.CostMoveReturn); len(r) != 0 {
		t.Fatalf("CostMovesInWindow(CostMoveReturn) = %v, want empty (kind must select the marker)", r)
	}
}
