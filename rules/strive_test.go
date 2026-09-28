package rules

// kw:Strive (CR 702.52, task kw-strive-no-expansion) pinned end to end on
// the REAL corpus carrier Twinflame (K:Strive:2 R, the Prismari Artistry
// deck's carrier): the strive cost is a MANDATORY additional cost of
// (targets - 1) payments, priced only once the CR 601.2c target answer is
// in (repriceForTargets' fold into pc.cost), so the mana window and the
// payment charge the composed total. The helpers come from
// paramcensus_gates_test.go (gateFixture, gateMoveFromLibrary), cast_test.go
// (castOptions, submitChoices, addMana, replayCheck) and
// replacement_updated_test.go (passUntilStackEmpty) -- all the same package.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// striveCastOption returns the plain cast option for id.
func striveCastOption(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	for _, o := range castOptions(t, e) {
		if o.Obj == id && o.Mode == "" {
			return o
		}
	}
	t.Fatalf("no plain cast option for %d: %+v", id, castOptions(t, e))
	return decision.Option{}
}

// chooseStriveTargets submits the pending KTarget decision's options whose
// object is one of the given ids, in order -- one Intent carrying every
// choice, the shape the multi-target ask's answer handler appends from.
func chooseStriveTargets(t *testing.T, e *Engine, ids ...state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a KTarget decision, got %+v", d)
	}
	var idxs []int
	for _, want := range ids {
		found := false
		for _, o := range d.Options {
			if o.Obj == want {
				idxs = append(idxs, o.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("target %d not offered: %+v", want, d.Options)
		}
	}
	submitChoices(t, e, idxs...)
}

// TestTwinflameStriveTwoTargetsChargesBasePlusOneStrive drives the real
// corpus carrier end to end: two targets pay {1}{R} + one {2}{R} =
// {3}{R}{R}, exactly the five red mana in the pool -- base + (N-1) x strive
// cost. The spell resolves and mints one haste token copy per target.
func TestTwinflameStriveTwoTargetsChargesBasePlusOneStrive(t *testing.T) {
	t.Parallel()
	e, cfg, tf := gateFixture(t, 931, "Twinflame", gateRaiderSrc, gateRaiderSrc)
	r1 := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	r2 := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	addMana(t, e, 0, "RRRRR") // {3}{R}{R} exactly, payable from five red

	submitChoices(t, e, striveCastOption(t, e, tf).Index)
	chooseStriveTargets(t, e, r1, r2)

	// The payment charged base + one strive payment: the pool drained to
	// exactly zero. (Without the fold the cast would charge {1}{R} and one
	// red mana would survive in the pool.)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after two-target strive cast = %d, want 0 (charged {3}{R}{R})", got)
	}
	if o := e.G.Obj(tf); o.Zone != state.ZStack || len(o.Targets) != 2 {
		t.Fatalf("Twinflame on the stack with both targets: zone %v targets %v", o.Zone, o.Targets)
	}

	passUntilStackEmpty(t, e, 20)
	// One haste token copy per target, beside the two Raiders themselves.
	tokens := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o.ID != r1 && o.ID != r2 && o.IsToken {
			tokens++
		}
	}
	if tokens != 2 {
		t.Fatalf("token copies on the battlefield = %d, want 2", tokens)
	}
	replayCheck(t, e, cfg)
}

// TestTwinflameStriveOneTargetChargesBaseOnly is the (N-1)=0 control: a
// single target pays exactly the printed {1}{R} -- no strive payment, and
// two mana would be left if the fold misread the count.
func TestTwinflameStriveOneTargetChargesBaseOnly(t *testing.T) {
	t.Parallel()
	e, cfg, tf := gateFixture(t, 932, "Twinflame", gateRaiderSrc, gateRaiderSrc)
	r1 := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	addMana(t, e, 0, "RR") // {1}{R} exactly

	submitChoices(t, e, striveCastOption(t, e, tf).Index)
	chooseStriveTargets(t, e, r1)

	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after one-target strive cast = %d, want 0 (charged {1}{R})", got)
	}
	if o := e.G.Obj(tf); o.Zone != state.ZStack || len(o.Targets) != 1 {
		t.Fatalf("Twinflame on the stack with one target: zone %v targets %v", o.Zone, o.Targets)
	}
	replayCheck(t, e, cfg)
}

// TestTwinflameStriveExtraTargetNeedsTheStriveMana proves the strive charge
// BINDS: with only the printed {1}{R} in the pool, a two-target selection
// prices {3}{R}{R}, which the pool cannot pay, and the proposal reverses (CR
// 733.1) -- the card back in hand, the pool untouched, nothing on the stack.
func TestTwinflameStriveExtraTargetNeedsTheStriveMana(t *testing.T) {
	t.Parallel()
	e, cfg, tf := gateFixture(t, 933, "Twinflame", gateRaiderSrc, gateRaiderSrc)
	r1 := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	r2 := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	addMana(t, e, 0, "RR") // base only: the strive payment is unpayable

	submitChoices(t, e, striveCastOption(t, e, tf).Index)
	chooseStriveTargets(t, e, r1, r2)

	if o := e.G.Obj(tf); o.Zone != state.ZHand {
		t.Fatalf("reversed strive cast: Twinflame zone %v, want back in hand", o.Zone)
	}
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("pool after the reversal = %d, want the untouched 2", got)
	}
	replayCheck(t, e, cfg)
}
