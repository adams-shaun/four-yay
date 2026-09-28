// Task fb-9d2338cc: the conditional enters-tapped land family. Blooming
// Marsh ("enters tapped unless you control two or fewer OTHER lands") and the
// fast / check lands gate their entry Tap on a ConditionPresent$ whose Forge
// default group is the battlefield. effects/conditionMet now resolves that
// shape (conditionMetBattlefield) and — because for the ReplacementResult$
// Updated path the Move (the land entering) is emitted BEFORE the With
// resolves, so the entering land is already a battlefield permanent when the
// gate counts — excludes the entering/replaced object so the fast lands'
// `Land.YouCtrl` GT2 really means "two or fewer OTHER lands".
//
// Card text below is authored for this test in the same R:/SVar$ shape as the
// corpus cards — never copied from Forge's .cards/cardsfolder.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// fastLandSrc is the Blooming Marsh / fast-land shape: a Moved-entry
// replacement whose ReplaceWith$ taps the entering land only when the player
// controls MORE THAN TWO other lands. The corpus fast lands use
// ConditionCompare$ GT2 on `Land.YouCtrl`; the check lands use EQ0 on a
// two-type YouCtrl spec.
const fastLandSrc = `Name:Fast Marsh
Types:Land
R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ LandTapped | ReplacementResult$ Updated | Description$ enters tapped unless you control two or fewer other lands
SVar:LandTapped:DB$ Tap | Defined$ Self | ETB$ True | ConditionPresent$ Land.YouCtrl | ConditionCompare$ GT2
Oracle:x
`

// checkLandSrc is the Glacial Fortress / check-land shape: entered tapped
// unless you control a Plains or an Island (ConditionCompare$ EQ0 on the
// count of Plains/Islands you control).
const checkLandSrc = `Name:Check Fortress
Types:Land
R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ LandTapped | ReplacementResult$ Updated | Description$ enters tapped unless you control a Plains or an Island
SVar:LandTapped:DB$ Tap | Defined$ Self | ETB$ True | ConditionPresent$ Plains.YouCtrl,Island.YouCtrl | ConditionCompare$ EQ0
Oracle:x
`

// otherLandSrc is a generic land for the "other lands" side of a count.
const otherLandSrc = `Name:Other Land
Types:Land
Oracle:x
`

// condPlainsSrc / condIslandSrc are the typed lands the check-land gate
// counts (named distinctly from the existing plainsSrc in this package).
const condPlainsSrc = `Name:Test Plains
Types:Land Plains
Oracle:x
`

const condIslandSrc = `Name:Test Island
Types:Land Island
Oracle:x
`

// playConditionalLand drives newFixtureDeck's conditional land through the
// play_land option with `others` generic lands already on the battlefield
// under seat 0, and returns the engine and the land's id.
func playConditionalLand(t *testing.T, seed uint64, src string, others int) (*Engine, state.ObjID) {
	t.Helper()
	e, _, id := newFixtureDeck(t, seed, src)
	for i := 0; i < others; i++ {
		onBoard(t, e, 0, otherLandSrc)
	}
	driveToStep(t, e, 1, 0, state.StepMain1)
	playFixtureLand(t, e, id)
	return e, id
}

// playFixtureLand submits the play_land option for id from seat 0's pending
// priority decision at main1.
func playFixtureLand(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after genesis, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "play_land" && opt.Obj == id {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for the fixture land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("zone = %s, want battlefield after playing the land", got)
	}
}

// TestFastLandEntersUntappedAtTwoOrFewerOtherLands pins the ORACLE boundary
// (not a guess): with 0, 1 or 2 OTHER lands the fast land enters UNTAPPED
// (the GT2 gate is unmet), and with 3 or more it enters TAPPED. The 2-other
// and 3-other cases are the edge the off-by-one would get wrong: a naive
// count that included the entering land would tap at 2 other lands instead
// of 3. Each row is an independent game through the real play_land path.
func TestFastLandEntersUntappedAtTwoOrFewerOtherLands(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		others int
		tapped bool
	}{
		{0, false}, {1, false}, {2, false}, // two or fewer other lands: untapped
		{3, true}, {5, true}, // three or more: tapped
	} {
		e, id := playConditionalLand(t, 300+uint64(tc.others), fastLandSrc, tc.others)
		if got := e.G.Obj(id).Tapped; got != tc.tapped {
			t.Errorf("others=%d other lands: entered tapped=%v, want %v", tc.others, got, tc.tapped)
		}
	}
}

// TestCheckLandEntersUntappedWhenYouControlAPlainsOrIsland pins the
// Glacial Fortress shape: the EQ0 gate is unmet (so no tap) exactly when you
// control a Plains or an Island, and met (tap) when you control neither.
func TestCheckLandEntersUntappedWhenYouControlAPlainsOrIsland(t *testing.T) {
	t.Parallel()
	// Control a Plains: enters untapped.
	e, _, id := newFixtureDeck(t, 400, checkLandSrc)
	onBoard(t, e, 0, condPlainsSrc)
	driveToStep(t, e, 1, 0, state.StepMain1)
	playFixtureLand(t, e, id)
	if got := e.G.Obj(id).Tapped; got {
		t.Fatal("with a Plains controlled, Check Fortress entered tapped, want untapped")
	}

	// Control neither a Plains nor an Island: enters tapped.
	e2, _, id2 := newFixtureDeck(t, 401, checkLandSrc)
	driveToStep(t, e2, 1, 0, state.StepMain1)
	playFixtureLand(t, e2, id2)
	if got := e2.G.Obj(id2).Tapped; !got {
		t.Fatal("with no Plains/Island controlled, Check Fortress entered untapped, want tapped")
	}

	// Control only an Island: enters untapped (the OR alternative).
	e3, _, id3 := newFixtureDeck(t, 402, checkLandSrc)
	onBoard(t, e3, 0, condIslandSrc)
	driveToStep(t, e3, 1, 0, state.StepMain1)
	playFixtureLand(t, e3, id3)
	if got := e3.G.Obj(id3).Tapped; got {
		t.Fatal("with an Island controlled, Check Fortress entered tapped, want untapped")
	}
}
