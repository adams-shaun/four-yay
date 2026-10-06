package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestLoseManaCompetingCarriersApplyOnlyTheChosenReplacement: Horizon Stone
// ("becomes colorless instead") and Ozai ("becomes red instead") both apply
// to one player's proposed ManaClear, so the affected player chooses which
// applies (CR 616.1). Each is ReplacementResult$ Replaced and converts all
// the unspent mana, so after the chosen one applies nothing is lost any more
// and the other is no longer applicable at this boundary (CR 616.1f): the
// answer alone decides the pool's colour.
func TestLoseManaCompetingCarriersApplyOnlyTheChosenReplacement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		chosen string
		slot   int
	}{
		{"Ozai, the Phoenix King", state.MR},
		{"Horizon Stone", state.MC},
	} {
		t.Run(tc.chosen, func(t *testing.T) {
			reg := testutil.CorpusRegistry(t)
			e, cfg, ids := realCardEngine(t, reg, 8675320, "Horizon Stone", "Ozai, the Phoenix King")
			if len(ids) != 2 {
				t.Fatalf("precondition: got %d carrier objects, want 2", len(ids))
			}
			for i, id := range ids {
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: replacement %d is not on battlefield", i)
				}
			}
			e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 2, Counter: "G"})
			if p := e.G.Players[0].Pool; p.Total() != 2 || p[state.MG] != 2 {
				t.Fatal("precondition: two unspent green mana not established")
			}
			e.pending = nil // Exercise the step-boundary event directly, outside priority.
			e.finishStepBoundary(state.StepMain1, state.StepBeginCombat)
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
				t.Fatalf("initial pending = %+v, want replacement-order choice over both carriers", d)
			}
			if p := e.G.Players[0].Pool; p[state.MG] != 2 {
				t.Fatalf("pool changed before the order choice was answered: %v", p)
			}
			chosen := state.ObjID(0)
			for _, id := range ids {
				if o := e.G.Obj(id); o != nil && o.Card != nil && len(o.Card.Faces) > 0 && o.Card.Faces[0].Name == tc.chosen {
					chosen = id
				}
			}
			if chosen == 0 || optionForObj(d, chosen) < 0 {
				t.Fatalf("precondition: %s not among replacement options: %+v", tc.chosen, d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{optionForObj(d, chosen)}}); err != nil {
				t.Fatal(err)
			}
			if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
				t.Fatalf("the unchosen replacement must not be offered again at this boundary: %+v", d)
			}
			if got := e.G.Players[0].Pool; got.Total() != 2 || got[tc.slot] != 2 {
				t.Fatalf("pool after choosing %s = %v, want two mana in slot %d only", tc.chosen, got, tc.slot)
			}
			replayCheck(t, e, cfg)
		})
	}
}
