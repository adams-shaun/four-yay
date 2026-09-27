// Task agent-20260924T100320Z-65e9b8fe (castwindow-probe remaining shapes):
// the CR 601.2g target-affordability probe (castWindowUnits ->
// castWindowReachable) previously priced only free literal productions, a
// dynamic amount on a SINGLE-type free producer, a literal generic netted
// against a SINGLE-type production, PayLife and deterministic self-sacrifice.
// These leaves pin the three remaining representable shapes -- a choice-shaped
// production with a resolvable non-literal amount, a generic activation cost
// with multi-colour production, and a generic fee funded by mana an earlier
// same-window activation produced -- plus the two boundaries that stay
// fail-closed (RestrictValid$ and one tap per permanent). Each positive case
// shows a target previously withheld is offered once its reduced cost is
// payable; each negative case shows the same board with the source removed or
// underfunded stays withheld.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const (
	// cwDynAnySrc produces two mana of any ONE colour whose amount is a
	// resolvable SVar (Count$CardPower over its own 2/2 body). Free cost, so
	// only the choice-shaped dynamic amount is the new shape.
	cwDynAnySrc = "Name:DynAny\nManaCost:1 G\nTypes:Creature Elf Druid\nPT:2/2\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Any | Amount$ X | SpellDescription$ Add X mana of any one color.\n" +
		"SVar:X:Count$CardPower\nOracle:x\n"
	// cwTriRockSrc is the Knotvine-Mystic shape: a literal generic {1}
	// activation cost whose production is THREE colours ({R}{G}{W}), so the
	// fee is not expressible as a net against a single mana type.
	cwTriRockSrc = "Name:TriRock\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ 1 T | Produced$ R G W | SpellDescription$ Add {R}{G}{W}.\nOracle:x\n"
	// cwTriRockFeeSrc is the shape-2 negative control: the same multi-colour
	// production behind a {7} fee, so its net is zero and it cannot fund the
	// difference.
	cwTriRockFeeSrc = "Name:TriRockFee\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ 7 T | Produced$ R G W | SpellDescription$ Add {R}{G}{W}.\nOracle:x\n"
	// cwFreeRockSrc is a plain free colourless producer, the mana an earlier
	// same-window activation needs to fund a paid one.
	cwFreeRockSrc = "Name:FreeRock\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n"
	// cwPaidRockSrc pays a {7} fee for eight colourless: its fee exceeds the
	// board's {6} floating pool, so it can be funded ONLY by mana an earlier
	// same-window activation produced.
	cwPaidRockSrc = "Name:PaidRock\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ 7 T | Produced$ C | Amount$ 8 | SpellDescription$ Add {C}{C}{C}{C}{C}{C}{C}{C}.\nOracle:x\n"
	// cwDualSrc has two free mana abilities sharing one tap. Only ONE may be
	// counted: a probe that summed both would promise two mana from one
	// permanent.
	cwDualSrc = "Name:DualRock\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ W | SpellDescription$ Add {W}.\n" +
		"A:AB$ Mana | Cost$ T | Produced$ U | SpellDescription$ Add {U}.\nOracle:x\n"
	// cwRestrictedSrc is a free green producer whose produced batch is
	// RestrictValid$-governed. The dotted matcher only admits a subset of the
	// grammar, so pricing it would be unsound; the probe must leave it out.
	cwRestrictedSrc = "Name:RestrictedRock\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ G | RestrictValid$ Spell | SpellDescription$ Add {G}. Spend only on spells.\nOracle:x\n"
)

// cwArmBelt drives the shared real-corpus Belt board to its equip target ask
// with pool floating and returns the target decision, so a leaf can inspect
// exactly which reduced targets the probe admitted.
func cwArmBelt(t *testing.T, e *Engine, beltID state.ObjID) *decision.Decision {
	t.Helper()
	opt, ok := findAbilityOption(e, beltID, 0)
	if !ok {
		t.Fatal("Belt of Giant Strength's equip not offered; the board is wrong")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the equip's target decision, got %+v", d)
	}
	return d
}

