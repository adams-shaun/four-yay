package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestStaticGoadSurvivesInDiesTriggerLKI verifies Baeloth's real goaded-
// attacker dies trigger evaluates IsGoaded against the creature's last-known
// battlefield state, even though the object has already moved to the graveyard.
func TestStaticGoadSurvivesInDiesTriggerLKI(t *testing.T) {
	t.Parallel()
	e := goadGrantEngine(t)
	baelothCard := corpusAlternativeCard(t, "Baeloth Barrityl, Entertainer")
	baeloth := onBoardCard(t, e, 0, baelothCard)
	victim := onBoardReady(t, e, 1, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n")
	v := e.G.Obj(victim)
	v.IsAttacking = true
	v.Attacking = 2

	// Preconditions: both objects occupy the battlefield where the static is
	// active, the victim is an attacking creature, and the printed static (not
	// an event-backed Goad event) currently goads it.
	if e.G.Obj(baeloth).Zone != state.ZBattlefield || v.Zone != state.ZBattlefield || !v.IsAttacking {
		t.Fatal("precondition: Baeloth and its attacking victim must be on the battlefield")
	}
	if len(v.Goads) != 0 || !e.hasActiveGoad(v) {
		t.Fatalf("precondition: victim's event-backed goads=%v, active static goad=%v; want no event goad and one active static", v.Goads, e.hasActiveGoad(v))
	}

	var diesTrigger *cards.Trigger
	for i := range e.G.Obj(baeloth).Face().Triggers {
		tr := &e.G.Obj(baeloth).Face().Triggers[i]
		if tr.Mode == "ChangesZone" && strings.Contains(tr.Params["ValidCard"], "IsGoaded") {
			diesTrigger = tr
			break
		}
	}
	if diesTrigger == nil {
		t.Fatal("precondition: Baeloth's corpus ChangesZone trigger with IsGoaded was not parsed")
	}

	lki := v.CloneDeep()
	ev := e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield,
		To: state.ZGraveyard, Player: v.Controller})
	if live := e.G.Obj(victim); live == nil || live.Zone != state.ZGraveyard {
		t.Fatalf("precondition: victim's current object is not in the graveyard: %+v", live)
	}
	if lki.Zone != state.ZBattlefield || !lki.IsAttacking {
		t.Fatalf("precondition: LKI must retain attacking battlefield state: zone=%v attacking=%v", lki.Zone, lki.IsAttacking)
	}
	if !e.zoneChangeMatches(*diesTrigger, baeloth, ev, &lki) {
		t.Fatal("Baeloth's goaded-attacker dies trigger did not match the statically goaded LKI")
	}
}
