package oraclediff

import "testing"

// XMage's test framework throws this on a card its database does not hold;
// the driver surfaces it as the harness message. It is XMage not
// implementing the card, so runDiff routes it to the no-XMage bucket rather
// than HARNESS.
func TestXMageLacksCard(t *testing.T) {
	lacks := []struct{ message, card string }{
		{"IllegalArgumentException: [TEST] Couldn't find a card: Decorum Dissertation", "Decorum Dissertation"},
		{"Couldn't find a card: Echocasting Symposium", "Echocasting Symposium"},
	}
	for _, tc := range lacks {
		if !XMageLacksCard(tc.message, tc.card) {
			t.Errorf("XMageLacksCard(%q, %q) = false, want true", tc.message, tc.card)
		}
	}
	// The driver may fail while loading an auxiliary setup/target card. That
	// must not classify the implemented primary card as absent from XMage.
	if XMageLacksCard("IllegalArgumentException: [TEST] Couldn't find a card: Grizzly Bears", "Decorum Dissertation") {
		t.Fatal("missing setup card classified the scenario card as XMAGE_LACKS")
	}
	notLacks := []string{
		"AssertionError: Can't find ability to activate command: Cast X",
		"AssertionError: Wrong skip command found",
		"",
	}
	for _, m := range notLacks {
		if XMageLacksCard(m, "Decorum Dissertation") {
			t.Errorf("XMageLacksCard(%q, primary card) = true, want false", m)
		}
	}
}
