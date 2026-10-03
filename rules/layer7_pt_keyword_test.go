package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestLayer7PTLordSeesGrantedKeyword pins the layer-7 half of the kw:Flanking
// row's layer-walk narrowing: a layer-7 P/T static gated on a GRANTED keyword
// must see the grant. Windstorm Drake's `Mode$ Continuous |
// Affected$ Creature.withFlying+Other+YouCtrl | AddPower$ 1` must pump a
// creature whose flying comes from Levitation's layer-6
// `AddKeyword$ Flying` grant (CR 613 orders layer 6 strictly before layer 7,
// so the P/T applicability gate reads the finished grant stream). Before the
// fix chars.PT bound no keyword list at all, so the withFlying gate
// fell back to the printed face, missed the granted flying, and the bear sat
// at its printed 2/2.
func TestLayer7PTLordSeesGrantedKeyword(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	drake := onBoardCard(t, e, 0, corpusCard(t, "Windstorm Drake"))
	levitation := onBoardCard(t, e, 0, corpusCard(t, "Levitation"))
	bear := onBoardCard(t, e, 0, corpusCard(t, "Runeclaw Bear"))
	oppBear := onBoardCard(t, e, 1, corpusCard(t, "Runeclaw Bear"))

	// Preconditions: every permanent is on the battlefield the walks read;
	// the bear's printed face carries no Flying (the match must come from the
	// grant, not the printed face); Levitation's layer-6 grant is live in the
	// derived list; Windstorm Drake's layer-7 static is registered, is the
	// keyword-reading shape, and the bear's printed power differs from the
	// asserted value so the +1/+0 is observable.
	for _, id := range []state.ObjID{drake, levitation, bear, oppBear} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("permanent %d zone = %v, want battlefield", id, o.Zone)
		}
	}
	if e.G.Obj(bear).Face().HasKeyword("Flying") {
		t.Fatal("Runeclaw Bear prints K:Flying; the granted-keyword case is not exercised")
	}
	if p, tp := e.G.Obj(bear).Face().Power(), e.G.Obj(bear).Face().Toughness(); p != 2 || tp != 2 {
		t.Fatalf("Runeclaw Bear printed P/T = %d/%d, want 2/2 so the +1/+0 is observable", p, tp)
	}
	if !e.HasKeyword(bear, "Flying") {
		t.Fatal("Levitation's layer-6 flying grant did not reach the bear; setup is vacuous")
	}
	drakePump := false
	for i := range e.active() {
		ce := &e.active()[i]
		if ce.Source == drake && ce.Layer == LPT && effects.SpecReadsKeywords(ce.Affects) {
			drakePump = true
		}
	}
	if !drakePump {
		t.Fatal("Windstorm Drake's keyword-gated layer-7 pump is not active; setup is vacuous")
	}

	// The granted-flying bear gets the drake's +1/+0; the drake itself is
	// excluded by Other (despite printing Flying) and the opponent's bear by
	// YouCtrl.
	if got := e.Power(bear); got != 3 {
		t.Fatalf("granted-flying bear power = %d, want 3 (printed 2 + Windstorm Drake's +1/+0 over the Levitation grant)", got)
	}
	if got := e.Toughness(bear); got != 2 {
		t.Fatalf("granted-flying bear toughness = %d, want 2 (+1/+0 only)", got)
	}
	if got := e.Power(drake); got != 3 {
		t.Fatalf("Windstorm Drake power = %d, want 3 (Other excludes the drake's own pump)", got)
	}
	if got := e.Power(oppBear); got != 2 {
		t.Fatalf("opponent's bear power = %d, want 2 (YouCtrl excludes seat 1)", got)
	}
}

// TestLayer7NegativeKeywordGateSeesGrant pins the negative direction of the
// same binding through synthetic layer registrations: a layer-7 pump gated
// `withoutFlying` must STOP matching a creature once a layer-6 grant gives
// it flying (CR 613.8's test: applying the grant changes the match).
func TestLayer7NegativeKeywordGateSeesGrant(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	grantor := onBoard(t, e, 0, "Name:Grantor\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	pumper := onBoard(t, e, 0, "Name:Pumper\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bear := onBoard(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// A layer-6 flying grant (Other-scoped, so the grantor itself stays
	// flightless) and an older layer-7 `withoutFlying`-gated pump.
	// CR 613 orders layer 6 strictly before layer 7, so the pump's gate reads
	// the POST-grant list: the bear (granted flying) loses the pump, the
	// grantor and pumper keep it (they stay flightless).
	e.AddContinuous(state.ContinuousEffect{
		Source: grantor, Timestamp: 2, Layer: LAbilities,
		Affects: "Creature.Other+YouCtrl", Controller: 0,
		AddKeywords: []string{"Flying"},
	})
	e.AddContinuous(state.ContinuousEffect{
		Source: pumper, Timestamp: 1, Layer: LPT, Sub: SubModify,
		Affects: "Creature.Other+withoutFlying+YouCtrl", Controller: 0,
		AddPower: 1, AddToughness: 1,
	})

	// Preconditions: the flying grant is live on the bear, and the pump is
	// registered as a keyword-reading layer-7 effect.
	if !e.HasKeyword(bear, "Flying") {
		t.Fatal("the flying grant did not reach the bear; setup is vacuous")
	}
	pumpLive := false
	for i := range e.active() {
		ce := &e.active()[i]
		if ce.Source == pumper && ce.Layer == LPT && effects.SpecReadsKeywords(ce.Affects) {
			pumpLive = true
		}
	}
	if !pumpLive {
		t.Fatal("the withoutFlying layer-7 pump is not active; setup is vacuous")
	}

	if got := e.Power(bear); got != 2 {
		t.Fatalf("granted-flying bear power = %d, want 2 (the withoutFlying pump must stop matching after the grant)", got)
	}
	if got := e.Power(pumper); got != 2 {
		t.Fatalf("pumper power = %d, want 2 (Other excludes the pump's own source)", got)
	}
	if got := e.Power(grantor); got != 3 {
		t.Fatalf("flightless grantor power = %d, want 3 (printed 2 + the withoutFlying pump it still matches)", got)
	}
}
