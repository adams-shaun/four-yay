package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Rings of Brighthearth watches the activation, not the discard: the
// keyword-granted ability must be visible to AbilityCast just like a printed
// activation. The second watcher exercises SpellAbilityCast's activation arm.
func TestGrantedCyclingActivationFiresAbilityCastWatchers(t *testing.T) {
	t.Parallel()
	mystic := mshCorpusCard(t, "Rhet-Tomb Mystic")
	rings := mshCorpusCard(t, "Rings of Brighthearth")
	const watcher = "Name:Activation Watcher\nTypes:Enchantment\n" +
		"T:Mode$ SpellAbilityCast | ValidSA$ Activated.!ManaAbility | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigDraw\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\nOracle:x\n"
	const beast = "Name:Vanilla Beast\nManaCost:1 G\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"
	e := handEngine(t, card(t, beast))
	mysticID := onBoardCard(t, e, 0, mystic)
	ringsID := onBoardCard(t, e, 0, rings)
	watchID := onBoard(t, e, 0, watcher)
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand || o.Face().HasKeyword("Cycling") {
		t.Fatalf("precondition: beast must be in hand without printed cycling: %+v", o)
	}
	for _, src := range []state.ObjID{mysticID, ringsID, watchID} {
		if o := e.G.Obj(src); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: watcher/grantor %d not on battlefield: %+v", src, o)
		}
	}
	if param, ok := e.derivedKeywordParam(id, "Cycling"); !ok || param != "1 U" {
		t.Fatalf("precondition: derived cycling = (%q, %v), want (1 U, true)", param, ok)
	}
	if len(e.G.Obj(ringsID).Face().Triggers) == 0 || e.G.Obj(ringsID).Face().Triggers[0].Mode != "AbilityCast" ||
		len(e.G.Obj(watchID).Face().Triggers) == 0 || e.G.Obj(watchID).Face().Triggers[0].Mode != "SpellAbilityCast" {
		t.Fatal("precondition: the two battlefield watchers must carry their activation triggers")
	}
	addMana(t, e, 0, "CCCU") // CU pays cycling; two colorless remain for Rings' copy
	opt, ok := grantedCyclingOption(e, 0, id)
	if !ok || opt.Keyword != "Cycling:1 U" {
		t.Fatalf("precondition: granted activation not offered: %+v, %v", opt, ok)
	}
	e.beginActivation(0, opt)
	submitChoices(t, e, 0)
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("precondition: paid cycling cost did not discard card: %s", got)
	}
	if got := countKind(e.L.Events, events.KeywordAbilityPush, id); got != 1 {
		t.Fatalf("precondition: keyword ability not minted: %d", got)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("two activation triggers must pose an order ask: %+v", d)
	}
	if len(e.pendingTriggers) != 2 || len(e.G.Stack) != 1 {
		t.Fatalf("precondition: wanted two pending triggers over one minted ability, got %d over %d", len(e.pendingTriggers), len(e.G.Stack))
	}
	ability := e.G.Stack[0]
	for _, tr := range e.pendingTriggers {
		if tr.Ctx.TriggerAbility != ability || e.G.Obj(ability).Ability == nil {
			t.Fatalf("activation trigger did not capture the minted stack ability: %+v", tr.Ctx.TriggerContext)
		}
	}
	submitChoices(t, e, 0, 1)
	for _, src := range []state.ObjID{ringsID, watchID} {
		if got := countKind(e.L.Events, events.TriggerPush, src); got != 1 {
			t.Fatalf("activation watcher %d pushed %d triggers, want 1", src, got)
		}
	}
	if len(e.G.Stack) != 3 {
		t.Fatalf("stack depth = %d, want cycling ability and both triggers", len(e.G.Stack))
	}
	hand := len(e.G.Zone(state.ZHand, 0))
	drainCopyPayAsks(t, e, 40, "pay")
	if got := copyCount(e, ability); got != 1 {
		t.Fatalf("Rings paid copy of the granted cycling ability = %d, want 1", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+3 {
		t.Fatalf("hand after watcher draw and original + copied cycling draws = %d, want %d", got, hand+3)
	}
}
