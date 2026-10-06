// Reproduces feedback report 20261006T100041Z-ec09a3af (Party Thrasher: "unable
// to play the selected card from exile"). The snapshot under
// testdata/feedback/20261006T100041Z-ec09a3af/ is replayed to every recorded
// intent: seat 0 is asked which exiled card Party Thrasher's may-play covers.
// It is the external test package: feedback imports the engine tier.
package rules_test

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil/feedback"
	"github.com/adams-shaun/gorge/state"
)

// ragavanObj is the exiled card the reporter picked (obj 98).
const ragavanObj = state.ObjID(98)

func TestFeedbackRepro20261006T100041Z_ec09a3af(t *testing.T) {
	e := feedback.EngineAt(t, filepath.Join("testdata", "feedback", "20261006T100041Z-ec09a3af"), -1)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 {
		t.Fatalf("pending = %#v, want seat 0's choose of the exiled card", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
		t.Fatalf("Submit choose [1]: %v", err)
	}
	pd := e.Pending()
	if pd == nil || pd.Kind != decision.KPriority {
		t.Fatalf("after the choice pending = %#v, want priority", pd)
	}
	// Preconditions: Ragavan is in exile, the pool is empty, and the plain
	// priority options carry the mayplay cast (so the gap is the payment
	// action, not the offer).
	if o := e.G.Obj(ragavanObj); o == nil || o.Zone != state.ZExile {
		t.Fatalf("obj %d = %#v, want a card in exile", ragavanObj, o)
	}
	if e.G.Players[0].Pool != (state.Mana{}) {
		t.Fatalf("pool = %v, want empty", e.G.Players[0].Pool)
	}
	offered := false
	for _, o := range e.PotentialActions(0) {
		if o.Kind == "cast" && o.Obj == ragavanObj && o.Mode == "mayplay" {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("PotentialActions(0) does not offer cast %d mode=mayplay", ragavanObj)
	}

	var action *decision.PaymentAction
	for _, a := range e.EnsurePaymentActions() {
		if a.Cast.Object == ragavanObj {
			a := a
			action = &a
		}
	}
	if action == nil {
		t.Fatalf("no payment action for the may-play cast of %d", ragavanObj)
	}
	if action.Cast.Origin != "exile" || len(action.Plans) == 0 {
		t.Fatalf("action = %#v, want Origin exile with a plan", action)
	}
	if err := e.Submit(decision.Intent{Seq: pd.Seq, Player: pd.Player, Announce: &decision.AnnounceSelection{ActionID: action.ID}}); err != nil {
		t.Fatalf("Submit announce: %v", err)
	}
	w := e.Pending()
	if w == nil || w.ManaPayment == nil {
		t.Fatalf("after announce pending = %#v, want the mana window", w)
	}
	fill := -1
	for _, o := range w.Options {
		if o.Kind == decision.OptAutoFill {
			fill = o.Index
		}
	}
	if fill < 0 {
		t.Fatalf("mana window has no auto-fill option: %#v", w.Options)
	}
	if err := e.Submit(decision.Intent{Seq: w.Seq, Player: w.Player, Choices: []int{fill}}); err != nil {
		t.Fatalf("Submit auto-fill: %v", err)
	}
	onStack := false
	for _, id := range e.G.Stack {
		if id == ragavanObj {
			onStack = true
		}
	}
	if !onStack {
		t.Fatalf("obj %d is not on the stack after paying (zone %v, pending %#v)", ragavanObj, e.G.Obj(ragavanObj).Zone, e.Pending())
	}
}
