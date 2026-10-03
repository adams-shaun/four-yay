package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPathOfTheAnimistSearchThenVoteResolvesThePlaneswalkOutcome is the
// real-corpus end-to-end pin the planechase-verbs brief names: Path of the
// Animist is the "Will of the Planeswalkers" carrier whose front half is a
// real hidden-library search (SP$ ChangeZone | Origin$ Library |
// ChangeType$ Land.Basic | ChangeNum$ 2 | Tapped$ True | SubAbility$
// DBSpace) that CHAINS through DBSpace (DB$ BlankLine) into DBVote
// (DB$ Vote | Choices$ DBPlaneswalk,DBChaos | VoteTiedAbility$ DBChaos),
// whose winning outcome DBPlaneswalk is DB$ Planeswalk. It exercises the
// BlankLine spacer on the card whose compiled SA carries it, together with
// the search the Ghosthunter pin does not have: the spacer must be silent,
// the vote must record its Notes, and the planeswalk outcome must resolve
// as the documented no-planar-deck degrade -- never the generic
// "unimplemented API" Note.
//
// The card is in no repo deck and no legacy golden deck, so no chain head
// depends on it; replayCheck certifies the whole chain replays
// byte-identically.
func TestPathOfTheAnimistSearchThenVoteResolvesThePlaneswalkOutcome(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	card := searchCorpusCard(t, reg, "Path of the Animist")
	var voteBody *string
	for _, f := range card.Faces {
		if body, ok := f.SVars["DBVote"]; ok {
			b := body
			voteBody = &b
		}
	}
	if voteBody == nil || !strings.Contains(*voteBody, "DB$ Vote") ||
		!strings.Contains(*voteBody, "VoteTiedAbility$ DBChaos") {
		t.Fatalf("Path of the Animist DBVote = %v, want the Will-of-the-Planeswalkers ballot", voteBody)
	}
	sup := effects.Supported()
	if !sup["api:BlankLine"] || !sup["api:Planeswalk"] || !sup["api:ChaosEnsues"] {
		t.Fatal("the planechase verbs are not all registered in effects.Supported()")
	}

	e, cfg := animistTestEngine(t, reg)
	id := searchMoveByName(t, e, "Path of the Animist", state.ZHand)
	addMana(t, e, 0, "CCCG")
	d := castFixture(t, e, id, -1)

	// The front half asks first: the hidden-library search, up to two
	// basics (stated quality, so Min 0 per CR 701.23b), one option per
	// library basic land.
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("after casting Path of the Animist: %+v, want the library search KChoose", d)
	}
	if d.Max != 2 {
		t.Fatalf("search ask max = %d, want 2 (ChangeNum$ 2)", d.Max)
	}
	basics := basicLibraryIDs(e)
	if len(d.Options) != len(basics) {
		t.Fatalf("search ask has %d options, want one per library basic land (%d)",
			len(d.Options), len(basics))
	}

	// Answer with two non-adjacent basics so honouring the answer is
	// distinguishable from a deterministic first-two pick.
	wantSearched := []state.ObjID{d.Options[0].Obj, d.Options[2].Obj}
	submitChoices(t, e, d.Options[0].Index, d.Options[2].Index)
	// The vote now poses a real per-voter ask (task vote_card_self1) where
	// the pre-ask engine silently took the ballot's first option; the drain
	// answers every voter option 0 (planeswalk), preserving the outcome this
	// test asserts.
	passUntilStackEmpty(t, e, 40)

	// The two answered lands entered tapped; the shuffle happened.
	for _, id2 := range wantSearched {
		o := e.G.Obj(id2)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("searched land %d not on the battlefield: %+v", id2, o)
		}
		if !o.Tapped {
			t.Fatalf("searched land %d (%s) entered untapped, want Tapped$ True", id2, objName(t, e, id2))
		}
	}
	shuffled := false
	for _, ev := range e.L.Events {
		shuffled = shuffled || (ev.Kind == events.Shuffle && ev.Player == 0)
	}
	if !shuffled {
		t.Fatal("the post-search shuffle never happened")
	}

	// The BlankLine spacer was silent, the vote recorded one Note per
	// voting player, and the winning planeswalk outcome resolved exactly
	// once -- with no "unimplemented API" Note anywhere in the chain.
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

// animistTestEngine deals seat 0 a 40-card deck whose first four cards are
// Path of the Animist, Rockfall Vale, a Swamp and an Island, followed by
// eight Forests and eight Mountains (the search pool) and Grizzly Bears; the
// opponent gets 40 Mountains. Compiled corpus cards only, so no Forge script
// text is committed.
func animistTestEngine(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{
		searchCorpusCard(t, reg, "Path of the Animist"),
		searchCorpusCard(t, reg, "Rockfall Vale"),
		searchCorpusCard(t, reg, "Swamp"),
		searchCorpusCard(t, reg, "Island"),
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9204, Names: []string{"animist", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}
