package rules

// Kernel-era restoration of TestPathOfTheAnimistTiedVoteRunsTheTiedBranch
// (planechase_animist_test.go). The old test filled the legacy Ctx.Votes seam;
// the per-voter ballot is now a real tape ask, so the tie is produced by the
// voters' own answers.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPathOfTheAnimistTiedVoteRunsTheTiedBranchKernel resolves Path of the
// Animist's compiled DBVote SA (Choices$ DBPlaneswalk,DBChaos |
// VoteTiedAbility$ DBChaos) under a kernel probe and answers each voter's
// ballot: a 1-1 tie must run the tied branch (chaos), never the planeswalk
// winner; the control (both voters on planeswalk) runs planeswalk and not
// chaos, so the assertion discriminates tie from non-tie.
func TestPathOfTheAnimistTiedVoteRunsTheTiedBranchKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	animist := searchCorpusCard(t, reg, "Path of the Animist")
	voteSA := cards.ResolveSVar(animist.Faces[0].SVars, "DBVote")
	if voteSA == nil || voteSA.Params["VoteTiedAbility"] != "DBChaos" {
		t.Fatalf("Path of the Animist DBVote SA = %+v, want a VoteTiedAbility$ DBChaos", voteSA)
	}
	run := func(t *testing.T, ballots []int) (chaos, planeswalk int) {
		t.Helper()
		e, _ := animistTestEngine(t, reg)
		src := e.G.AddObject(animist, 0)
		src.Zone = state.ZStack
		e.G.SetZone(state.ZStack, 0, []state.ObjID{src.ID})
		e.pending = nil
		before := len(e.L.Events)
		e.probe(func() {
			effects.Resolve(e, &effects.Ctx{Source: src.ID, Controller: 0, SVars: animist.Faces[0].SVars}, voteSA)
		})
		for i := 0; ; i++ {
			d := e.Pending()
			if d == nil || d.ResumeKind != "vote" {
				if i != len(ballots) {
					t.Fatalf("answered %d ballots, want %d (pending %+v)", i, len(ballots), d)
				}
				break
			}
			if i >= len(ballots) {
				t.Fatalf("unexpected extra ballot %+v", d)
			}
			if d.Player != state.PlayerID(i) || len(d.Options) != 2 {
				t.Fatalf("ballot %d = %+v, want seat %d choosing between the two choices", i, d, i)
			}
			submitChoices(t, e, d.Options[ballots[i]].Index)
		}
		for _, ev := range e.L.Events[before:] {
			if ev.Kind != events.Note {
				continue
			}
			switch ev.Text {
			case "chaos ensues (no planar deck)":
				chaos++
			case "planeswalk (no planar deck)":
				planeswalk++
			}
		}
		return chaos, planeswalk
	}
	t.Run("tie", func(t *testing.T) {
		chaos, planeswalk := run(t, []int{0, 1})
		if chaos != 1 || planeswalk != 0 {
			t.Fatalf("tied vote ran chaos %d / planeswalk %d times, want 1 / 0 (VoteTiedAbility$ DBChaos)", chaos, planeswalk)
		}
	})
	t.Run("planeswalk wins", func(t *testing.T) {
		chaos, planeswalk := run(t, []int{0, 0})
		if chaos != 0 || planeswalk != 1 {
			t.Fatalf("non-tied vote ran chaos %d / planeswalk %d times, want 0 / 1", chaos, planeswalk)
		}
	})
}
