package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Squadron Hawk is the corpus card the `Card.named<Name>` predicate work was
// filed from (the empty-choose-softlock escalation's live soft-lock): its
// ETB trigger is `ChangeType$ Card.namedSquadron Hawk | ChangeNum$ 3`, and
// before the named predicate existed the search was always a fail-to-find.
// Every card here is the real compiled corpus script loaded through the
// gitignored corpus registry -- no Forge script text is committed.

// hawkSeats seeds a game whose seat-0 zones hold exactly nHawks in hand and
// nLib in library, moving real cards with logged MoveZone events (the same
// logged-mutation shape searchMoveByName uses).
func hawkSeats(t *testing.T, nHand, nLib int) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	deck := make([]*cards.Card, 0, 40)
	for i := 0; i < nHand+nLib; i++ {
		deck = append(deck, searchCorpusCard(t, reg, "Squadron Hawk"))
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, searchCorpusCard(t, reg, "Mountain"))
	}
	for len(deck) < 40 {
		deck = append(deck, searchCorpusCard(t, reg, "Grizzly Bears"))
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = searchCorpusCard(t, reg, "Mountain")
	}
	cfg := Config{Seed: 9202, Names: []string{"searcher", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	var hand, lib []state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if hawkNamed(e, id) {
			hand = append(hand, id)
		}
	}
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if hawkNamed(e, id) {
			lib = append(lib, id)
		}
	}
	move := func(id state.ObjID, from, to state.Zone) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
		e.pending = nil
		e.priorityRound()
	}
	for len(hand) > nHand {
		id := hand[len(hand)-1]
		hand = hand[:len(hand)-1]
		move(id, state.ZHand, state.ZLibrary)
		lib = append(lib, id)
	}
	for len(hand) < nHand && len(lib) > 0 {
		id := lib[len(lib)-1]
		lib = lib[:len(lib)-1]
		move(id, state.ZLibrary, state.ZHand)
		hand = append(hand, id)
	}
	if len(hand) != nHand || len(lib) != nLib {
		t.Fatalf("could not seat %d hand / %d library hawks (got %d/%d)", nHand, nLib, len(hand), len(lib))
	}
	return e, cfg, hand[0]
}

func hawkNamed(e *Engine, id state.ObjID) bool {
	o := e.G.Obj(id)
	return o != nil && o.Face() != nil && o.Face().Name == "Squadron Hawk"
}

func hawkIn(e *Engine, z state.Zone) int {
	n := 0
	for _, id := range e.G.Zone(z, 0) {
		if hawkNamed(e, id) {
			n++
		}
	}
	return n
}

// hawkCastAndAccept casts the hand Hawk (1 W paid) and answers the optional
// ETB trigger's CR 603.5 yes/no with yes, returning the search decision the
// effect suspends on.
func hawkCastAndAccept(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	addMana(t, e, 0, "WC")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KTriggerOptional || d.Player != 0 {
		t.Fatalf("pending = %+v, want seat 0's optional-trigger ask", d)
	}
	submitChoices(t, e, 0) // yes — apply the trigger's effect
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("pending = %+v, want the library search KChoose", d)
	}
	return d
}