// cwOffersTarget reports whether the target decision lists id.
func cwOffersTarget(d *decision.Decision, id state.ObjID) bool {
	for _, o := range d.Options {
		if o.Obj == id {
			return true
		}
	}
	return false
}

// cwProbeAlts counts the alternatives castWindowUnits priced for a source, so
// a leaf can assert the widened layer actually contributed the activation
// (the precondition a withheld-target assertion would otherwise not check).
func cwProbeAlts(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	if e.cast == nil {
		t.Fatal("no pending cast while asserting probe alternatives")
	}
	n := 0
	for _, u := range e.castWindowUnits(e.cast) {
		if u.id == id {
			n += len(u.alts)
		}
	}
	return n
}

// cwActivateInWindow answers the open CR 601.2g mana window by activating id
// if it is offered, returning false when the window is gone or does not offer
// it. The engine re-poses the window after each activation.
func cwActivateInWindow(t *testing.T, e *Engine, id state.ObjID) bool {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == id {
			submitChoices(t, e, o.Index)
			return true
		}
	}
	return false
}

// TestCastWindowChoiceDynamicAmountAdmitsTarget is shape (1): a free producer
// whose choice-shaped Amount$ is a resolvable SVar. With {6} floating and the
// 2/2 DynAny ({G}{G} of any one colour), the weak target's repriced {8} is
// payable, so it must be offered. Without DynAny it stays withheld.
func TestCastWindowChoiceDynamicAmountAdmitsTarget(t *testing.T) {
	b := equipWindowGame(t, 710, cwDynAnySrc)
	e, cfg, beltID, bruteID, smallID := b.engine, b.cfg, b.belt, b.brute, b.small
	dynID := b.byName["DynAny"]
	if dynID == 0 {
		t.Fatal("DynAny fixture was not seeded onto the battlefield")
	}
	// Preconditions: the repriced prices really differ, the dynamic source is
	// untapped on the battlefield, and the pool alone is short of {8}.
	if e.Power(bruteID) != 4 || e.Power(smallID) != 2 || e.Power(dynID) != 2 {
		t.Fatalf("powers %d/%d/%d, want 4/2/2", e.Power(bruteID), e.Power(smallID), e.Power(dynID))
	}
	if o := e.G.Obj(dynID); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("DynAny precondition: %+v, want an untapped battlefield permanent", o)
	}
	addMana(t, e, 0, "CCCCCC")
	if got := e.G.Players[0].Pool.Total(); got != 6 {
		t.Fatalf("pool precondition = %d, want 6", got)
	}
	d := cwArmBelt(t, e, beltID)
	if !cwOffersTarget(d, bruteID) {
		t.Fatalf("4-power target withheld at {6}: %+v", d.Options)
	}
	if alts := cwProbeAlts(t, e, dynID); alts == 0 {
		t.Fatal("probe priced no alternative for the dynamic choice-shaped source")
	}
	if !cwOffersTarget(d, smallID) {
		t.Fatalf("2-power target withheld although {6} plus DynAny's {2} of one colour funds its {8}: %+v", d.Options)
	}

	// Negative control: the same board WITHOUT the dynamic source stays
	// withheld, proving the admission above came from the widened probe.
	b2 := equipWindowGame(t, 710)
	if got := b2.byName["DynAny"]; got != 0 {
		t.Fatalf("negative-control board unexpectedly carries DynAny (%d)", got)
	}
	addMana(t, b2.engine, 0, "CCCCCC")
	d2 := cwArmBelt(t, b2.engine, b2.belt)
	if cwOffersTarget(d2, b2.small) {
		t.Fatalf("2-power target offered from {6} alone without the dynamic source: %+v", d2.Options)
	}
	targetObject(t, e, smallID)
	cwDrainManaWindow(t, e, dynID)
	passUntilStackEmpty(t, e, 30)
	if e.G.Obj(beltID).AttachedTo != smallID {
		t.Fatalf("Belt attached to %d, want the 2/2 %d", e.G.Obj(beltID).AttachedTo, smallID)
	}
	replayCheck(t, e, cfg)
}

