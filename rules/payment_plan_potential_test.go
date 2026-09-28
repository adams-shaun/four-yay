package rules

import (
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestPaymentPlanPotentialUrborgGrantedIntrinsic(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	urborgCard, ok := reg.Lookup("Urborg, Tomb of Yawgmoth")
	if !ok {
		t.Fatal("corpus card Urborg missing")
	}
	e, _, spell := newFixtureDeck(t, 9411, "Name:Black Plan\nManaCost:B B\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	e.landTypeWords = corpusLandTypeWords(reg.Cards)
	e.layer4InPool = true
	urborg := onBoardCard(t, e, 0, urborgCard)
	mountain := onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	e.typesEpoch = -1
	e.refreshDerivedTypes()
	if !slices.Contains(e.Derived(mountain).Types, "Swamp") {
		t.Fatalf("precondition: Urborg did not grant Swamp to Mountain: %v", e.Derived(mountain).Types)
	}

	mana := e.availableManaAbilities(0, mountain)
	if len(mana) != 2 {
		t.Fatalf("precondition: Mountain mana abilities = %d, want printed R plus granted B", len(mana))
	}
	if !reflect.DeepEqual(e.PotentialMana(0), state.Mana{0, 0, 2, 1, 0, 0}) {
		t.Fatalf("PotentialMana = %v, want two black from Urborg and Mountain", e.PotentialMana(0))
	}
	if !slices.ContainsFunc(e.PotentialActions(0), func(a decision.PotentialAction) bool {
		return a.Kind == "cast" && a.Obj == spell
	}) {
		t.Fatalf("PotentialActions(%d) does not include cast %d", 0, spell)
	}

	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	used := map[state.ObjID]bool{}
	mountainBlack := false
	for _, activation := range a.Plans[0].Activations {
		used[activation.Source] = true
		if activation.Source == mountain && activation.Produces == (decision.ManaAmount{0, 0, 1, 0, 0, 0}) {
			mountainBlack = true
		}
	}
	if !used[mountain] || !mountainBlack {
		t.Fatalf("witness %#v, want Mountain's granted {B}", a.Plans[0].Activations)
	}
	if !used[urborg] {
		t.Fatalf("witness %#v does not use Urborg", a.Plans[0].Activations)
	}
	start := len(e.L.Events)
	submitPaymentPlan(t, e, d, a)
	if got := producedManaSince(e, start); !reflect.DeepEqual(got, []string{"B", "B"}) {
		t.Fatalf("planned payment produced %v, want [B B]", got)
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("spell zone = %s, want stack", e.G.Obj(spell).Zone)
	}
}
