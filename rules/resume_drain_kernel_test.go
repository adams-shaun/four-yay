package rules

// Kernel-era restoration of resume_drain_suspended_test.go: resumeTriggerDrain
// is inert while a resolution's mid-resolution ask is outstanding. On the
// kernel the resolving object stays on the stack with its ask posed (a held
// tape checkpoint) instead of a suspended resume frame; the drain must still
// run no SBA, place no trigger, grant no priority, move nothing and leave the
// outstanding ask untouched.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Chain Lightning's DealDamage->CopySpellAbility chain poses a genuine
// mid-resolution UnlessCost$ ("unless_pay") ask for the damaged seat. Three
// seats so the game keeps running while the ask sits.
func TestKr8ResumeTriggerDrainIsInertWhileAResolutionAsks(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg := Config{Seed: 42, Tokens: reg.Tokens}
	for _, n := range []string{"ur-delver", "death-n-taxes", "ur-delver"} {
		cfg.Names = append(cfg.Names, n)
		cfg.Decks = append(cfg.Decks, testutil.RepoDeck(t, reg, n))
	}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	chain := crAbortMove(t, e, 0, "Chain Lightning", state.ZHand)
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -17})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 2})
	e.askPriority(0)
	crAbortAnswer(t, e, "Chain Lightning", crAbortOption(t, e, "Chain Lightning", "cast", chain))
	crResolutionPlayerTarget(t, e, 1)
	crResolutionRound(t, e)

	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" || !TapePosed(e) || e.G.Obj(chain).Zone != state.ZStack {
		t.Fatalf("want a posed unless_pay modes decision with Chain Lightning on the stack; pending=%+v posed=%v chainZone=%v",
			d, TapePosed(e), e.G.Obj(chain).Zone)
	}
	turn, step, active := e.G.Turn, e.G.Step, e.G.Active
	stackLen := len(e.G.Stack)
	start := len(e.L.Events)

	e.resumeTriggerDrain()

	if got := e.Pending(); got != d {
		t.Fatalf("the outstanding ask was replaced: before=%p after=%p", d, got)
	}
	if !TapePosed(e) {
		t.Fatal("the drain dropped the posed resolution it must not touch")
	}
	if e.G.Obj(chain).Zone != state.ZStack {
		t.Fatalf("chain zone=%v, want still on the stack (the drain moved a half-resolved object)", e.G.Obj(chain).Zone)
	}
	if len(e.G.Stack) != stackLen {
		t.Fatalf("stack length = %d, want %d (the drain placed or removed a stack object)", len(e.G.Stack), stackLen)
	}
	if e.G.Turn != turn || e.G.Step != step || e.G.Active != active {
		t.Fatalf("turn advanced underneath a resolving object: turn %d->%d step %v->%v active %d->%d",
			turn, e.G.Turn, step, e.G.Step, active, e.G.Active)
	}
	if added := len(e.L.Events) - start; added != 0 {
		t.Fatalf("resumeTriggerDrain emitted %d events while a resolution was asking, want 0", added)
	}
}
