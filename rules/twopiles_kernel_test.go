package rules

// Kernel-era restorations of effects/twopiles_test.go's staged pins (deleted
// with the W3 legacy removal), driven through the real Fact or Fiction,
// Split the Spoils and Brilliant Ultimatum: the separator's split, the
// chooser's pick, and where each pile goes.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

var kr3PileNames = []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon"}

// kr3PileBoard is a 2-seat game whose seat 0 holds the corpus card spell and
// whose library's top five are Alpha..Epsilon, in order (returned).
func kr3PileBoard(t *testing.T, seed uint64, spell string) (*Engine, Config, []state.ObjID) {
	t.Helper()
	deck := []*cards.Card{corpusAlternativeCard(t, spell)}
	for _, n := range kr3PileNames {
		deck = append(deck, card(t, kr3Creature(n)))
	}
	e, cfg := kr3Game(t, seed, deck, nil)
	kr3Move(t, e, 0, spell, state.ZHand)
	var ids []state.ObjID
	for _, n := range kr3PileNames {
		ids = append(ids, kr3Move(t, e, 0, n, state.ZLibrary))
	}
	kr3LibraryTop(t, e, 0, ids...)
	return e, cfg, ids
}

// kr3FoFSplit casts Fact or Fiction and returns its posed split ask.
func kr3FoFSplit(t *testing.T, seed uint64) (*Engine, Config, []state.ObjID, *decision.Decision) {
	t.Helper()
	e, cfg, ids := kr3PileBoard(t, seed, "Fact or Fiction")
	d := kr3Cast(t, e, "Fact or Fiction", "UUUU", -1)
	return e, cfg, ids, d
}

func kr3Indexes(t *testing.T, d *decision.Decision, ids ...state.ObjID) []int {
	t.Helper()
	out := []int{}
	for _, id := range ids {
		out = append(out, kr3OptionObj(t, d, id))
	}
	return out
}

// TestTwoPilesSplitAskPosesToTheSeparatorAndSuspends: the separator (the
// opponent) is posed a KChoose over the five revealed cards, Min 0 (piles
// can be empty) Max 5, in reveal order, before anything moves.
func TestTwoPilesSplitAskPosesToTheSeparatorAndSuspends(t *testing.T) {
	t.Parallel()
	e, _, ids, d := kr3FoFSplit(t, 231)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "twopiles_split" || d.Player != 1 {
		t.Fatalf("split ask = %+v, want the separator's (seat 1) twopiles_split KChoose", d)
	}
	if d.Min != 0 || d.Max != 5 {
		t.Fatalf("bounds = %d..%d, want 0..5", d.Min, d.Max)
	}
	if got := kr3OptionObjs(d); !slices.Equal(got, ids) {
		t.Fatalf("options = %v, want the revealed cards in order %v", got, ids)
	}
	for _, id := range ids {
		if kr3Zone(e, id) != state.ZLibrary {
			t.Fatalf("card %d moved before the split was answered", id)
		}
	}
}

// TestTwoPilesAnsweredSplitAsksThePick: the split answer poses the chooser's
// (Defined$ You) two-option pile pick, not a second split, with nothing moved.
func TestTwoPilesAnsweredSplitAsksThePick(t *testing.T) {
	t.Parallel()
	e, _, ids, d := kr3FoFSplit(t, 232)
	mark := len(e.L.Events)
	d = kr3Answer(t, e, kr3Indexes(t, d, ids[0], ids[2])...)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "twopiles_pick" || d.Player != 0 {
		t.Fatalf("pick ask = %+v, want seat 0's twopiles_pick", d)
	}
	if d.Min != 1 || d.Max != 1 || len(d.Options) != 2 || d.Options[0].Kind != "pile-a" || d.Options[1].Kind != "pile-b" {
		t.Fatalf("pick options = %+v, want pile-a/pile-b at Min=Max=1", d.Options)
	}
	if n := kr3Count(e, mark, events.MoveZone); n != 0 {
		t.Fatalf("%d card(s) moved before the pick was answered", n)
	}
}

// kr3PilesAfter answers FoF's split with pileA and the pick with kind, and
// checks that pileA's cards went to want and every other card to the other
// of hand/graveyard.
func kr3PilesAfter(t *testing.T, seed uint64, pileA []int, kind string, aZone, bZone state.Zone) {
	t.Helper()
	e, cfg, ids, d := kr3FoFSplit(t, seed)
	var a []state.ObjID
	for _, i := range pileA {
		a = append(a, ids[i])
	}
	d = kr3Answer(t, e, kr3Indexes(t, d, a...)...)
	if d == nil || d.ResumeKind != "twopiles_pick" {
		t.Fatalf("pick ask = %+v", d)
	}
	if d := kr3Answer(t, e, kr3OptionKind(t, d, kind)); d != nil {
		t.Fatalf("a fully answered TwoPiles posed %+v", d)
	}
	for _, id := range ids {
		want := bZone
		if slices.Contains(a, id) {
			want = aZone
		}
		if got := kr3Zone(e, id); got != want {
			t.Fatalf("card %d zone = %v, want %v", id, got, want)
		}
	}
	replayCheck(t, e, cfg)
}

