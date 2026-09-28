package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func assertDamageCostLKI(t *testing.T, e *Engine, source state.ObjID, wantControllerLife int32) {
	t.Helper()
	if e.G.Obj(source).Zone != state.ZExile {
		t.Fatalf("source zone = %s, want exile", e.G.Obj(source).Zone)
	}
	if e.HasKeyword(source, "Infect") || e.HasKeyword(source, "Lifelink") {
		t.Fatal("precondition: departed source must no longer read Infect or Lifelink live")
	}
	var damage *events.Event
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.Damage && ev.Player == 1 {
			damage = ev
		}
	}
	if damage == nil || damage.Counter != "infect" || damage.Amount != 4 {
		t.Fatalf("payer damage = %+v, want DamageYou<4> in infect form", damage)
	}
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("payer life = %d, want 20 (infect damage does not reduce life)", got)
	}
	if got := e.G.Players[0].Life; got != wantControllerLife {
		t.Fatalf("source controller life = %d, want %d from lifelink LKI", got, wantControllerLife)
	}
}

// Exercise the real payCast snapshot: the activation exiles its keyword-bearing
// source as the first non-mana cost, then DamageYou is paid from payCast's saved
// pre-cost characteristics rather than a fabricated LKI value.
func TestDamageYouCostUsesSourceDamageKeywordLKI(t *testing.T) {
	t.Parallel()
	// The source's own battlefield statics disappear when it leaves, so the
	// live and captured keyword sets differ at damage payment.
	src := card(t, "Name:Cost LKI Engine\nTypes:Artifact Creature\nPT:1/1\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddKeyword$ Infect | Description$ CARDNAME has infect.\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddKeyword$ Lifelink | Description$ CARDNAME has lifelink.\n"+
		"A:AB$ GainLife | Cost$ Exile<1/CARDNAME> DamageYou<4> | Defined$ You | LifeAmount$ 0\nOracle:x\n")
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{src})
	source := e.G.Zone(state.ZBattlefield, 0)[0]
	if !e.HasKeyword(source, "Infect") || !e.HasKeyword(source, "Lifelink") {
		t.Fatal("precondition: battlefield source must have Infect and Lifelink before paying")
	}
	if e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatalf("precondition: source zone = %s, want battlefield", e.G.Obj(source).Zone)
	}
	e.askPriority(0)
	opt, ok := findAbilityOption(e, source, 0)
	if !ok {
		t.Fatal("the Exile+DamageYou ability was not offered")
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(source).Zone != state.ZExile {
		t.Fatalf("source after paying Exile cost = %s, want exile", e.G.Obj(source).Zone)
	}
	if e.HasKeyword(source, "Infect") || e.HasKeyword(source, "Lifelink") {
		t.Fatal("precondition: live source keywords must differ from the captured pre-cost keywords")
	}
	var found bool
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Player == 0 && ev.Counter == "infect" && ev.Amount == 4 {
			found = true
		}
	}
	if !found {
		t.Fatal("no infect-form DamageYou<4> event from the real activation cost")
	}
	if got := e.G.Players[0].Life; got != 24 {
		t.Fatalf("payer/controller life = %d, want 24 (infect prevents life loss; lifelink gains 4)", got)
	}
	replayCheck(t, e, cfg)
}

// Exercise the real resolution capture-and-forward path: Vexing Devil's ETB
// trigger exists on the stack when the Devil leaves, so Engine.emit must save
// the source's damage keywords into the trigger context before it resolves.
func TestUnlessDamageCostUsesSourceDamageKeywordLKI(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	original := mustCorpusCard(t, reg, "Vexing Devil")
	modified := *original
	modified.Faces = append([]*cards.Face(nil), original.Faces...)
	face := *original.Faces[0]
	face.Statics = append(append([]cards.Static(nil), face.Statics...),
		cards.Static{Mode: "Continuous", Params: map[string]string{"Affected": "Card.Self", "AddKeyword": "Infect"}},
		cards.Static{Mode: "Continuous", Params: map[string]string{"Affected": "Card.Self", "AddKeyword": "Lifelink"}})
	modified.Faces[0] = &face
	e := handEngine(t, &modified)
	var source state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Vexing Devil" {
			source = id
			break
		}
	}
	if source == 0 {
		t.Fatal("precondition: Vexing Devil is missing from seat 0's hand")
	}
	e.G.Players[0].Pool = state.Mana{state.MR: 1}
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, source))

	// Resolve the spell, stopping at the priority window while its ETB trigger
	// is waiting. This is the production stack/LKI boundary under test.
	var triggerWaiting bool
	for i := 0; i < 60 && e.Pending() != nil; i++ {
		triggerWaiting = false
		for _, id := range e.G.Stack {
			o := e.G.Obj(id)
			if o != nil && o.Ability != nil && o.Source == source {
				triggerWaiting = true
			}
		}
		if triggerWaiting && e.Pending().Kind == decision.KPriority {
			break
		}
		if e.Pending().Kind != decision.KPriority {
			t.Fatalf("unexpected decision %s before trigger priority window", e.Pending().Kind)
		}
		castFirst(t, e, "pass")
	}
	if !triggerWaiting {
		t.Fatal("precondition: Vexing Devil ETB trigger was not waiting on the stack")
	}
	if e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatalf("precondition: triggered source zone = %s, want battlefield", e.G.Obj(source).Zone)
	}
	if !e.HasKeyword(source, "Infect") || !e.HasKeyword(source, "Lifelink") {
		t.Fatal("precondition: trigger source must have Infect and Lifelink before departure")
	}
	// Leave while the real trigger is waiting; emit's LKI capture records these
	// characteristics on the resolving ability's context.
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZBattlefield, To: state.ZExile})
	if e.HasKeyword(source, "Infect") || e.HasKeyword(source, "Lifelink") {
		t.Fatal("precondition: after departure live keywords must differ from the saved values")
	}

	ask := drainUntilKModes(t, e, 60)
	if ask == nil {
		t.Fatal("no damage offer posed for the resolving Vexing Devil")
	}
	if ask.Player != 1 {
		t.Fatalf("damage offer player = %d, want opponent seat 1", ask.Player)
	}
	submitChoices(t, e, ask.Options[0].Index)
	assertDamageCostLKI(t, e, source, 24)
}
