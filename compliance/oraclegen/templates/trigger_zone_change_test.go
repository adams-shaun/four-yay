package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// stackedAfterCause replays the item's cause with the generator's own
// settle-and-look check: the trailing resolves are dropped (a prelude's
// resolve stays), the cause is tried bare and followed by both players
// passing, and some snapshot must show the trigger at slot on the stack.
func stackedAfterCause(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, name, slot string) bool {
	t.Helper()
	steps := sc.Steps
	for len(steps) > 0 && steps[len(steps)-1].Op == "resolve" {
		steps = steps[:len(steps)-1]
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	for _, try := range [][]oraclegen.Step{steps, append(append([]oraclegen.Step(nil), steps...), passes...)} {
		sc.Steps = try
		if _, res, ok := oraclegen.Settle(reg, sc); ok && abilityOnStack(res.Snapshots, stackSourceWants(reg, name, wantsFace(t, reg, name)), slot) {
			return true
		}
	}
	return false
}

func TestZoneChangeTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, sub, castProbe string }{
		{"Spirit Mascot", "trigger#0.0", "trigger.leaves-graveyard", "p0:Raise Dead"},
		{"Ninja Teen", "trigger#0.0", "trigger.ltb-other", "p0:Unsummon"},
		{"Kaya, Spirits' Justice", "trigger#0.0", "trigger.ltb-other", "p0:Swords to Plowshares"},
		{"Woodland Champion", "trigger#0.0", "trigger.etb-other", "p0:Sprout"},
		{"Mister Fantastic, Reed Richards", "trigger#0.0", "trigger.etb-other", "p0:Sprout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			cast := false
			for _, st := range it.Scenario.Steps {
				cast = cast || (st.Op == "cast" && st.Card == tc.castProbe)
			}
			if !cast {
				t.Fatalf("precondition: scenario never casts %s: %+v", tc.castProbe, it.Scenario.Steps)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !stackedAfterCause(t, reg, it.Scenario, tc.name, "0") {
				t.Fatalf("%s trigger slot 0 never reaches the stack", tc.name)
			}
		})
	}
}

// TestKayaTriggerNeedsATokenTarget: Kaya's trigger targets a token you
// control. The scenario the target fixtures build carries a resolved token
// maker before the exile; without it the trigger is removed from the stack
// for want of a target, so the stack assertion is not vacuous.
func TestKayaTriggerNeedsATokenTarget(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Kaya, Spirits' Justice", "trigger#0.0", "trigger.ltb-other")
	steps := it.Scenario.Steps
	maker := -1
	for i, st := range steps {
		if st.Op == "cast" && st.Card != "p0:Swords to Plowshares" && st.Card != "p0:Path to Exile" {
			maker = i
			break
		}
	}
	if maker < 0 || maker+1 >= len(steps) || steps[maker+1].Op != "resolve" {
		t.Fatalf("precondition: no resolved token maker before the exile: %+v", steps)
	}
	var bare []oraclegen.Step
	bare = append(bare, steps[:maker]...)
	bare = append(bare, steps[maker+2:]...)
	sc := it.Scenario
	sc.Steps = bare
	if stackedAfterCause(t, reg, sc, "Kaya, Spirits' Justice", "0") {
		t.Fatal("trigger stacked without a token: the token maker proves nothing")
	}
}

func TestZoneChangeResiduesHaveNamedSkips(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, want string }{
		{"Zenos yae Galvus", "trigger#0.1", "trigger no recipe: zone-change filter ChosenCardStrict"},
		// Hedge Shredder left this table when main implemented
		// ChangeType$ Card.TriggeredCards (merge ec0a97a7d): its
		// library-to-graveyard row is served now, asserted by
		// TestHedgeShredderResidueIsServed.
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				if r.Sub != "trigger.zone-change-residue" {
					t.Fatalf("precondition: %s %s classified %s", tc.name, tc.key, r.Sub)
				}
				_, skip := GenerateB(reg, tc.name, r)
				if skip == nil || skip.Reason != tc.want {
					t.Fatalf("skip = %v, want %q", skip, tc.want)
				}
				return
			}
			t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
		})
	}

	// Hedge Shredder used to hold the "library to graveyard" named skip: its
	// body's ChangeType$ Card.TriggeredCards was an unknown predicate, so the
	// cause builder refused rather than generate a scenario whose body moves
	// nothing. The predicate is registered now (ticket
	// agent-20261009T090319Z-4d835724, commit 8e9f1097c), so the residue
	// builds its mill cause and the named skip is gone.
	t.Run("Hedge Shredder", func(t *testing.T) {
		c, ok := reg.Lookup("Hedge Shredder")
		if !ok {
			t.Fatal("Hedge Shredder not in the corpus")
		}
		for _, r := range levelb.Requirements(c) {
			if r.Key != "trigger#0.1" {
				continue
			}
			if r.Sub != "trigger.zone-change-residue" {
				t.Fatalf("precondition: Hedge Shredder trigger#0.1 classified %s", r.Sub)
			}
			it, skip := GenerateB(reg, "Hedge Shredder", r)
			if skip != nil {
				t.Fatalf("Hedge Shredder trigger#0.1 keeps a named skip after Card.TriggeredCards registered: %q", skip.Reason)
			}
			if len(it.Steps) < 2 || it.Steps[0].Op != "cast" || it.Steps[0].Card != "p0:Tome Scour" {
				t.Fatalf("Hedge Shredder trigger#0.1 generated without the mill cause: %+v", it.Steps)
			}
			return
		}
		t.Fatal("precondition: Hedge Shredder carries no requirement trigger#0.1")
	})
	for _, tc := range []struct {
		filter, origin, destination, want string
	}{
		{"ChosenCardStrict", "Any", "Graveyard", "zone-change filter ChosenCardStrict"},
		{"Land", "Library", "Graveyard", "library to graveyard"},
		{"Card", "Any", "Graveyard", "put into graveyard from anywhere"},
	} {
		got := zoneChangeSkip(tc.filter, &cards.Trigger{Params: map[string]string{"Origin": tc.origin, "Destination": tc.destination}})
		if got != tc.want {
			t.Errorf("skip(%q, %q -> %q) = %q, want %q", tc.filter, tc.origin, tc.destination, got, tc.want)
		}
	}
}

