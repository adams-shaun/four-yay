package oraclediff

import "testing"

// XMage's test framework throws this on a card its database does not hold;
// the driver surfaces it as the harness message. It is XMage not
// implementing the card, so runDiff routes it to the no-XMage bucket rather
// than HARNESS.
func TestXMageLacksCard(t *testing.T) {
	lacks := []string{
		"IllegalArgumentException: [TEST] Couldn't find a card: Decorum Dissertation",
		"Couldn't find a card: Echocasting Symposium",
	}
	for _, m := range lacks {
		if !XMageLacksCard(m) {
			t.Errorf("XMageLacksCard(%q) = false, want true", m)
		}
	}
	notLacks := []string{
		"AssertionError: Can't find ability to activate command: Cast X",
		"AssertionError: Wrong skip command found",
		"",
	}
	for _, m := range notLacks {
		if XMageLacksCard(m) {
			t.Errorf("XMageLacksCard(%q) = true, want false", m)
		}
	}
}
