package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// boneSplintersShape is the reported card's cost shape: a fixed-count
// mandatory sacrifice on the spell ability beside a V1-clean mana half. The
// effect is an untargeted Draw so the fixture isolates the payment flow.
const boneSplintersShape = "Name:Bone Test\nManaCost:B\nTypes:Instant\nA:SP$ Draw | Cost$ B Sac<1/Creature> | NumCards$ 1\nOracle:x\n"

// TestPaymentPlanSeeksSacrificeOffer is the offer half of the fix: a
// Bone-Splinters-shaped cast receives a one-click payment action whose plan
// covers the mana half only. The mandatory sacrifice is answered through the
// ordinary in-flow ask after the plan is submitted (never baked into the
// witness), so the whole route -- offer builder, ValidateCastPayment, execute,
// sacrifice choose -- must settle exactly as the manual path does.
func TestPaymentPlanSeeksSacrificeOffer(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9601, boneSplintersShape)
	swamp := onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	victim := onBoard(t, e, 0, "Name:Victim Test\nManaCost:1\nTypes:Creature Test\nPT:1/1\nOracle:x\n")

	// Preconditions the assertions below depend on: the castable spell really
	// is in hand and the sacrifice candidate really is on the battlefield.
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: fixture spell object = %+v, want it in hand", o)
	}
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: sacrifice candidate = %+v, want it on the battlefield", o)
	}
	if detail := e.paymentPlanCastShapeDetail(0, spell); detail != "" {
		t.Fatalf("shape detail = %q, want the fixed-count sacrifice shape admitted", detail)
	}

	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	if len(a.Plans) != 1 || len(a.Plans[0].Activations) == 0 {
		t.Fatalf("offered plan = %+v, want one mana-only witness", a.Plans)
	}
	if a.Plans[0].Cost.Generic != 0 || a.Plans[0].Cost.Mana[state.ManaIndex('B')] != 1 {
		t.Fatalf("plan cost = %+v, want exactly {B}", a.Plans[0].Cost)
	}

	start := len(e.L.Events)
	submitPaymentPlan(t, e, d, a)

	// The additional cost is asked BEFORE the planned activation runs, exactly
	// as the executor's existing cast-time-choice coverage asserts.
	pd := e.Pending()
	if pd == nil || pd.Kind != decision.KChoose {
		t.Fatalf("pending = %s, want the sacrifice choice", paymentPlanPendingSummary(pd))
	}
	paymentPlanChooseObj(t, e, victim)

	nd := e.Pending()
	if nd == nil || nd.Kind != decision.KPriority {
		t.Fatalf("pending = %s, want the caster's priority after the settled cast", paymentPlanPendingSummary(nd))
	}
	if e.cast != nil || e.choosing != chooseNone {
		t.Errorf("priority posed mid-cast: cast pending=%v choosing=%d", e.cast != nil, e.choosing)
	}
	if !e.G.Obj(swamp).Tapped {
		t.Error("the witness's Swamp was not tapped")
	}
	if z := e.G.Obj(victim).Zone; z != state.ZGraveyard {
		t.Errorf("sacrificed creature zone = %s, want graveyard (the additional cost was not paid)", z)
	}
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Errorf("spell zone = %s, want stack", z)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Errorf("pool after payment = %d, want 0 (the produced B must pay the cost)", got)
	}
	if n := len(producedManaSince(e, start)); n != 1 {
		t.Errorf("produced %d mana, want exactly the witness's one black", n)
	}
}

