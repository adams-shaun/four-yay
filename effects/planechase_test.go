package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
)

// TestBlankLineIsASilentNoOp pins the display spacer's contract: DB$ BlankLine
// resolves to nothing at all. It is registered so the resolver does not emit
// its generic "unimplemented API BlankLine" Note (the Path cycle's DBSpace
// SVar is one), and it must stay SILENT -- a Note would put noise in every
// log that walks through it.
func TestBlankLineIsASilentNoOp(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Source: 0}, sa(t, "SP$ BlankLine"))
	if len(h.log) != 0 {
		t.Fatalf("BlankLine emitted %d events, want none: %+v", len(h.log), h.log)
	}
}

// TestPlaneswalkRecordsTheNoPlanarDeckDegrade pins CR 901.8's degrade: with no
// planar deck the planeswalk resolves as a recorded no-op (there is no next
// plane and no planeswalk-away trigger to fire).
func TestPlaneswalkRecordsTheNoPlanarDeckDegrade(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Source: 0}, sa(t, "SP$ Planeswalk"))
	if len(h.log) != 1 || h.log[0].Kind != events.Note {
		t.Fatalf("Planeswalk log = %+v, want one Note", h.log)
	}
	if h.log[0].Text != "planeswalk (no planar deck)" {
		t.Fatalf("Planeswalk note = %q", h.log[0].Text)
	}
}

// TestOptionalPlaneswalkNoHostDeclinesAndRunsTheChain proves the optional
// handler is reached even on the effects test double, whose Ask method has no
// decision channel. Without the registration, Resolve would emit the generic
// unimplemented-API Note for the Planeswalk SA but STILL walk its SubAbility$
// chain, so the chained probe running is not by itself evidence of the
// registration -- the election Note and the no-fallback assertion below are.
func TestOptionalPlaneswalkNoHostDeclinesAndRunsTheChain(t *testing.T) {
	var ran bool
	Register("TestPlaneswalkChain", func(_ Host, _ *Ctx, _ *cards.SA) { ran = true })
	t.Cleanup(func() { unregister("TestPlaneswalkChain") })
	c, _ := cards.ParseBytes("planeswalk.txt", []byte(
		"Name:Planeswalk probe\nTypes:Sorcery\n"+
			"A:SP$ Planeswalk | Optional$ True | SubAbility$ Next\n"+
			"SVar:Next:DB$ TestPlaneswalkChain\nOracle:x\n"))
	c.Link()
	h := newHost(t, 2)
	Resolve(h, &Ctx{Source: 0}, c.Faces[0].Abilities[0])
	if !ran {
		t.Fatal("optional Planeswalk did not continue into SubAbility")
	}
	for _, e := range h.log {
		if strings.HasPrefix(e.Text, "unimplemented API") {
			t.Fatalf("Planeswalk reached generic fallback: %q", e.Text)
		}
	}
	found := false
	for _, e := range h.log {
		if e.Text == "planeswalk election: no" {
			found = true
		}
		// The decline contract: nothing may claim the planeswalk resolved.
		if e.Text == "planeswalk (no planar deck)" {
			t.Fatalf("a declined election recorded the no-planar-deck no-op: %+v", h.log)
		}
	}
	if !found {
		t.Fatalf("no recorded deterministic decline in %+v", h.log)
	}
}

// TestChaosEnsuesRecordsTheNoPlanarDeckDegrade pins CR 901.9's degrade the
// same way.
func TestChaosEnsuesRecordsTheNoPlanarDeckDegrade(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Source: 0}, sa(t, "SP$ ChaosEnsues"))
	if len(h.log) != 1 || h.log[0].Kind != events.Note {
		t.Fatalf("ChaosEnsues log = %+v, want one Note", h.log)
	}
	if h.log[0].Text != "chaos ensues (no planar deck)" {
		t.Fatalf("ChaosEnsues note = %q", h.log[0].Text)
	}
}

// TestPlanechaseVerbsNeverEmitUnimplemented is the regression guard for the
// gap the brief named: a registered verb must not reach Resolve's
// "unimplemented API" fallback.
func TestPlanechaseVerbsNeverEmitUnimplemented(t *testing.T) {
	for _, api := range []string{"BlankLine", "Planeswalk", "ChaosEnsues"} {
		if !Supported()["api:"+api] {
			t.Fatalf("api:%s is not registered", api)
		}
		h := newHost(t, 2)
		Resolve(h, &Ctx{Source: 0}, sa(t, "SP$ "+api))
		for _, e := range h.log {
			if e.Kind == events.Note && strings.HasPrefix(e.Text, "unimplemented API") {
				t.Fatalf("%s emitted %q", api, e.Text)
			}
		}
	}
}