// cwDrainManaWindow taps id in the open CR 601.2g window and answers any
// follow-up colour/mana sub-ask, then submits "done" when the window is still
// short, so the cast completes. It is deliberately permissive about the
// colour answer: the target-admission assertion is the test's subject, this
// only has to bring the payment home.
func cwDrainManaWindow(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			return
		}
		answered := false
		for _, o := range d.Options {
			if o.Kind == "activate" && o.Obj == id {
				submitChoices(t, e, o.Index)
				answered = true
				break
			}
		}
		if !answered {
			for _, o := range d.Options {
				switch o.Kind {
				case "mana", "W", "U", "B", "R", "G", "C":
					submitChoices(t, e, o.Index)
					answered = true
				}
				if answered {
					break
				}
			}
		}
		if !answered {
			for _, o := range d.Options {
				if o.Kind == "done" {
					submitChoices(t, e, o.Index)
					answered = true
					break
				}
			}
		}
		if !answered {
			return
		}
	}
}

// TestCastWindowGenericMultiColourAdmitsTarget is shape (2): a literal
// generic {1} activation cost producing three colours. With {5} floating plus
// TriRock (pay {1}, add {R}{G}{W}) the weak target's {8} is payable, so it
// must be offered; with only {4} floating it is not, and stays withheld.
func TestCastWindowGenericMultiColourAdmitsTarget(t *testing.T) {
	b := equipWindowGame(t, 711, cwTriRockSrc)
	e, beltID, smallID, bruteID := b.engine, b.belt, b.small, b.brute
	triID := b.byName["TriRock"]
	if triID == 0 {
		t.Fatal("TriRock fixture was not seeded onto the battlefield")
	}
	if e.Power(bruteID) != 4 || e.Power(smallID) != 2 {
		t.Fatalf("powers %d/%d, want 4/2", e.Power(bruteID), e.Power(smallID))
	}
	addMana(t, e, 0, "CCCCCC")
	d := cwArmBelt(t, e, beltID)
	if alts := cwProbeAlts(t, e, triID); alts == 0 {
		t.Fatal("probe priced no alternative for the multi-colour generic producer")
	}
	if !cwOffersTarget(d, smallID) {
		t.Fatalf("2-power target withheld although {6} plus TriRock's net {2} funds its {8}: %+v", d.Options)
	}
	if !cwOffersTarget(d, bruteID) {
		t.Fatalf("4-power target withheld at {6}: %+v", d.Options)
	}

	// Negative control: the same multi-colour production behind a {7} fee
	// (net zero), so {6} alone stays short of {8} and the weak target must
	// remain withheld even though the widened layer priced the source.
	b2 := equipWindowGame(t, 712, cwTriRockFeeSrc)
	addMana(t, b2.engine, 0, "CCCCCC")
	d2 := cwArmBelt(t, b2.engine, b2.belt)
	if cwOffersTarget(d2, b2.small) {
		t.Fatalf("2-power target offered although {6} plus a net-zero multi-colour source = 6 < 8: %+v", d2.Options)
	}
}

