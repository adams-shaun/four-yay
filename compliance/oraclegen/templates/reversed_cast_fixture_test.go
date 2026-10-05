package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// TestReversedCastFixturesSettle names each cast whose old fixture was
// reversed under CR 601.2c/733.1 rather than making a spell on the stack.
func TestReversedCastFixturesSettle(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Long River's Pull", "Urgent Necropsy", "Doppelgang"} {
		t.Run(name, func(t *testing.T) {
			c, ok := reg.Lookup(name)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("precondition: %s absent from corpus", name)
			}
			var it oraclegen.Item
			var skip *oraclegen.Skip
			f := c.Faces[0]
			mana, reason := oraclegen.PoolFor(f.ManaCost)
			if reason != "" {
				t.Fatalf("precondition: no mana pool: %s", reason)
			}
			if name == "Long River's Pull" {
				it, skip = counterSpell(reg, f, name, mana)
			} else {
				it, skip = castResolve(reg, f, name, mana)
			}
			if skip != nil {
				t.Fatalf("%s skipped: %s", name, skip.Reason)
			}
			if name == "Urgent Necropsy" && len(it.Scenario.Setup["p0"].Graveyard) < 2 {
				t.Fatal("precondition: no collect-evidence graveyard to pay the targeted permanents' mana value")
			}
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil || len(res.Fails) != 0 {
				t.Fatalf("%s replay err=%v fails=%v", name, err, res.Fails)
			}
			for i, st := range it.Scenario.Steps {
				if st.Op != "cast" || st.Card != "p0:"+name {
					continue
				}
				if i+1 >= len(res.Snapshots) {
					t.Fatal("precondition: missing post-cast snapshot")
				}
				if len(st.Targets) == 0 {
					t.Fatalf("precondition: %s has no cast targets", name)
				}
				if name == "Doppelgang" {
					if len(st.Targets) != 1 || len(st.Answers) == 0 || len(st.Answers[0].Pick) == 0 || st.Answers[0].Pick[0] != "X = 1" {
						t.Fatalf("Doppelgang: X must be one affordable target, got %+v", st)
					}
				}
				before, after := 0, 0
				for _, card := range res.Snapshots[i].Players[0].Hand {
					if card == name {
						before++
					}
				}
				for _, card := range res.Snapshots[i+1].Players[0].Hand {
					if card == name {
						after++
					}
				}
				if before == 0 || after >= before {
					t.Fatalf("cast reversed to hand: before=%d after=%d", before, after)
				}
				return
			}
			t.Fatal("precondition: missing named cast")
		})
	}
}
