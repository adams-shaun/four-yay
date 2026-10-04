package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// waterbendXOverpaySrc is a synthetic activated ability whose cost carries a
// Waterbend<X> part AND a separate fixed generic component: `Cost$
// Waterbend<X> 2`. It exists because no repo deck card pairs the two, and the
// pair is exactly what exposes the overpayment an unscoped waterbend cap
// allows: after the payer announces two waterbend taps, a buggy cap lets the
// second tap pay the unrelated {2} (convokeAbsorbs sees enough generic) while
// the announced X is only 1 (CR 701.67a allows each tap to pay only the
// WATERBEND amount).
const waterbendXOverpaySrc = "Name:Water Fiend\nManaCost:U\nTypes:Creature Elemental\nPT:1/1\n" +
	"A:AB$ Effect | Cost$ Waterbend<X> 2 | XMin$ 1 | SpellDescription$ x.\nOracle:x\n"

// TestWaterbendXCostPartCapsTapsAtAnnouncedX pins the finding on this ticket's
// fix: a Waterbend<X> cost part's amount is the announced X, so the waterbend
// taps may cover at most X of the cost's generic -- never a tap redirected
// onto a separate fixed generic component. With the ability `Waterbend<X> 2`
// and two eligible permanents tapped for waterbend, X=1 must NOT be offered
// (its cap is one tap); the smallest legal announcement is X=2.
func TestWaterbendXCostPartCapsTapsAtAnnouncedX(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 7, waterbendXOverpaySrc)
	// Two untapped artifacts/creatures for the waterbend announcement.
	tapA := onBoardReady(t, e, 0, "Name:Tap A\nManaCost:0\nTypes:Creature Crab\nPT:1/1\nOracle:x\n")
	tapB := onBoardReady(t, e, 0, "Name:Tap B\nManaCost:0\nTypes:Creature Crab\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	// Fund {4}: enough that X=2 is payable after the two taps, so the ONLY
	// reason X=1 could be withheld is the waterbend cap under test -- not a
	// mana shortfall.
	addMana(t, e, 0, "CCCC")
	if got := e.G.Players[0].Pool.Total(); got != 4 {
		t.Fatalf("precondition: pool must hold {4} so X=2 is payable after the taps, got %d", got)
	}
	// The ability must be offered (precondition for the whole scenario).
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)

	// CR 601.2b: the waterbend contribution announcement is posed before X.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the waterbend contribution announcement, got %+v", d)
	}
	var waters []int
	for _, o := range d.Options {
		if o.Kind == "waterbend_generic" {
			waters = append(waters, o.Index)
		}
	}
	if len(waters) < 2 {
		t.Fatalf("precondition: want at least two waterbend options (%v and %v untapped), got %+v", tapA, tapB, d.Options)
	}
	// Announce exactly two taps in one answer: that is the over-announcement
	// the cap must reject at X=1.
	submitChoices(t, e, waters[0], waters[1])
	if e.cast == nil {
		t.Fatalf("precondition: the activation must still be pending after the announcement")
	}
	if got := waterbendTaps(e.cast.Convoke); got != 2 {
		t.Fatalf("precondition: both waterbend taps must be recorded, got %d (%+v)", got, e.cast.Convoke)
	}

	// The announced X must cover the announced taps: X=1 cannot.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the announced-X ask, got %+v", d)
	}
	offered := map[int32]bool{}
	for _, o := range d.Options {
		if o.Kind == "x" {
			offered[int32(o.Amount)] = true
		}
	}
	if offered[1] {
		t.Fatalf("X=1 offered with two announced waterbend taps: its cap is one tap, so the second would pay the unrelated fixed {2} (CR 701.67a): %+v", d.Options)
	}
	if !offered[2] {
		t.Fatalf("precondition: X=2 must be offered -- it is payable with {4} and its cap covers both taps: %+v", d.Options)
	}
}

// TestWaterbendCapCountsEachXFormSeparately locks waterbendCap's formula: the
// cap is the fixed Waterbend<N> amount plus the announced X once per X-form
// part, and a RaiseCost Waterbend<X> credit (recorded in raiseX) must not be
// double-counted when a Waterbend<X> cost part is also present.
func TestWaterbendCapCountsEachXFormSeparately(t *testing.T) {
	t.Parallel()
	fixed := costMods{Waterbend: 3}
	if got := waterbendCap(fixed, 5); got != 3 {
		t.Fatalf("fixed Waterbend<3> cap = %d, want 3 (independent of X)", got)
	}
	partX := costMods{WaterbendX: true, WaterbendPartX: 1}
	if got := waterbendCap(partX, 4); got != 4 {
		t.Fatalf("Waterbend<X> cost-part cap at X=4 = %d, want 4", got)
	}
	raiseX := costMods{WaterbendX: true, RaiseX: 2}
	if got := waterbendCap(raiseX, 4); got != 8 {
		t.Fatalf("two RaiseCost Waterbend<X> parts at X=4 = %d, want 8", got)
	}
	mixed := costMods{Waterbend: 1, WaterbendX: true, WaterbendPartX: 1, RaiseX: 1}
	if got := waterbendCap(mixed, 4); got != 1+4+4 {
		t.Fatalf("fixed 1 + cost-part X(4) + RaiseCost X(4) = %d, want 9", got)
	}
}
