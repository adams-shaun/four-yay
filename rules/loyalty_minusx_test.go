package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file closes one sub-shape of the pw1 "Known approximations" row: the
// dynamic [-X] loyalty cost. At this commit the sub-shape is already
// implemented -- ParseCost models `SubCounter<X/LOYALTY>` as an announced
// SubCounter part (no generic fallback, no Unknown census entry), the shared
// xAsk announces X bounded by the walker's loyalty, activateCast pays that
// many loyalty counters, and the CastInfo event binds xPaid so the ability's
// body (`NumDmg$ X` -> `SVar:X:Count$xPaid`) reads the announced value. These
// tests pin that contract end to end on the real corpus card Chandra,
// Awakened Inferno (the only [-X] carrier in the repo decks), and would fail
// against the pre-X Grammar's one-generic fallback the row describes.

// TestParseCostAnnouncedLoyaltySubCounter pins the cost grammar for the [-X]
// symbol: `SubCounter<X/LOYALTY>` is a real announced SubCounter part of the
// LOYALTY kind, priced at nothing (no phantom generic) and never reported as
// an Unknown token. The companion classifier must call it a loyalty ability
// even when the ability carries no Planeswalker$ marker, because the cost
// itself identifies it (CR 606.1).
func TestParseCostAnnouncedLoyaltySubCounter(t *testing.T) {
	t.Parallel()
	c := ParseCost("SubCounter<X/LOYALTY>")
	// Precondition: the compared values actually differ -- a fixed removal
	// must NOT be the announced form.
	fixed := ParseCost("SubCounter<3/LOYALTY>")
	if len(fixed.SubCounter) != 1 || fixed.SubCounter[0].Announced {
		t.Fatalf("precondition: SubCounter<3/LOYALTY> must be a fixed (non-announced) part, got %+v", fixed.SubCounter)
	}
	if len(c.SubCounter) != 1 {
		t.Fatalf("SubCounter<X/LOYALTY> parsed to %d SubCounter parts (%+v); the [-X] grammar must be a real part", len(c.SubCounter), c)
	}
	part := c.SubCounter[0]
	if !part.Announced || !strings.EqualFold(part.Spec, "LOYALTY") || part.N != 0 {
		t.Fatalf("SubCounter<X/LOYALTY> part = %+v, want an announced LOYALTY part", part)
	}
	if c.Generic != 0 {
		t.Fatalf("[-X] costed %d phantom generic mana (the row's one-generic fallback)", c.Generic)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("[-X] reported Unknown tokens %v; the head must be modelled", c.Unknown)
	}
	// The classifier identifies the dynamic form from the cost alone -- no
	// Planeswalker$ parameter is read here.
	ab := &cards.SA{Kind: "AB", API: "DealDamage", Params: map[string]string{"Cost": "SubCounter<X/LOYALTY>"}}
	if !isLoyaltyAbility(ab) {
		t.Fatal("isLoyaltyAbility missed the dynamic [-X] cost with no Planeswalker$ marker")
	}
}

// TestChandraMinusXAnnouncesPaysLoyaltyAndBindsX drives the real [-X] ability
// of Chandra, Awakened Inferno: the activation asks for X, offers one value
// per loyalty the walker has, pays exactly X loyalty counters and deals X
// damage (the body's `NumDmg$ X` reading `SVar:X:Count$xPaid`).
func TestChandraMinusXAnnouncesPaysLoyaltyAndBindsX(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, chandra := walkerBoard(t, reg, "Chandra, Awakened Inferno", bear)
	bearID := bearObjID(t, e, bear)
	// Precondition: the walker is on the battlefield with 6 loyalty and the
	// target creature is a real battlefield object, so the assertions below
	// compare values that actually differ.
	o := e.G.Obj(chandra)
	if o.Zone != state.ZBattlefield || o.Counter("LOYALTY") != 6 {
		t.Fatalf("precondition: Chandra zone %s loyalty %d, want battlefield/6", o.Zone, o.Counter("LOYALTY"))
	}
	if e.G.Obj(bearID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: bear zone %s, want battlefield", e.G.Obj(bearID).Zone)
	}
	e.Advance()
	// Ability 2 is the [-X] DealDamage (see the corpus script).
	opt := abilityOption(t, e, chandra, 2)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want the announced-X ask, got %+v", d)
	}
	if len(d.Options) != 7 {
		t.Fatalf("[-X] at 6 loyalty offered %d X values, want 0..6", len(d.Options))
	}
	for i, op := range d.Options {
		if op.Kind != "x" || op.Amount != i {
			t.Fatalf("X option %d = %+v, want X = %d", i, op, i)
		}
	}
	submitChoices(t, e, 4) // announce X = 4
	dt := e.Pending()
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("want a target ask after the X announcement, got %+v", dt)
	}
	targetObject(t, e, bearID)
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(chandra).Counter("LOYALTY"); got != 2 {
		t.Fatalf("loyalty after [-X=4] = %d, want 2 (X loyalty counters must be paid, CR 606.3)", got)
	}
	// The body bound xPaid: exactly X=4 damage reached the bear.
	var dmg int32 = -1
	sawPay := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == bearID {
			dmg = ev.Amount
		}
		if ev.Kind == events.CounterChange && ev.Obj == chandra && ev.Counter == "LOYALTY" {
			sawPay = sawPay || ev.Amount == -4
		}
	}
	if dmg != 4 {
		t.Fatalf("bear took %d damage, want X=4 (the body must read the announced xPaid)", dmg)
	}
	if !sawPay {
		t.Fatal("no -4 LOYALTY CounterChange for the announced X payment")
	}
	replayCheck(t, e, cfg)
}

// TestChandraMinusXBoundAndOncePerTurn pins the two gating halves: the
// announced X is bounded by the walker's actual loyalty (announcing more
// could never be settled, CR 601.2b), and the activation consumes the CR
// 606.3 once-per-turn window for the whole permanent.
func TestChandraMinusXBoundAndOncePerTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, chandra := walkerBoard(t, reg, "Chandra, Awakened Inferno", bear)
	// Spend the walker down to exactly 3 loyalty through a logged event, so
	// the X bound is a real, checkable number.
	e.emit(events.Event{Kind: events.CounterChange, Obj: chandra, Counter: "LOYALTY", Amount: -3})
	if got := e.G.Obj(chandra).Counter("LOYALTY"); got != 3 {
		t.Fatalf("precondition: loyalty %d, want 3", got)
	}
	e.Advance()
	opt := abilityOption(t, e, chandra, 2)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want the announced-X ask, got %+v", d)
	}
	if len(d.Options) != 4 {
		t.Fatalf("[-X] at 3 loyalty offered %d X values, want 0..3", len(d.Options))
	}
	submitChoices(t, e, 1)
	if dt := e.Pending(); dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("want a target ask, got %+v", dt)
	}
	targetObject(t, e, bearObjID(t, e, bear))
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(chandra).Counter("LOYALTY"); got != 2 {
		t.Fatalf("loyalty after [-X=1] = %d, want 2", got)
	}
	// CR 606.3: after one loyalty ability, NO loyalty ability of that walker
	// is offered again this turn.
	if loyaltyAbilityOffered(e, 0, chandra, 2) {
		t.Fatal("[-X] re-offered in the same turn (CR 606.3 once-per-turn)")
	}
	if loyaltyAbilityOffered(e, 0, chandra, 0) {
		t.Fatal("a sibling loyalty ability offered after [-X] (CR 606.3 is per permanent)")
	}
	replayCheck(t, e, cfg)
}
