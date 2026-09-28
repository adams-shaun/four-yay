package rules

// The TargetMax$ X / TargetMin$ X dynamic-bound task: the announcement ask
// (targetAsk, rules/cast.go) and the placement ask (askTarget, fed by
// trigger_queue.go) resolve a non-literal bound through the effects numeric
// grammar instead of silently dropping it to the default 1. The shared
// resolver's unit contract is pinned in stack_test.go
// (TestResolvedTargetBoundsDynamic); the Mantle placement pin lives in
// changezone_attachedto_test.go. This file pins the announcement ask end to
// end on synthetic fixtures (script text written inline -- never a committed
// corpus file).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const xBoundVolleySrc = "Name:Bound Volley\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ Draw | Defined$ You | NumCards$ 1 | ValidTgts$ Creature | TargetMax$ X\n" +
	"SVar:X:Count$Valid Creature.YouCtrl\n" +
	"Oracle:x\n"

const xBoundPickSrc = "Name:Bound Pick\nManaCost:X R\nTypes:Sorcery\n" +
	"A:SP$ Draw | Defined$ You | NumCards$ 1 | ValidTgts$ Creature | TargetMax$ X\n" +
	"Oracle:x\n"

// TestAnnouncementAskResolvesTargetMaxX pins the CR 601.2c announcement ask
// on the SVar-bound shape: TargetMax$ X with SVar:X:Count$Valid
// Creature.YouCtrl bounds the ask by the LIVE battlefield count (two bears ->
// Max 2), both bears are offered and selectable, and the spell then resolves
// normally.
func TestAnnouncementAskResolvesTargetMaxX(t *testing.T) {
	t.Parallel()
	e, cfg, volley := newFixtureDeck(t, 4101, xBoundVolleySrc, testBearSrc, testBearSrc)
	putCreature(t, e, 0, testBearSrc)
	putCreature(t, e, 0, testBearSrc)
	addMana(t, e, 0, "R")
	opt := castByName(t, e, 0, "Bound Volley")
	if opt == nil {
		t.Fatal("Bound Volley must be castable from hand")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the target ask, got %+v", d)
	}
	if d.Max != 2 || len(d.Options) != 2 {
		t.Fatalf("target ask = %+v, want Max 2 with both bears offered", d)
	}
	// The target ask's Max is 2 and BOTH bears are selectable: submit them in
	// reverse option order so the recorded targets are not order-luck.
	submitChoices(t, e, d.Options[1].Index, d.Options[0].Index)
	if n := len(e.G.Obj(volley).Targets); n != 2 {
		t.Fatalf("recorded %d targets on the stack object, want 2", n)
	}
	attachDrain(t, e, 80)
	if o := e.G.Obj(volley); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Bound Volley on %s, want graveyard after resolving with two targets", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// TestAnnouncementAskBareXReadsThePaidX pins the bare-X shape: a spell cast
// for {X} with NO SVar:X whose TargetMax$ X is the paid X -- announced 3 ->
// the target ask's Max is 3.
func TestAnnouncementAskBareXReadsThePaidX(t *testing.T) {
	t.Parallel()
	e, cfg, pick := newFixtureDeck(t, 4102, xBoundPickSrc, testBearSrc, testBearSrc, testBearSrc)
	for i := 0; i < 3; i++ {
		putCreature(t, e, 0, testBearSrc)
	}
	addMana(t, e, 0, "RRRR")
	opt := castByName(t, e, 0, "Bound Pick")
	if opt == nil {
		t.Fatal("Bound Pick must be castable from hand")
	}
	submitChoices(t, e, opt.Index)
	// Announce X = 3 (the value rides on Option.Amount, not Index).
	xd := e.Pending()
	if xd == nil || xd.Options[0].Kind != "x" {
		t.Fatalf("want the X announce first, got %+v", xd)
	}
	xidx := -1
	for _, o := range xd.Options {
		if o.Label == "X = 3" {
			xidx = o.Index
		}
	}
	if xidx < 0 {
		t.Fatalf("no X = 3 option: %+v", xd.Options)
	}
	submitChoices(t, e, xidx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the target ask, got %+v", d)
	}
	if d.Max != 3 {
		t.Fatalf("target ask Max = %d, want 3 (the paid X)", d.Max)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index, d.Options[2].Index)
	if n := len(e.G.Obj(pick).Targets); n != 3 {
		t.Fatalf("recorded %d targets on the stack object, want 3", n)
	}
	attachDrain(t, e, 80)
	if o := e.G.Obj(pick); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Bound Pick on %s, want graveyard after resolving with three targets", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// TestUnresolvableTargetMaxXKeepsTheDefault pins the failure direction: a
// bound token the grammar cannot price (TargetMax$ Y, no SVar:Y) must NOT
// degrade to zero -- the ask keeps today's Max-1 shape (the M1
// single-target contract), never a Max-0 ask that could wedge or silently
// resolve untargeted.
func TestUnresolvableTargetMaxXKeepsTheDefault(t *testing.T) {
	t.Parallel()
	src := "Name:Bound Y\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ Draw | Defined$ You | NumCards$ 1 | ValidTgts$ Creature | TargetMax$ Y\nOracle:x\n"
	e, cfg, volley := newFixtureDeck(t, 4103, src, testBearSrc, testBearSrc)
	putCreature(t, e, 0, testBearSrc)
	putCreature(t, e, 0, testBearSrc)
	addMana(t, e, 0, "R")
	opt := castByName(t, e, 0, "Bound Y")
	if opt == nil {
		t.Fatal("Bound Y must be castable from hand")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the target ask, got %+v", d)
	}
	if d.Max != 1 {
		t.Fatalf("target ask Max = %d, want the default 1 for an unresolvable bound", d.Max)
	}
	submitChoices(t, e, d.Options[0].Index)
	attachDrain(t, e, 80)
	if o := e.G.Obj(volley); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Bound Y on %s, want graveyard after resolving", o.Zone)
	}
	replayCheck(t, e, cfg)
}
