package host

import "testing"

// The search policies read the engine a live table must never hand a seat
// (az's clairvoyant world clones the REAL engine, spec 2026-09-27 §1), so
// none of them may become a hosted opponent under any spelling of the
// factory. internal/archtest additionally forbids host from linking
// internal/azmcts at all.
func TestHostedVocabularyRefusesSearchPolicies(t *testing.T) {
	for _, name := range []string{"az", "search", "policynet"} {
		if _, err := NormalizeBotPolicy(name); err == nil {
			t.Errorf("NormalizeBotPolicy(%q) accepted a bench-only search policy", name)
		}
		if _, err := NewBotPolicySeat(name, 1); err == nil {
			t.Errorf("NewBotPolicySeat(%q) built a seat", name)
		}
		if _, err := NewBotPolicySeatWithAutoPayMana(name, 1, true); err == nil {
			t.Errorf("NewBotPolicySeatWithAutoPayMana(%q) built a seat", name)
		}
	}
}
