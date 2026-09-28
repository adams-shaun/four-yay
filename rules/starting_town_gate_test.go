// Task fb-20260918T073138Z-1ccd3f2d: Starting Town enters tapped on the
// first three turns. The corpus card's ETB-tapped replacement gates on
// SVar:X:Count$Compare Y GE1.Z.4 with
// SVar:Y:PlayerCountPropertyYou$HasPropertyActive and SVar:Z:Count$YourTurns
// — tapped only when X > 3, i.e. untapped on your first, second and third
// turns of the game (CR 305.7's "enters tapped unless" family). The sole
// defect was the PlayerCountPropertyYou$HasPropertyActive count head
// (unresolvable, Y=0), which took the Compare's ifFalse branch 4 on every
// turn and held the GT3 gate always; effects/count.go now resolves the head.
//
// Card text below is authored for this test in the same R:/SVar$ shape as
// the corpus card — never copied from Forge's .cards/cardsfolder (GPL).
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// startingTownSrc is the Starting-Town-shaped inline fixture: the identical
// R: Moved / ReplaceWith$ LandTapped / ConditionCheckSVar$ shape the corpus
// card carries, with its exact SVar table.
const startingTownSrc = `Name:Test Town
Types:Land
R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ LandTapped | ReplacementResult$ Updated | Description$ This land enters tapped unless it's your first, second, or third turn of the game.
SVar:LandTapped:DB$ Tap | Defined$ Self | ETB$ True | ConditionCheckSVar$ X | ConditionSVarCompare$ GT3
SVar:X:Count$Compare Y GE1.Z.4
SVar:Y:PlayerCountPropertyYou$HasPropertyActive
SVar:Z:Count$YourTurns
Oracle:x
`

// TestStartingTownEntersUntappedFirstThreeTurns pins the ORACLE boundary on
// real driven turns: seat 0's first, second and third turns of the game
// (game turns 1, 3 and 5 under the alternating two-seat rotation) enter
// UNTAPPED, and the fourth (game turn 7) enters TAPPED. Each row is an
// independent game through the real play_land path; TurnsTaken is folded
// from TurnChange events, so driving real turns exercises the whole gate.
func TestStartingTownEntersUntappedFirstThreeTurns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		seatTurn int // the controller's Nth turn of the game
		gameTurn int32
		tapped   bool
	}{
		{1, 1, false}, {2, 3, false}, {3, 5, false}, // turns 1-3: untapped
		{4, 7, true}, // turn 4: tapped
		{5, 9, true}, // stays tapped
	} {
		e, _, id := newFixtureDeck(t, 500+uint64(tc.seatTurn), startingTownSrc)
		driveToStep(t, e, tc.gameTurn, 0, state.StepMain1)
		playFixtureLand(t, e, id)
		if got := e.G.Obj(id).Tapped; got != tc.tapped {
			t.Errorf("your turn %d (game turn %d): entered tapped=%v, want %v",
				tc.seatTurn, tc.gameTurn, got, tc.tapped)
		}
	}
}