// TestCastWindowGenericFundedByEarlierSourceAdmitsTarget is shape (3): with
// {6} floating, PaidRock's {7} fee EXCEEDS the pool, so it can be funded only
// after a free source is tapped first ({6} + one free {C} = 7). Its {8}
// production then reaches the weak target's {8}. The old pool-up-front
// exclusion withheld the target. Without the free producer the fee is
// unfundable and the target stays withheld.
func TestCastWindowGenericFundedByEarlierSourceAdmitsTarget(t *testing.T) {
	b := equipWindowGame(t, 713, cwFreeRockSrc, cwPaidRockSrc)
	e, cfg, beltID, smallID := b.engine, b.cfg, b.belt, b.small
	paidID := b.byName["PaidRock"]
	freeID := b.byName["FreeRock"]
	if paidID == 0 || freeID == 0 {
		t.Fatalf("funding fixtures not seeded: PaidRock=%d FreeRock=%d", paidID, freeID)
	}
	addMana(t, e, 0, "CCCCCC")
	if got := e.G.Players[0].Pool.Total(); got != 6 {
		t.Fatalf("shape-3 precondition: pool = %d, want exactly 6 so the {7} fee needs a window source", got)
	}
	d := cwArmBelt(t, e, beltID)
	if alts := cwProbeAlts(t, e, paidID); alts == 0 {
		t.Fatal("probe priced no alternative for the over-pool generic producer")
	}
	if !cwOffersTarget(d, smallID) {
		t.Fatalf("2-power target withheld although {6} plus a free {C} funds PaidRock's {7} fee and its {8} reaches {8}: %+v", d.Options)
	}
	// Prove the window can execute the sequence the probe promised: tap the
	// free producer, then the paid one.
	targetObject(t, e, smallID)
	tapped := 0
	for i := 0; i < 20 && e.Pending() != nil && e.Pending().Kind == decision.KChoose; i++ {
		if cwActivateInWindow(t, e, freeID) {
			tapped++
			continue
		}
		if cwActivateInWindow(t, e, paidID) {
			tapped++
			continue
		}
		break
	}
	passUntilStackEmpty(t, e, 30)
	if tapped == 0 {
		t.Fatal("the CR 601.2g window offered neither the free nor the paid funding source")
	}
	if !e.G.Obj(paidID).Tapped {
		t.Fatal("PaidRock was never tapped, so the funded fee path was not exercised")
	}
	if e.G.Obj(beltID).AttachedTo != smallID {
		t.Fatalf("Belt attached to %d, want the 2/2 %d after the funded payment", e.G.Obj(beltID).AttachedTo, smallID)
	}
	replayCheck(t, e, cfg)

	// Negative control: the same board without the free producer leaves the
	// {7} fee unfundable from the {6} pool, so the weak target stays withheld.
	b2 := equipWindowGame(t, 714, cwPaidRockSrc)
	addMana(t, b2.engine, 0, "CCCCCC")
	d2 := cwArmBelt(t, b2.engine, b2.belt)
	if cwOffersTarget(d2, b2.small) {
		t.Fatalf("2-power target offered although PaidRock's {7} fee exceeds the {6} pool with no window source: %+v", d2.Options)
	}
}

// TestCastWindowSamePermanentNotDoubleTapped is the exclusivity boundary: a
// dual land's two free abilities share ONE tap, so with {6} floating the
// dual's single unit reaches 7, not 8. A probe that counted both alternatives
// as two taps would admit the weak target; the correct probe withholds it.
func TestCastWindowSamePermanentNotDoubleTapped(t *testing.T) {
	b := equipWindowGame(t, 715, cwDualSrc)
	e, beltID, smallID := b.engine, b.belt, b.small
	dualID := b.byName["DualRock"]
	if dualID == 0 {
		t.Fatal("DualRock fixture was not seeded onto the battlefield")
	}
	if o := e.G.Obj(dualID); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("DualRock precondition: %+v, want an untapped battlefield permanent", o)
	}
	addMana(t, e, 0, "CCCCCC")
	d := cwArmBelt(t, e, beltID)
	// PRECONDITION: both alternatives were priced (so a double-count bug is
	// actually reachable), yet the target is withheld because one permanent
	// taps once.
	units := e.castWindowUnits(e.cast)
	n := 0
	for _, u := range units {
		if u.id == dualID {
			n += len(u.alts)
		}
	}
	if n < 2 {
		t.Fatalf("probe priced %d alternatives for the dual land, want its two abilities", n)
	}
	if cwOffersTarget(d, smallID) {
		t.Fatalf("2-power target offered from {6} plus one dual land (one tap = 1 mana, not 2): %+v", d.Options)
	}
}