// TestSquadronHawkSearchFindsUpToThreeNamed is the brief's headline: cast the
// Hawk, accept the optional ETB trigger, and the search offers the library's
// Hawks (up to ChangeNum 3); taking all of them moves exactly those cards to
// the hand and shuffles once.
func TestSquadronHawkSearchFindsUpToThreeNamed(t *testing.T) {
	t.Parallel()
	e, cfg, id := hawkSeats(t, 1, 3)
	d := hawkCastAndAccept(t, e, id)
	if d.Min != 0 || d.Max != 3 || len(d.Options) != 3 {
		t.Fatalf("Min/Max/options = %d/%d/%d, want 0/3/3", d.Min, d.Max, len(d.Options))
	}
	for _, o := range d.Options {
		if o.Label != "Squadron Hawk" {
			t.Fatalf("option label %q, want the printed name", o.Label)
		}
	}
	// The choice is shown to the searcher only: the opponent's projection
	// carries no decision at all (a hidden library's contents are private,
	// CR 701.23's redaction rule).
	if opp := view.Project(e.G, e, 1, d); opp.Decision != nil {
		t.Fatalf("opponent received the hidden search decision: %+v", opp.Decision)
	}
	start := len(e.L.Events)
	submitChoices(t, e, 0, 1, 2)
	// ShuffleNonMandatory$: the fetch's may-shuffle confirm (searchmay1) —
	// accept it, so the pinned "shuffles once" below still counts the shuffle
	// this test was written around.
	msYes, _ := mayShuffleConfirm(t, e, 0)
	submitChoices(t, e, msYes)
	if got := hawkIn(e, state.ZHand); got != 3 {
		t.Fatalf("hand hawks after search = %d, want 3 (the found ones)", got)
	}
	if bf := hawkIn(e, state.ZBattlefield); bf != 1 {
		t.Fatalf("battlefield hawks = %d, want 1 (the cast one)", bf)
	}
	if got := hawkIn(e, state.ZLibrary); got != 0 {
		t.Fatalf("library hawks after search = %d, want 0", got)
	}
	shuffles := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if shuffles != 1 {
		t.Fatalf("seat 0 shuffles after the answer = %d, want 1", shuffles)
	}
	replayCheck(t, e, cfg)
}

// TestSquadronHawkSearchWithNoHawksResolvesSilently is the fail-to-find side:
// the only Hawk is the one in play, so the search has no selectable cards.
// It must shuffle and complete directly -- publishing an empty KChoose would
// suspend a live host until somebody submitted a meaningless empty answer.
func TestSquadronHawkSearchWithNoHawksResolvesSilently(t *testing.T) {
	t.Parallel()
	e, cfg, id := hawkSeats(t, 1, 0)
	addMana(t, e, 0, "WC")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KTriggerOptional || d.Player != 0 {
		t.Fatalf("pending = %+v, want seat 0's optional-trigger ask", d)
	}
	start := len(e.L.Events)
	submitChoices(t, e, 0) // accept the trigger; the empty search is direct
	if e.G.Over {
		t.Fatalf("game wedged: over=%v", e.G.Over)
	}
	// searchmay1: the fail-to-find shape now poses the may-shuffle confirm --
	// the search still owes its mandatory shuffle (CR 701.23b), and the
	// confirm is what may spare it. Accept it, so the pinned "shuffles once"
	// below still counts the shuffle this test was written around.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind == "search" {
		t.Fatalf("empty library search published a search KChoose: %+v", d)
	}
	msYes, _ := mayShuffleConfirm(t, e, 0)
	submitChoices(t, e, msYes)
	moves, shuffles := 0, 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary {
			moves++
		}
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if moves != 0 {
		t.Fatalf("fail-to-find emitted %d library moves, want 0", moves)
	}
	if shuffles != 1 {
		t.Fatalf("fail-to-find shuffles = %d, want 1", shuffles)
	}
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}

// TestSquadronHawkTwoHawksInLibrary offers exactly two: ChangeNum 3 clamps to
// the cards actually present, and the answer order (not the offer order) is
// what the moves record.
func TestSquadronHawkTwoHawksInLibrary(t *testing.T) {
	t.Parallel()
	e, cfg, id := hawkSeats(t, 1, 2)
	d := hawkCastAndAccept(t, e, id)
	if d.Max != 2 || len(d.Options) != 2 {
		t.Fatalf("Max/options = %d/%d, want 2/2", d.Max, len(d.Options))
	}
	start := len(e.L.Events)
	submitChoices(t, e, d.Options[1].Index, d.Options[0].Index)
	// ShuffleNonMandatory$: the fetch's may-shuffle confirm (searchmay1) —
	// decline it, so the pinned move-order assertion below is unchanged and
	// this pin also holds the decline branch on a second corpus shape.
	_, msNo := mayShuffleConfirm(t, e, 0)
	submitChoices(t, e, msNo)
	want := []state.ObjID{d.Options[1].Obj, d.Options[0].Obj}
	var moved []state.ObjID
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZHand {
			moved = append(moved, ev.Obj)
		}
	}
	if !slices.Equal(moved, want) {
		t.Fatalf("move order = %v, want the answer's order %v", moved, want)
	}
	replayCheck(t, e, cfg)
}
