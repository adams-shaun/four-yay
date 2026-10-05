package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func excessGalleonBoard(t *testing.T, reg *cards.Registry) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	e, _ := damageAllBoard4(t, reg, []string{"Magmatic Galleon"}, []string{"Grizzly Bears", "Hill Giant", "Kitesail Corsair"})
	g := findBattlefield(t, e, 0, "Magmatic Galleon", 0)
	f := e.G.Obj(g).Face()
	found := false
	if f != nil {
		for _, tr := range f.Triggers {
			if tr.Mode == "ExcessDamageAll" && tr.ParamStr(cards.PKCombatDamage) == "False" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("precondition: galleon not on battlefield with excess noncombat trigger: %+v", f)
	}
	targets := []state.ObjID{findBattlefield(t, e, 1, "Grizzly Bears", 0), findBattlefield(t, e, 1, "Hill Giant", 0), findBattlefield(t, e, 1, "Kitesail Corsair", 0)}
	for _, id := range targets {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || !e.IsCreature(id) || e.Toughness(id) <= 0 || o.Damage != 0 {
			t.Fatalf("precondition: target %d must be an undamaged battlefield creature", id)
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	// The fixture's battlefield move queued Galleon's separate ETB damage
	// ability. Isolate the excess line before testing its batch latch.
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != g {
		t.Fatalf("precondition: unexpected entry triggers: %+v", e.pendingTriggers)
	}
	e.pendingTriggers = nil
	e.dmgSrcOverride = g
	return e, g, targets
}

func TestExcessDamageAllMagmaticGalleonIsSupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card := searchCorpusCard(t, reg, "Magmatic Galleon")
	if d := card.Link(); len(d) != 0 {
		t.Fatalf("link Magmatic Galleon: %v", d)
	}
	if !effects.Supported()["trig:ExcessDamageAll"] {
		t.Fatal("effects.Supported() is missing trig:ExcessDamageAll")
	}
	found := false
	for _, face := range card.Faces {
		for _, trigger := range face.Triggers {
			if trigger.Mode == "ExcessDamageAll" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("precondition: Magmatic Galleon has no ExcessDamageAll trigger")
	}
	if unsupported := reg.Unsupported(card, effects.Supported()); len(unsupported) != 0 {
		t.Fatalf("Magmatic Galleon still unsupported: %v", unsupported)
	}
}

func TestExcessDamageAllAggregatesOnlyExcessPerBatch(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, g, targets := excessGalleonBoard(t, reg)
	a, b, c := targets[0], targets[1], targets[2]
	ta, tb, tc := e.Toughness(a), e.Toughness(b), e.Toughness(c)
	if ta == tb || tb == tc {
		t.Fatalf("precondition: distinct target toughness values: %d %d %d", ta, tb, tc)
	}
	// The first ordinary hit must not move the simultaneous baseline for a.
	e.BeginDamageBatch()
	e.emit(events.Event{Kind: events.Damage, Obj: a, Amount: 1})
	e.emit(events.Event{Kind: events.Damage, Obj: a, Amount: ta}) // equal to baseline: not excess
	e.emit(events.Event{Kind: events.Damage, Obj: b, Amount: tb + 2})
	e.emit(events.Event{Kind: events.Damage, Obj: c, Amount: tc + 3})
	e.EndDamageBatch()
	if e.G.Obj(a).Damage != ta+1 || e.G.Obj(b).Damage != tb+2 || e.G.Obj(c).Damage != tc+3 {
		t.Fatalf("precondition: damage did not land: %d %d %d", e.G.Obj(a).Damage, e.G.Obj(b).Damage, e.G.Obj(c).Damage)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("batch queued %d triggers, want 1", len(e.pendingTriggers))
	}
	ctx := e.pendingTriggers[0].Ctx.TriggerContext
	if ctx.TriggerAmount != 5 || len(ctx.TriggerDamageSources) != 1 || ctx.TriggerDamageSources[0] != g || len(ctx.TriggerDamageTargets) != 2 || ctx.TriggerDamageTargets[0].Obj != b || ctx.TriggerDamageTargets[1].Obj != c {
		t.Fatalf("batch excess context = %+v, want amount 5, source galleon, targets b/c", ctx)
	}
	// A separately closed batch has a new baseline and a new instance.
	e.BeginDamageBatch()
	e.emit(events.Event{Kind: events.Damage, Obj: a, Amount: 1})
	e.EndDamageBatch()
	if e.G.Obj(a).Damage != ta+2 || len(e.pendingTriggers) != 2 || e.pendingTriggers[1].Ctx.TriggerContext.TriggerAmount != 1 {
		t.Fatalf("second batch: damage=%d triggers=%d", e.G.Obj(a).Damage, len(e.pendingTriggers))
	}
	if got := e.pendingTriggers[1].Ctx.TriggerContext.TriggerDamageTargets; len(got) != 1 || got[0].Obj != a {
		t.Fatalf("second batch targets = %+v, want a", got)
	}
	e.dmgSrcOverride = 0
	e.resumeTriggerDrain()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder || len(d.Options) != 2 {
		t.Fatalf("two closed batches should pose order for two triggers: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
		t.Fatalf("submit trigger order: %v", err)
	}
	passUntilStackEmpty(t, e, 80)
	if got := combatTokenCount(e, 0, "Treasure Token"); got != 2 {
		t.Fatalf("resolved %d Treasures, want one per closed batch (2); pending=%d stack=%v token events=%d", got, len(e.pendingTriggers), e.G.Stack, countTokenCreations(e, 0))
	}
}

func TestExcessDamageAllNoncombatOnly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, targets := excessGalleonBoard(t, reg)
	a, b := targets[0], targets[1]
	ta, tb := e.Toughness(a), e.Toughness(b)
	if ta <= 0 || tb <= 0 || ta == tb {
		t.Fatalf("precondition: distinct positive toughness: %d %d", ta, tb)
	}
	e.emit(events.Event{Kind: events.Damage, Obj: a, Amount: ta + 1})
	if e.G.Obj(a).Damage != ta+1 || len(e.pendingTriggers) != 1 {
		t.Fatalf("noncombat: damage=%d queued=%d", e.G.Obj(a).Damage, len(e.pendingTriggers))
	}
	e.combatDamaging = true
	e.damaging = targets[0]
	e.emit(events.Event{Kind: events.Damage, Obj: b, Amount: tb + 1})
	e.combatDamaging = false
	e.damaging = 0
	if e.G.Obj(b).Damage != tb+1 || len(e.pendingTriggers) != 1 {
		t.Fatalf("combat: damage=%d queued=%d, want unchanged queue", e.G.Obj(b).Damage, len(e.pendingTriggers))
	}
}
