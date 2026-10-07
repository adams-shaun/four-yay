package rules

// vote-tally-resume: api:Vote's per-subject tally (Ctx.VoteCounts) was lost
// when an AmountFromVotes$ RepeatEach iteration suspended on a downstream
// mid-resolution ask. Círdan the Shipwright is the live corpus carrier: its
// DBRepeatPut walks every player, and a player who received no votes may put
// a permanent card from their hand onto the battlefield -- a hidden-hand
// ChangeZone that poses a real KChoose when the player holds more than one
// eligible permanent. Answering it suspends the loop; the resumed body and
// every remaining iteration then re-derive their per-player "Votes" value,
// which the pre-fix engine read off the (now-empty) fresh Ctx as 0.
//
// This test drives the real compiled card through the real vote, a real
// hidden-hand choice answered with a NON-FIRST option, and asserts both
// halves of the outcome: a later player who received votes must NOT take the
// zero-vote branch, while a later zero-vote player still must.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cirdanCorpusCard resolves a corpus card by an ASCII-folded name FRAGMENT
// (lower-case ASCII letters only). The one card this test names with a
// diacritic (Círdán) may be encoded NFC or NFD in the corpus, and the
// registry's NormalizeName does not fold accents, so a byte-identical literal
// cannot be relied on. Folding both sides to ASCII removes the ambiguity
// without depending on the corpus's exact bytes. The fragment must identify
// exactly one card, so a silent multi-match cannot select the wrong card.
func cirdanCorpusCard(t *testing.T, reg *cards.Registry, fragment string) *cards.Card {
	t.Helper()
	fold := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(s) {
			if r < 128 {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	var found *cards.Card
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if strings.Contains(fold(f.Name), fragment) {
				if found != nil && found != c {
					t.Fatalf("corpus fragment %q matches both %q and %q", fragment, found.Faces[0].Name, f.Name)
				}
				found = c
			}
		}
	}
	if found == nil {
		t.Fatalf("missing corpus card matching %q", fragment)
	}
	return found
}

// cirdanMove moves card c owned by seat p from hand or library to zone `to`
// with a logged MoveZone. It deliberately leaves the trigger queue intact (no
// priorityRound) so the caller owns trigger draining, and clears any stale
// priority decision first. Matching is by the compiled card pointer, not by
// name, so the corpus name's exact encoding cannot matter.
func cirdanMove(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o != nil && o.Card == c {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("corpus card %q absent from seat %d hand/library", c.Faces[0].Name, p)
	return 0
}

// cirdanBattlefieldId returns the id of card c on seat p's battlefield, or 0.
func cirdanBattlefieldId(e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Card == c {
			return id
		}
	}
	return 0
}

// cirdanInHand reports whether card c is in seat p's hand.
func cirdanInHand(e *Engine, p state.PlayerID, c *cards.Card) bool {
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Card == c {
			return true
		}
	}
	return false
}

// cirdanZoneHas reports whether object id is in zone z for seat p.
func cirdanZoneHas(e *Engine, p state.PlayerID, z state.Zone, id state.ObjID) bool {
	for _, got := range e.G.Zone(z, p) {
		if got == id {
			return true
		}
	}
	return false
}

// cirdanHandId returns the object id of card c in seat p's hand, fatal when
// absent.
func cirdanHandId(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Card == c {
			return id
		}
	}
	t.Fatalf("corpus card %q not in seat %d hand", c.Faces[0].Name, p)
	return 0
}

// cirdanFixture holds the compiled cards a scenario needs, so the test can
// identify game objects by pointer rather than by name.
type cirdanFixture struct {
	cirdan    *cards.Card
	brontodon *cards.Card
	bears     *cards.Card
	runeclaw  *cards.Card
}