func TestZoneProbesByDestination(t *testing.T) {
	has := func(xs []string, x string) bool {
		for _, v := range xs {
			if v == x {
				return true
			}
		}
		return false
	}
	any := zoneProbes("Any")
	for _, want := range []string{"Unsummon", "Swords to Plowshares", "Murder"} {
		if !has(any, want) {
			t.Errorf("Any lacks %s: %v", want, any)
		}
	}
	if got := zoneProbes("Hand"); len(got) != 1 || got[0] != "Unsummon" {
		t.Errorf("Hand = %v, want only the bounce probe", got)
	}
	if got := zoneProbes("Library"); got != nil {
		t.Errorf("Library = %v, want no probe", got)
	}
}

// TestTokenFilterIsServedOnlyForCreatureTokens: a ChangesZoneAll token filter
// is served by the Saproling maker only when a creature token satisfies it.
func TestTokenFilterIsServedOnlyForCreatureTokens(t *testing.T) {
	for _, tc := range []struct {
		filter string
		want   bool
	}{
		{"Card.token+YouCtrl", true},
		{"Creature.token+YouCtrl,Land.YouCtrl", true},
		{"Artifact.token+YouCtrl", false},
		{"Card.!token+YouCtrl", false},
		{"Creature.nonToken+YouCtrl", false},
		{"Creature.token+OppCtrl", false},
	} {
		if got := etbTokenServed(tc.filter); got != tc.want {
			t.Errorf("etbTokenServed(%q) = %v, want %v", tc.filter, got, tc.want)
		}
	}
}

// TestTokenChangesZoneFilterGetsItsTokenMaker: a ChangesZoneAll token filter
// is handled by tokenEnterCauses' one-token maker for every positive-token
// base (ticket g17): the maker is the candidate cause and the trigger's own
// matcher decides at replay what the token satisfies. The corpus sweep
// behind the cause table found every positive-token ChangesZone filter on a
// Creature/Card/Permanent base (e.g. Belladonna Took, Junk Winder), so the
// Artifact base here pins only the maker-as-candidate behaviour, not a
// served row.
func TestTokenChangesZoneFilterGetsItsTokenMaker(t *testing.T) {
	reg := loadGenRegistry(t)
	tr := &cards.Trigger{Mode: "ChangesZoneAll", Params: map[string]string{"Destination": "Battlefield", "ValidCards": "Artifact.token+YouCtrl"}}
	causes, why := etbProbeCauses(reg, "Some Card", tr)
	if why != "" || len(causes) != 1 || causes[0].steps[0].Card != "p0:Sprout" {
		t.Fatalf("causes=%d why=%q, want the one Sprout token maker", len(causes), why)
	}
	// The creature filter is served by exactly the one-token maker too.
	tr.Params["ValidCards"] = "Card.token+YouCtrl"
	causes, why = etbProbeCauses(reg, "Some Card", tr)
	if why != "" || len(causes) != 1 || causes[0].steps[0].Card != "p0:Sprout" {
		t.Fatalf("causes=%+v why=%q, want one Sprout cause", causes, why)
	}
}

// wantsFace looks name up for stackSourceWants; the two shared helpers here
// predate the wants form of abilityOnStack.
func wantsFace(t *testing.T, reg *cards.Registry, name string) *cards.Face {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	return c.Faces[0]
}
