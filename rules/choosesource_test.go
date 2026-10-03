package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// resolveChooseSource resolves the SP$ ChooseSource head of a spell object
// controlled by its owner, answers the posed KChoose with the option whose
// object id is want, and asserts the answer landed on the spell's own Chosen
// list (the binding the registered replacement's ValidSource$ gate reads).
func resolveChooseSource(t *testing.T, e *Engine, spell state.ObjID, want state.ObjID) {
	t.Helper()
	o := e.G.Obj(spell)
	sa := o.Face().SpellAbility()
	effects.Resolve(e, &effects.Ctx{Source: spell, Controller: o.Controller, SVars: o.Face().SVars}, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("ChooseSource did not pose a KChoose, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == want {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("object %d not among ChooseSource options %+v", want, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit ChooseSource answer: %v", err)
	}
	if len(o.Chosen) != 1 || o.Chosen[0].Obj != want {
		t.Fatalf("chosen = %+v, want [%d]", o.Chosen, want)
	}
}

// TestChooseSourceNoCandidateDoesNotWedge pins the zero-candidate shape: a
// ChooseSource whose Choices$ filter matches nothing poses no decision (the
// empty-answer-only KChoose is absorbed by effects.Ask), records no chosen
// source, and the follow-up replacement therefore never fires — damage flows
// through untouched rather than the seat being stranded on an ask it cannot
// answer.
func TestChooseSourceNoCandidateDoesNotWedge(t *testing.T) {
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	// Only a green creature exists, so Card.BlueSource matches nothing — not
	// even the (white/red) Deflecting Palm itself.
	onBoard(t, e, 1, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	palm := e.G.AddObject(mustCorpusCard(t, reg, "Deflecting Palm"), 0)
	palm.Zone = state.ZStack
	sa := palm.Face().SpellAbility()
	redSpec := *sa
	redSpec.Params = map[string]string{}
	for k, v := range sa.Params {
		redSpec.Params[k] = v
	}
	redSpec.Params["Choices"] = "Card.BlueSource"
	effects.Resolve(e, &effects.Ctx{Source: palm.ID, Controller: 0, SVars: palm.Face().SVars}, &redSpec)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("zero-candidate ChooseSource posed a decision: %+v", d)
	}
	if len(palm.Chosen) != 0 {
		t.Fatalf("chosen = %+v, want nothing", palm.Chosen)
	}
}