// cirdanEngine builds a four-seat table. Seat 0 holds Círdan plus TWO
// distinct permanents in hand (so its zero-vote DBChangeZone must ask); seat
// 1 holds one permanent (the positively voted decoy) and draws four cards
// for its four votes; seats 2 and 3 each hold one permanent (a later
// zero-vote player each). Círdán is moved onto seat 0's battlefield last, so
// its ChangesZone trigger is the sole queued trigger.
func cirdanEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, cirdanFixture) {
	t.Helper()
	fx := cirdanFixture{
		cirdan:    cirdanCorpusCard(t, reg, "shipwright"),
		brontodon: searchCorpusCard(t, reg, "Ancient Brontodon"),
		bears:     searchCorpusCard(t, reg, "Grizzly Bears"),
		runeclaw:  searchCorpusCard(t, reg, "Runeclaw Bear"),
	}
	mountain := searchCorpusCard(t, reg, "Mountain")
	makeDeck := func(first ...*cards.Card) []*cards.Card {
		deck := append([]*cards.Card(nil), first...)
		for len(deck) < 40 {
			deck = append(deck, mountain)
		}
		return deck
	}
	cfg := seatZeroStart(Config{Seed: 4413, Names: []string{"a", "b", "c", "d"},
		Decks: [][]*cards.Card{
			makeDeck(fx.cirdan, fx.brontodon, fx.bears),
			makeDeck(fx.brontodon), makeDeck(fx.bears), makeDeck(fx.runeclaw),
		},
		Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	// Seat 0: two eligible permanents, so DBChangeZone offers a real pick.
	cirdanMove(t, e, 0, fx.brontodon, state.ZHand)
	cirdanMove(t, e, 0, fx.bears, state.ZHand)
	// Seat 1: one permanent that must STAY in hand (four votes received).
	cirdanMove(t, e, 1, fx.brontodon, state.ZHand)
	// Seats 2 and 3: one permanent each, the later zero-vote branches.
	cirdanMove(t, e, 2, fx.bears, state.ZHand)
	cirdanMove(t, e, 3, fx.runeclaw, state.ZHand)
	// Círdán enters: the ChangesZone trigger queues.
	cirdanMove(t, e, 0, fx.cirdan, state.ZBattlefield)
	return e, cfg, fx
}

// TestCirdanVoteTallySurvivesChangeZoneSuspension is the end-to-end pin for
// the tally riding an AmountFromVotes$ loop's suspension.
func TestCirdanVoteTallySurvivesChangeZoneSuspension(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, fx := cirdanEngine(t, reg)

	// Precondition: Círdán really entered and queued exactly one trigger; a
	// silently-inert ETB would make every later assertion vacuous.
	if got := cirdanBattlefieldId(e, 0, fx.cirdan); got == 0 {
		t.Fatal("precondition: Círdán is not on seat 0's battlefield")
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("precondition: Círdán ETB queued %d triggers, want 1", len(e.pendingTriggers))
	}

	hand0, hand1 := len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZHand, 1))
	hand2, hand3 := len(e.G.Zone(state.ZHand, 2)), len(e.G.Zone(state.ZHand, 3))
	// Precondition: seat 0 has its two eligible permanents in hand, or the
	// hidden-hand ask this test is built on cannot be posed.
	if !cirdanInHand(e, 0, fx.brontodon) || !cirdanInHand(e, 0, fx.bears) {
		t.Fatal("precondition: seat 0 lacks its two eligible permanents in hand")
	}

	// Drain the ETB trigger. The vote's four KChoose asks are posed one at a
	// time, synchronously, in voter order from the caster: 0, 1, 2, 3.
	e.putTriggersOnStack()
	e.resolveTop()

	wantVoters := []state.PlayerID{0, 1, 2, 3}
	for i, voter := range wantVoters {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "vote" {
			t.Fatalf("vote %d: pending = %+v, want a vote KChoose", i, d)
		}
		if d.Player != voter {
			t.Fatalf("vote ask %d went to seat %d, want voter %d", i, d.Player, voter)
		}
		// Every voter casts for seat 1: option index 1 of the [0,1,2,3]
		// ballot universe (VotePlayer$ Player offers the voter their own seat
		// too). Seat 1 then receives all four votes; seats 0, 2 and 3 none.
		submitChoices(t, e, 1)
	}

	// The ballot completed; the draw-per-vote loop and then DBRepeatPut run
	// inside the same Submit. DBRepeatPut walks every player in seat order
	// (0,1,2,3); each zero-vote player poses a hidden-hand KChoose because the
	// opening hand's Mountains are permanents too. Seat 0's ask IS the
	// suspension this test exists for; seats 2 and 3 ask as well, so driving
	// the whole sequence also proves the repeat cursor advanced past the
	// suspension.
	type cirdanAsk struct {
		player state.PlayerID
		chosen decision.Option
		other  decision.Option
	}
	var asks []cirdanAsk
	seen := map[state.PlayerID]bool{}
	for {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		if d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
			t.Fatalf("unexpected mid-resolution ask while DBRepeatPut ran: %+v", d)
		}
		if seen[d.Player] {
			// The card's trailing SubAbility$ DBChangeZone runs once more after
			// the RepeatEach (DefinedPlayer$ Remembered resolves to the
			// controlling seat). It is a SECOND, optional offer -- already
			// covered by the loop iteration -- so decline it (Min 0 allows the
			// empty answer) to keep the hand accounting below exact.
			if d.Min != 0 {
				t.Fatalf("seat %d's repeat offer has range %d..%d, cannot decline", d.Player, d.Min, d.Max)
			}
			submitChoices(t, e)
			continue
		}
		seen[d.Player] = true
		if d.Min != 0 || d.Max != 1 {
			t.Fatalf("seat %d hidden-hand ask range = %d..%d, want 0..1 (Círdán's 'may put' is optional)",
				d.Player, d.Min, d.Max)
		}
		// The player this ask belongs to must be a zero-vote player: with the
		// ballot above, only seat 1 received votes, so an ask to seat 1 is
		// exactly the defect this test pins.
		var want *cards.Card
		switch d.Player {
		case 0:
			want = fx.brontodon
		case 2:
			want = fx.bears
		case 3:
			want = fx.runeclaw
		default:
			t.Fatalf("seat %d received votes from the ballot but was offered Círdán's zero-vote branch", d.Player)
		}
		var opts []decision.Option
		for _, o := range d.Options {
			if o.Kind == "hand_move" {
				opts = append(opts, o)
			}
		}
		if len(opts) < 2 {
			t.Fatalf("seat %d's hidden-hand ask offered %d options, want its whole eligible permanents pool",
				d.Player, len(opts))
		}
		wantID := cirdanHandId(t, e, d.Player, want)
		chosen := decision.Option{Index: -1}
		for _, o := range opts {
			if o.Obj == wantID {
				chosen = o
			}
		}
		if chosen.Index < 0 {
			t.Fatalf("seat %d's hidden-hand ask did not offer %q (obj %d): %+v",
				d.Player, want.Faces[0].Name, wantID, d.Options)
		}
		asks = append(asks, cirdanAsk{player: d.Player, chosen: chosen, other: opts[0]})
		submitChoices(t, e, chosen.Index)
	}

	// The first ask must be seat 0 (the earliest zero-vote player), and a
	// NON-FIRST option must have been the answer -- a deterministic/no-choice
	// path could never move the Brontodon, so this is what proves a real
	// suspension happened.
	if len(asks) == 0 {
		t.Fatal("DBRepeatPut posed no hidden-hand ask at all")
	}
	if asks[0].player != 0 {
		t.Fatalf("the first zero-vote ChangeZone ask went to seat %d, want seat 0", asks[0].player)
	}
	if asks[0].chosen.Index == 0 {
		t.Fatal("seat 0's Brontodon was the first option; the answer must be non-first to prove a suspension")
	}
	// Every zero-vote player (0, 2, 3) asked; seat 1, which received votes,
	// never did (the default arm above fails loudly if it had).
	for _, p := range []state.PlayerID{0, 2, 3} {
		if !seen[p] {
			t.Fatalf("zero-vote seat %d was never offered its zero-vote branch", p)
		}
	}
	if seen[1] {
		t.Fatal("seat 1 received four votes but was offered the zero-vote branch")
	}

	// The vote distribution precondition, observed through its consequences:
	// seat 1 draws exactly four cards (one per vote received); seats 0, 2 and
	// 3 draw none. A non-degenerate ballot is what makes the branch
	// assertions below discriminating.
	if got := len(e.G.Zone(state.ZHand, 1)); got != hand1+4 {
		t.Fatalf("seat 1 hand = %d, want %d (four draws for four votes)", got, hand1+4)
	}
	if got := len(e.G.Zone(state.ZHand, 2)); got != hand2-1 {
		t.Fatalf("seat 2 hand = %d, want %d (zero draws, one permanent put)", got, hand2-1)
	}
	if got := len(e.G.Zone(state.ZHand, 3)); got != hand3-1 {
		t.Fatalf("seat 3 hand = %d, want %d (zero draws, one permanent put)", got, hand3-1)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0-1 {
		t.Fatalf("seat 0 hand = %d, want %d (zero draws, one permanent put)", got, hand0-1)
	}

	// The answered NON-FIRST permanent moved (the Ancient Brontodon); the first
	// option (a Mountain) did not.
	if !cirdanZoneHas(e, 0, state.ZBattlefield, asks[0].chosen.Obj) {
		t.Fatalf("seat 0's chosen permanent %d is not on the battlefield", asks[0].chosen.Obj)
	}
	if cirdanZoneHas(e, 0, state.ZBattlefield, asks[0].other.Obj) {
		t.Fatalf("seat 0's UNCHOSEN first option %d moved to the battlefield: the answer was not honoured",
			asks[0].other.Obj)
	}
	if !cirdanZoneHas(e, 0, state.ZHand, asks[0].other.Obj) {
		t.Fatalf("seat 0's unchosen first option %d left its hand", asks[0].other.Obj)
	}

	// The positively voted later player (seat 1) must NOT take the zero-vote
	// branch: its Brontodon stays in hand and nothing entered for it.
	if !cirdanInHand(e, 1, fx.brontodon) {
		t.Fatal("seat 1 received four votes but still put a permanent onto the battlefield")
	}
	if cirdanBattlefieldId(e, 1, fx.brontodon) != 0 {
		t.Fatal("seat 1 received four votes but its Brontodon entered the battlefield")
	}

	// The later zero-vote players still take their branch: this is a tally
	// fix, not one that aborts the repeat cursor after the first suspension.
	if cirdanInHand(e, 2, fx.bears) {
		t.Fatal("seat 2 received no votes but its Grizzly Bears stayed in hand")
	}
	if cirdanBattlefieldId(e, 2, fx.bears) == 0 {
		t.Fatal("seat 2's zero-vote Grizzly Bears did not enter the battlefield")
	}
	if cirdanInHand(e, 3, fx.runeclaw) {
		t.Fatal("seat 3 received no votes but its Runeclaw Bear stayed in hand")
	}
	if cirdanBattlefieldId(e, 3, fx.runeclaw) == 0 {
		t.Fatal("seat 3's zero-vote Runeclaw Bear did not enter the battlefield")
	}

	replayCheck(t, e, cfg)
}
