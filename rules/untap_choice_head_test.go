package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The corpus's printed bare keyword line "You may choose not to untap
// CARDNAME during your untap step." (CR 502.2) carries no colon, so
// cards.KeywordHead keeps the WHOLE sentence as the head and the coverage walk
// interns `kw:<sentence>`. The behaviour is implemented rules-side —
// rules/untap.go hasUntapStepChoice recognises the sentence verbatim and
// rules/turn.go's untap scan poses the untap/keep-tapped choose — but without
// the effects registration (effects/kw_untap_choice.go, the exact-sentence
// pattern cards/kw_prevent.go's doc comment names) that head read as a phantom
// unsupported primitive on every carrier: 45 corpus card files at the
// 2026-09-28 pin carried a gap that named no real missing work, and
// rules/venture_test.go's ventureExceptionTable carried Immovable Rod SOLELY
// for it.
//
// This file is the classification ratchet: the head must stay in
// effects.Supported() (an exact sentence, so a corpus spelling drift re-opens
// the phantom and fails here loudly), and every carrier must report the head
// as supported in cards.Registry.Unsupported.

// untapChoiceHead is the exact sentence the corpus prints and the engine
// reads verbatim.
const untapChoiceHead = "kw:You may choose not to untap CARDNAME during your untap step."

// untapChoiceCarrierNames pins two carriers by name so a registry that lost
// the cards entirely (or a walk that silently found zero) cannot read as a
// pass; the count assertion below is the corpus-pin ratchet.
var untapChoiceCarrierNames = []string{"Vedalken Shackles", "Immovable Rod"}

// TestUntapChoiceHeadIsClassifiedSupported walks the corpus for every card
// whose primitives include the exact-sentence head and asserts the
// registration named it supported — so no carrier reports the phantom gap
// anymore. Fails loudly when the registration is removed (every carrier then
// reports the head unsupported) or when a corpus pin changes the carrier
// count.
func TestUntapChoiceHeadIsClassifiedSupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	if !supported[untapChoiceHead] {
		t.Fatal("effects.Supported() does not name the untap-choice sentence head: " +
			"the effects/kw_untap_choice.go registration is gone or was renamed")
	}
	carriers := 0
	for _, c := range reg.Cards {
		if !slices.Contains(c.Primitives(), untapChoiceHead) {
			continue
		}
		carriers++
		name := c.Faces[0].Name
		if miss := reg.Unsupported(c, supported); slices.Contains(miss, untapChoiceHead) {
			t.Errorf("card %q reports %q unsupported: the head is not classified", name, untapChoiceHead)
		}
	}
	if carriers != 45 {
		t.Errorf("corpus carriers of the untap-choice sentence = %d, want 45 (the measured count at this corpus pin)", carriers)
	}
	for _, name := range untapChoiceCarrierNames {
		if !slices.ContainsFunc(reg.Cards, func(c *cards.Card) bool { return c.Faces[0].Name == name }) {
			t.Fatalf("precondition: corpus lost carrier %q entirely", name)
		}
	}
}
