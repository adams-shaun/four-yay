package effects

import "testing"

// TestEladamriChosenCardMovesFromEitherOrigin restores the no-ask leg the
// deleted TestMemoryLeakCompoundFetchPlayerChoosesFromBothZones ran at its
// end (the W3 legacy removal left the helper uncalled): Eladamri's real
// ChooseCard sub moves the already-chosen card from either origin zone with
// no second, unrestricted choice.
func TestEladamriChosenCardMovesFromEitherOrigin(t *testing.T) {
	assertEladamriChosenCardMovesFromEitherOrigin(t)
}
