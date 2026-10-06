package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestTriggerAttackPreludeCensus pins the brief's 51 attacked-after-activation
// rows independently of the shared four-set trigger census.
func TestTriggerAttackPreludeCensus(t *testing.T) {
	reg := loadGenRegistry(t)
	names := []string{
		"Alacrian Jaguar", "Autarch Mammoth", "Brightfield Glider", "Brightfield Mustang", "Bulwark Ox", "District Mascot", "Dracosaur Auxiliary", "Gilded Ghoda", "Gloryheath Lynx", "Guardian Sunmare", "Lagorin, Soul of Alacria", "Lumbering Worldwagon", "Rocketeer Boostbuggy", "Salvation Engine", "Unswerving Sloth", "Venomsac Lagac",
		"Hedge Shredder", "Adventurer's Airship", "The Regalia", "Careening Mine Cart", "Restless Anchorage", "Restless Prairie", "Restless Reef", "Restless Ridgeline", "Restless Vents", "Subterranean Schooner", "The Belligerent", "Kylox's Voltstrider",
		"Bounding Felidar", "Bridled Bighorn", "Calamity, Galloping Inferno", "Congregation Gryff", "Drover Grizzly", "Fortune, Loyal Steed", "Giant Beaver", "Gila Courser", "Luxurious Locomotive", "Mobile Homestead", "Ornery Tumblewagg", "Quilled Charger", "Rambling Possum", "Seraphic Steed", "Trained Arynx",
		"Passenger Ferry", "Spider-Mobile", "Tundra Tank", "Turtle Van", "Restless Bivouac", "Restless Fortress", "Restless Spire", "Restless Vinestalk",
	}
	served, skips := 0, map[string]int{}
	for _, name := range names {
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("precondition: %s is absent from corpus", name)
		}
		found := false
		for _, req := range levelb.Requirements(card) {
			if req.Family != "trigger" || req.Sub != "trigger.attacks" {
				continue
			}
			found = true
			_, skip := GenerateB(reg, name, req)
			if skip == nil {
				served++
			} else {
				skips[name+": "+skip.Reason]++
			}
		}
		if !found {
			t.Errorf("precondition: %s has no trigger.attacks requirement", name)
		}
	}
	if served != 49 || len(skips) != 2 || skips["Giant Beaver: trigger attack activation did not fire"] != 1 || skips["Restless Vinestalk: trigger did not fire"] != 1 {
		t.Fatalf("attack-prelude census: served=%d skips=%v, want served=49 with named Giant Beaver and Restless Vinestalk skips", served, skips)
	}
}

func TestTriggerAttackPrelude(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key string }{
		{"Alacrian Jaguar", "trigger#0.0"},
		{"Luxurious Locomotive", "trigger#0.0"},
		{"Restless Anchorage", "trigger#0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, "trigger.attacks")
			if !containsString(it.Scenario.Setup["p0"].Battlefield, tc.name) {
				t.Fatalf("precondition: source %s is not on p0's battlefield", tc.name)
			}
			ops := make([]string, 0, len(it.Scenario.Steps))
			for _, step := range it.Scenario.Steps {
				ops = append(ops, step.Op)
			}
			if len(ops) < 3 || ops[0] != "activate" || ops[1] != "resolve" || ops[2] != "attack" {
				t.Fatalf("steps = %v, want activate, resolve, attack in order", ops)
			}
			if len(it.XAbility) < 1 || it.XAbility[0] == "" {
				t.Fatalf("activation has no XMage ability prefix: %v", it.XAbility)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerShownOnStack(t, reg, it.Scenario, tc.name) {
				t.Fatalf("no snapshot shows the attack trigger sourced by %s", tc.name)
			}
		})
	}
}