// TestTwoPilesAnsweredPickMovesThePiles: pile A chosen -> its cards to hand,
// pile B to the graveyard.
func TestTwoPilesAnsweredPickMovesThePiles(t *testing.T) {
	t.Parallel()
	kr3PilesAfter(t, 233, []int{0, 2}, "pile-a", state.ZHand, state.ZGraveyard)
}

// TestTwoPilesPickBChoosesTheSecondPile: pile B chosen -> B to hand, A to
// the graveyard.
func TestTwoPilesPickBChoosesTheSecondPile(t *testing.T) {
	t.Parallel()
	kr3PilesAfter(t, 234, []int{0, 2}, "pile-b", state.ZGraveyard, state.ZHand)
}

// TestTwoPilesEmptyPileAnswerMovesEverythingToUnchosen: an empty split (Min
// 0 makes it legal) with the empty pile A chosen sends all five cards to
// the graveyard.
func TestTwoPilesEmptyPileAnswerMovesEverythingToUnchosen(t *testing.T) {
	t.Parallel()
	kr3PilesAfter(t, 235, nil, "pile-a", state.ZHand, state.ZGraveyard)
}

// TestTwoPilesSplitTheSpoilsTargetedChooserOpponent: the real Split the
// Spoils reads its targets (DefinedCards$ Targeted) and swaps the roles --
// the caster (Separator$ You) splits the three exiled targets, the opponent
// (Chooser$ Opponent) picks, and the chosen pile goes from exile to hand,
// the other to the graveyard.
func TestTwoPilesSplitTheSpoilsTargetedChooserOpponent(t *testing.T) {
	t.Parallel()
	e, cfg, ids := kr3PileBoard(t, 236, "Split the Spoils")
	gy := ids[:3]
	for _, id := range gy {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
	e.pending = nil
	addMana(t, e, 0, "GGG")
	submitChoices(t, e, castOptionFor(t, e, kr3Find(e, 0, state.ZHand, "Split the Spoils")).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("decision = %+v, want the up-to-five graveyard target ask", d)
	}
	d = kr3Answer(t, e, kr3Indexes(t, d, gy...)...)
	if d == nil || d.ResumeKind != "twopiles_split" || d.Player != 0 || len(d.Options) != 3 {
		t.Fatalf("split ask = %+v, want the caster's split over the three exiled targets", d)
	}
	d = kr3Answer(t, e, kr3OptionObj(t, d, gy[1]))
	if d == nil || d.ResumeKind != "twopiles_pick" || d.Player != 1 {
		t.Fatalf("pick ask = %+v, want Chooser$ Opponent (seat 1)", d)
	}
	if d := kr3Answer(t, e, kr3OptionKind(t, d, "pile-a")); d != nil {
		t.Fatalf("unexpected follow-up %+v", d)
	}
	if z := kr3Zone(e, gy[1]); z != state.ZHand {
		t.Fatalf("chosen pile card zone = %v, want hand", z)
	}
	for _, id := range []state.ObjID{gy[0], gy[2]} {
		if z := kr3Zone(e, id); z != state.ZGraveyard {
			t.Fatalf("unchosen pile card %d zone = %v, want graveyard", id, z)
		}
	}
	replayCheck(t, e, cfg)
}

// TestTwoPilesPlayBodyHandsThePileToPlay: Brilliant Ultimatum's ChosenPile$
// is a DB$ Play body; the chosen pile is handed to Play, whose play ask is
// posed over exactly that pile's cards.
func TestTwoPilesPlayBodyHandsThePileToPlay(t *testing.T) {
	t.Parallel()
	e, _, ids := kr3PileBoard(t, 237, "Brilliant Ultimatum")
	d := kr3Cast(t, e, "Brilliant Ultimatum", "WWUUUBB", -1)
	if d == nil || d.ResumeKind != "twopiles_split" || d.Player != 1 {
		t.Fatalf("split ask = %+v, want the opponent's split", d)
	}
	d = kr3Answer(t, e, kr3Indexes(t, d, ids[0], ids[1])...)
	if d == nil || d.ResumeKind != "twopiles_pick" || d.Player != 0 {
		t.Fatalf("pick ask = %+v, want the caster's pick", d)
	}
	d = kr3Answer(t, e, kr3OptionKind(t, d, "pile-a"))
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("decision = %+v, want the play KModes over the chosen pile", d)
	}
	played := map[state.ObjID]bool{}
	for _, o := range d.Options {
		if o.Obj != 0 {
			played[o.Obj] = true
		}
	}
	if len(played) != 2 || !played[ids[0]] || !played[ids[1]] {
		t.Fatalf("play options = %+v, want exactly the chosen pile's two cards", d.Options)
	}
}
