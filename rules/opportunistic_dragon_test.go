// Opportunistic Dragon (Temur Roar deck) pins animate:RemoveAllAbilities$ end
// to end on the real corpus card. Its ETB rider is the control-theft shape:
//
//	SVar:TrigGainControl:DB$ GainControl | ValidTgts$ Human.OppCtrl,Artifact.OppCtrl | LoseControl$ LeavesPlay | SubAbility$ DBPump
//	SVar:DBPump:DB$ Animate | Defined$ Targeted | HiddenKeywords$ CARDNAME can't attack or block. | RemoveAllAbilities$ True | Duration$ UntilHostLeavesPlay
//
// Before this ticket effects.parseAnimateGrant never read RemoveAllAbilities$,
// so the stolen permanent kept every printed ability while the Dragon
// remained. The grant is now a layer-6 removal on the shared animation path,
// its Duration$ UntilHostLeavesPlay anchored to the ANIMATING source through
// ContinuousEffect.DurationSource, so the abilities return when the Dragon
// leaves the battlefield.
//
// The HiddenKeywords$ half of the same line ("it can't attack or block") is a
// DIFFERENT, still-unread Animate parameter and is filed as a separate ticket;
// this test asserts the RemoveAllAbilities$ half the ticket names.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestOpportunisticDragonStripsStolenPermanentAbilities: while the Dragon
// remains, the stolen artifact creature loses all printed abilities; when the
// Dragon leaves the battlefield the abilities return.
func TestOpportunisticDragonStripsStolenPermanentAbilities(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Opportunistic Dragon")},
		[]*cards.Card{lookup(t, reg, "Ornithopter")})

	// The theft target: seat 1's artifact creature, with its printed keyword.
	// This is the PRECONDITION every ability-strip assertion below rests on --
	// a target with no printed keyword would make "loses all abilities"
	// vacuously true.
	vid := moveByName(t, e, 1, "Ornithopter", state.ZBattlefield)
	vo := e.G.Obj(vid)
	if vo == nil || vo.Zone != state.ZBattlefield || vo.Controller != 1 {
		t.Fatalf("precondition: Ornithopter not on seat 1's battlefield: %+v", vo)
	}
	if !e.HasKeyword(vid, "Flying") {
		t.Fatalf("precondition: Ornithopter printed abilities = %v, want Flying", e.Derived(vid).Keywords)
	}

	// The Dragon enters: its ChangesZone ETB trigger fires.
	did := moveByName(t, e, 0, "Opportunistic Dragon", state.ZBattlefield)
	do := e.G.Obj(did)
	if do == nil || do.Zone != state.ZBattlefield || do.Controller != 0 {
		t.Fatalf("precondition: Dragon not on seat 0's battlefield: %+v", do)
	}

	// The ETB asks for the theft target; choose the Ornithopter.
	d := passToTargetAsk(t, e)
	idx := -1
	for _, o := range d.Options {
		if o.Obj == vid {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Ornithopter not offered as the Dragon's theft target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 30)

	// The control change itself resolved (the Animate is its SubAbility): the
	// Ornithopter is now seat 0's. This is also the precondition that the
	// RemoveAllAbilities$ grant had a real, resolved animation to ride.
	if got := e.G.Obj(vid).Controller; got != 0 {
		t.Fatalf("stolen Ornithopter controller = %d, want 0 (the Dragon's controller)", got)
	}
	if !e.HasKeyword(did, "Flying") {
		t.Fatal("precondition: the Dragon itself must keep its own printed Flying")
	}
	// The grant ran: it must be a registered layer-6 removal on the shared
	// animation path, not a silent no-op.
	removal := false
	for _, ce := range e.active() {
		if ce.Source == vid && ce.Layer == state.LAbilities && ce.RemoveAbilities {
			removal = true
		}
	}
	if !removal {
		t.Fatalf("no RemoveAllAbilities layer-6 effect registered on the stolen Ornithopter (continuous=%+v)", e.continuous)
	}

	// The stolen permanent lost its printed Flying.
	if e.HasKeyword(vid, "Flying") {
		t.Fatalf("stolen Ornithopter kept Flying: printed abilities = %v, want none while the Dragon remains",
			e.Derived(vid).Keywords)
	}

	// The Dragon leaves the battlefield: the UntilHostLeavesPlay grant ends
	// and the Ornithopter has its printed abilities again.
	e.emit(events.Event{Kind: events.MoveZone, Obj: did,
		From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(did); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: the Dragon did not leave: %+v", o)
	}
	if !e.HasKeyword(vid, "Flying") {
		t.Fatalf("stolen Ornithopter did not regain Flying after the Dragon left: abilities = %v",
			e.Derived(vid).Keywords)
	}
	replayCheck(t, e, cfg)
}
