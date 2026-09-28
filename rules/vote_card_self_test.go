package rules

// vote_card_self1 (round 3): a card ballot's bot vote must not exile the
// VOTER's own permanent when a foreign one is offered. Council's Judgment's
// VoteCard$ excludes only the CASTER's permanents ("a nonland permanent you
// don't control"), so at 3+ seats every other seat's permanents — including
// the voter's OWN — are on the ballot. The round-2 policy ranked every
// offered option by worth and so had a voter pick its own highest-worth
// permanent over an opponent's lower-worth one; the option carried no
// controller, so the policy could not tell them apart. `askCardVote` now
// stamps each option's subject controller on Option.Player (public under CR
// 400.2), and the policy ranks the non-self options first.
//
// This is the REAL compiled corpus card, answered through the production bot
// path (testBot.answer = botpolicy.Decide over botpolicy.BoardFromGame, the
// same Board a hosted bot seat builds), at three seats where a two-seat game
// cannot show the defect (there the ballot holds only one seat's permanents).

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// voteThreeSeatEngine builds a three-seat engine from one shared corpus deck,
// then moves the named cards to their seats with LOGGED MoveZones so
// replayCheck is meaningful. Seat 0 starts (seatZeroStart) so the caster's
// Council's Judgment is castable and the vote opens with seat 0.
func voteThreeSeatEngine(t *testing.T, reg *cards.Registry, hand0, board1, board2 []string) (*Engine, Config) {
	t.Helper()
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("missing corpus card \"Mountain\"")
	}
	judgment, ok := reg.Lookup("Council's Judgment")
	if !ok {
		t.Fatal("missing corpus card \"Council's Judgment\"")
	}
	var deck []*cards.Card
	for _, name := range slices.Concat(hand0, board1, board2) {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("missing corpus card %q", name)
		}
		deck = append(deck, c)
	}
	deck = append(deck, judgment)
	for len(deck) < 40 {
		deck = append(deck, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"caster", "voter1", "voter2"},
		Decks: [][]*cards.Card{deck, deck, deck}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	for _, name := range hand0 {
		miscMoveByName(t, e, 0, name, state.ZHand)
	}
	for _, name := range board1 {
		miscMoveByName(t, e, 1, name, state.ZBattlefield)
	}
	for _, name := range board2 {
		miscMoveByName(t, e, 2, name, state.ZBattlefield)
	}
	return e, cfg
}

