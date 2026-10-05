package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func runTriggeredCostBlight(t *testing.T, cardName string, amount int32, body func(*testing.T, *Engine, int, state.ObjID) bool) {
	t.Helper()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, cardName, "Grizzly Bears")
	moveCorpusCardToBattlefield := func(name string) state.ObjID {
		for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(zone, 0) {
				o := e.G.Obj(id)
				if o != nil && o.Face() != nil && o.Face().Name == name {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: zone, To: state.ZBattlefield})
					return id
				}
			}
		}
		t.Fatalf("corpus fixture %q absent from hand/library", name)
		return 0
	}
	source := moveCorpusCardToBattlefield(cardName)
	creature := moveCorpusCardToBattlefield("Grizzly Bears")
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: source %q is not on battlefield: %v", cardName, o)
	}
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield || !o.EffectiveIsCreature() {
		t.Fatalf("precondition: blight candidate %d is not a battlefield creature: %v", creature, o)
	}
	// Dream Seizer needs an opponent card in hand for its post-payment body.
	var opponentCard state.ObjID
	if cardName == "Dream Seizer" {
		for _, id := range e.G.Zone(state.ZLibrary, 1) {
			opponentCard = id
			break
		}
		if opponentCard == 0 {
			t.Fatal("precondition: opponent has no library card")
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: opponentCard, From: state.ZLibrary, To: state.ZHand})
		if o := e.G.Obj(opponentCard); o == nil || o.Zone != state.ZHand || o.Owner != 1 {
			t.Fatalf("precondition: opponent discard card %d is not in opponent hand: %v", opponentCard, o)
		}
	}
	e.pending = nil
	e.priorityRound()
	d := passUntilNonPriority(t, e, 20)
	if d.Kind == decision.KTriggerOptional {
		submitChoices(t, e, 0)
	}
	payIndex, _ := triggerCostWindowAsk(t, e)
	if payIndex < 0 {
		t.Fatalf("Blight<%d> was not offered as payable: %+v", amount, e.Pending())
	}
	mark := len(e.L.Events)
	submitChoices(t, e, payIndex)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "trigger_cost_blight" {
		t.Fatalf("expected triggered blight creature choice, got %+v", d)
	}
	blightIndex := -1
	for _, option := range d.Options {
		if option.Obj == creature {
			blightIndex = option.Index
		}
	}
	if blightIndex < 0 {
		t.Fatalf("candidate creature %d not offered: %+v", creature, d.Options)
	}
	submitChoices(t, e, blightIndex)
	if cardName == "Dream Seizer" {
		d = passUntilNonPriority(t, e, 20)
		if d == nil {
			t.Fatal("expected opponent discard choice in paid body")
		}
		found := false
		for _, option := range d.Options {
			if option.Obj == opponentCard {
				submitChoices(t, e, option.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("opponent card %d not offered for discard: %+v", opponentCard, d.Options)
		}
	}
	passUntilStackEmpty(t, e, 30)
	if got := countCounterChanges(e, creature, "M1M1", amount); got != 1 {
		t.Fatalf("got %d CounterChange(M1M1,+%d) events on chosen creature, want exactly 1", got, amount)
	}
	if !body(t, e, mark, opponentCard) {
		t.Fatalf("paid trigger body for %q did not resolve", cardName)
	}
}

func TestTriggeredCostBlightBlightedBlackthorn(t *testing.T) {
	runTriggeredCostBlight(t, "Blighted Blackthorn", 2, func(t *testing.T, e *Engine, mark int, _ state.ObjID) bool {
		for _, event := range e.L.Events[mark:] {
			if event.Kind == events.Draw && event.Player == 0 {
				for _, life := range e.L.Events[mark:] {
					if life.Kind == events.LifeChange && life.Player == 0 && life.Amount == -1 {
						return true
					}
				}
			}
		}
		return false
	})
}

func TestTriggeredCostBlightDreamSeizer(t *testing.T) {
	runTriggeredCostBlight(t, "Dream Seizer", 1, func(_ *testing.T, e *Engine, _ int, card state.ObjID) bool {
		if card == 0 {
			return false
		}
		o := e.G.Obj(card)
		return o != nil && o.Zone == state.ZGraveyard
	})
}

func TestTriggeredCostBlightSourbreadAuntie(t *testing.T) {
	runTriggeredCostBlight(t, "Sourbread Auntie", 2, func(_ *testing.T, e *Engine, mark int, _ state.ObjID) bool {
		count := 0
		for _, event := range e.L.Events[mark:] {
			if event.Kind == events.TokenCreate && event.Text == "br_1_1_goblin" {
				count++
			}
		}
		return count == 2
	})
}
