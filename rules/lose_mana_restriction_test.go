package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestHorizonStonePreservesRestrictedManaProvenance(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675320, "Horizon Stone")
	if o := e.G.Obj(ids[0]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Horizon Stone must be on the battlefield")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 2, Counter: "R", Text: events.ManaRestrictionText("Spell", ids[0])})
	if got := e.G.Players[0].RestrictedMana; len(got) != 1 || got[0].Amount != 2 || got[0].Valid != "Spell" {
		t.Fatalf("precondition: restricted batch = %+v, want two Spell-only red mana", got)
	}
	e.finishStepBoundary(state.StepMain1, state.StepBeginCombat)
	p := e.G.Players[0]
	if p.Pool[state.MC] != 2 || p.Pool[state.MR] != 0 || len(p.RestrictedMana) != 1 || p.RestrictedMana[0].Color != "C" || p.RestrictedMana[0].Amount != 2 || p.RestrictedMana[0].Valid != "Spell" {
		t.Fatalf("converted restricted batch lost its provenance: pool=%v restrictions=%+v", p.Pool, p.RestrictedMana)
	}
	replayCheck(t, e, cfg)
}