// TestCastWindowNewShapesSubsetOfOffers is the superset/anti-abort invariant
// for the widened layer: over a board carrying the choice-shaped dynamic
// producer, the multi-colour generic producer, a free producer, a paid
// producer (payable at this pool) and an InstantSpeed$ producer the shared
// walk withholds, every source castWindowUnits promises is a member of the
// source list manaWindowAsk offers. The shape-3 source whose fee the CURRENT
// pool cannot cover is proven activatable in order by its own leaf instead
// (TestCastWindowGenericFundedByEarlierSourceAdmitsTarget drives the real
// window for it).
func TestCastWindowNewShapesSubsetOfOffers(t *testing.T) {
	b := equipWindowGame(t, 717, cwDynAnySrc, cwTriRockSrc, cwFreeRockSrc, cwPaidRockSrc, equipWindowInstantSrc)
	e := b.engine
	instantID := b.byName["InstantRock"]
	if instantID == 0 {
		t.Fatal("InstantSpeed fixture was not seeded onto the battlefield")
	}
	// A pool large enough that every paid source is offered NOW, so the
	// membership check is exact for every shape the superset claim covers.
	addMana(t, e, 0, "CCCCCCCC")
	cwArmBelt(t, e, b.belt)

	got := e.castWindowUnits(e.cast)
	for _, name := range []string{"DynAny", "TriRock", "FreeRock", "PaidRock"} {
		id := b.byName[name]
		found := false
		for _, u := range got {
			if u.id == id && len(u.alts) > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("castWindowUnits priced no alternative for %s (%d): the widened layer is absent", name, id)
		}
	}
	offered := map[state.ObjID]bool{}
	for _, z := range []state.Zone{state.ZBattlefield, state.ZHand, state.ZGraveyard} {
		for _, id := range e.G.Zone(z, 0) {
			if !e.convokeCommitted(e.cast, id) && e.untappedManaSource(0, id) {
				offered[id] = true
			}
		}
	}
	for _, u := range got {
		if !offered[u.id] {
			t.Errorf("probe promised source %d, which manaWindowAsk will not offer", u.id)
		}
	}
	if offered[instantID] {
		t.Fatal("precondition: InstantSpeed$ True source is in the offer set; the test board is wrong")
	}
	for _, u := range got {
		if u.id == instantID {
			t.Errorf("probe priced InstantSpeed$ True source %d, which no payment window offers", instantID)
		}
	}
}

// TestCastWindowRestrictedAbilityStaysFailClosed is the restriction boundary:
// a RestrictValid$-governed producer must be priced by NEITHER the probe nor
// reachability, even though the live window still offers its tap. Pricing it
// would claim a batch the dotted matcher cannot prove can pay the cast.
func TestCastWindowRestrictedAbilityStaysFailClosed(t *testing.T) {
	b := equipWindowGame(t, 716, cwRestrictedSrc)
	e, beltID, smallID, bruteID := b.engine, b.belt, b.small, b.brute
	resID := b.byName["RestrictedRock"]
	if resID == 0 {
		t.Fatal("RestrictedRock fixture was not seeded onto the battlefield")
	}
	if e.Power(bruteID) != 4 || e.Power(smallID) != 2 {
		t.Fatalf("powers %d/%d, want 4/2", e.Power(bruteID), e.Power(smallID))
	}
	addMana(t, e, 0, "CCCCCC")
	d := cwArmBelt(t, e, beltID)
	if alts := cwProbeAlts(t, e, resID); alts != 0 {
		t.Fatalf("probe priced %d alternatives for a RestrictValid$-governed source; it must stay fail-closed", alts)
	}
	if cwOffersTarget(d, smallID) {
		t.Fatalf("2-power target offered although its {8} needs a RestrictValid$ source the probe must not price: %+v", d.Options)
	}
	if !cwOffersTarget(d, bruteID) {
		t.Fatalf("4-power target withheld at {6}: %+v", d.Options)
	}
	// The boundary is fail-closed, not a removal of the offer: the live window
	// still offers the restricted source's tap (untappedManaSource ignores
	// RestrictValid), so a later ticket that widens the matcher must add its
	// own pricing rather than rely on this probe.
	offered := false
	for _, z := range []state.Zone{state.ZBattlefield, state.ZHand, state.ZGraveyard} {
		for _, id := range e.G.Zone(z, 0) {
			if id == resID && e.untappedManaSource(0, id) {
				offered = true
			}
		}
	}
	if !offered {
		t.Fatal("precondition: the restricted source is not offered by the live window; the boundary test is vacuous")
	}
}
