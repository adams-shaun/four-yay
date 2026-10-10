package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestCastFamilyBackFaceAndNamedOwnershipSkip(t *testing.T) {
	reg := loadGenRegistry(t)

	card, ok := reg.Lookup("The Enigma Jewel")
	if !ok || len(card.Faces) < 2 || card.Faces[1].Name != "Locus of Enlightenment" {
		t.Fatalf("precondition: expected The Enigma Jewel's Locus back face, got %+v", card)
	}
	var locusReq *levelb.Requirement
	for _, req := range levelb.Requirements(card) {
		if req.Key == "trigger#1.0" {
			copy := req
			locusReq = &copy
			break
		}
	}
	if locusReq == nil {
		t.Fatal("precondition: Locus trigger requirement trigger#1.0 missing")
	}
	item, skip := GenerateB(reg, "The Enigma Jewel", *locusReq)
	if skip != nil {
		t.Fatalf("Locus back-face trigger skipped: %s", skip.Reason)
	}
	if !triggerShownOnStack(t, reg, item.Scenario, "Locus of Enlightenment") {
		t.Fatal("Locus of Enlightenment's trigger never appears on stack")
	}

	gonti, ok := reg.Lookup("Gonti, Night Minister")
	if !ok || len(gonti.Faces) == 0 || len(gonti.Faces[0].Triggers) == 0 {
		t.Fatal("precondition: Gonti trigger fixture missing")
	}
	var gontiReq *levelb.Requirement
	for _, req := range levelb.Requirements(gonti) {
		if req.Family == "trigger" && req.Sub == "trigger.spell-cast" {
			copy := req
			gontiReq = &copy
			break
		}
	}
	if gontiReq == nil {
		t.Fatal("precondition: Gonti SpellCast requirement missing")
	}
	item, skip = GenerateB(reg, "Gonti, Night Minister", *gontiReq)
	if skip != nil {
		t.Fatalf("Gonti ownership trigger skipped: %s", skip.Reason)
	}
	// The cause needs a resolve between Nita's activate and the cast it
	// grants, so triggerShownOnStack's resolve-stripping replay cannot show
	// it: replay the served scenario verbatim and look for the trigger slot.
	res, ok := oraclegen.PlaysThrough(reg, item.Scenario)
	if !ok || !abilityOnStack(res.Snapshots, stackSourceWants(reg, "Gonti, Night Minister", gonti.Faces[0]), gontiReq.Slot) {
		t.Fatalf("Gonti, Night Minister's ownership trigger never appears on stack (ok=%v)", ok)
	}
}

func TestCastFamilyTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, sub, probe string
		inHand, inGrave       bool
	}{
		{"Solarium Sentry", "trigger#0.0", "trigger.spell-cast-opponent", "p1:", false, false},
		{"Mai, Scornful Striker", "trigger#0.0", "trigger.spell-cast", "p0:", false, false},
		{"Photon Blast Barrage", "trigger#0.0", "trigger.spell-cast-self-cast", "Photon Blast Barrage", true, false},
		{"At Knifepoint", "trigger#0.0", "trigger.commit-crime", "Shock", false, false},
		{"Forsaken Miner", "trigger#0.0", "trigger.commit-crime", "Shock", false, true},
		{"Rangers' Refueler", "trigger#0.0", "trigger.ability-activated", "Rangers' Refueler", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			p0 := it.Scenario.Setup["p0"]
			if tc.inHand {
				if !inZone(p0.Hand, tc.name) || inZone(p0.Battlefield, tc.name) {
					t.Fatalf("precondition: source should be in hand; hand=%v battlefield=%v", p0.Hand, p0.Battlefield)
				}
			} else if tc.inGrave {
				if !inZone(p0.Graveyard, tc.name) || inZone(p0.Battlefield, tc.name) {
					t.Fatalf("precondition: source should be in graveyard; graveyard=%v battlefield=%v", p0.Graveyard, p0.Battlefield)
				}
			} else if !inZone(p0.Battlefield, tc.name) || inZone(p0.Hand, tc.name) {
				t.Fatalf("precondition: source should be on battlefield; hand=%v battlefield=%v", p0.Hand, p0.Battlefield)
			}
			found := false
			for _, step := range it.Scenario.Steps {
				if strings.Contains(step.Card, tc.probe) {
					found = true
				}
			}
			if !found {
				t.Fatalf("cause does not include expected probe %q: %+v", tc.probe, it.Scenario.Steps)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("scenario does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerShownOnStack(t, reg, it.Scenario, tc.name) {
				t.Fatalf("%s's trigger never appears on stack", tc.name)
			}
		})
	}
}
