package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// staticAt returns the cards.Static at name's requirement key.
func staticAt(t *testing.T, reg *cards.Registry, name, key string) cards.Static {
	t.Helper()
	req := counterReq(t, reg, name, key)
	c, _ := reg.Lookup(name)
	st, _ := staticSlotOf(c.Faces[0], req)
	return st
}

// TestStaticGatedGrantFallsThroughToGenericPath pins the rows the gated
// self-grant helper must NOT take ownership of. A gated grant that also
// carries an observable effect (a level creature's SetPower/SetToughness --
// Echo Mage, Joraga Treespeaker) is served by the generic probe path; the
// helper's failure must fall through to it instead of returning a skip. Echo
// Mage / Joraga Treespeaker live in ROE, which no census pins, so the
// regression this test guards was invisible to every other test in the
// package.
func TestStaticGatedGrantFallsThroughToGenericPath(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key string }{
		{"Echo Mage", "static#0.0"},
		{"Echo Mage", "static#0.1"},
		{"Joraga Treespeaker", "static#0.0"},
	} {
		card, key := tc.card, tc.key
		t.Run(card+"/"+key, func(t *testing.T) {
			st := staticAt(t, reg, card, key)
			// The static in question carries an AddAbility$ grant beside its
			// P/T set, which the generic path observes. Assert that shape so a
			// corpus change cannot make this test vacuous.
			if !st.HasParam(cards.PKAddAbility) {
				t.Fatalf("precondition: %s %s carries no AddAbility$", card, key)
			}
			if !staticSelfGrantedAbility(&st) {
				t.Fatalf("precondition: %s %s is not a self grant", card, key)
			}
			req := counterReq(t, reg, card, key)
			item, skip := GenerateB(reg, card, req)
			if skip != nil {
				t.Fatalf("GenerateB skip = %v; want the generic path to serve it", skip)
			}
			// The generic observation compares the keyword line; the gated
			// self-grant helper instead asserts an offered activation on the
			// card. Require the generic shape so a helper that wrongly claims
			// the row is caught even if it happens to serve something.
			hasKeywords := false
			for _, cm := range item.Compare {
				if cm == oraclediff.CompareKeywords {
					hasKeywords = true
				}
			}
			if !hasKeywords {
				t.Fatalf("%s %s served without the generic keyword comparison: %+v", card, key, item.Compare)
			}
			for _, step := range item.Scenario.Steps {
				for _, ex := range step.Expect {
					if ex.Offered != nil && ex.Offered.Card == "p0:"+card {
						t.Fatalf("%s %s: generic serving must not assert an offered option on the card: %+v", card, key, ex.Offered)
					}
				}
			}
		})
	}
}

// TestStaticSelfGrantedAbilityRecipient pins the recipient predicate the
// gated self-grant helper consults: a grant is a self grant only when its
// Affected$ names Self (or is absent, the engine's Card.Self default). Joraga
// Treespeaker's LEVEL 5+ static grants to Elf.YouCtrl, so it is not; Squadron
// Carrier grants to Spacecraft.YouCtrl.
func TestStaticSelfGrantedAbilityRecipient(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, key string
		want      bool
	}{
		{"Echo Mage", "static#0.0", true},
		{"Muraganda Raceway", "static#0.0", true},
		{"Joraga Treespeaker", "static#0.0", true},
		{"Joraga Treespeaker", "static#0.2", false},
		{"Squadron Carrier", "static#0.0", false},
	} {
		st := staticAt(t, reg, tc.card, tc.key)
		if got := staticSelfGrantedAbility(&st); got != tc.want {
			t.Errorf("staticSelfGrantedAbility(%s %s) = %v, want %v (Affected=%q)",
				tc.card, tc.key, got, tc.want, st.ParamStr(cards.PKAffected))
		}
	}
}
