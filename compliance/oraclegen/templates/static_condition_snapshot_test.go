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

func TestStaticIsPresentGraveyardSnapshots(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, key, pt, keywords string
		count                   int
	}{
		{"Basking Capybara", "static#0.0", "4/3", "", 4},
		{"Echo of Dusk", "static#0.0", "3/3", "lifelink", 4},
		{"Didact Echo", "static#0.0", "3/2", "flying", 4},
		{"Frilled Cave-Wurm", "static#0.0", "4/5", "", 4},
		{"Akawalli, the Seething Tower", "static#0.0", "5/5", "trample", 4},
		{"Akawalli, the Seething Tower", "static#0.1", "7/7", "trample", 8},
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			card, ok := reg.Lookup(tc.card)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("precondition: %s in corpus", tc.card)
			}
			face := card.Faces[0]
			if face.PT == tc.pt && oraclediff.EvergreenKeywords(face.Keywords) == tc.keywords {
				t.Fatal("precondition: expected characteristics differ from printed")
			}
			var req *levelb.Requirement
			for _, r := range levelb.Requirements(card) {
				if r.Key == tc.key && r.Sub == "static.continuous" {
					rr := r
					req = &rr
					break
				}
			}
			if req == nil {
				t.Fatalf("precondition: requirement %s exists", tc.key)
			}
			item, skip := templates.GenerateB(reg, tc.card, *req)
			if skip != nil {
				t.Fatalf("unexpected skip: %s", skip.Reason)
			}
			if len(item.Setup["p0"].Graveyard) < tc.count {
				t.Fatalf("precondition: graveyard=%d want >=%d", len(item.Setup["p0"].Graveyard), tc.count)
			}
			res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				t.Fatalf("scenario replay: err=%v fails=%v", err, res.Fails)
			}
			for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
				if p.Name == tc.card && p.Controller == 0 {
					if p.PT != tc.pt || oraclediff.EvergreenKeywords(p.Keywords) != tc.keywords {
						t.Fatalf("got %+v, want %s keywords %q", p, tc.pt, tc.keywords)
					}
					return
				}
			}
			t.Fatalf("precondition: %s is on p0 battlefield", tc.card)
		})
	}
}

// TestStaticContinuousClassLevelDispositions pins each class-level continuous
// static's level-B disposition after the shared Class level-up prelude landed.
// Ninja Teen's level-2 static is now served (the prelude raises the Class to
// level 2); the other three still skip, but on their OWN condition fixture (an
// equipped permanent, a token, counters), not on the obsolete class-level
// reason. The prelude removes the class gate; a static whose condition needs a
// fixture this generator cannot build remains a named skip.
func TestStaticContinuousClassLevelDispositions(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const classLevelReason = "static needs a class level (the driver has no level-up op)"
	for _, tc := range []struct {
		name   string
		served bool
		reason string
	}{
		{name: "Ninja Teen", served: true},
		// Served since the state-fixture ticket (cli-20261009T031407Z-8f4b4f49):
		// Blacksmith's Talent's level-3 static selects equipped creatures, which
		// the equip fixture supplies (Bonesplitter attached to the Bear);
		// Caretaker's Talent's level-3 static pumps creature tokens, which the
		// token fixture casts into being (Dragon Fodder).
		{name: "Blacksmith's Talent", served: true},
		{name: "Caretaker's Talent", served: true},
		// Innkeeper's Talent served since the setup-counters fixture ticket
		// (cli-20261009T031409Z-a76de35a): the level-2 ward static's
		// Permanent.YouCtrl+HasCounters gate is now probed with a countered
		// Bear.
		{name: "Innkeeper's Talent", served: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("precondition: %s missing from corpus", tc.name)
			}
			for _, req := range levelb.Requirements(card) {
				if req.Key != "static#0.0" {
					continue
				}
				if req.Sub != "static.continuous" || card.Faces[0].Statics[0].Params["ClassBand"] == "" {
					t.Fatalf("precondition: %s static#0.0 is not a class-level continuous static", tc.name)
				}
				_, skip := templates.GenerateB(reg, tc.name, req)
				if tc.served {
					if skip != nil {
						t.Fatalf("static#0.0 skip = %q, want a served scenario", skip.Reason)
					}
					return
				}
				if skip == nil {
					t.Fatal("static#0.0 served, want the named condition skip")
				}
				// The obsolete class-level reason must not be the remaining one:
				// the prelude removed that gate.
				if skip.Reason == classLevelReason {
					t.Fatalf("static#0.0 still reports the class-level reason %q", skip.Reason)
				}
				if skip.Reason != tc.reason {
					t.Fatalf("static#0.0 skip = %q, want %q", skip.Reason, tc.reason)
				}
				return
			}
			t.Fatal("precondition: static#0.0 requirement absent")
		})
	}
}
