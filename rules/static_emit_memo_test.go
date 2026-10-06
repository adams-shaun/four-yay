package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestStaticMemoReusesAcrossColdTokenCreates(t *testing.T) {
	e := layerEngine(t)
	e.G.Tokens = make(map[string]*cards.Card)
	token := card(t, "Name:Plain token\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	setFixtureToken(e, "plain", token)
	e.refreshStaticContinuous()
	builds := e.staticBuildSeq
	for i := 0; i < 3; i++ {
		before := len(e.G.Objs)
		got := e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "plain"})
		if got.Kind != events.TokenCreate {
			t.Fatalf("precondition: TokenCreate emitted as %v", got.Kind)
		}
		if len(e.G.Objs) != before+1 {
			t.Fatalf("precondition: TokenCreate produced %d objects, want %d", len(e.G.Objs)-before, 1)
		}
		created := &e.G.Objs[len(e.G.Objs)-1]
		if created.Zone != state.ZBattlefield || objectStaticHotOn(created) {
			t.Fatal("precondition: created token is not a static-cold battlefield object")
		}
		e.refreshStaticContinuous()
		if e.staticBuildSeq != builds {
			t.Fatalf("cold TokenCreate %d rebuilt staticEffects: builds %d -> %d", i, builds, e.staticBuildSeq)
		}
	}
}

func TestStaticMemoDoesNotReuseAcrossStaticTokenCreates(t *testing.T) {
	e := layerEngine(t)
	e.G.Tokens = make(map[string]*cards.Card)
	token := card(t, "Name:Static token\nTypes:Creature Goblin\nPT:1/1\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddPower$ 1 | Description$ gets +1/+0\nOracle:x\n")
	setFixtureToken(e, "static", token)
	e.refreshStaticContinuous()
	builds := e.staticBuildSeq

	got := e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "static"})
	if got.Kind != events.TokenCreate || len(e.G.Objs) == 0 {
		t.Fatal("precondition: static TokenCreate did not create an object")
	}
	created := &e.G.Objs[len(e.G.Objs)-1]
	if created.Zone != state.ZBattlefield || !objectStaticHotOn(created) {
		t.Fatal("precondition: static token is not a static-hot battlefield object")
	}
	e.refreshStaticContinuous()
	if e.staticBuildSeq <= builds {
		t.Fatalf("static TokenCreate reused a stale memo: builds stayed at %d", builds)
	}
}