// TestCouncilsJudgmentBotDoesNotVoteItsOwnPermanent: seat 1 owns the
// highest-worth permanent on the ballot (Ancient Brontodon, 9 power); seat 2
// owns a lower-worth one (Grizzly Bears, 2 power). Voter 1's own Brontodon
// and seat 2's Bear are BOTH on the ballot. The round-2 policy would exile
// seat 1's own Brontodon (the global highest); the fixed policy votes the
// foreign Bear, and the caster (seat 0) and seat 2 — whose own Bear is the
// lower one — still vote the best FOREIGN permanent, the Brontodon.
func TestCouncilsJudgmentBotDoesNotVoteItsOwnPermanent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := voteThreeSeatEngine(t, reg,
		[]string{"Council's Judgment"},
		[]string{"Ancient Brontodon"},
		[]string{"Grizzly Bears"})
	addMana(t, e, 0, "CWW")
	judgment := miscHandObj(t, e, 0, "Council's Judgment")
	brontodon := miscBoardObj(t, e, 1, "Ancient Brontodon")
	bear := miscBoardObj(t, e, 2, "Grizzly Bears")
	// Precondition: both ballot subjects really are on the battlefield, under
	// the seats the assertions below name — the defect (voting one's own
	// card) needs an owned option and a foreign option for the SAME voter.
	if brontodon == 0 || bear == 0 {
		t.Fatal("the ballot creatures are not both on the battlefield")
	}
	if o := e.G.Obj(brontodon); o == nil || o.Controller != 1 {
		t.Fatalf("brontodon controller = %+v, want seat 1 (voter 1's own)", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Controller != 2 {
		t.Fatalf("bear controller = %+v, want seat 2 (voter 1's opponent)", o)
	}
	submitChoices(t, e, miscCastOption(t, e, judgment))
	miscPass(t, e) // seat 0's follow-up priority

	bot := newTestBot(11)
	type vote struct {
		voter      state.PlayerID
		choseBear  bool
		choseOther bool
		ownOffered bool
	}
	var votes []vote
	for len(e.G.Stack) > 0 {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while draining Council's Judgment")
		}
		if d.Kind != decision.KChoose {
			passUntilStackEmpty(t, e, 1)
			continue
		}
		if d.ResumeKind != "vote" {
			t.Fatalf("KChoose with ResumeKind %q, want the vote ask", d.ResumeKind)
		}
		var bearOpt, brontOpt = -1, -1
		own := false
		for _, o := range d.Options {
			switch o.Obj {
			case bear:
				bearOpt = o.Index
				if o.Player != 2 {
					t.Fatalf("bear option player = %d, want 2 (its controller)", o.Player)
				}
			case brontodon:
				brontOpt = o.Index
				if o.Player != 1 {
					t.Fatalf("brontodon option player = %d, want 1 (its controller)", o.Player)
				}
				if d.Player == 1 {
					own = true
				}
			}
		}
		if bearOpt < 0 || brontOpt < 0 {
			t.Fatalf("vote ask options = %+v, want both the bear and the brontodon", d.Options)
		}
		if d.Player == 1 && !own {
			t.Fatal("voter 1's own brontodon is not on the ballot — the defect under test cannot arise")
		}
		in := bot.answer(e, d)
		if err := d.Validate(in); err != nil {
			t.Fatalf("bot vote answer rejected by Decision.Validate (livelock risk): %v", err)
		}
		if len(in.Choices) != 1 {
			t.Fatalf("bot answered %v, want one choice", in.Choices)
		}
		chose := d.Options[in.Choices[0]]
		votes = append(votes, vote{
			voter:      d.Player,
			choseBear:  chose.Obj == bear,
			choseOther: chose.Obj != bear && chose.Obj != brontodon,
			ownOffered: own,
		})
		if err := e.Submit(in); err != nil {
			t.Fatalf("submit bot vote: %v", err)
		}
	}
	if len(votes) != 3 {
		t.Fatalf("Council's Judgment posed %d VoteCard asks, want one per voter (3)", len(votes))
	}
	// The defect, asserted directly: voter 1 was offered its OWN brontodon
	// (the ballot's highest worth) and must not vote for it — it votes seat
	// 2's foreign bear instead.
	var v1 *vote
	for i := range votes {
		if votes[i].voter == 1 {
			v1 = &votes[i]
		}
	}
	if v1 == nil {
		t.Fatal("no vote recorded for seat 1")
	}
	if !v1.ownOffered {
		t.Fatal("voter 1's own brontodon was not among its offered options — the assertion is vacuous")
	}
	if !v1.choseBear {
		t.Fatalf("voter 1 voted %+v, want the foreign Grizzly Bears, never its own Ancient Brontodon", votes)
	}
	// The other two voters still name the best FOREIGN permanent, so the
	// policy is not simply "avoid the caster's side": seat 0 (whose own are
	// excluded by VoteCard$) and seat 2 both vote the 9-power Brontodon.
	for _, v := range votes {
		if v.voter == 0 || v.voter == 2 {
			if v.choseBear || v.choseOther {
				t.Fatalf("voter %d's choice = %+v, want the higher-worth foreign brontodon", v.voter, v)
			}
		}
	}
	// The tally: Brontodon 2 (seats 0 and 2), Bear 1 (seat 1), so exile hits
	// the Brontodon — the same permanent the round-2 unanimous vote exiled,
	// which is why this defect is observable in the VOTES, not the outcome.
	if o := e.G.Obj(brontodon); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the brontodon = %+v, want exile (2 of 3 votes)", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the bear = %+v, want it untouched on the battlefield", o)
	}
	bearVotes, brontVotes := 0, 0
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note {
			continue
		}
		switch ev.Text {
		case "votes for Ancient Brontodon":
			brontVotes++
		case "votes for Grizzly Bears":
			bearVotes++
		}
	}
	if brontVotes != 2 || bearVotes != 1 {
		t.Fatalf("%d notes for the brontodon and %d for the bear, want 2 and 1", brontVotes, bearVotes)
	}
	replayCheck(t, e, cfg)
}

// TestVoteCardOptionCarriesSubjectController is the wire-level unit pin for
// the same fix: the VoteCard option must carry the subject's controller so a
// rules-ignorant client (the bot included) can tell its own permanent from a
// foreign one. Without this stamp the policy arm cannot prefer a foreign
// permanent, whatever it computes. It runs the real corpus card through the
// same production bot path as the test above and inspects the offered
// options' Player fields.
func TestVoteCardOptionCarriesSubjectController(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := voteThreeSeatEngine(t, reg,
		[]string{"Council's Judgment"},
		[]string{"Ancient Brontodon"},
		[]string{"Grizzly Bears"})
	addMana(t, e, 0, "CWW")
	judgment := miscHandObj(t, e, 0, "Council's Judgment")
	brontodon := miscBoardObj(t, e, 1, "Ancient Brontodon")
	bear := miscBoardObj(t, e, 2, "Grizzly Bears")
	submitChoices(t, e, miscCastOption(t, e, judgment))
	// Drain priority until the first private vote ask (the caster's, seat 0).
	var d *decision.Decision
	for len(e.G.Stack) > 0 {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision while draining Council's Judgment")
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "vote" {
			break
		}
		passUntilStackEmpty(t, e, 1)
		d = nil
	}
	if d == nil {
		t.Fatalf("pending = %+v, want the first vote ask", e.Pending())
	}
	if d.Player != 0 {
		t.Fatalf("first vote ask is for seat %d, want the caster seat 0", d.Player)
	}
	// The first ask is the caster's (seat 0); its own permanents are excluded
	// by VoteCard$, so both subjects are foreign to it and each option must
	// still name its own controller.
	if got := len(d.Options); got != 2 {
		t.Fatalf("first vote ask offered %d options, want 2", got)
	}
	controllers := map[state.ObjID]state.PlayerID{}
	for _, o := range d.Options {
		if o.Kind != "vote_card" {
			t.Fatalf("option %+v is not a vote_card", o)
		}
		controllers[o.Obj] = o.Player
	}
	if controllers[bear] != 2 {
		t.Fatalf("bear option player = %d, want 2", controllers[bear])
	}
	if controllers[brontodon] != 1 {
		t.Fatalf("brontodon option player = %d, want 1", controllers[brontodon])
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatalf("a legal answer to the vote ask was rejected: %v", err)
	}
}
