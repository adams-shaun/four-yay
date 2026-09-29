package manabrew

// MBX-7: the api:Vote asks must be answerable on the ManaBrew wire, and any
// KChoose this package cannot specifically map must fall back to an
// answerable prompt rather than the ErrUnmapped error that left a live
// ManaBrew seat (a web-client session casting Council's Judgment) waiting
// forever after its answer came back invalidShape. The tests here pin, for
// every vote option shape the engine poses (effects/misc.go's askFixedVote
// and askCardVote, effects/vote.go's effPlayerVote), both directions: the
// prompt built, and every offered option selectable with its answer mapping
// back to exactly that option's native index.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// voteView is a three-seat view whose battlefields carry the ballot
// permanents: player 1 owns o2 (Ancient Brontodon) and player 2 owns o3
// (Grizzly Bears). Council's Judgment's ballot (Permanent.nonLand+YouDontCtrl,
// cast by seat 0) is exactly those two.
func voteView() view.View {
	return view.View{Viewer: 0, Turn: 4, Step: "main1", Active: 0, Priority: 0, Players: []view.PlayerView{
		{ID: 0, Name: "Caster", Life: 20, HandSize: 0, LibrarySize: 30},
		{ID: 1, Name: "Voter1", Life: 20, Battlefield: []view.CardView{
			{ID: 2, Name: "Ancient Brontodon", Printing: view.Printing{Name: "Ancient Brontodon"}, Types: "Creature — Elephant"},
		}},
		{ID: 2, Name: "Voter2", Life: 20, Battlefield: []view.CardView{
			{ID: 3, Name: "Grizzly Bears", Printing: view.Printing{Name: "Grizzly Bears"}, Types: "Creature — Bear"},
		}},
	}}
}

// cardBallotDecision is the real shape askCardVote poses: one vote_card
// option per ballot permanent, Label its name, Obj its id, Player its
// controller (CR 400.2).
func cardBallotDecision(seq uint64, voter state.PlayerID) *decision.Decision {
	d := newDec(seq, voter, decision.KChoose,
		decision.Option{Index: 0, Kind: "vote_card", Label: "Ancient Brontodon", Obj: 2, Player: 1},
		decision.Option{Index: 1, Kind: "vote_card", Label: "Grizzly Bears", Obj: 3, Player: 2})
	d.ResumeKind = "vote"
	d.Source = 9
	d.Prompt = "for a nonland permanent you don't control"
	return d
}

// answerViaWire runs an output value through the real response path (the
// TranslateResponse entry point) against p and returns the translated intent.
func answerViaWire(t *testing.T, p *Pending, out mb.PromptOutputValue) decision.Intent {
	t.Helper()
	o := New("table", 2, nil).TranslateResponse(respFor(p, out), p, p.Decision.Player)
	return *mustIntent(t, o)
}

// TestVoteFixedListIsAnswerable covers the fixed-list ballot (askFixedVote):
// one "vote" option per named choice, SYNTHETIC Obj (index+1) that is not
// any view object. The prompt must be chooseFromSelection over the labels in
// option order, flagged as a SPECIFIC mapping (no fallback), and every
// option must be selectable with the answer mapping back to that exact
// native option index.
func TestVoteFixedListIsAnswerable(t *testing.T) {
	d := newDec(11, 1, decision.KChoose,
		decision.Option{Index: 0, Kind: "vote", Label: "planeswalk", Obj: 1},
		decision.Option{Index: 1, Kind: "vote", Label: "chaosEnsues", Obj: 2})
	d.ResumeKind = "vote"
	d.Prompt = "Vote for an option"
	v := voteView()
	tr := New("table", 2, nil)
	msg, fellBack, err := tr.PromptFlagged(d, &v)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if fellBack {
		t.Fatalf("a mapped vote ask must NOT be flagged as fallback")
	}
	in, ok := msg.Input.Value.(mb.ChooseFromSelectionInput)
	if !ok {
		t.Fatalf("prompt type = %T, want ChooseFromSelectionInput", msg.Input.Value)
	}
	if len(in.Options) != 2 || in.Options[0].Label != "planeswalk" || in.Options[1].Label != "chaosEnsues" {
		t.Fatalf("selection options = %+v, want the two ballot labels in option order", in.Options)
	}
	if in.MinTotal != 1 || in.MaxTotal != 1 {
		t.Fatalf("min/max = %d/%d, want 1/1", in.MinTotal, in.MaxTotal)
	}
	// Precondition for the per-option assertion: the native answer space
	// really accepts each single pick (if it did not, the round-trip below
	// would be vacuous).
	for i := 0; i < 2; i++ {
		if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}); err != nil {
			t.Fatalf("precondition: native option %d is not a legal answer: %v", i, err)
		}
		p := pendingFor(d, v)
		got := answerViaWire(t, p, mb.SelectionDecision{ChosenIndices: []int{i}})
		if len(got.Choices) != 1 || got.Choices[0] != i {
			t.Fatalf("option %d answered to %v, want exactly that native index", i, got.Choices)
		}
	}
}

