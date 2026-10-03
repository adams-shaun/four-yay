package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// These are event-role tests, not claims of complete card/trigger support.
func TestTriggerReferentsUseEventRoles(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := e.G.Zone(state.ZLibrary, 0)[0]
	other := e.G.Zone(state.ZLibrary, 1)[0]
	e.damaging = other
	for _, tt := range []struct {
		mode string
		ev   events.Event
		want effects.TriggerContext
	}{
		{"BecomesTarget", events.Event{Kind: events.TargetsChosen, Obj: other, IDs: []state.ObjID{source}}, effects.TriggerContext{TriggerTarget: state.Target{Obj: source}, TriggerSource: other, TriggerStack: other}},
		{"DamageDone", events.Event{Kind: events.Damage, Obj: source}, effects.TriggerContext{TriggerTarget: state.Target{Obj: source}, TriggerSource: other}},
		{"DamageDone", events.Event{Kind: events.Damage, Player: 0}, effects.TriggerContext{TriggerTarget: state.Target{IsPlayer: true, Player: 0}, TriggerSource: other}},
		{"ChangesZone", events.Event{Kind: events.MoveZone, Obj: other}, effects.TriggerContext{TriggerCard: other}},
		{"SpellCast", events.Event{Kind: events.PutOnStack, Obj: other, Player: 1}, effects.TriggerContext{TriggerCard: other, TriggerSource: other, TriggerActivator: state.Target{IsPlayer: true, Player: 1}}},
		{"Phase", events.Event{Kind: events.StepChange, Step: state.StepUpkeep}, effects.TriggerContext{TriggerPlayer: state.Target{IsPlayer: true, Player: 0}}},
		{"Attacks", events.Event{Kind: events.DeclareAttackers, IDs: []state.ObjID{source}, Player: 1}, effects.TriggerContext{TriggerCard: source, TriggerSource: source, DefendingPlayer: state.Target{IsPlayer: true, Player: 1}, AttackingPlayer: state.Target{IsPlayer: true, Player: 0}, AttackedTarget: state.Target{IsPlayer: true, Player: 1}}},
		{"AttackersDeclaredOneTarget", events.Event{Kind: events.DeclareAttackers, IDs: []state.ObjID{other}, Player: 1}, effects.TriggerContext{DefendingPlayer: state.Target{IsPlayer: true, Player: 1}, AttackingPlayer: state.Target{IsPlayer: true, Player: 1}, AttackedTarget: state.Target{IsPlayer: true, Player: 1}}},
		{"Always", events.Event{Kind: events.Damage, Obj: other}, effects.TriggerContext{}},
	} {
		if got := e.triggerReferents(cards.Trigger{Mode: tt.mode}, source, tt.ev, nil); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %+v, want %+v", tt.mode, got, tt.want)
		}
	}
	if sc := e.specCtx(source, 0); !reflect.DeepEqual(sc.TriggerContext, effects.TriggerContext{}) {
		t.Fatal("ordinary static/trigger-match context inherited damage provenance")
	}
}

// Found by a compiled-corpus walk of abilities, trigger effects, replacement
// bodies and resolved SVars, deduped per face by Kind/API/Params. Master of
// Diversion's real Tap SA carries ControlledBy TriggeredDefendingPlayer.
func TestMasterOfDiversionTriggerReferent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	master, ok := reg.Lookup("Master of Diversion")
	if !ok {
		t.Fatal("missing corpus card")
	}
	bear, ok := reg.Lookup("Runeclaw Bear")
	if !ok {
		t.Fatal("missing corpus bear")
	}
	deck := append(mountainDeck(t, 40), master, bear)
	// Seat 0 (the attacker) is the protagonist; seatZeroStart advances the
	// seed until the CR 103.1 toss starts seat 0.
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"attacker", "bystander", "defender"}, Decks: [][]*cards.Card{deck, deck, deck}}))
	e.Advance()
	attacker := crAbortMove(t, e, 0, "Master of Diversion", state.ZBattlefield)
	mine := crAbortMove(t, e, 0, "Runeclaw Bear", state.ZBattlefield)
	bystander := crAbortMove(t, e, 1, "Runeclaw Bear", state.ZBattlefield)
	target := crAbortMove(t, e, 2, "Runeclaw Bear", state.ZBattlefield)
	tr := crTriggerFixture(t, e, attacker, "Attacks", "Tap")
	if tr.Effect.Params["ValidTgts"] != "Creature.ControlledBy TriggeredDefendingPlayer" {
		t.Fatal("corpus fixture changed")
	}
	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	d := e.Pending()
	pick := -1
	for _, o := range d.Options {
		if o.Obj == attacker && o.Player == 2 {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatal("attack against seat 2 not offered")
	}
	crAbortAnswer(t, e, "Master of Diversion attack", pick)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want target choice at placement, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != target {
		t.Fatalf("defender-only target options=%+v", d.Options)
	}
	clone := e.Clone()
	for _, engine := range []*Engine{e, clone} {
		crAbortAnswer(t, engine, "Master of Diversion target", 0)
		for i := 0; i < 3; i++ {
			crAbortAnswer(t, engine, "pass", crAbortOption(t, engine, "pass", "pass", 0))
		}
		if !engine.G.Obj(target).Tapped || engine.G.Obj(mine).Tapped || engine.G.Obj(bystander).Tapped {
			t.Fatal("must tap only defending player's creature")
		}
		if len(engine.triggerContexts) != 0 {
			t.Fatal("resolved trigger context retained")
		}
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across trigger target/resolve")
	}
}
