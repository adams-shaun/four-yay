package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestCostStaticTargetFixtures(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Swampsnare Trap", "Grow Extra Arms", "Depower", "Champions of the Perfect", "Out of Air", "Ice Out"} {
		t.Run(name, func(t *testing.T) {
			if name == "Out of Air" || name == "Ice Out" {
				c, ok := reg.Lookup(name)
				if !ok {
					t.Fatalf("precondition: %s missing from corpus", name)
				}
				for _, req := range levelb.Requirements(c) {
					if req.Key != "static#0.0" {
						continue
					}
					_, skip := GenerateB(reg, name, req)
					if skip == nil || !strings.Contains(skip.Reason, "cost static probe not supported:") {
						t.Fatalf("%s must retain a named skip until its prerequisite is scripted; got %v", name, skip)
					}
					return
				}
				t.Fatalf("precondition: %s has no static#0.0", name)
			}
			it := staticCostItem(t, reg, name)
			cast := probeCast(t, it)
			if cast.Mana == "" {
				t.Fatalf("precondition: %s probe has no reduced-price payment", name)
			}
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("precondition: %s reduced-price scenario does not play through: %+v", name, it.Steps)
			}
			if name == "Swampsnare Trap" || name == "Grow Extra Arms" || name == "Depower" {
				if len(cast.Targets) == 0 || cast.Targets[0] == "p1" {
					t.Fatalf("precondition: %s target is not a fixture permanent: %+v", name, cast.Targets)
				}
				owner := strings.SplitN(cast.Targets[0], ":", 2)[0]
				ref := strings.TrimPrefix(cast.Targets[0], owner+":")
				found := false
				for _, card := range it.Scenario.Setup[owner].Battlefield {
					found = found || card == ref
				}
				if !found {
					t.Fatalf("precondition: target %q is absent from %s battlefield", cast.Targets[0], owner)
				}
			}
			if name == "Champions of the Perfect" {
				found := false
				for _, card := range it.Scenario.Setup["p0"].Hand {
					c, ok := reg.Lookup(card)
					if ok && len(c.Faces) > 0 {
						for _, typ := range c.Faces[0].Types {
							found = found || strings.EqualFold(typ, "Elf")
						}
					}
				}
				if !found {
					t.Fatalf("precondition: Champions of the Perfect has no Elf in hand: %v", it.Scenario.Setup["p0"].Hand)
				}
			}
		})
	}
}