// TestVoteCardBallotIsAnswerable covers the candidate ballot (askCardVote,
// Council's Judgment's shape): one vote_card option per ballot permanent.
// The prompt must be chooseCards carrying the ballot permanents (with their
// real view card ids, so a ManaBrew client can pick by id), and every option
// selectable with the answer mapping back to that exact native option index.
func TestVoteCardBallotIsAnswerable(t *testing.T) {
	d := cardBallotDecision(12, 1)
	v := voteView()
	tr := New("table", 2, nil)
	msg, fellBack, err := tr.PromptFlagged(d, &v)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if fellBack {
		t.Fatalf("a mapped card-ballot vote ask must NOT be flagged as fallback")
	}
	in, ok := msg.Input.Value.(mb.ChooseCardsInput)
	if !ok {
		t.Fatalf("prompt type = %T, want ChooseCardsInput", msg.Input.Value)
	}
	if len(in.Cards) != 2 || in.Cards[0].Identity.Name != "Ancient Brontodon" || in.Cards[1].Identity.Name != "Grizzly Bears" {
		t.Fatalf("ballot cards = %+v, want the two ballot permanents in option order", in.Cards)
	}
	if in.Min != 1 || in.Max != 1 {
		t.Fatalf("min/max = %d/%d, want 1/1", in.Min, in.Max)
	}
	// Every ballot entry selectable, answer mapped back by native index.
	for i, wantID := range []string{cardID(2), cardID(3)} {
		if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}); err != nil {
			t.Fatalf("precondition: native option %d is not a legal answer: %v", i, err)
		}
		p := pendingFor(d, v)
		got := answerViaWire(t, p, mb.ChooseCardsDecision{ChosenCardIDs: []string{wantID}})
		if len(got.Choices) != 1 || got.Choices[0] != i {
			t.Fatalf("ballot entry %d (%s) answered to %v, want that native index", i, wantID, got.Choices)
		}
	}
	// A ref that was never offered is what Validate cannot express -- the
	// option-index fence: invalidShape, never a silently-empty pick.
	p := pendingFor(d, v)
	o := New("table", 2, nil).TranslateResponse(respFor(p, mb.ChooseCardsDecision{ChosenCardIDs: []string{cardID(99)}}), p, d.Player)
	wantErrCode(t, o, mb.CodeInvalidShape)
}

// TestVotePlayerBallotIsAnswerable covers the player ballot (effPlayerVote,
// Mob Verdict's shape): one "player" option per ballot player. It maps onto
// the shared chooseBoardTargets target-ref path; the answer maps back to the
// exact native option index.
func TestVotePlayerBallotIsAnswerable(t *testing.T) {
	d := newDec(13, 1, decision.KChoose,
		decision.Option{Index: 0, Kind: "player", Label: "Voter2 (Grizzly Bears)", Player: 2},
		decision.Option{Index: 1, Kind: "player", Label: "Caster", Player: 0})
	d.ResumeKind = "vote"
	d.Prompt = "Vote for a player"
	v := voteView()
	msg, fellBack, err := New("table", 2, nil).PromptFlagged(d, &v)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if fellBack {
		t.Fatalf("a mapped player-ballot vote ask must NOT be flagged as fallback")
	}
	if _, ok := msg.Input.Value.(mb.ChooseBoardTargetsInput); !ok {
		t.Fatalf("prompt type = %T, want ChooseBoardTargetsInput", msg.Input.Value)
	}
	for i, wantRef := range []mb.TargetRef{{Kind: mb.RefPlayer, ID: "player-2"}, {Kind: mb.RefPlayer, ID: "player-0"}} {
		if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}); err != nil {
			t.Fatalf("precondition: native option %d is not a legal answer: %v", i, err)
		}
		p := pendingFor(d, v)
		got := answerViaWire(t, p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{wantRef}})
		if len(got.Choices) != 1 || got.Choices[0] != i {
			t.Fatalf("ballot player %d answered to %v, want that native index", i, got.Choices)
		}
	}
}

