package rules

// vote_card1 (round 2): the card-ballot bot vote must reach the ballot
// facts in a REAL match. Round 1 gave botpolicy.Decide a vote_card arm that
// votes for the highest-worth offered permanent, but both Board adapters
// (botpolicy.BoardFromGameInto, seat's boardFromView) filled b.Cards from
// the deciding seat's own zones alone — a Council's Judgment ballot offers
// only permanents the CASTER does not control, so every entry read as a
// zero Card and the strict `>` comparison always retained option 0: the
// bot voted the ballot's first entry in every real match while the round-1
// unit test injected the facts by hand and hid exactly that. The adapter
// widening is under test in seat/integration_test.go's agreement pins; this
// file pins the behaviour end to end on the real corpus card, answered
// through the production bot path (testBot.answer = botpolicy.Decide over
// botpolicy.BoardFromGame, the same Board host/match.go builds a bot seat).

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCouncilsJudgmentBotVotesTheBestBallotPermanent: seat 1 controls two
// creatures of different worth (Grizzly Bears 2/2, Ancient Brontodon 9/9),
// the ballot carries both with the CHEAPER bear first (battlefield zone
// order), and both voters answer through the production bot path — so the
// higher-worth, non-first Brontodon must be the unanimous vote and the exile
// must hit it, not the first ballot entry.
func TestCouncilsJudgmentBotVotesTheBestBallotPermanent(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Council's Judgment"}, nil,
		nil, []string{"Grizzly Bears", "Ancient Brontodon"})
	addMana(t, e, 0, "CWW")
	judgment := miscHandObj(t, e, 0, "Council's Judgment")
	bear := miscBoardObj(t, e, 1, "Grizzly Bears")
	brontodon := miscBoardObj(t, e, 1, "Ancient Brontodon")
	if bear == 0 || brontodon == 0 {
		t.Fatal("seat 1's ballot creatures are not both on the battlefield")
	}
	// Precondition: the ballot order really is [bear, brontodon] — the
	// battlefield zone order the VoteCard$ walk builds — so "the bot must
	// not take the first option" is a claim about a real choice, and the
	// board the production path builds really carries the worth facts the
	// policy prices (Creature + printed Power; 9 > 2 is what makes the
	// non-first entry the best one).
	submitChoices(t, e, miscCastOption(t, e, judgment))
	miscPass(t, e) // seat 0's follow-up priority

	bot := newTestBot(11)
	voteAsks := 0
	for len(e.G.Stack) > 0 {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while draining Council's Judgment")
		}
		if d.Kind != decision.KChoose {
			passUntilStackEmpty(t, e, 1)
			continue
		}
		if d.ResumeKind != "vote" || len(d.Options) != 2 ||
			d.Options[0].Kind != "vote_card" || d.Options[0].Obj != bear ||
			d.Options[1].Obj != brontodon {
			t.Fatalf("vote ask = %+v, want the 2-entry ballot [bear, brontodon]", d.Options)
		}
		if voteAsks == 0 {
			brd := botpolicy.BoardFromGame(e.G, e, d.Player)
			bf, bok := brd.Cards[bear]
			tf, tok := brd.Cards[brontodon]
			if !bok || !bf.Creature || bf.Power != 2 {
				t.Fatalf("bot board facts for the bear = %+v (present %v), want a 2-power creature", bf, bok)
			}
			if !tok || !tf.Creature || tf.Power != 9 {
				t.Fatalf("bot board facts for the brontodon = %+v (present %v), want a 9-power creature", tf, tok)
			}
		}
		voteAsks++
		in := bot.answer(e, d)
		if err := d.Validate(in); err != nil {
			t.Fatalf("bot vote answer rejected by Decision.Validate (livelock risk): %v", err)
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("submit bot vote: %v", err)
		}
	}
	if voteAsks != 2 {
		t.Fatalf("Council's Judgment posed %d VoteCard asks, want one per voter (2)", voteAsks)
	}

	// The unanimous vote named the HIGHER-WORTH, NON-FIRST Brontodon, so the
	// exile sub-ability hit it and the first ballot entry survives.
	if o := e.G.Obj(brontodon); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the brontodon (worth 9 power, ballot entry 1) = %+v, want exile", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the bear (worth 2 power, ballot entry 0) = %+v, want it untouched on the battlefield", o)
	}
	votes, bearVotes := 0, 0
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note {
			continue
		}
		switch ev.Text {
		case "votes for Ancient Brontodon":
			votes++
		case "votes for Grizzly Bears":
			bearVotes++
		}
	}
	if votes != 2 || bearVotes != 0 {
		t.Fatalf("%d \"votes for Ancient Brontodon\" and %d \"votes for Grizzly Bears\" notes, want both voters on the brontodon", votes, bearVotes)
	}
	replayCheck(t, e, cfg)
}
