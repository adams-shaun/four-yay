package templates

// The spell-cast condition recipes: one real card per condition shape the
// spell-cast families serve. Each item is generated through
// triggerRequirement (GenerateB), plays through gorge
// (oraclegen.PlaysThrough), and a snapshot shows the card's trigger on the
// stack. Shapes:
//
//   - opponent-turn (main1@p1 checkpoint): Nightmare Sower, Dream Spoilers,
//     Brineborn Cutthroat (the census-pinned FDN carrier).
//   - attacking presence: Fire Lord Azula (IsPresent$ Card.Self+attacking,
//     the attack-then-cast cause).
//   - singleTarget: Spinerock Tyrant (ValidSA$ ...singleTarget).
//   - prepared-copy cast: Codie, Ravenous Codex (ValidCard$ Card.prepared,
//     the Wordsmith's exile copy cast through cast_mode prepared_copy).
//   - ManaCostPartialBlue: Namor the Sub-Mariner.

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestSpellCastConditionRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, sub   string
		wantCastMode     string // a cast step's cast_mode the cause must carry, "" for none
		wantSetupCard    string // a carrier the setup must place on p0's battlefield
		wantOpponentTurn bool   // the cause casts on p1's turn (a main1@p1 checkpoint)
	}{
		{"Nightmare Sower", "trigger#0.0", "trigger.spell-cast-opponent-turn", "", "", true},
		{"Dream Spoilers", "trigger#0.0", "trigger.spell-cast-opponent-turn", "", "", true},
		{"Brineborn Cutthroat", "trigger#0.0", "trigger.spell-cast-opponent-turn", "", "", true},
		{"Fire Lord Azula", "trigger#0.0", "trigger.spell-cast", "", "", false},
		{"Spinerock Tyrant", "trigger#0.0", "trigger.spell-cast", "", "", false},
		{"Codie, Ravenous Codex", "trigger#0.0", "trigger.spell-cast", "prepared_copy", "Whiplash Wordsmith", false},
		{"Namor the Sub-Mariner", "trigger#0.0", "trigger.spell-cast", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			if !stackHasCauseShape(t, it, tc) {
				t.Fatalf("the served scenario does not carry the wanted cause shape: %+v", it.Scenario.Steps)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			res := runSteps(t, reg, it.Scenario, it.Scenario.Steps)
			if !stackHasSource(res.Snapshots, tc.name) {
				t.Fatalf("%s's trigger is not on the stack at the scenario end: %v", tc.name, it.Scenario.Steps)
			}
		})
	}
}

// stackHasCauseShape asserts the PRECONDITION the served item depends on:
// the cast_mode the cause must select, the setup carrier it must place, and
// (for the opponent-turn family) the main1@p1 checkpoint the cast happens
// after. A served item that lost its cause shape passes nothing.
func stackHasCauseShape(t *testing.T, it oraclegen.Item, tc struct {
	name, key, sub   string
	wantCastMode     string
	wantSetupCard    string
	wantOpponentTurn bool
}) bool {
	t.Helper()
	foundMode, foundSetup, foundCheckpoint := tc.wantCastMode == "", tc.wantSetupCard == "", !tc.wantOpponentTurn
	for _, bf := range it.Scenario.Setup["p0"].Battlefield {
		if bf == tc.wantSetupCard {
			foundSetup = true
		}
	}
	for _, st := range it.Scenario.Steps {
		if tc.wantCastMode != "" && st.Op == "cast" && st.CastMode == tc.wantCastMode {
			foundMode = true
		}
		if tc.wantOpponentTurn && st.Op == "pass_to" && st.Step == "main1" && st.Active == "p1" {
			foundCheckpoint = true
		}
	}
	return foundMode && foundSetup && foundCheckpoint
}
