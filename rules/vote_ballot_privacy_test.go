package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestSecretVoteTriggerRetainsReferentsWithoutLoggingBallots(t *testing.T) {
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
	if got := card.Faces[0].Abilities[0].Params["Choices"]; got != "AChoice,BChoice" || votes[0] == votes[1] {
		t.Fatalf("the two vote choices must differ: %q, votes %v", got, votes)
	}
	src := e.G.AddObject(card, 0)
	src.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{src.ID})
	// Seat 1 disagrees with seat 0; the same-vote control [0,0,0]
	// would cause zero Grudge Keeper life loss instead of two.
	before := e.G.Players[1].Life
	effects.Resolve(e, &effects.Ctx{Source: src.ID, Controller: 0, SVars: card.Faces[0].SVars, Votes: votes}, card.Faces[0].Abilities[0])
	found := false
	for _, ev := range e.L.Events {
		if _, ballots, _, ok := effects.VoteFinishedResult(ev); ok {
			found = true
			if len(ballots) != 0 || len(ev.Pairs) != 0 || ev.Amount == 0 {
				t.Fatalf("secret completion leaked ballots or lost ballot existence: %+v", ev)
			}
		}
		if strings.HasPrefix(ev.Text, "votes for ") {
			t.Fatalf("secret per-voter note leaked: %+v", ev)
		}
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

func TestVaultVoteMessageAndUpToWithSecretBallot(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := voteCarrierEngine(t, reg, "Grudge Keeper")
	creature := enterCarrier(t, e, "Grudge Keeper")
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("vote option must be a battlefield creature")
	}
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
	effects.Resolve(e, &effects.Ctx{Source: src.ID, Controller: 0, SVars: vault.Faces[0].SVars}, vote)
	for i := 0; i < 3; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "vote" || d.Player != state.PlayerID(i) || d.Min != 0 || d.Max != 1 || d.Prompt != "for a creature" {
			t.Fatalf("voter %d: unexpected vote ask %+v", i, d)
		}
		if len(d.Options) == 0 || d.Options[0].Obj != creature {
			t.Fatalf("voter %d: battlefield creature not offered: %+v", i, d.Options)
		}
		// UpTo$ allows a real empty answer even with an eligible creature.
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
		if strings.HasPrefix(ev.Text, "votes for ") {
			t.Fatalf("secret vote leaked through note: %+v", ev)
		}
	}
	if !found {
		t.Fatal("vote never completed")
	}
}
