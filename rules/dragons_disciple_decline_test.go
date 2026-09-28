package rules

// The engine-level end to end for the round-2 review finding (fx45), on the
// REAL compiled corpus card: a DECLINED "you may reveal a Dragon card from
// your hand" (Dragon's Disciple's ETB replacement) used to resume into a
// MANDATORY reveal_pick over the eligible Dragons -- the decline's
// `continue` sits after the pick block in effReveal, and the pick's
// deferToOptionalAsk guard only covers the first pass. The effects-package
// leaf (effects/reveal_decline_test.go) pins the synthetic shapes; this file
// drives the whole cast through the engine's own frame stack, which is what
// a real seat experiences.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDragonsDiscipleDeclinedRevealPosesNoPick casts the real Dragon's
// Disciple with TWO Dragons in hand (so the narrowed eligible pool is
// strictly larger than the one card the reveal must show -- the condition
// under which the pick exists at all) and declines the may-reveal. The
// pre-fix build then posed a Min/Max 1/1 reveal_pick anyway; the fixed build
// finishes the resolution with nothing revealed.
func TestDragonsDiscipleDeclinedRevealPosesNoPick(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	disciple := mustCorpusCard(t, reg, "Dragon's Disciple")
	dragon := mustCorpusCard(t, reg, "Shivan Dragon")
	hillGiant := mustCorpusCard(t, reg, "Hill Giant")

	e := layerEngine(t)

	// Seat 0's hand: the disciple plus two Dragons (the reveal candidates)
	// and a non-Dragon (so the RevealValid$ Dragon narrowing is doing real
	// work -- the pool is the Dragons alone, not the whole hand).
	spell := e.G.AddObject(disciple, 0)
	spell.Zone = state.ZHand
	d1 := e.G.AddObject(dragon, 0)
	d1.Zone = state.ZHand
	d2 := e.G.AddObject(dragon, 0)
	d2.Zone = state.ZHand
	other := e.G.AddObject(hillGiant, 0)
	other.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{spell.ID, d1.ID, d2.ID, other.ID})

	// Preconditions the assertions below depend on.
	if got := e.G.Obj(spell.ID).Zone; got != state.ZHand {
		t.Fatalf("precondition: disciple zone = %s, want hand", got)
	}
	if len(e.G.Zone(state.ZHand, 0)) != 4 {
		t.Fatalf("precondition: hand size = %d, want 4", len(e.G.Zone(state.ZHand, 0)))
	}
	if len(e.G.Zone(state.ZBattlefield, 0)) != 0 {
		t.Fatalf("precondition: seat 0's battlefield is not empty")
	}

	// {1}{W}: two W cover both pips.
	addW := func() {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 1})
	}
	addW()
	addW()
	e.pending = nil
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell.ID})
	e.Advance()

	declined := false
	for i := 0; i < 200; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			if declined {
				// The resolution settled after the decline; the rest is
				// turn machinery out of this test's scope.
				break
			}
			submitChoices(t, e, tutorPassIndex(t, d))
			continue
		}
		switch d.ResumeKind {
		case "reveal_optional":
			if d.Player != 0 {
				t.Fatalf("may-reveal asked seat %d, want the hand's owner 0", d.Player)
			}
			if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
				t.Fatalf("may-reveal ask = %+v, want a yes/no", d)
			}
			submitChoices(t, e, 1) // decline
			declined = true
		case "reveal_pick":
			t.Fatalf("fx45 regression: a DECLINED may-reveal posed a mandatory reveal_pick: %+v", d)
		default:
			t.Fatalf("unexpected mid-resolution ask: kind=%s resume=%q %+v", d.Kind, d.ResumeKind, d)
		}
	}
	if !declined {
		t.Fatal("the may-reveal ask was never posed")
	}
	// The decline path completed: the disciple entered with NO +1/+1 counter
	// (declined the reveal, no Dragon controlled) and settled on the
	// battlefield.
	if got := e.G.Obj(spell.ID).Zone; got != state.ZBattlefield {
		t.Fatalf("disciple zone = %s, want battlefield after the declined reveal", got)
	}
	if n := e.G.Obj(spell.ID).Counter("P1P1"); n != 0 {
		t.Fatalf("declined reveal left %d P1P1 counters on the disciple, want 0", n)
	}
	// Nothing was revealed: no public reveal Note carrying ids exists.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && !ev.Secret && len(ev.IDs) > 0 {
			t.Fatalf("a declined reveal emitted a public Note: %+v", ev)
		}
	}
}
