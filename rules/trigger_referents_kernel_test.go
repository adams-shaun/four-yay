package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Synthetic: isolate target vs source with different controllers, then ask
// (a scry) before the filtered DamageAll; the trigger's TriggeredTarget
// referent must survive the ask, its re-executed answer, a stack copy's
// removal, and a clone taken at the posed decision.
func TestTriggeredTargetSurvivesAMidResolutionAsk(t *testing.T) {
	t.Parallel()
	watcher := card(t, `Name:Referent watcher
Types:Creature Wizard
PT:3/3
T:Mode$ DamageDone | ValidTarget$ Player | Execute$ Look
SVar:Look:DB$ Scry | Defined$ You | ScryNum$ 1 | SubAbility$ Hurt
SVar:Hurt:DB$ DamageAll | ValidCards$ Creature.ControlledBy TriggeredTarget | NumDmg$ 1
Oracle:synthetic context probe
`)
	bear := card(t, "Name:Probe bear\nTypes:Creature Bear\nPT:2/2\nOracle:synthetic\n")
	deck := append(mountainDeck(t, 40), watcher, bear)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"source", "target"}, Decks: [][]*cards.Card{deck, deck}}))
	e.Advance()
	source := crAbortMove(t, e, 0, "Referent watcher", state.ZBattlefield)
	mine := crAbortMove(t, e, 0, "Probe bear", state.ZBattlefield)
	theirs := crAbortMove(t, e, 1, "Probe bear", state.ZBattlefield)
	e.pending = nil
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	e.damaging = 0
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("queued=%d want 1", len(e.pendingTriggers))
	}
	tc := e.pendingTriggers[0].Ctx.TriggerContext
	if !tc.TriggerTarget.IsPlayer || tc.TriggerTarget.Player != 1 || tc.TriggerSource != source {
		t.Fatalf("wrong event provenance: %+v", tc)
	}
	e.putTriggersOnStack()
	original := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.StackCopy, Obj: original, Player: 0})
	copyID := e.G.Stack[len(e.G.Stack)-1]
	if copyID == original || !reflect.DeepEqual(e.triggerContexts[copyID], tc) {
		t.Fatal("stack copy lost trigger provenance")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: copyID, From: state.ZStack, To: state.ZExile})
	if len(e.triggerContexts) != 1 || !reflect.DeepEqual(e.triggerContexts[original], tc) {
		t.Fatal("removing copy damaged original trigger context")
	}
	// Resolve through real priority passes (a Submit-driven resolution, so
	// a clone at the posed decision re-runs its own engine's tape).
	e.priorityRound()
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		submitChoices(t, e, tapePassIndex(d))
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KArrange {
		t.Fatalf("want the posed scry, got %+v", d)
	}
	clone := e.Clone()
	for _, engine := range []*Engine{e, clone} {
		crAbortAnswer(t, engine, "scry", 0)
		if got := engine.G.Obj(theirs).Damage; got != 1 {
			t.Fatalf("TriggeredTarget creature damage = %d, want 1 (target player 1, source player 0)", got)
		}
		if got := engine.G.Obj(mine).Damage; got != 0 {
			t.Fatalf("source player's creature damage = %d, want 0", got)
		}
		if len(engine.triggerContexts) != 0 {
			t.Fatal("completed context leaked")
		}
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the posed trigger")
	}
}
