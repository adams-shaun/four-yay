package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestDamagedByMatchesOnlyTheBoundSourceThisTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	h := newHost(t, 2)
	a := putOnBattlefield(t, h.g, reg, "Hawkeye, Avenging Archer", 0)
	b := putOnBattlefield(t, h.g, reg, "Lightning Bolt", 0)
	victim := putOnBattlefield(t, h.g, reg, "Grizzly Bears", 1)
	untouched := putOnBattlefield(t, h.g, reg, "Runeclaw Bear", 1)
	recordDamageProvenance(h, a.ID, victim.ID)
	if len(victim.DamageTakenThisTurnBy) != 1 || victim.Zone != state.ZBattlefield {
		t.Fatalf("precondition: victim must be a battlefield object with Hawkeye in this-turn provenance: %+v", victim)
	}
	if got := UnknownPredicates("Creature.DamagedBy"); len(got) != 0 {
		t.Fatalf("DamagedBy is not classified: %v", got)
	}
	if !MatchesObjectCtx(h.g, "Creature.DamagedBy", victim, SpecContext{Source: a.ID, You: 0}) {
		t.Fatal("victim damaged by bound source must match")
	}
	if MatchesObjectCtx(h.g, "Creature.DamagedBy", victim, SpecContext{Source: b.ID, You: 0}) {
		t.Fatal("victim must not match a different bound source")
	}
	if MatchesObjectCtx(h.g, "Creature.DamagedBy", untouched, SpecContext{Source: a.ID, You: 0}) {
		t.Fatal("undamaged creature must not match")
	}
}

func TestDamagedByIsPerTurnNotPerGame(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	h := newHost(t, 2)
	src := putOnBattlefield(t, h.g, reg, "Hawkeye, Avenging Archer", 0)
	victim := putOnBattlefield(t, h.g, reg, "Grizzly Bears", 1)
	recordDamageProvenance(h, src.ID, victim.ID)
	if len(victim.DamageTakenByGame) == 0 || len(victim.DamageTakenThisTurnBy) == 0 {
		t.Fatal("precondition: damage fold must populate both distinct provenance records")
	}
	events.Apply(h.g, events.Event{Kind: events.TurnChange, Player: 1})
	if len(victim.DamageTakenByGame) == 0 || len(victim.DamageTakenThisTurnBy) != 0 {
		t.Fatalf("TurnChange must preserve game history and clear turn history: game=%v turn=%v", victim.DamageTakenByGame, victim.DamageTakenThisTurnBy)
	}
	sc := SpecContext{Source: src.ID, You: 0}
	if !MatchesObjectCtx(h.g, "Creature.wasDealtDamageByThisGame", victim, sc) {
		t.Fatal("game-long predicate must still match after TurnChange")
	}
	if MatchesObjectCtx(h.g, "Creature.DamagedBy", victim, sc) {
		t.Fatal("per-turn DamagedBy must not match after TurnChange")
	}
}

func TestDamagedByTypeFormsMatchTheSource(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	h := newHost(t, 2)
	giant := putOnBattlefield(t, h.g, reg, "Aegar, the Freezing Flame", 0)
	spider := putOnBattlefield(t, h.g, reg, "Shelob, Child of Ungoliant", 0)
	card := putOnBattlefield(t, h.g, reg, "Hawkeye, Avenging Archer", 0)
	victim := putOnBattlefield(t, h.g, reg, "Grizzly Bears", 1)
	if giant.Face() == nil || !giant.Face().IsCreature() || spider.Face() == nil || !spider.Face().IsCreature() {
		t.Fatal("precondition: type-form sources must be real battlefield creatures")
	}
	for _, tc := range []struct {
		name string
		src  *state.Object
		spec string
	}{
		{"Giant", giant, "Creature.DamagedByGiant.YouCtrl"},
		{"Card", card, "Creature.DamagedByCard.YouCtrl"},
		{"Spider", spider, "Creature.DamagedBySpider.YouCtrl"},
	} {
		recordDamageProvenance(h, tc.src.ID, victim.ID)
		if !MatchesObjectCtx(h.g, tc.spec, victim, SpecContext{Source: card.ID, You: 0}) {
			t.Errorf("%s source should satisfy %s", tc.name, tc.spec)
		}
		if MatchesObjectCtx(h.g, tc.spec, victim, SpecContext{Source: victim.ID, You: 1}) {
			t.Errorf("uncontrolled %s source should not satisfy %s", tc.name, tc.spec)
		}
	}
	for _, spec := range []string{
		"Creature.DamagedByCard.YouCtrl",
		"Creature.DamagedByGiant.YouCtrl",
		"Creature.DamagedBySpider.YouCtrl",
	} {
		if got := UnknownPredicates(spec); len(got) != 0 {
			t.Errorf("type form %q unexpectedly unknown: %v", spec, got)
		}
	}

	if MatchesObjectCtx(h.g, "Creature.!DamagedBy", victim, SpecContext{You: 0}) {
		t.Fatal("negated bare DamagedBy without a bound source must fail closed")
	}
}
