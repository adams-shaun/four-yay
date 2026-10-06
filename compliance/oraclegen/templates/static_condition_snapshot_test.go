package templates_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// Check the tested card itself, not merely that some permanent differs from
// printed characteristics. In particular an artifact fixture must not count as
// proof that the IsPresent static actually applied to Goblin Tomb Raider.
func TestStaticContinuousConditionalSnapshots(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, pt, keywords string
	}{
		{"Boneclub Berserker", "4/4", ""},
		{"Nightwhorl Hermit", "2/4", "vigilance"},
		{"Omenport Vigilante", "2/2", "double strike"},
		{"Brightspear Zealot", "4/4", "vigilance"},
		{"Goblin Tomb Raider", "2/2", "haste"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("precondition: %s missing from corpus", tc.name)
			}
			face := card.Faces[0]
			if face.PT == tc.pt && oraclediff.EvergreenKeywords(face.Keywords) == tc.keywords {
				t.Fatal("precondition: expected characteristics must differ from printed ones")
			}
			item, served := staticRequirement(t, tc.name)
			if !served {
				t.Fatalf("%s conditional static was not served", tc.name)
			}
			if tc.name == "Goblin Tomb Raider" && !hasCard(item.Setup["p0"].Battlefield, "Sol Ring") {
				t.Fatalf("IsPresent fixture lacks p0's artifact: %+v", item.Setup)
			}
			res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				t.Fatalf("scenario replay: err=%v fails=%v", err, res.Fails)
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			found := false
			for _, permanent := range last.Permanents {
				if permanent.Name == tc.name && permanent.Controller == 0 {
					found = true
					if permanent.PT != tc.pt || oraclediff.EvergreenKeywords(permanent.Keywords) != tc.keywords {
						t.Fatalf("conditional static: got %+v, want P/T %s keywords %q", permanent, tc.pt, tc.keywords)
					}
				}
			}
			if !found {
				t.Fatalf("precondition: tested card absent from p0's battlefield: %+v", last.Permanents)
			}
		})
	}
}

func TestStaticContinuousClassLevelNamedSkips(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Blacksmith's Talent", "Caretaker's Talent", "Innkeeper's Talent", "Ninja Teen"} {
		t.Run(name, func(t *testing.T) {
			card, ok := reg.Lookup(name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("precondition: %s missing from corpus", name)
			}
			for _, req := range levelb.Requirements(card) {
				if req.Key != "static#0.0" {
					continue
				}
				if req.Sub != "static.continuous" || card.Faces[0].Statics[0].Params["ClassBand"] == "" {
					t.Fatalf("precondition: %s static#0.0 is not a class-level continuous static", name)
				}
				_, skip := templates.GenerateB(reg, name, req)
				if skip == nil || skip.Reason != "static needs a class level (the driver has no level-up op)" {
					t.Fatalf("class-level skip = %v", skip)
				}
				return
			}
			t.Fatal("precondition: static#0.0 requirement absent")
		})
	}
}
