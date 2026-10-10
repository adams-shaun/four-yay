package templates_test

import (
	"strings"
	"testing"
)

// TestStaticG7NamedGaps: the two level-B static rows the G7 slice measured
// and could not serve carry their own measured reason instead of the generic
// "effect not observable on a probe or the card".
//
//   - The Lunar Whale static#0.1 (IsPresent$ Card.Self+attackedThisTurn):
//     the Whale crewed and attacked still reads false, because the
//     attackedThisTurn object predicate is not in the filter grammar.
//   - Emet-Selch, Unsundered static#1.0 (Hades, Sorcerer of Eld's graveyard
//     MayPlay$): setup places the graveyard probe through the card's own
//     "exile instead of graveyard" replacement, so the probe ends in exile.
func TestStaticG7NamedGaps(t *testing.T) {
	for _, row := range []struct{ card, key, want string }{
		{"The Lunar Whale", "static#0.1", "attackedThisTurn filter predicate"},
		{"Emet-Selch, Unsundered", "static#1.0", "exiled by the card's own Moved replacement"},
	} {
		got := staticSkipReason(t, row.card, row.key)
		if !strings.Contains(got, row.want) {
			t.Errorf("%s %s skip = %q, want it to contain %q", row.card, row.key, got, row.want)
		}
		if strings.Contains(got, "not observable on a probe or the card") {
			t.Errorf("%s %s still carries the generic reason: %q", row.card, row.key, got)
		}
	}
}
