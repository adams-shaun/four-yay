package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// TestReplaceDamagePrimitiveIsRegistered pins the support declaration for the
// damage-prevention replacement class (Heart-Shaped Herb, Battletide
// Alchemist, Thunderstaff, ...): cards' census derives api:ReplaceDamage from
// the ReplaceWith$ body, and rules/replacement.go's init registers it because
// api:ReplaceDamage is implemented inline by applyReplaceDamageBody rather
// than through effects.Register. Omitting the registration leaves all 38
// corpus carrier cards reporting as unsupported even though prevention works
// in play.
func TestReplaceDamagePrimitiveIsRegistered(t *testing.T) {
	t.Parallel()
	supported := effects.Supported()
	if !supported["api:ReplaceDamage"] {
		t.Fatal(`effects.Supported() is missing "api:ReplaceDamage"`)
	}
}

// TestReplaceDamageCarrierHasNoGap asserts the census consequence on a real
// carrier: Heart-Shaped Herb's unsupported set must no longer contain
// api:ReplaceDamage. It asserts its own precondition -- the card is found in
// the corpus registry -- so a missing card fails loudly rather than passing
// vacuously.
func TestReplaceDamageCarrierHasNoGap(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	herb := mustCorpusCard(t, reg, "Heart-Shaped Herb")
	prims := herb.Primitives()
	if !slices.Contains(prims, "api:ReplaceDamage") {
		t.Fatalf("precondition failed: Heart-Shaped Herb primitives %v do not contain api:ReplaceDamage", prims)
	}
	if missing := reg.Unsupported(herb, effects.Supported()); slices.Contains(missing, "api:ReplaceDamage") {
		t.Fatalf("Heart-Shaped Herb still reports api:ReplaceDamage unsupported: %v", missing)
	}
}
