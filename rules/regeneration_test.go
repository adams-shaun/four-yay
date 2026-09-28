package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func regenFixture(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Regenerator\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	regenEffect(e, id, "Regenerate", nil)
	return e, id
}
func regenEffect(e *Engine, id state.ObjID, api string, params map[string]string) {
	effects.Resolve(e, &effects.Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, &cards.SA{API: api, Params: params})
}
func regenSurvived(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || !o.Tapped || o.Damage != 0 || o.Counter("Shield") != 0 || o.IsAttacking {
		t.Fatalf("replacement incomplete: zone=%v tapped=%v damage=%d shields=%d attacking=%v", o.Zone, o.Tapped, o.Damage, o.Counter("Shield"), o.IsAttacking)
	}
}
func TestRegenerationCombat(t *testing.T) {
	t.Parallel()
	for _, blocker := range []bool{false, true} {
		name := "attacker"
		if blocker {
			name = "blocker"
		}
		t.Run(name, func(t *testing.T) {
			e, id := regenFixture(t)
			other := onBoard(t, e, 1, "Name:Other\nManaCost:1 G\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
			aid, bid := id, other
			if blocker {
				aid, bid = other, id
			}
			e.emit(events.Event{Kind: events.TurnChange, Player: e.G.Obj(aid).Controller, Amount: 1})
			e.emit(events.Event{Kind: events.DeclareAttackers, IDs: []state.ObjID{aid}, Player: e.G.Obj(bid).Controller})
			e.emit(events.Event{Kind: events.DeclareBlockers, Pairs: [][2]state.ObjID{{aid, bid}}})
			replayed := e.G.Clone()
			start := len(e.L.Events)
			e.damageStep(false)
			e.checkStateBased()
			regenSurvived(t, e, id)
			for _, ev := range e.L.Events[start:] {
				events.Apply(replayed, ev)
			}
			if !reflect.DeepEqual(replayed, e.G) {
				t.Fatal("regeneration combat events do not replay exactly")
			}
			if blocker {
				if len(e.G.Obj(aid).BlockedBy) == 0 {
					t.Fatal("attacker must remain blocked")
				}
				life := e.G.Players[0].Life
				e.damageStep(false)
				if e.G.Players[0].Life != life || e.G.Obj(id).Damage != 0 {
					t.Fatal("removed blocker must neither fight again nor unblock the attacker")
				}
				for _, b := range e.G.Obj(aid).BlockedBy {
					if b == id {
						t.Fatal("regenerated blocker remains in combat")
					}
				}
			}
		})
	}
}
func TestRegenerationConsumed(t *testing.T) {
	t.Parallel()
	e, id := regenFixture(t)
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 2})
	e.checkStateBased()
	regenSurvived(t, e, id)
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 2})
	e.checkStateBased()
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatal("second lethal damage must kill")
	}
}
func TestRegenerationDeathtouch(t *testing.T) {
	t.Parallel()
	e, id := regenFixture(t)
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "Deathtouched", Amount: 1})
	e.checkStateBased()
	regenSurvived(t, e, id)
	if e.G.Obj(id).Counter("Deathtouched") != 0 {
		t.Fatal("regeneration must clear the deathtouch damage marker")
	}
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 1})
	e.checkStateBased()
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("old deathtouch marker killed creature on fresh nonlethal damage")
	}
}

