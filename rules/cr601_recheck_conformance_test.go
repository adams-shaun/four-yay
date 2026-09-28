package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCR601RecheckAfterXAndLegalTarget is genuinely 601.2e, not a 601.2c
// shortage or a 601.2h failure. Sanctum Prelate names 2. Power Sink ({X}{U})
// can be proposed from hand (mana value 1), but choosing X=1 gives the spell
// mana value 2 (202.3e), forbidden by Prelate. Targeting Aether Vial succeeds;
// Power Sink requires no division (601.2d). The recheck must reverse it.
//
// The restriction becomes applicable because of the PROPOSAL'S X choice,
// not an injected opponent action between 601.2d and 601.2e. No player gets
// such a response window. X=2 is the legal control, with the same board and
// target. Power Sink is an explicit corpus supplement to the repo deck.
//
// This is end-to-end conformance, not proof that adding castRestricted at
// one line fixes it: the current cmc predicate also ignores stack X.
func TestCR601RecheckAfterXAndLegalTarget(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, x := range []int{1, 2} {
		name := "forbidden_X1"
		if x == 2 {
			name = "legal_X2_control"
		}
		t.Run(name, func(t *testing.T) {
			// Graduated: the forbidden_proposal and legal control both pass with
			// the conformance flag on, so both run in the ordinary lane.
			e := crAbortEngine(t, reg, "ur-delver", "Power Sink")
			prelate := crAbortMove(t, e, 1, "Sanctum Prelate", state.ZBattlefield)
			e.emit(events.Event{Kind: events.Choose, Obj: prelate, Counter: "number", Amount: 2})
			guard := false
			for _, st := range e.G.Obj(prelate).Face().Statics {
				if st.Mode == "CantBeCast" && st.Params["ValidCard"] == "Card.nonCreature+cmcEQChosen" {
					guard = true
				}
			}
			if !guard {
				t.Fatalf("CR 601.2e Power Sink/Sanctum Prelate seq %d: restriction fixture changed", len(e.L.Events))
			}
			// Earlier spell as setup, before the Pyromancer probe is present.
			target := crAbortMove(t, e, 1, "Aether Vial", state.ZHand)
			e.emit(events.Event{Kind: events.PutOnStack, Obj: target, Player: 1, From: state.ZHand, To: state.ZStack})
			pyro := crAbortPyromancer(t, e)
			id := crAbortMove(t, e, 0, "Power Sink", state.ZHand)
			f := e.G.Obj(id).Face()
			sa := f.SpellAbility()
			if f.ManaCost != "X U" || sa == nil || sa.API != "Counter" || sa.Params["TargetType"] != "Spell" || sa.Params["ValidTgts"] != "Card" || sa.Params["DividedAsYouChoose"] != "" {
				t.Fatalf("CR 601.2c-e Power Sink seq %d: targeted, non-dividing fixture changed", len(e.L.Events))
			}
			e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 3})
			e.askPriority(0)
			before, start := e.G.Clone(), len(e.L.Events)
			checked++ // examined legal AND illegal proposals, never only failures
			crAbortAnswer(t, e, "Power Sink", crAbortOption(t, e, "Power Sink", "cast", id))
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || d.Source != id {
				t.Fatalf("CR 601.2b Power Sink seq %d: no X decision: %+v", start, d)
			}
			idx := -1
			for _, opt := range d.Options {
				if opt.Kind == "x" && opt.Amount == x {
					idx = opt.Index
				}
			}
			if idx < 0 {
				t.Fatalf("CR 601.2b Power Sink seq %d: no X=%d proposal choice", d.Seq, x)
			}
			crAbortAnswer(t, e, "Power Sink", idx)
			d = e.Pending()
			if d == nil || d.Kind != decision.KTarget || d.Source != id {
				t.Fatalf("CR 601.2c Power Sink seq %d: expected legal target decision: %+v", start, d)
			}
			targetSeq := d.Seq
			t.Logf("MEASURED Power Sink X=%d target seq %d: pendingCast=%t pool=%v queued triggers=%d", x, targetSeq, e.cast != nil, e.G.Players[0].Pool, len(e.pendingTriggers))
			crAbortAnswer(t, e, "Power Sink", crAbortOption(t, e, "Power Sink", "spell", target))
			chosen := false
			for _, ev := range e.L.Events[start:] {
				if ev.Kind == events.TargetsChosen && ev.Obj == id && slices.Contains(ev.IDs, target) {
					chosen = true
					t.Logf("MEASURED CR 601.2c Power Sink X=%d: target ask seq %d answered with Aether Vial at seq %d; 601.2d requires no division", x, targetSeq, ev.Seq)
				}
			}
			if !chosen {
				t.Fatalf("CR 601.2c Power Sink seq %d: no recorded successful target selection", start)
			}
			if x == 1 {
				crAbortUnchanged(t, e, before, start, "Power Sink (X=1, Prelate=2; CR 601.2e/202.3e)", pyro)
			} else {
				pushes := 0
				for _, ev := range e.L.Events[start:] {
					if ev.Kind == events.TriggerPush && ev.Obj == pyro {
						pushes++
					}
				}
				if e.G.Obj(id).Zone != state.ZStack || e.G.Obj(id).X != 2 || e.G.Players[0].Pool.Total() != 0 || pushes != 1 {
					t.Errorf("CR 601.2e/202.3e Power Sink seq %d: legal mana-value-3 control failed: zone=%s X=%d pool=%v Pyromancer=%d", start, e.G.Obj(id).Zone, e.G.Obj(id).X, e.G.Players[0].Pool, pushes)
				}
				t.Logf("MEASURED Power Sink X=2 seq %d: legal control Pyromancer pushes=%d", start, pushes)
			}
		})
	}
	if checked == 0 {
		t.Fatal("CR 601.2e Power Sink seq 0: no X/restriction cases examined")
	}
}
