package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const powerUpFixtureSrc = "Name:PowerUpBeast\nManaCost:0\nTypes:Creature Beast\nPT:2/2\n" +
	"A:AB$ PutCounter | Cost$ 1 | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | PowerUp$ True | SpellDescription$ Put a +1/+1 counter on this creature.\nOracle:x\n"

func assertPowerUpOfferedOnce(t *testing.T, e *Engine, id state.ObjID, ability int) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: PowerUp source %d not on battlefield: %+v", id, o)
	}
	if ability < 0 || ability >= len(o.Face().Abilities) ||
		o.Face().Abilities[ability].Params["PowerUp"] != "True" {
		t.Fatalf("precondition: ability %d is not PowerUp$ True", ability)
	}
	if used := e.activationUsedCount(id, ability, "", false); used != 0 {
		t.Fatalf("precondition: expected no previous activation, got %d", used)
	}
	// Two colorless mana fund two {1} activations; withholding the second
	// option therefore cannot be attributed to the pool.
	addMana(t, e, 0, "CC")
	if _, ok := findAbilityOption(e, id, ability); !ok {
		t.Fatalf("PowerUp ability absent before its first use: %+v", e.Pending().Options)
	}
	opt := abilityOption(t, e, id, ability)
	submitChoices(t, e, opt.Index)
	if used := e.activationUsedCount(id, ability, "", false); used != 1 {
		t.Fatalf("precondition: first activation census = %d, want 1", used)
	}
	if _, ok := findAbilityOption(e, id, ability); ok {
		t.Fatalf("PowerUp ability offered after first activation despite once-per-card-life limit: %+v", e.Pending().Options)
	}
}

func TestPowerUpFixtureWithholdsSecondActivation(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 77, powerUpFixtureSrc)
	moveByName(t, e, 0, "PowerUpBeast", state.ZBattlefield)
	assertPowerUpOfferedOnce(t, e, id, 0)
}

func TestPowerUpRealCorpusBoldBiochemistWithholdsSecondActivation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Bold Biochemist")
	if !ok {
		t.Fatal("corpus fixture: Bold Biochemist missing")
	}
	e := New(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{card}, mountainDeck(t, 40)...), mountainDeck(t, 40)}})
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Bold Biochemist", state.ZBattlefield)
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Bold Biochemist not on battlefield: %+v", e.G.Obj(id))
	}
	face := e.G.Obj(id).Face()
	ability := -1
	for i, sa := range face.Abilities {
		if sa.Params["PowerUp"] == "True" {
			ability = i
			break
		}
	}
	if ability < 0 {
		t.Fatal("precondition: Bold Biochemist has no PowerUp$ True ability")
	}
	// Its {5}{U} cost twice is funded in full, independent of entry-turn
	// reduction; any second-offer suppression is the PowerUp restriction.
	addMana(t, e, 0, "CCCCCCCCCCCCUU")
	if _, ok := findAbilityOption(e, id, ability); !ok {
		t.Fatalf("Bold Biochemist PowerUp absent before first use: %+v", e.Pending().Options)
	}
	opt := abilityOption(t, e, id, ability)
	submitChoices(t, e, opt.Index)
	if used := e.activationUsedCount(id, ability, "", false); used != 1 {
		t.Fatalf("precondition: first PowerUp activation census = %d, want 1", used)
	}
	if _, ok := findAbilityOption(e, id, ability); ok {
		t.Fatalf("Bold Biochemist PowerUp offered a second time with two {5}{U} payments funded: %+v", e.Pending().Options)
	}
}
