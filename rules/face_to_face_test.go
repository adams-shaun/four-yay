package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// faceToFaceMatch casts Face to Face at the opponent and plays the Rock,
// Paper, Scissors match: the caster always throws Rock and the opponent
// throws oppThrows[round] (Rock ties, Scissors loses to the caster's Rock,
// Paper beats it). It returns the rounds played (the opponent's throws
// asked) and the opponent's life afterwards.
func faceToFaceMatch(t *testing.T, oppThrows []string) (rounds int, oppLife int32) {
	t.Helper()
	e, cfg, id, caster := corpusCardConfig(t, 7311, "Face to Face")
	addMana(t, e, caster, "RR") // {1}{R}
	opp := 1 - caster
	d := castFixture(t, e, id, int(opp))
	pick := func(d *decision.Decision, label string) int {
		for _, o := range d.Options {
			if strings.EqualFold(strings.TrimSpace(o.Label), label) {
				return o.Index
			}
		}
		t.Fatalf("no %q option in %+v", label, d.Options)
		return -1
	}
	for i := 0; i < 80 && d != nil && len(e.G.Stack) > 0; i++ {
		switch {
		case d.Kind == decision.KPriority:
			castFirst(t, e, "pass")
		case d.Player == caster:
			submitChoices(t, e, pick(d, "Rock"))
		case d.Player == opp:
			if rounds >= len(oppThrows) {
				t.Fatalf("round %d asked, but the match was already decided after %d rounds", rounds+1, len(oppThrows))
			}
			submitChoices(t, e, pick(d, oppThrows[rounds]))
			rounds++
		default:
			t.Fatalf("unexpected ask %s/%q for seat %d", d.Kind, d.ResumeKind, d.Player)
		}
		d = e.Pending()
	}
	if len(e.G.Stack) > 0 {
		t.Fatalf("the spell never finished resolving (pending %+v)", d)
	}
	replayCheck(t, e, cfg)
	return rounds, e.G.Players[opp].Life
}

// TestFaceToFaceIsBestTwoOutOfThree: Face to Face repeats its round while
// SVar:Total = SVar$Wins/LimitMin.Losses (Forge's LimitMin: the larger of
// the two, max(Wins, Losses)) is below 2. A win, a loss and a win is three
// rounds and 5 damage; two losses are two rounds and no damage; a tie does
// not count. Before the fix the /LimitMin.<SVar> operand was not evaluated.
func TestFaceToFaceIsBestTwoOutOfThree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		throws []string
		life   int32
	}{
		{"win-loss-win", []string{"Scissors", "Paper", "Scissors"}, 15},
		{"loss-tie-loss", []string{"Paper", "Rock", "Paper"}, 20},
		{"win-win", []string{"Scissors", "Scissors"}, 15},
	} {
		rounds, life := faceToFaceMatch(t, tc.throws)
		if rounds != len(tc.throws) || life != tc.life {
			t.Errorf("%s: %d rounds, opponent at %d life; want %d rounds, %d life",
				tc.name, rounds, life, len(tc.throws), tc.life)
		}
	}
}
