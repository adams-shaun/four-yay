package host

import (
	"testing"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/seat"
)

// The bench-only search policies read the engine a live table must never hand
// a seat (az's clairvoyant world clones the REAL engine, spec 2026-09-27 §1),
// so none of them may become a hosted opponent under any spelling of the
// factory. internal/archtest additionally forbids host from linking
// internal/azmcts at all.
//
// The real L10 teacher (`search`, BP-11) is the deliberate exception: it is a
// hosted policy ONLY when its package is linked into this binary (host links
// just bots/bot; host tests link bots/all). Its honesty is proven by the
// honest-root machinery (BP-08), not by absence — so the factory accepts it
// if, and only if, bots.Lookup finds it.
func TestHostedVocabularyRefusesSearchPolicies(t *testing.T) {
	for _, name := range []string{"az", "policynet", "legacy", "explore"} {
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

	_, linked := bots.Lookup(searchPolicy)
	name, err := NormalizeBotPolicy(searchPolicy)
	if linked {
		if err != nil || name != searchPolicy {
			t.Fatalf("NormalizeBotPolicy(%q) = %q, %v; want the normalised name", searchPolicy, name, err)
		}
		s, err := NewBotPolicySeat(searchPolicy, 7)
		if err != nil {
			t.Fatalf("NewBotPolicySeat(%q): %v", searchPolicy, err)
		}
		if _, ok := s.(bots.EnvSeat); !ok {
			t.Errorf("NewBotPolicySeat(%q) built %T, which does not implement bots.EnvSeat", searchPolicy, s)
		}
		if _, ok := s.(seat.BoardSeat); !ok {
			t.Errorf("NewBotPolicySeat(%q) built %T, which does not implement seat.BoardSeat", searchPolicy, s)
		}
		if _, err := NewBotPolicySeatWithAutoPayMana(searchPolicy, 7, true); err != nil {
			t.Errorf("NewBotPolicySeatWithAutoPayMana(%q, auto-pay): %v", searchPolicy, err)
		}
	} else {
		if err == nil {
			t.Errorf("NormalizeBotPolicy(%q) accepted an unlinked policy", searchPolicy)
		}
		if _, err := NewBotPolicySeat(searchPolicy, 7); err == nil {
			t.Errorf("NewBotPolicySeat(%q) built a seat for an unlinked policy", searchPolicy)
		}
		if _, err := NewBotPolicySeatWithAutoPayMana(searchPolicy, 7, true); err == nil {
			t.Errorf("NewBotPolicySeatWithAutoPayMana(%q, auto-pay) built a seat for an unlinked policy", searchPolicy)
		}
	}
}