func TestRegenerationDestroy(t *testing.T) {
	t.Parallel()
	for _, api := range []string{"Destroy", "DestroyAll"} {
		t.Run(api, func(t *testing.T) {
			e, id := regenFixture(t)
			e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 1})
			regenEffect(e, id, api, nil)
			regenSurvived(t, e, id)
		})
	}
}
func TestRegenerationCannotReplaceNoRegen(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Terror", "Wrath of God", "Nekrataal"} {
		t.Run(name, func(t *testing.T) {
			card, ok := reg.Lookup(name)
			if !ok {
				t.Fatalf("missing corpus card %s", name)
			}
			abilities := append([]*cards.SA(nil), card.Faces[0].Abilities...)
			for _, trigger := range card.Faces[0].Triggers {
				abilities = append(abilities, trigger.Effect)
			}
			var destroy *cards.SA
			for _, sa := range abilities {
				if sa != nil && (sa.API == "Destroy" || sa.API == "DestroyAll") {
					destroy = sa
					break
				}
			}
			if destroy == nil || destroy.Params["NoRegen"] != "True" {
				t.Fatal("missing real NoRegen destruction SA")
			}
			e, id := regenFixture(t)
			source := e.G.AddObject(card, 0)
			start := len(e.L.Events)
			effects.Resolve(e, &effects.Ctx{Source: source.ID, Controller: 0, Targets: []state.Target{{Obj: id}}, TargetsOffered: true}, destroy)
			if e.G.Obj(id).Zone != state.ZGraveyard {
				t.Fatalf("shielded creature survived; zone=%v", e.G.Obj(id).Zone)
			}
			for _, ev := range e.L.Events[start:] {
				if ev.Kind == events.CounterChange && ev.Counter == "Shield" && ev.Amount < 0 {
					t.Fatal("NoRegen destruction consumed a regeneration shield")
				}
			}
		})
	}
}

func TestRegenerationDoesNotReplaceOtherMoves(t *testing.T) {
	t.Parallel()
	for _, api := range []string{"Sacrifice", "ChangeZone", "zero toughness"} {
		t.Run(api, func(t *testing.T) {
			e, id := regenFixture(t)
			want := state.ZGraveyard
			switch api {
			case "zero toughness":
				e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LPT, Sub: SubModify, Affects: "Card.Self", Controller: 0, AddToughness: -2})
				e.checkStateBased()
			case "ChangeZone":
				want = state.ZExile
				regenEffect(e, id, api, map[string]string{"Origin": "Battlefield", "Destination": "Exile"})
			default:
				regenEffect(e, id, api, nil)
			}
			if e.G.Obj(id).Zone != want {
				t.Fatalf("%s: shield incorrectly saved creature", api)
			}
		})
	}
}
func TestRegenerationExpires(t *testing.T) {
	t.Parallel()
	e, id := regenFixture(t)
	regenEffect(e, id, "Regenerate", nil)
	if e.G.Obj(id).Counter("Shield") != 2 {
		t.Fatal("two grants must stack")
	}
	e.setStep(state.StepCleanup)
	e.priorityRound()
	if e.G.Obj(id).Counter("Shield") != 0 {
		t.Fatal("unused shields survived cleanup")
	}
}
func TestRegenerationTwoShields(t *testing.T) {
	t.Parallel()
	e, id := regenFixture(t)
	regenEffect(e, id, "Regenerate", nil)
	regenEffect(e, id, "Destroy", nil)
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Counter("Shield") != 1 {
		t.Fatal("first destruction should consume exactly one shield")
	}
	start := len(e.L.Events)
	regenEffect(e, id, "Destroy", nil)
	regenSurvived(t, e, id)
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Tap && ev.Obj == id {
			t.Fatal("already-tapped regenerator emitted a redundant Tap")
		}
	}
	regenEffect(e, id, "Destroy", nil)
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatal("third destruction must kill")
	}
}
func TestRegenerationExperimentOne(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Experiment One")
	if !ok {
		t.Fatal("missing Experiment One")
	}
	e := layerEngine(t)
	o := e.G.AddObject(c, 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
	found := false
	for _, sa := range c.Faces[0].Abilities {
		if sa.API == "Regenerate" {
			effects.Resolve(e, &effects.Ctx{Source: o.ID, Controller: 0}, sa)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing real regeneration ability")
	}
	e.emit(events.Event{Kind: events.Damage, Obj: o.ID, Amount: 1})
	e.checkStateBased()
	regenSurvived(t, e, o.ID)
}
