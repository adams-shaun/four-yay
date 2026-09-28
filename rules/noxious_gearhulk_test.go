package rules

// Noxious Gearhulk's ETB is the corpus shape that exposes Destroy's missing
// RememberLKI$ True rider:
//
//	T:Mode$ ChangesZone | ... | Execute$ TrigDestroy | OptionalDecider$ You
//	SVar:TrigDestroy:DB$ Destroy | ValidTgts$ Creature.Other |
//	    RememberLKI$ True | SubAbility$ DBGainLife
//	SVar:DBGainLife:DB$ GainLife | LifeAmount$ X
//	SVar:X:RememberedLKI$CardToughness
//
// The chained RememberedLKI$CardToughness reads the destroyed creature's
// LAST-KNOWN toughness (CR 603.10 look-back), which effDestroy must capture
// before events.Apply's Move folds the permanent away. The test's target is a
// 2/2 with three +1/+1 counters: a 5/5 on the battlefield but a 2/2 card in
// the graveyard, so an LKI read gains +5 life while the post-move printed-face
// fallback gains only +2 -- the assertion discriminates the two.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestNoxiousGearhulkGainsDestroyedCreatureToughness(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	gearhulk := mustCorpusCard(t, reg, "Noxious Gearhulk")

	// A 2/2 with three +1/+1 counters is a 5/5 on the battlefield but a 2/2
	// card in the graveyard.
	victimCard := card(t, "Name:Test Titan\nManaCost:2\nTypes:Creature\nPT:2/2\nOracle:x\n")

	e, cfg := tokenReplGameSeats(t, 71, []*cards.Card{gearhulk}, []*cards.Card{victimCard})

	victim := moveSeededCard(t, e, 1, victimCard, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: victim, Counter: "P1P1", Amount: 3})

	// Preconditions: the target is a real battlefield permanent under the
	// opponent, with battlefield toughness 5 while its card is a 2/2, so the
	// LKI (5) and post-move printed-face fallback (2) genuinely differ.
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("victim precondition: %+v", e.G.Obj(victim))
	}
	if got := e.Toughness(victim); got != 5 {
		t.Fatalf("victim battlefield toughness = %d, want 5 (2/2 + three +1/+1 counters)", got)
	}
	if int32(victimCard.Faces[0].Toughness()) == e.Toughness(victim) {
		t.Fatalf("test is not discriminating: printed toughness %d equals battlefield toughness %d",
			victimCard.Faces[0].Toughness(), e.Toughness(victim))
	}
	lifeBefore := e.G.Players[0].Life

	// The Gearhulk enters from the library, firing its ETB.
	gh := moveSeededCard(t, e, 0, gearhulk, state.ZBattlefield)
	if o := e.G.Obj(gh); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Gearhulk precondition: %+v", e.G.Obj(gh))
	}
	e.pending = nil
	e.Advance()

	// The ETB trigger poses its "another target creature" ask and its
	// OptionalDecider$ You yes/no in one order or the other; answer whichever
	// arrives until the stack drains.
	for i := 0; i < 30 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KPriority:
			submitChoicePass(t, e)
		case decision.KTriggerOptional:
			submitChoices(t, e, optionIndexOfKind(t, d, "yes"))
		case decision.KTarget:
			idx := indexOfObjOption(d, victim)
			if idx < 0 {
				t.Fatalf("destroy target ask does not offer the 5/5 Test Titan %d: %+v", victim, d.Options)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected decision while resolving the ETB trigger: %+v", d)
		}
	}

	passUntilStackEmpty(t, e, 30)

	// The trigger's effect actually ran on a real permanent: the target left
	// the battlefield, so a zero-life-gain outcome cannot be a setup no-op.
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("victim after resolution: %+v, want it destroyed into the graveyard", e.G.Obj(victim))
	}
	if got := e.G.Players[0].Life - lifeBefore; got != 5 {
		t.Fatalf("controller gained %d life, want 5 (the destroyed 5/5's last-known toughness)", got)
	}
	replayCheck(t, e, cfg)
}
