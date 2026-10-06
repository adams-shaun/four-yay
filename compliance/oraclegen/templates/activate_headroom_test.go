package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
)

// setupLoyalty is the LOYALTY the item's setup adds to the card on p0.
func setupLoyalty(t *testing.T, reg *cards.Registry, name, key string) int {
	t.Helper()
	it, _ := activateRequirement(t, reg, name, key)
	return int(it.Scenario.Setup["p0"].Counters[name]["LOYALTY"])
}

// printedLoyalty is the precondition every headroom case rests on: the cost
// (or gate) really is above the printed loyalty.
func printedLoyalty(t *testing.T, reg *cards.Registry, name string) int {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	n, err := strconv.Atoi(c.Faces[0].Loyalty)
	if err != nil {
		t.Fatalf("precondition: %s printed loyalty %q", name, c.Faces[0].Loyalty)
	}
	return n
}

// TestActivateLoyaltyHeadroom: a minus ability above the printed loyalty
// (CR 606.6) gets exactly the missing loyalty at setup, and an ability the
// printed loyalty already pays gets none.
func TestActivateLoyaltyHeadroom(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, prefix string
		cost              int
	}{
		{"Ajani Resolute", "activate#0.1", "-4", 4},
		{"Ajani Resolute", "activate#0.2", "-10", 10},
		{"The Theorist, Jace Beleren", "activate#0.2", "-6", 6},
	} {
		printed := printedLoyalty(t, reg, tc.name)
		if printed >= tc.cost {
			t.Fatalf("precondition: %s loyalty %d already pays %d", tc.name, printed, tc.cost)
		}
		assertActivateItem(t, reg, tc.name, tc.key, tc.prefix)
		if got := setupLoyalty(t, reg, tc.name, tc.key); got != tc.cost-printed {
			t.Fatalf("%s %s setup loyalty = %d, want %d - %d", tc.name, tc.key, got, tc.cost, printed)
		}
	}
	if got := setupLoyalty(t, reg, "Ajani Resolute", "activate#0.0"); got != 0 {
		t.Fatalf("Ajani Resolute [0] setup loyalty = %d, want none", got)
	}
}

// TestActivateLoyaltyGate: Jace, Reality Sculptor's [0] "activate only if
// there are twenty-five or more loyalty counters among Jaces you control"
// gets the source topped up to 25.
func TestActivateLoyaltyGate(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Jace, Reality Sculptor"
	printed := printedLoyalty(t, reg, name)
	c, _ := reg.Lookup(name)
	if sa := c.Faces[0].Abilities[2]; sa.Params["SVarCompare"] != "GE25" {
		t.Fatalf("precondition: %s ability 2 SVarCompare = %q, want GE25", name, sa.Params["SVarCompare"])
	}
	assertActivateItem(t, reg, name, "activate#0.2", "0")
	if got := setupLoyalty(t, reg, name, "activate#0.2"); got != 25-printed {
		t.Fatalf("%s setup loyalty = %d, want 25 - %d", name, got, printed)
	}
}

// TestActivateTargetFixtures: a legendary-creature target and a
// creature-with-a-+1/+1-counter target each get a fixture that matches, and
// "target creature that attacked this turn" is a named skip.
func TestActivateTargetFixtures(t *testing.T) {
	reg := loadGenRegistry(t)
	assertActivateItem(t, reg, "Yoshimaru, Beloved Companion", "activate#0.0", "{6}")
	it, _ := activateRequirement(t, reg, "Yoshimaru, Beloved Companion", "activate#0.0")
	if tg := it.Scenario.Steps[0].Targets; len(tg) != 1 || tg[0] != "p1:Isamaru, Hound of Konda" {
		t.Fatalf("Yoshimaru targets = %v, want [p1:Isamaru, Hound of Konda]", tg)
	}
	assertActivateItem(t, reg, "Tomik, Orzhov Lawmage", "activate#0.0", "{T}")
	it, _ = activateRequirement(t, reg, "Tomik, Orzhov Lawmage", "activate#0.0")
	if tg := it.Scenario.Steps[0].Targets; len(tg) != 1 || tg[0] != "p1:Grizzly Bears" {
		t.Fatalf("Tomik targets = %v, want [p1:Grizzly Bears]", tg)
	}
	if got := it.Scenario.Setup["p1"].Counters["Grizzly Bears"]["P1P1"]; got != 1 {
		t.Fatalf("Tomik fixture p1 counters = %v, want Grizzly Bears P1P1=1", it.Scenario.Setup["p1"].Counters)
	}
	c, _ := reg.Lookup("Hexhaven Dueling Arena")
	var req *levelb.Requirement
	for _, r := range levelb.Requirements(c) {
		if r.Key == "activate#0.1" {
			req = &r
		}
	}
	if req == nil {
		t.Fatal("precondition: Hexhaven Dueling Arena carries no activate#0.1")
	}
	_, skip := GenerateB(reg, "Hexhaven Dueling Arena", *req)
	if skip == nil || !strings.Contains(skip.Reason, "attackedThisTurn needs a combat prelude") {
		t.Fatalf("Hexhaven Dueling Arena activate#0.1 skip = %+v, want the named attackedThisTurn gap", skip)
	}
}
