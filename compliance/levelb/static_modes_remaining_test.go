package levelb_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// gapOf finds one static requirement by key and asserts its named-gap
// classification: the sub-family stays the gap label and the reason names the
// exact missing probe -- so the generic "static mode X" string from a
// classifier this ticket touched can never come back for these rows.
func gapOf(t *testing.T, card, key, sub, reasonContains string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("precondition: %s absent from corpus", card)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key != key {
			continue
		}
		if req.Sub != sub || req.Gap == "" || !strings.Contains(req.Gap, reasonContains) {
			t.Fatalf("%s %s classifies as %q gap %q, want %q gap containing %q", card, key, req.Sub, req.Gap, sub, reasonContains)
		}
		return
	}
	t.Fatalf("precondition: %s has no requirement %s", card, key)
}

// TestRemainingStaticModesClassify pins the classification of every row in
// the remaining-static-modes ticket's measured table: the served rows carry
// their sub-family with no gap (reqOf), the rows whose probe the engine or
// the fixture grammar cannot reach carry a named gap (gapOf).
func TestRemainingStaticModesClassify(t *testing.T) {
	for _, tc := range []struct{ card, key, sub string }{
		// Activations: a positive PowerUp ceiling (Wonder Man).
		{"Wonder Man, Hollywood Hero", "static#0.0", "static.activations-powerup"},
		// CantBlockUnless: the attacking self tax shape (the template keeps
		// it a named skip -- no paid-block checkpoint in the grammar).
		{"Archangel of Tithes", "static#0.1", "static.cant-block-unless-tax"},
		// ManaConvert: the Case-spell and activated-ability shapes.
		{"Case File Auditor", "static#0.0", "static.mana-convert-case-spells"},
		{"Agatha's Soul Cauldron", "static#0.0", "static.mana-convert-abilities"},
		// CantPreventDamage: unconditional and combat-only.
		{"Sunspine Lynx", "static#0.1", "static.cant-prevent-damage"},
		{"Frenzied Baloth", "static#0.0", "static.cant-prevent-damage-combat"},
		// CantAttackUnless: the untapped self tax (Archangel of Tithes).
		{"Archangel of Tithes", "static#0.0", "static.cant-attack-unless-tax"},
		// IgnoreHexproof, CantPutCounter, NoCleanupDamage, CantBeSuspected.
		{"Nowhere to Run", "static#0.0", "static.ignore-hexproof"},
		{"Blossombind", "static#0.0", "static.cant-put-counter"},
		{"Ancient Adamantoise", "static#0.0", "static.no-cleanup-damage"},
		{"Airtight Alibi", "static#0.1", "static.cant-be-suspected"},
		// ActivateAbilityAsIfHaste, PlotZone, CantBeCopied.
		{"Shang-Chi, Master of Kung Fu", "static#0.0", "static.activate-as-if-haste"},
		{"Fblthp, Lost on the Range", "static#0.2", "static.plot-zone"},
		{"Choreographed Sparks", "static#0.0", "static.cant-be-copied"},
		// UnspentMana and IgnoreLegendRule.
		{"Electro, Assaulting Battery", "static#0.0", "static.unspent-mana"},
		{"Spider-Verse", "static#0.0", "static.ignore-legend-rule"},
	} {
		reqOf(t, tc.card, tc.key, tc.sub)
	}
}

// TestRemainingStaticModesNamedGaps pins the named gaps of the rows this
// ticket does not serve: each reason names the exact missing probe, and the
// sub stays the mode's gap label rather than a blank or a served family.
func TestRemainingStaticModesNamedGaps(t *testing.T) {
	for _, tc := range []struct{ card, key, sub, reason string }{
		// Elvish Refueler's ceiling is negative and the ability is Exhaust.
		{"Elvish Refueler", "static#0.0", "static.gap:Activations", "is not the positive PowerUp ceiling"},
		// Dáin's Condition$ EnduringStory gate cannot be set up.
		{"Dáin, Lord of the Iron Hills", "static#0.0", "static.gap:CantAttackUnless", "is not a served unless-tax shape"},
		// Both AlternativeCost shapes: the fixture grammar and the gate.
		{"Leyline of Mutation", "static#0.0", "static.gap:AlternativeCost", "needs an alternative-cost cast option"},
		{"Kíli the Resourceful", "static#0.0", "static.gap:AlternativeCost", "EnduringStory"},
		// FlipCoinMod: the engine does not read the mode.
		{"Edgar, King of Figaro", "static#0.0", "static.gap:FlipCoinMod", "no coin-flip probe"},
	} {
		gapOf(t, tc.card, tc.key, tc.sub, tc.reason)
	}
}