// TestChooseFallbackIsAnswerable is the MBX-7 class closure: a KChoose with
// an option shape the translator has no specific mapping for -- here a
// single-Kind list of a Kind that does not exist yet, and a mixed-Kind list
// none of the recognised mixed shapes claims -- must still build an
// answerable prompt (chooseFromSelection over the native option labels,
// flagged as fallback), and every option must be selectable with the answer
// mapping back to exactly that native option index. Before this ticket both
// shapes returned ErrUnmapped, and a ManaBrew seat then waited forever.
func TestChooseFallbackIsAnswerable(t *testing.T) {
	v := voteView()
	cases := []struct {
		name string
		d    *decision.Decision
	}{
		{"unknown single kind", func() *decision.Decision {
			d := newDec(21, 1, decision.KChoose,
				decision.Option{Index: 0, Kind: "totally_new_shape", Label: "First"},
				decision.Option{Index: 1, Kind: "totally_new_shape", Label: "Second"})
			d.ResumeKind = "some_future_resume"
			return d
		}()},
		{"mixed kinds no shape claims", func() *decision.Decision {
			d := newDec(22, 1, decision.KChoose,
				decision.Option{Index: 0, Kind: "vote_card", Label: "Brontodon", Obj: 2},
				decision.Option{Index: 1, Kind: "vote", Label: "branch"})
			d.ResumeKind = "vote"
			return d
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.d
			msg, fellBack, err := New("table", 2, nil).PromptFlagged(d, &v)
			if err != nil {
				t.Fatalf("Prompt: %v", err)
			}
			if !fellBack {
				t.Fatalf("an unmapped option shape must be flagged as fallback, got a specific prompt %T", msg.Input.Value)
			}
			in, ok := msg.Input.Value.(mb.ChooseFromSelectionInput)
			if !ok {
				t.Fatalf("prompt type = %T, want ChooseFromSelectionInput", msg.Input.Value)
			}
			if len(in.Options) != len(d.Options) {
				t.Fatalf("fallback offered %d options, want %d (one per native option)", len(in.Options), len(d.Options))
			}
			for i := range d.Options {
				if in.Options[i].Label != d.Options[i].Label {
					t.Fatalf("fallback option %d label = %q, want the native label %q", i, in.Options[i].Label, d.Options[i].Label)
				}
				if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}); err != nil {
					t.Fatalf("precondition: native option %d is not a legal answer: %v", i, err)
				}
				p := pendingFor(d, v)
				got := answerViaWire(t, p, mb.SelectionDecision{ChosenIndices: []int{i}})
				if len(got.Choices) != 1 || got.Choices[0] != i {
					t.Fatalf("fallback option %d answered to %v, want exactly that native index", i, got.Choices)
				}
			}
			// The plain Prompt entry point keeps working (nil error, same
			// prompt) -- PromptFlagged is the flagged variant of it, not a
			// divergent mapping.
			msg2, err := New("table", 2, nil).Prompt(d, &v)
			if err != nil {
				t.Fatalf("plain Prompt on a fallback shape: %v", err)
			}
			if _, ok := msg2.Input.Value.(mb.ChooseFromSelectionInput); !ok {
				t.Fatalf("plain Prompt type = %T", msg2.Input.Value)
			}
		})
	}
}

// TestChooseFallbackZeroOptionsIsAnswerable pins the degenerate arm: an
// option-less KChoose (an empty ballot) still builds the fallback selection
// with zero options, and -- because the engine's own ask is Min 0 -- the
// empty selection the client answers is a legal intent. A Min-0 empty pick
// must NOT be an unanswerable prompt or an error.
func TestChooseFallbackZeroOptionsIsAnswerable(t *testing.T) {
	d := newDec(31, 1, decision.KChoose)
	d.ResumeKind = "vote"
	d.Min, d.Max = 0, 1
	v := voteView()
	msg, fellBack, err := New("table", 2, nil).PromptFlagged(d, &v)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if !fellBack {
		t.Fatalf("an option-less ask must be flagged as fallback")
	}
	in, ok := msg.Input.Value.(mb.ChooseFromSelectionInput)
	if !ok {
		t.Fatalf("prompt type = %T, want ChooseFromSelectionInput", msg.Input.Value)
	}
	if len(in.Options) != 0 || in.MinTotal != 0 || in.MaxTotal != 1 {
		t.Fatalf("fallback selection = %+v, want zero options, 0..1", in)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
		t.Fatalf("precondition: the engine's empty Min-0 pick is not legal: %v", err)
	}
	p := pendingFor(d, v)
	got := answerViaWire(t, p, mb.SelectionDecision{ChosenIndices: nil})
	if len(got.Choices) != 0 {
		t.Fatalf("empty answer mapped to %v, want no choices", got.Choices)
	}
}
