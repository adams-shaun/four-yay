package templates_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// handSizeLandPlayReq is name's static.continuous requirement with the given
// key, failing on a missing card or key so no test passes on an empty loop.
func handSizeLandPlayReq(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key == key {
			if r.Sub != "static.continuous" {
				t.Fatalf("precondition: %s %s is %s, want static.continuous", name, key, r.Sub)
			}
			return r
		}
	}
	t.Fatalf("precondition: %s has no requirement %s", name, key)
	return levelb.Requirement{}
}

// handSizeLandPlayRun generates name/key, fails on a skip, and replays it in
// gorge, returning the item and the final snapshot.
func handSizeLandPlayRun(t *testing.T, reg *cards.Registry, name, key string) (oraclegen.Item, rules.OracleSnapshot) {
	t.Helper()
	it, skip := templates.GenerateB(reg, name, handSizeLandPlayReq(t, reg, name, key))
	if skip != nil {
		t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("%s %s does not replay: err=%v fails=%v", name, key, err, res.Fails)
	}
	return it, res.Snapshots[len(res.Snapshots)-1]
}

// faceHasType reports whether the corpus card name prints the type.
func faceHasType(reg *cards.Registry, name, typ string) bool {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return false
	}
	for _, got := range c.Faces[0].Types {
		if got == typ {
			return true
		}
	}
	return false
}

// TestStaticAdjustLandPlaysIsPresentFixture: Thranduil's Company's extra land
// drop is gated on "as long as you control another Elf", so the observation
// must hold that board. The test asserts the fixture really placed a second
// Elf (the precondition the IsPresent gate reads), that the second land is
// the asserted offered play, and that the card's own Landfall trigger is
// resolved before the assertion (otherwise the stack is occupied and the
// offer checkpoint is not at sorcery speed).
func TestStaticAdjustLandPlaysIsPresentFixture(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Thranduil's Company"
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	if !strings.Contains(c.Faces[0].Statics[0].ParamStr(cards.PKIsPresent), "Elf") {
		t.Fatalf("precondition: %s's static carries no Elf IsPresent gate: %+v", name, c.Faces[0].Statics[0].Params)
	}
	it, final := handSizeLandPlayRun(t, reg, name, "static#0.0")
	p0 := it.Setup["p0"]
	if !containsCard(p0.Battlefield, name) && !containsCard(p0.Hand, name) {
		t.Fatalf("precondition: source %s neither on p0's battlefield %v nor in hand %v", name, p0.Battlefield, p0.Hand)
	}
	elf := ""
	for _, b := range p0.Battlefield {
		if b != name && faceHasType(reg, b, "Elf") {
			elf = b
		}
	}
	if elf == "" {
		t.Fatalf("precondition: no Elf other than the source on p0's battlefield %v, so the IsPresent gate is false", p0.Battlefield)
	}
	last := it.Steps[len(it.Steps)-1]
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
		t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
	}
	if got := last.Expect[0].Offered; got.Kind != "play" || got.Card != "p0:Forest" {
		t.Fatalf("offered assertion = %+v, want the second land p0:Forest offered as a play", got)
	}
	played := false
	for _, s := range it.Steps {
		if s.Op == "play" && s.Card == "p0:Plains" {
			played = true
		}
	}
	if !played {
		t.Fatalf("precondition: no first land played before the assertion: %+v", it.Steps)
	}
	var source *rules.OracleSnapPerm
	for i := range final.Permanents {
		if final.Permanents[i].Name == name {
			source = &final.Permanents[i]
		}
	}
	if source == nil {
		t.Fatalf("%s is not on the final battlefield: %+v", name, final.Permanents)
	}
	if source.Controller != 0 {
		t.Fatalf("%s controlled by seat %d, want p0", name, source.Controller)
	}
}

// containsCard reports whether xs holds name.
func containsCard(xs []string, name string) bool {
	for _, x := range xs {
		if x == name {
			return true
		}
	}
	return false
}

// TestStaticMaxHandSizeSVarFixture: Winter, Misanthropic Guide's Delirium
// maximum is "seven minus the number of card types in your graveyard", so the
// observation needs a graveyard with the count it prices and must assert the
// AFFECTED seat (the opponent's), not p0's. The test asserts the fixture
// really carries distinct card types, that the expectation is the computed
// non-default value, and that the card is on the battlefield in the replay.
func TestStaticMaxHandSizeSVarFixture(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Winter, Misanthropic Guide"
	it, final := handSizeLandPlayRun(t, reg, name, "static#0.0")
	p0 := it.Setup["p0"]
	if !containsCard(p0.Battlefield, name) {
		t.Fatalf("precondition: %s not on p0's battlefield %v", name, p0.Battlefield)
	}
	if len(p0.Graveyard) < 4 {
		t.Fatalf("precondition: graveyard fixture %v has %d cards, want at least 4 card types", p0.Graveyard, len(p0.Graveyard))
	}
	types := map[string]bool{}
	for _, g := range p0.Graveyard {
		c, ok := reg.Lookup(g)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("precondition: graveyard fixture %q not in the corpus", g)
		}
		for _, ty := range c.Faces[0].Types {
			switch ty {
			case "Artifact", "Battle", "Creature", "Enchantment", "Instant", "Land", "Planeswalker", "Sorcery":
				types[ty] = true
			}
		}
	}
	if len(types) != 4 {
		t.Fatalf("precondition: graveyard fixture types = %v, want exactly 4 distinct card types", types)
	}
	want := 7 - len(types)
	found := false
	for _, s := range it.Steps {
		for _, e := range s.Expect {
			if v, ok := e.MaxHandSize["p1"]; ok {
				found = true
				if v != want {
					t.Fatalf("%s max_hand_size p1 = %d, want %d", name, v, want)
				}
			}
			if _, ok := e.MaxHandSize["p0"]; ok {
				t.Fatalf("%s asserts p0's maximum, but Affected$ Opponent makes it p1's: %+v", name, e.MaxHandSize)
			}
		}
	}
	if !found {
		t.Fatalf("%s scenario carries no p1 max_hand_size expectation: %+v", name, it.Steps)
	}
	if want == 7 {
		t.Fatalf("vacuous: the computed maximum %d equals the default", want)
	}
	var perm *rules.OracleSnapPerm
	for i := range final.Permanents {
		if final.Permanents[i].Name == name {
			perm = &final.Permanents[i]
		}
	}
	if perm == nil {
		t.Fatalf("%s is not on the final battlefield: %+v", name, final.Permanents)
	}
}