// TestPaymentPlanSacrificeStalenessRejectedAtSubmit is the PP-14 half: a plan
// offered while a legal sacrifice candidate exists must NOT be committed once
// that candidate has left the battlefield. The submit is rejected by
// ValidateCastPayment with a clear error and nothing half-executes -- no mana
// produced, the spell still in hand.
func TestPaymentPlanSacrificeStalenessRejectedAtSubmit(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9602, boneSplintersShape)
	swamp := onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	victim := onBoard(t, e, 0, "Name:Victim Test\nManaCost:1\nTypes:Creature Test\nPT:1/1\nOracle:x\n")

	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: sacrifice candidate = %+v, want it on the battlefield", o)
	}
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	if len(a.Plans) == 0 {
		t.Fatal("planner offered no plan while the sacrifice candidate existed")
	}

	// The post-offer change: the only legal sacrifice candidate dies.
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: sacrifice candidate = %+v, want it in the graveyard", o)
	}

	start := len(e.L.Events)
	err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0,
		Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}})
	if err == nil {
		t.Fatal("submitted a plan whose sacrifice cost is no longer payable")
	}
	if !strings.Contains(err.Error(), "sacrifice") {
		t.Errorf("rejection error = %q, want it to name the sacrifice cost", err)
	}
	if n := len(producedManaSince(e, start)); n != 0 {
		t.Errorf("rejected submit produced %d mana, want 0 (never half-execute)", n)
	}
	if e.G.Obj(swamp).Tapped {
		t.Error("rejected submit tapped the planned source")
	}
	if z := e.G.Obj(spell).Zone; z != state.ZHand {
		t.Errorf("spell zone = %s, want hand (the cast must not happen)", z)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %s, want the origin priority preserved for a manual fallback", paymentPlanPendingSummary(d))
	}
}

// TestPaymentPlanOffersCorpusBoneSplinters covers the reported card itself:
// the real corpus script `Cost$ B Sac<1/Creature>` receives a V1 plan once a
// Swamp and a sacrificeable creature are available. This is the direct
// regression for the demo report.
func TestPaymentPlanOffersCorpusBoneSplinters(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Bone Splinters")
	if !ok {
		t.Fatal("corpus card Bone Splinters missing")
	}
	e, _, _ := newFixtureDeck(t, 9620, paymentPlanShock)
	toMain1(t, e)
	onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	victim := onBoard(t, e, 0, "Name:Victim\nManaCost:1\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
	spell := putInHand(t, e, 0, card)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand || o.Face().Name != "Bone Splinters" {
		t.Fatalf("precondition: got spell object %+v, want Bone Splinters in hand", o)
	}
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: sacrifice candidate = %+v, want it on the battlefield", o)
	}
	if detail := e.paymentPlanCastShapeDetail(0, spell); detail != "" {
		t.Fatalf("Bone Splinters shape detail = %q, want the fixed-count sacrifice shape admitted", detail)
	}
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil {
		t.Fatalf("Bone Splinters outcome = %+v, want a mana-only plan", got)
	}
	if got.Plan.Cost.Generic != 0 || got.Plan.Cost.Mana[state.ManaIndex('B')] != 1 {
		t.Fatalf("Bone Splinters plan cost = %+v, want exactly {B}", got.Plan.Cost)
	}
}

// TestPaymentPlanSacrificeShapeStillDeclines pins the boundary the gate
// change must not cross: variable-count sacrifice (Sac<X/...>), sacrifice-all
// (Sac<All/...>, which parses as Unknown) and a fixed sacrifice combined with
// any OTHER non-mana part stay withheld. This is the "class, not the
// instance" companion to the sacrifice offer above.
func TestPaymentPlanSacrificeShapeStillDeclines(t *testing.T) {
	t.Parallel()
	for i, tc := range []struct {
		name, face string
	}{
		{"sacX", "A:SP$ Draw | Cost$ B Sac<X/Creature> | NumCards$ 1"},
		{"sacAll", "A:SP$ Draw | Cost$ B Sac<All/Creature> | NumCards$ 1"},
		{"sacPlusDiscard", "A:SP$ Draw | Cost$ B Sac<1/Creature> Discard<1/Card> | NumCards$ 1"},
		{"sacPlusLife", "A:SP$ Draw | Cost$ B Sac<1/Creature> PayLife<1> | NumCards$ 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:Gate Boundary Spell\nManaCost:B\nTypes:Instant\n" + tc.face + "\nOracle:x\n"
			e, _, spell := newFixtureDeck(t, uint64(9610+i), src)
			onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
			victim := onBoard(t, e, 0, "Name:Victim\nManaCost:1\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
			// Precondition: the boundary cases that carry a Sac part DO have a
			// legal sacrifice candidate, so their decline is the shape gate and
			// not an empty board.
			if strings.Contains(tc.face, "Sac<") {
				if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: sacrifice candidate = %+v, want it on the battlefield", o)
				}
			}
			if detail := e.paymentPlanCastShapeDetail(0, spell); detail != "shape:additional_cost" {
				t.Fatalf("shape detail = %q, want shape:additional_cost", detail)
			}
			if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil {
				t.Fatalf("plan = %+v, want none for %s", got, tc.name)
			}
		})
	}
}