// voteFixtureSA builds a Vote SA with real SVar bodies so the fixed-list
// outcome resolution can run a named sub. The chosen SVar's body is a
// Registered probe, so the test observes the winner without any corpus text.
func voteFixtureSA(t *testing.T, line string) (*fakeHost, *cards.SA, map[string]string, *[]string) {
	t.Helper()
	var order []string
	Register("TestVoteA", func(_ Host, _ *Ctx, _ *cards.SA) { order = append(order, "A") })
	Register("TestVoteB", func(_ Host, _ *Ctx, _ *cards.SA) { order = append(order, "B") })
	t.Cleanup(func() { unregister("TestVoteA", "TestVoteB") })
	c, _ := cards.ParseBytes("v.txt", []byte("Name:T\nTypes:Sorcery\nA:"+line+"\nOracle:x\n"))
	c.Link()
	h := newHost(t, 2)
	return h, c.Faces[0].Abilities[0], c.Faces[0].SVars, &order
}

// TestVoteFixedListRunsTheWinningOutcome is the core of the brief's premise
// correction: the fixed-list Vote used to resolve NOTHING, so the Path
// cycle's DBPlaneswalk/DBChaos never ran and the registered planechase verbs
// were unreachable from a vote. Every voter takes the first option (the
// deterministic stand-in), so the first SVar wins and runs.
func TestVoteFixedListRunsTheWinningOutcome(t *testing.T) {
	h, s, svars, order := voteFixtureSA(t, "SP$ Vote | Defined$ Player | Choices$ AChoice,BChoice "+
		"| VoteTiedAbility$ BChoice\nSVar:AChoice:DB$ TestVoteA\nSVar:BChoice:DB$ TestVoteB\n")
	Resolve(h, &Ctx{Source: 0, SVars: svars}, s)
	if len(*order) != 1 || (*order)[0] != "A" {
		t.Fatalf("winning outcome ran %v, want exactly [A]", *order)
	}
	// One Note per voting player, naming the option voted for.
	notes := 0
	for _, e := range h.log {
		if e.Kind == events.Note && e.Text == "votes for AChoice" {
			notes++
		}
	}
	if notes != 2 {
		t.Fatalf("%d vote Notes, want 2", notes)
	}
}

// TestVoteWinnerPicksTheHighestAndReportsTies exercises the tally directly.
// The deterministic stand-in cannot produce a live tie today (every vote goes
// to option 0), so a live resolution never enters the tie branch; the
// real-SA tie path is pinned by TestVoteAnsweredTieRunsVoteTiedAbility
// (effects) and TestPathOfTheAnimistTiedVoteRunsTheTiedBranch (rules). The
// first highest index wins the tie, matching the oracle's "if planeswalk
// gets more votes" over "or tied".
func TestVoteWinnerPicksTheHighestAndReportsTies(t *testing.T) {
	cases := []struct {
		counts []int
		want   int
		tied   bool
	}{
		{[]int{2, 0}, 0, false},
		{[]int{0, 3, 1}, 1, false},
		{[]int{1, 1}, 0, true},
		{[]int{1, 0, 1}, 0, true},
		{[]int{0, 0, 2}, 2, false},
		{[]int{0, 0}, 0, true},
		{nil, 0, false},
	}
	for _, tc := range cases {
		got, tied := voteWinner(tc.counts)
		if got != tc.want || tied != tc.tied {
			t.Fatalf("voteWinner(%v) = (%d,%v), want (%d,%v)", tc.counts, got, tied, tc.want, tc.tied)
		}
	}
}

// TestVoteSplitEmptyBallotBindsNeitherSet is the empty-ballot regression
// (review finding): when a vote offered no ballot option at all -- a card
// ballot whose VoteCard$ matched no permanent, or a fixed-list Vote with no
// Choices$ -- no voter cast a real vote, so every pick is -1. The anchorPick
// != -1 guard must NOT then route every voting opponent into the diff set:
// "voted for a choice you didn't vote for" is false when there was no
// choice. Without ballotExisted both sets read empty only by accident for a
// one-voter table; with three voters the old path returned diff=[1 2].
func TestVoteSplitEmptyBallotBindsNeitherSet(t *testing.T) {
	ballots := []VoteBallot{
		{Player: 0, Pick: -1},
		{Player: 1, Pick: -1},
		{Player: 2, Pick: -1},
	}
	same, diff := VoteSplit(0, ballots, false)
	if len(same) != 0 || len(diff) != 0 {
		t.Fatalf("empty ballot: same=%v diff=%v, want both empty", same, diff)
	}
	// Control: a real ballot with the anchor picking option 0 and both
	// opponents choosing option 1 puts both opponents in the diff set -- the
	// documented reading ballotExisted must not disturb.
	ballots = []VoteBallot{
		{Player: 0, Pick: 0},
		{Player: 1, Pick: 1},
		{Player: 2, Pick: 1},
	}
	same, diff = VoteSplit(0, ballots, true)
	if len(same) != 0 || len(diff) != 2 || diff[0] != 1 || diff[1] != 2 {
		t.Fatalf("real ballot: same=%v diff=%v, want same=[] diff=[1 2]", same, diff)
	}
}
