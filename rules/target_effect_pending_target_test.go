package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// pendingTargetFixture poses a KTarget ask for a DealDamage spell on the
// stack with creatures on seat 0's battlefield, the way round 1's
// stackTargetEffect does, and returns the pending decision.
func pendingTargetFixture(t *testing.T, src string, creatures ...string) (*Engine, *decision.Decision) {
	t.Helper()
	args := append([]string{src}, creatures...)
	e, _, spell := newFixtureDeck(t, 7, args[0], args[1:]...)
	for _, csrc := range creatures {
		moveSeeded(t, e, 0, csrc, state.ZBattlefield)
	}
	e.emit(events.Event{Kind: events.PutOnStack, Obj: spell, Player: 0,
		From: state.ZHand, To: state.ZStack, Text: "pending-target fidelity"})
	e.emit(events.Event{Kind: events.CastInfo, Obj: spell, Amount: 0})
	o := e.G.Obj(spell)
	if o == nil || o.Zone != state.ZStack {
		t.Fatalf("fixture precondition: spell is not on stack: %+v", o)
	}
	sa := o.Face().SpellAbility()
	if sa == nil || sa.API != "DealDamage" {
		t.Fatalf("fixture precondition: spell ability = %+v", sa)
	}
	e.askTarget(0, spell, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("fixture precondition: target decision = %+v", d)
	}
	return e, d
}

// TestTargetEffectWithholdsAmountForPendingTargetDependentBody pins the
// Kiku's Shadow shape: NumDmg$ X backed by SVar:X:Targeted$CardPower, where
// the amount the spell will deal is the CHOSEN creature's power. At the
// CR 601.2c ask no target is bound, so the unbound evaluation is the empty
// target set's sum -- a legitimate zero the evaluator returns -- and the
// payload must NOT publish it: against a legal 5/5 the spell deals 5, so a
// published 0 is a false nominal amount, not an unknown one. The amount
// stays null until a target-dependent representation exists.
func TestTargetEffectWithholdsAmountForPendingTargetDependentBody(t *testing.T) {
	t.Parallel()
	e, d := pendingTargetFixture(t, corpusCardText(t, "k/kikus_shadow.txt"),
		"Name:Bear Fixture\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
		"Name:Giant Fixture\nManaCost:3 R\nTypes:Creature Giant\nPT:3/3\nOracle:x\n")
	spell := d.Source
	sa := e.G.Obj(spell).Face().SpellAbility()
	if sa.Params["NumDmg"] != "X" || e.G.Obj(spell).Face().SVars["X"] != "Targeted$CardPower" {
		t.Fatalf("fixture precondition: corpus Kiku's Shadow shape moved: NumDmg=%q SVars=%v",
			sa.Params["NumDmg"], e.G.Obj(spell).Face().SVars)
	}
	// Precondition: the ask actually offers pickable creatures and their
	// powers differ, so the eventual amount genuinely depends on the choice.
	if len(d.Options) < 2 {
		t.Fatalf("fixture precondition: only %d target options: %+v", len(d.Options), d.Options)
	}
	powers := map[int32]bool{}
	for _, o := range d.Options {
		if o.Obj == 0 {
			t.Fatalf("fixture precondition: non-object option in a Creature ask: %+v", o)
		}
		powers[e.Power(o.Obj)] = true
	}
	if len(powers) < 2 {
		t.Fatalf("fixture precondition: candidate powers %v do not differ; "+
			"the pending-target dependence would be unprovable", powers)
	}
	if d.TargetEffect == nil || d.TargetEffect.Damage == nil {
		t.Fatalf("fixture precondition: no damage effect payload: %+v", d.TargetEffect)
	}
	if d.TargetEffect.Damage.Amount != nil {
		t.Fatalf("pending-target-dependent amount published a scalar %d; "+
			"the chosen creature's power is not known at the ask", *d.TargetEffect.Damage.Amount)
	}
}

// TestTargetEffectWithholdsInlinePendingTargetBody covers the second probe
// site: an INLINE Targeted$ NumDmg body (no SVar indirection) must be
// withheld the same way, while a genuinely target-independent body posed by
// the same helper still publishes (round 1's SVar coverage already pins
// Count$YourLifeTotal and Count$xPaid through the SVar branch).
func TestTargetEffectWithholdsInlinePendingTargetBody(t *testing.T) {
	t.Parallel()
	_, d := pendingTargetFixture(t,
		"Name:Inline Ref Bolt\nManaCost:R\nTypes:Instant\n"+
			"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ Targeted$CardPower\nOracle:x\n",
		"Name:Bear Fixture\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
		"Name:Giant Fixture\nManaCost:3 R\nTypes:Creature Giant\nPT:3/3\nOracle:x\n")
	if d.TargetEffect == nil || d.TargetEffect.Damage == nil {
		t.Fatalf("fixture precondition: no damage effect payload: %+v", d.TargetEffect)
	}
	if d.TargetEffect.Damage.Amount != nil {
		t.Fatalf("inline pending-target body published a scalar %d", *d.TargetEffect.Damage.Amount)
	}
}

// TestTargetEffectPublishesTargetIndependentBodyThroughTheSameProbe is the
// positive control the two withholding tests need: a Count$ body that does
// not read the target reference family keeps its round-1 publishable
// amount even though the probe now runs on every posed damage ask.
func TestTargetEffectPublishesTargetIndependentBodyThroughTheSameProbe(t *testing.T) {
	t.Parallel()
	e, d := pendingTargetFixture(t,
		"Name:Independent Bolt\nManaCost:R\nTypes:Instant\n"+
			"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ X\n"+
			"SVar:X:Count$YourLifeTotal\nOracle:x\n",
		"Name:Bear Fixture\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if d.TargetEffect == nil || d.TargetEffect.Damage == nil || d.TargetEffect.Damage.Amount == nil {
		t.Fatalf("fixture precondition: independent body lost its payload: %+v", d.TargetEffect)
	}
	if got := *d.TargetEffect.Damage.Amount; got != 20 {
		t.Fatalf("independent body amount = %d, want the controller's 20 life", got)
	}
	if len(d.Options) == 0 {
		t.Fatal("fixture precondition: no target options posed")
	}
	if e.Pending() == nil {
		t.Fatal("fixture precondition: pending decision cleared")
	}
}
