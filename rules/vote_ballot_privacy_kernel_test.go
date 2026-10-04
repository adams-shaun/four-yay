package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSecretVoteTriggerRetainsReferentsWithoutLoggingBallotsKernel: a
// Secretly$ vote's completion carries no ballots, the ballots are revealed
// only once every vote is in, and the same/diff referents still reach
// Grudge Keeper's trigger (seat 1 disagreed, so it loses 2).
func TestSecretVoteTriggerRetainsReferentsWithoutLoggingBallotsKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := voteCarrierEngine(t, reg, "Grudge Keeper")
	carrier := enterCarrier(t, e, "Grudge Keeper")
	if o := e.G.Obj(carrier); o == nil || o.Zone != state.ZBattlefield || len(o.Face().Triggers) == 0 {
		t.Fatal("Vote trigger carrier must be on battlefield with its trigger")
	}
	card, err := cards.ParseBytes("secret_vote.txt", []byte("Name:Secret Vote\nTypes:Sorcery\nA:SP$ Vote | Defined$ Player | Choices$ AChoice,BChoice | Secretly$ True\nSVar:AChoice:DB$ Draw\nSVar:BChoice:DB$ Draw\nOracle:x\n"))
	if err != nil {
		t.Fatal(err)
	}
	card.Link()
	votes := []int{0, 1, 0}
	src := e.G.AddObject(card, 0)
	src.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{src.ID})
	before := e.G.Players[1].Life
	kr9Probe(e, func() {
		effects.Resolve(e, &effects.Ctx{Source: src.ID, Controller: 0, SVars: card.Faces[0].SVars}, card.Faces[0].Abilities[0])
	})
	for i, v := range votes {
		d := e.Pending()
		if d == nil || d.ResumeKind != "vote" {
			t.Fatalf("voter %d: pending = %+v, want the vote ask", i, d)
		}
		if got := countVoteReveals(e); got != 0 {
			t.Fatalf("voter %d: %d secret ballots revealed before every vote was cast", i, got)
		}
		submitChoices(t, e, d.Options[v].Index)
	}
	found := false
	for _, ev := range e.L.Events {
		if _, ballots, _, ok := effects.VoteFinishedResult(ev); ok {
			found = true
			if len(ballots) != 0 || len(ev.Pairs) != 0 || ev.Amount == 0 {
				t.Fatalf("secret completion leaked ballots or lost ballot existence: %+v", ev)
			}
		}
	}
	if got := countVoteReveals(e); got != len(votes) {
		t.Fatalf("secret vote revealed %d ballots after completion, want %d", got, len(votes))
	}
	if !found {
		t.Fatal("no vote-finished emission with trigger on battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZStack, To: state.ZGraveyard})
	drainVoteTrigger(t, e, carrier)
	if got := before - e.G.Players[1].Life; got != 2 {
		t.Fatalf("secret vote difference did not reach Vote trigger: life loss %d, want 2", got)
	}
}

// TestVaultVoteMessageAndUpToWithSecretBallotKernel: Vault 11's real corpus
// card vote (UpTo$, Secretly$, VoteMessage$) poses one 0..1 ask per voter
// with the message as its prompt, accepts the empty ballot, and reveals the
// ballots only after completion.
func TestVaultVoteMessageAndUpToWithSecretBallotKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := voteCarrierEngine(t, reg, "Grudge Keeper")
	creature := enterCarrier(t, e, "Grudge Keeper")
	vault, ok := reg.Lookup("Vault 11: Voter's Dilemma")
	if !ok {
		t.Fatal("missing Vault 11 corpus card")
	}
	var vote *cards.SA
	for _, face := range vault.Faces {
		vote = cards.ResolveSVar(face.SVars, "DBVote")
		if vote != nil {
			break
		}
	}
	if vote == nil || vote.Params["UpTo"] != "True" || vote.Params["Secretly"] != "True" || vote.Params["VoteMessage"] == "" {
		t.Fatalf("real corpus vote preconditions absent: %+v", vote)
	}
	src := e.G.AddObject(vault, 0)
	src.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{src.ID})
	kr9Probe(e, func() {
		effects.Resolve(e, &effects.Ctx{Source: src.ID, Controller: 0, SVars: vault.Faces[0].SVars}, vote)
	})
	for i := 0; i < 3; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "vote" || d.Player != state.PlayerID(i) || d.Min != 0 || d.Max != 1 || d.Prompt != "for a creature" {
			t.Fatalf("voter %d: unexpected vote ask %+v", i, d)
		}
		if len(d.Options) == 0 || d.Options[0].Obj != creature {
			t.Fatalf("voter %d: battlefield creature not offered: %+v", i, d.Options)
		}
		if got := countVoteReveals(e); got != 0 {
			t.Fatalf("voter %d: %d secret ballots revealed before every vote was cast", i, got)
		}
		submitChoices(t, e)
	}
	if d := e.Pending(); d != nil && d.ResumeKind == "vote" {
		t.Fatalf("vote asked again after three declines: %+v", d)
	}
	found := false
	for _, ev := range e.L.Events {
		if _, ballots, _, ok := effects.VoteFinishedResult(ev); ok {
			found = true
			if len(ballots) != 0 || len(ev.Pairs) != 0 {
				t.Fatalf("secret vote leaked: %+v", ev)
			}
		}
	}
	if !found {
		t.Fatal("vote never completed")
	}
	if got := countVoteReveals(e); got != 3 {
		t.Fatalf("secret ballots revealed after completion = %d, want 3 (one per voter)", got)
	}
}
