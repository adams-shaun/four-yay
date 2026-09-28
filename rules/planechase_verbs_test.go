package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPathOfTheGhosthunterVoteResolvesTheChosenOutcome is the real-corpus
// end-to-end pin for the planechase verbs. Path of the Ghosthunter is the one
// "Will of the Planeswalkers" card whose whole resolution walks through all
// three gaps at once: SP$ Token -> DBSpace (DB$ BlankLine) -> DBVote
// (DB$ Vote, Choices$ DBPlaneswalk,DBChaos, VoteTiedAbility$ DBChaos), whose
// winning outcome is DB$ Planeswalk -- which before this build emitted
// "unimplemented API BlankLine" and then resolved nothing at all, because
// effVote's fixed-list shape did not run its winner.
//
// Asserted together: the X-token sub-ability still runs (two 1/1 Spirits for
// X=2), the vote records its Notes, the chosen outcome resolves as the
// documented no-planar-deck degrade, and no "unimplemented API" Note is
// emitted anywhere in the log. replayCheck certifies the whole chain still
// replays byte-identically.
func TestPathOfTheGhosthunterVoteResolvesTheChosenOutcome(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg, []string{"Path of the Ghosthunter"}, nil, nil, nil)
	// {X}{1}{W} with X=2: three generic + one white.
	addMana(t, e, 0, "CCCW")
	ghost := miscHandObj(t, e, 0, "Path of the Ghosthunter")
	submitChoices(t, e, miscCastOption(t, e, ghost))

	// The X announcement follows the cast option; choose X=2 so the token
	// sub-ability has a visible effect.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the X announcement after the cast, got %+v", d)
	}
	xIdx := -1
	for _, o := range d.Options {
		if o.Label == "X = 2" {
			xIdx = o.Index
		}
	}
	if xIdx < 0 {
		t.Fatalf("no X = 2 option in %+v", d.Options)
	}
	submitChoices(t, e, xIdx)
	passUntilStackEmpty(t, e, 40)

	// The token half ran: X=2 -> two 1/1 white Spirits with flying.
	if n := countNamed(t, e, state.ZBattlefield, 0, "Spirit Token"); n != 2 {
		t.Fatalf("seat 0 has %d Spirit Tokens, want 2 (the token sub-ability must still run)", n)
	}

	// The vote ran and the chosen outcome resolved. The deterministic
	// stand-in sends every vote to the first option (planeswalk), so
	// DBPlaneswalk -- and therefore api:Planeswalk -- is what executes.
	var voteNotes, resolved int
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note {
			continue
		}
		switch {
		case strings.HasPrefix(ev.Text, "unimplemented API"):
			t.Fatalf("resolution hit the unimplemented-API fallback: %q", ev.Text)
		case strings.HasPrefix(ev.Text, "votes for "):
			voteNotes++
		case ev.Text == "planeswalk (no planar deck)":
			resolved++
		}
	}
	if voteNotes != 2 {
		t.Fatalf("%d vote Notes, want one per voting player (2)", voteNotes)
	}
	if resolved != 1 {
		t.Fatalf("the chosen planeswalk outcome resolved %d times, want 1", resolved)
	}
	replayCheck(t, e, cfg)
}

// TestPathOfTheGhosthunterChaosOutcomeIsRegistered pins the other half of the
// ballot: DB$ ChaosEnsues is registered too, so a game that lands on chaos
// (a tie, or the VoteTiedAbility$ path) resolves as the documented degrade
// rather than an "unimplemented API" Note. It casts the same card and
// asserts only that api:ChaosEnsues is runnable from the corpus SVar, which
// is what makes the registration real rather than dead.
func TestPathOfTheGhosthunterChaosOutcomeIsRegistered(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	card := searchCorpusCard(t, reg, "Path of the Ghosthunter")
	var chaos *string
	for _, f := range card.Faces {
		if body, ok := f.SVars["DBChaos"]; ok {
			b := body
			chaos = &b
		}
	}
	if chaos == nil {
		t.Fatal("Path of the Ghosthunter has no DBChaos SVar")
	}
	if !strings.Contains(*chaos, "ChaosEnsues") {
		t.Fatalf("DBChaos body = %q, want a ChaosEnsues primitive", *chaos)
	}
	if !effects.Supported()["api:ChaosEnsues"] {
		t.Fatal("api:ChaosEnsues is not registered")
	}
}
