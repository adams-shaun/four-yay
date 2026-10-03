package effects

// The generic Charm mode loop's mid-mode suspension guard, at the primitive
// level (task cli-20260922T225138Z-a850f8be). The rules-level carriers in
// rules/charm_generic_suspend_test.go pin the engine-visible result; this
// file pinches the guard itself: once a chosen mode's own chain has posed a
// mid-resolution ask, effCharm must NOT run the remaining modes while that
// ask is pending -- it reports them through Host.SuspendCharmRest so they run
// once the answer lands.
//
// The carrier: a Charm whose first chosen mode is a NESTED Charm (which poses
// its own KModes ask and, under this double, suspends) and whose second mode
// is an observable LoseLife. With the guard the LoseLife never runs on the
// suspended pass; without it the loop walks straight into the next mode and
// the life drops while the decision is still outstanding.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// charmSuspendHost records the SuspendCharmRest reports the double would
// otherwise drop (fakeHost's is a no-op).
type charmSuspendHost struct {
	fakeHost
	restCalls []string
}

func (h *charmSuspendHost) SuspendCharmRest(_ *cards.SA, rest []string) {
	h.restCalls = append(h.restCalls, rest...)
}

// TestCharmModeLoopRunsTheRestAfterTheSuspension is the positive control: the
// same carrier re-entered with Ctx.Modes carrying only the rest runs DoLose
// exactly once and poses no further ask.
func TestCharmModeLoopRunsTheRestAfterTheSuspension(t *testing.T) {
	h := &charmSuspendHost{}
	h.g = state.NewGame(names(2))
	h.g.Players[0].Life = 20
	svars := map[string]string{
		"DoAsk":  "DB$ Charm | Choices$ InnerA,InnerB",
		"InnerA": "DB$ GainLife | Defined$ You | LifeAmount$ 1",
		"InnerB": "DB$ GainLife | Defined$ You | LifeAmount$ 2",
		"DoLose": "DB$ LoseLife | Defined$ You | LifeAmount$ 5",
	}
	src := sa(t, "SP$ Charm | Choices$ DoAsk,DoLose")
	Resolve(h, &Ctx{Controller: 0, SVars: svars, Modes: []string{"DoLose"}}, src)
	if h.g.Players[0].Life != 15 {
		t.Fatalf("life = %d, want 15: the reported rest did not run once after the answer", h.g.Players[0].Life)
	}
	if len(h.restCalls) != 0 {
		t.Fatalf("rest reports = %v, want none on an unsuspended pass", h.restCalls)
	}
}
