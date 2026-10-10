package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// servedStaticGrantRows are the appendix rows (levelb-static-granted-
// abilities) the granted-ability observation serves: a granted mana ability or
// loyalty ability offered on a probe, or an extra land drop. Each names the
// offered kind and a fragment of its label ("" for a land play, whose probe is
// the second land).
var servedStaticGrantRows = []struct {
	card, key, kind, label string
}{
	// "{T}: Add ..." granted to creatures, lands, artifacts, Foods, Caves, Frogs.
	{"Clement, the Worrywort", "static#0.0", "activate", "Add"},
	{"Enduring Vitality", "static#0.0", "activate", "Add any color"},
	{"A Realm Reborn", "static#0.0", "activate", "Add any color"},
	{"Great Divide Guide", "static#0.0", "activate", "Add any color"},
	{"Forgotten Monument", "static#0.0", "activate", "Add any color"},
	{"Resonating Lute", "static#0.0", "activate", "Add two mana of any one color"},
	{"Night of the Sweets' Revenge", "static#0.0", "activate", "Add G"},
	{"Mm'menon, the Right Hand", "static#0.2", "activate", "Add U"},
	// Petrified Hamlet: "{T}: Add {C}." granted to the LAND the source's ETB
	// trigger names (static_named_enters.go); the probe is the named land.
	{"Petrified Hamlet", "static#0.1", "activate", "Add"},
	// "Planeswalkers you control have '[-N]: ...'".
	{"Way of the Cryomancer", "static#0.0", "activate", "copy it"},
	{"Way of the Deathbringer", "static#0.0", "activate", "sacrifice a creature"},
	{"Way of the Healer", "static#0.0", "activate", "Wizard Soldier"},
	{"Way of the Warlord", "static#0.0", "activate", "2 damage"},
	{"Way of the Wildspeaker", "static#0.0", "activate", "4/4 green Beast"},
	// "You may play an additional land on each of your turns."
	{"Hugs, Grisly Guardian", "static#0.0", "play", ""},
	{"Prismatic Undercurrents", "static#0.0", "play", ""},
	{"Case of the Locked Hothouse", "static#0.0", "play", ""},
}

// TestStaticGrantedAbilityIsOfferedOnlyWithTheSource locks every served row:
// the scenario asserts the granted option on a probe that is really on the
// battlefield (or the second land really in hand), gorge accepts it, and the
// same checkpoint without the source rejects it, so the assertion is the
// static's doing and not an ability the probe already had.
func TestStaticGrantedAbilityIsOfferedOnlyWithTheSource(t *testing.T) {
	for _, r := range servedStaticGrantRows {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			it, skip := zoneItem(t, r.card, r.key)
			if skip != nil {
				t.Fatalf("GenerateB: %s (the granted-ability observation regressed to a skip)", skip.Reason)
			}
			if want := r.card + "/" + r.key + "/v1"; it.ID != want {
				t.Fatalf("identity = %q, want the level-B row %q", it.ID, want)
			}
			last := it.Steps[len(it.Steps)-1]
			if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
				t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
			}
			got := last.Expect[0].Offered
			if got.Kind != r.kind || !strings.Contains(strings.ToLower(got.Label), strings.ToLower(r.label)) {
				t.Fatalf("offered assertion = %+v, want kind %q label containing %q", got, r.kind, r.label)
			}
			probe := strings.TrimPrefix(got.Card, "p0:")
			p0 := it.Setup["p0"]
			if r.kind == "play" {
				// The extra drop: a first land is played, the probe is the second.
				if !slices.Contains(p0.Hand, probe) {
					t.Fatalf("precondition: second land %q not in p0's hand %v", probe, p0.Hand)
				}
				played := false
				for _, s := range it.Steps {
					played = played || (s.Op == "play" && s.Card != got.Card && slices.Contains(p0.Hand, strings.TrimPrefix(s.Card, "p0:")))
				}
				if !played {
					t.Fatalf("precondition: no first land play before the assertion: %+v", it.Steps)
				}
			} else if !slices.Contains(p0.Battlefield, probe) {
				t.Fatalf("precondition: probe %q is not on p0's battlefield %v", probe, p0.Battlefield)
			}
			if res := runZone(t, it); len(res.Fails) != 0 {
				t.Fatalf("with the source: %v", res.Fails)
			}
			ctl := withoutSource(it, r.card, true)
			// A land source arrives by a play step, not a cast: drop that too.
			ctl.Steps = slices.DeleteFunc(slices.Clone(ctl.Steps), func(s oraclegen.Step) bool {
				return s.Op == "play" && s.Card == "p0:"+r.card
			})
			if len(ctl.Steps) >= len(it.Steps) {
				t.Fatalf("precondition: control kept every step (%d of %d)", len(ctl.Steps), len(it.Steps))
			}
			if res := runZone(t, ctl); len(res.Fails) == 0 {
				t.Fatalf("without the source %s %s is still offered: the assertion is not the static's", got.Kind, got.Card)
			}
		})
	}
}

// TestStaticGrantedAbilityRowCountPinned is the brief's ">= 12 of the 46
// appendix rows" gate, and checks every named card is in the corpus so a rename
// is a clear failure and not a silent skip above.
func TestStaticGrantedAbilityRowCountPinned(t *testing.T) {
	if got := len(servedStaticGrantRows); got < 12 {
		t.Fatalf("served granted-ability rows = %d, want >= 12 (the brief's gate)", got)
	}
	reg := testutil.CorpusRegistry(t)
	for _, r := range servedStaticGrantRows {
		if _, ok := reg.Lookup(r.card); !ok {
			t.Errorf("precondition: %s absent from the corpus", r.card)
		}
	}
}

// TestStaticGrantedAbilityNamedSkips locks the shape each unserved appendix row
// is skipped for, so none falls back to the generic "effect not observable".
func TestStaticGrantedAbilityNamedSkips(t *testing.T) {
	for _, r := range []struct{ card, key, reason string }{
		// The Aetherspark static#0.1 and Barrensteppe Siege static#0.0 moved
		// to the granted-trigger observation (static_granted_trigger_test.go);
		// Frostcliff Siege static#0.1 and Tomik, Orzhov Lawmage static#0.0
		// moved to the granted-static observation (static_granted_static_test.go).
		{"Koh, the Face Stealer", "static#0.0", "static gains the activated abilities of other cards (needs a donor card)"},
		{"Etrata, Deadly Fugitive", "static#0.0", "static grants an activated ability (needs the driver's activate on a granted ability)"},
	} {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			it, skip := zoneItem(t, r.card, r.key)
			if skip == nil {
				t.Fatalf("served (%s), want the named skip %q", it.ID, r.reason)
			}
			if skip.Reason != r.reason {
				t.Fatalf("skip = %q, want %q", skip.Reason, r.reason)
			}
		})
	}
}
