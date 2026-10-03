package rules

// Secluded Courtyard's shape: "{T}: Add one mana of any color. Spend this mana
// only to cast a creature spell of the chosen type or activate an ability of
// a creature or creature card of the chosen type" -- RestrictValid$ with two
// dotted alternatives, Spell.Creature+ChosenType and
// Activated.Creature+ChosenType, both bound to the land's ETB ChooseType.
// The restricted unit pays for both alternatives of the chosen type, and for
// neither alternative of another type (CR 106.6 / 605.3a).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestChosenTypeRestrictedManaPaysBothAlternativesOnlyForTheChosenType(t *testing.T) {
	t.Parallel()
	courtyard := "Name:Courtyard\nManaCost:no cost\nTypes:Land\nK:ETBReplacement:Other:ChooseCT\n" +
		"SVar:ChooseCT:DB$ ChooseType | Defined$ You | Type$ Creature\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Any | RestrictValid$ Spell.Creature+ChosenType,Activated.Creature+ChosenType\nOracle:x\n"
	grunt := "Name:Grunt\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"
	pup := "Name:Pup\nManaCost:R\nTypes:Creature Jackal\nPT:2/1\nOracle:x\n"
	pumpGob := "Name:PumpGob\nManaCost:no cost\nTypes:Creature Goblin\nPT:1/1\n" +
		"A:AB$ Pump | Cost$ R | Defined$ Self | NumAtt$ +1 | SpellDescription$ +1/+0.\nOracle:x\n"
	pumpHound := "Name:PumpHound\nManaCost:no cost\nTypes:Creature Dog\nPT:1/1\n" +
		"A:AB$ Pump | Cost$ R | Defined$ Self | NumAtt$ +1 | SpellDescription$ +1/+0.\nOracle:x\n"
	s0 := []string{courtyard, grunt, pup, pumpGob, pumpHound}
	e, cfg, find := etbConfig(t, etbSeed(t, 7301, s0, nil), s0, nil)
	cy, gruntID, pupID := find("Courtyard", 0), find("Grunt", 0), find("Pup", 0)
	gob := moveByName(t, e, 0, "PumpGob", state.ZBattlefield)
	hound := moveByName(t, e, 0, "PumpHound", state.ZBattlefield)
	addMana(t, e, 0, "")

	play := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "play_land" && o.Obj == cy {
			play = o.Index
		}
	}
	if play < 0 {
		t.Fatalf("no play_land option for the courtyard: %+v", e.Pending().Options)
	}
	submitChoices(t, e, play)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("type choice = %+v", d)
	}
	goblin := -1
	for _, o := range d.Options {
		if o.Label == "Goblin" {
			goblin = o.Index
		}
	}
	if goblin < 0 {
		t.Fatalf("Goblin not among the type options: %+v", d.Options)
	}
	submitChoices(t, e, goblin)

	submitChoices(t, e, activateOption(t, e, cy))
	d = e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "mana" && o.Ability == 1 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the restricted ability was not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	submitChoices(t, e, manaOption(t, e.Pending(), "R"))
	if len(e.G.Players[0].RestrictedMana) == 0 {
		t.Fatalf("no restricted batch in the pool: %+v", e.G.Players[0].Pool)
	}

	offered := func(kind string, obj state.ObjID) bool {
		for _, o := range e.Pending().Options {
			if o.Kind == kind && o.Obj == obj {
				return true
			}
		}
		return false
	}
	if !offered("cast", gruntID) {
		t.Fatalf("a Goblin creature spell is payable with the restricted unit: %+v", e.Pending().Options)
	}
	if offered("cast", pupID) {
		t.Fatal("a Jackal creature spell must not be payable with Goblin-restricted mana")
	}
	if _, ok := findAbilityOption(e, gob, 0); !ok {
		t.Fatalf("a Goblin creature's ability is payable with the restricted unit: %+v", e.Pending().Options)
	}
	if _, ok := findAbilityOption(e, hound, 0); ok {
		t.Fatal("a Dog creature's ability must not be payable with Goblin-restricted mana")
	}
	castFirstOf(t, e, gruntID)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(gruntID).Zone; z != state.ZBattlefield {
		t.Fatalf("the Goblin paid with restricted mana is in %s, want the battlefield", z)
	}
	replayCheck(t, e, cfg)
}

// castFirstOf submits the cast option for obj.
func castFirstOf(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no cast option for %d: %+v", obj, e.Pending().Options)
}
