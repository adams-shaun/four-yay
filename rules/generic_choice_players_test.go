package rules

// The api:GenericChoice per-Defined$-player path (ticket
// agent-20260922T234314Z-bbfff2fb). A GenericChoice whose Defined$ resolves to
// more than the resolving controller must ask EACH defined player, one at a
// time, and run that player's chosen body with the chooser bound as
// Ctx.Remembered. Seize the Spotlight is the real carrier:
//
//	A:SP$ GenericChoice | Defined$ Opponent | Choices$ Fame,Fortune | SubAbility$ DBFame
//	SVar:Fame:DB$ Pump | Defined$ Remembered | NoteCards$ Self | NoteCardsFor$ Fame
//	SVar:Fortune:DB$ Pump | Defined$ Remembered | NoteCards$ Self | NoteCardsFor$ Fortune
//	SVar:DBFame:DB$ RepeatEach | RepeatPlayers$ Player.NotedForFame | ...
//
// The older effects-side tests (notecards_notation_test.go) drive the Fame/
// Fortune bodies only after binding Remembered by hand; this file drives the
// real spell through the engine so the GenericChoice ask itself is what fills
// that binding.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// seizeBoard builds a three-seat corpus game: seat 0 holds Seize the
// Spotlight in hand with three red mana, and each opponent controls one
// Grizzly Bears. The return trip is why seat 0's own battlefield must stay
// empty of creatures the DBFame loop could steal.
func seizeBoard(t *testing.T) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	seize := mustCorpusCard(t, reg, "Seize the Spotlight")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := seatZeroStart(Config{Seed: 77, Names: []string{"a", "b", "c"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{seize}, mountainDeck(t, 39)...),
			append([]*cards.Card{bear}, mountainDeck(t, 39)...),
			append([]*cards.Card{bear}, mountainDeck(t, 39)...),
		}})
	e := New(cfg)
	e.Advance()
	seizeObj := placeInDeck(t, e, 0, seize, state.ZHand)
	bear1 := placeInDeck(t, e, 1, bear, state.ZBattlefield)
	bear2 := placeInDeck(t, e, 2, bear, state.ZBattlefield)
	// PRECONDITIONS the assertions below depend on: the spell is in seat 0's
	// hand, each opponent really has a battlefield creature to steal, and the
	// controller has none.
	if z := e.G.Obj(seizeObj).Zone; z != state.ZHand {
		t.Fatalf("Seize the Spotlight zone = %s, want Hand", z)
	}
	if z := e.G.Obj(bear1).Zone; z != state.ZBattlefield {
		t.Fatalf("opponent 1 bear zone = %s, want Battlefield", z)
	}
	if z := e.G.Obj(bear2).Zone; z != state.ZBattlefield {
		t.Fatalf("opponent 2 bear zone = %s, want Battlefield", z)
	}
	addMana(t, e, 0, "RRR")
	return e, cfg, seizeObj
}

// castSeize casts the spell from the pending priority decision and returns
// once the resolution has suspended on the first opponent's KModes ask.
func castSeize(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	submitSeizeCast(t, e, id)
	return passUntilAskKind(t, e, decision.KModes, 40)
}

// submitSeizeCast finds and submits the cast option for id from the pending
// priority decision, without waiting for any follow-on ask.
func submitSeizeCast(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority before the cast", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the Seize spell: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
}

// TestSeizeTheSpotlightAsksEachOpponentOnce is the headline regression: the
// spell must ask BOTH opponents (never the resolving controller), each ask
// carrying the same Fame/Fortune options, and each chosen body must act on
// its own chooser. Distinct answers (opponent 1 Fame, opponent 2 Fortune)
// then split the card's own DBFame/DBFortune loops.
func TestSeizeTheSpotlightAsksEachOpponentOnce(t *testing.T) {
	t.Parallel()
	e, cfg, id := seizeBoard(t)

	// Opponent 1's ask.
	d := castSeize(t, e, id)
	if d.Player != 1 {
		t.Fatalf("first GenericChoice chooser = seat %d, want opponent 1 (never the controller)", d.Player)
	}
	if d.ResumeKind != "generic_players" {
		t.Fatalf("first chooser ResumeKind = %q, want generic_players", d.ResumeKind)
	}
	if len(d.Options) != 2 || d.Options[0].Label != "Fame" || d.Options[1].Label != "Fortune" {
		t.Fatalf("first chooser options = %+v, want [Fame Fortune]", d.Options)
	}
	// The spell has left hand and is on the stack; the only hand change left
	// is DBFortune's single DBDraw.
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, 0) // opponent 1 chooses Fame

	// Opponent 2's ask, before any continuation runs.
	d = passUntilAskKind(t, e, decision.KModes, 40)
	if d.Player != 2 {
		t.Fatalf("second GenericChoice chooser = seat %d, want opponent 2", d.Player)
	}
	if d.ResumeKind != "generic_players" {
		t.Fatalf("second chooser ResumeKind = %q, want generic_players", d.ResumeKind)
	}
	submitChoices(t, e, 1) // opponent 2 chooses Fortune

	passUntilStackEmpty(t, e, 40)

	// The chooser-bound branch bodies ran on the right seats: each branch's
	// NoteCards$ Self noted the CHOOSER, never the controller.
	if got := e.G.Players[1].Notes; len(got) != 1 || got[0] != "Fame" {
		t.Fatalf("opponent 1 notes = %v, want [Fame] (its own Fame branch)", got)
	}
	if got := e.G.Players[2].Notes; len(got) != 1 || got[0] != "Fortune" {
		t.Fatalf("opponent 2 notes = %v, want [Fortune] (its own Fortune branch)", got)
	}
	if got := e.G.Players[0].Notes; len(got) != 0 {
		t.Fatalf("the controller was noted %v, want nothing (it was never a chooser)", got)
	}

	// Exactly one mode answer per chooser was recorded, and the controller
	// was never asked a mode.
	var choosers []state.PlayerID
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen {
			choosers = append(choosers, ev.Player)
		}
	}
	if len(choosers) != 2 || choosers[0] != 1 || choosers[1] != 2 {
		t.Fatalf("ModeChosen players = %v, want [1 2] (exactly once per opponent, controller absent)", choosers)
	}

	// The outer SubAbility$ (DBFame/DBFortune) ran EXACTLY once after both
	// answers: the single fortune chooser (seat 2) drew the controller one
	// card and made one Treasure. A continuation that ran per chooser, or ran
	// twice, would draw twice and mint two Treasures.
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("seat 0 hand size = %d, want %d (DBFortune's single DBDraw ran once)", got, handBefore+1)
	}
	tokens := 0
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Zone == state.ZBattlefield && o.Controller == 0 && o.Face() != nil &&
			o.Face().Name == "Treasure Token" {
			tokens++
		}
	}
	if tokens != 1 {
		t.Fatalf("Treasure tokens = %d, want exactly 1 (DBFortune ran once)", tokens)
	}

	replayCheck(t, e, cfg)
}

// TestSeizeTheSpotlightSingleOpponentAsksThatOpponent pins the two-seat
// shape: Defined$ Opponent resolves to ONE player (the sole opponent), which
// is still not the resolving controller, so the per-player path must take it
// rather than asking seat 0.
func TestSeizeTheSpotlightSingleOpponentAsksThatOpponent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	seize := mustCorpusCard(t, reg, "Seize the Spotlight")
	cfg := seatZeroStart(Config{Seed: 78, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{seize}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()
	id := placeInDeck(t, e, 0, seize, state.ZHand)
	if z := e.G.Obj(id).Zone; z != state.ZHand {
		t.Fatalf("Seize the Spotlight zone = %s, want Hand", z)
	}
	addMana(t, e, 0, "RRR")
	d := castSeize(t, e, id)
	if d.Player != 1 {
		t.Fatalf("chooser = seat %d, want the sole opponent 1 (never the controller 0)", d.Player)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[1].Notes; len(got) != 1 || got[0] != "Fame" {
		t.Fatalf("opponent 1 notes = %v, want [Fame]", got)
	}
	if got := e.G.Players[0].Notes; len(got) != 0 {
		t.Fatalf("the controller was noted %v, want nothing", got)
	}
	replayCheck(t, e, cfg)
}

// genericChoiceEmptyDefined is an inline exact script whose Defined$ resolves
// to NO players at resolution (an unbound TriggeredPlayer on a spell). The
// per-player path must not fall back to asking the controller; it must record
// a Note and do nothing.
const genericChoiceEmptyDefined = "Name:Empty Trial\nManaCost:R\nTypes:Sorcery\n" +
	"A:SP$ GenericChoice | Defined$ TriggeredPlayer | Choices$ PickA,PickB | SpellDescription$ Nobody chooses.\n" +
	"SVar:PickA:DB$ LoseLife | Defined$ You | LifeAmount$ 3 | SpellDescription$ PickA\n" +
	"SVar:PickB:DB$ GainLife | Defined$ You | LifeAmount$ 3 | SpellDescription$ PickB\n" +
	"Oracle:x\n"

// TestGenericChoiceEmptyDefinedDoesNotAskTheController pins the empty
// Defined$ player set: a GenericChoice whose Defined$ names a player role that
// binds no player must NOT pose a mode ask to the resolving controller (the
// accidental fallback), and neither branch may run. The Note is asserted so
// the test fails if the handler never ran at all (a vacuous green).
func TestGenericChoiceEmptyDefinedDoesNotAskTheController(t *testing.T) {
	t.Parallel()
	empty := card(t, genericChoiceEmptyDefined)
	cfg := seatZeroStart(Config{Seed: 80, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{empty}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()
	id := placeInDeck(t, e, 0, empty, state.ZHand)
	if z := e.G.Obj(id).Zone; z != state.ZHand {
		t.Fatalf("Empty Trial zone = %s, want Hand", z)
	}
	addMana(t, e, 0, "R")
	handBefore, lifeBefore := len(e.G.Zone(state.ZHand, 0)), e.G.Players[0].Life

	submitSeizeCast(t, e, id)
	passUntilStackEmpty(t, e, 40)

	// The handler ran and declined to ask: exactly the no-players Note.
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "GenericChoice Defined$ TriggeredPlayer resolved no players") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("no-players Note count = %d, want exactly 1 (the handler ran and recorded why it did nothing)", n)
	}
	// No chooser was invented for the controller: its life and hand are
	// untouched (a fallback would have asked it and run one branch).
	if got := e.G.Players[0].Life; got != lifeBefore {
		t.Fatalf("controller life = %d, want %d (no branch may run for an empty Defined$)", got, lifeBefore)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore-1 {
		t.Fatalf("controller hand = %d, want %d (the spell left hand; no branch drew)", got, handBefore-1)
	}
	replayCheck(t, e, cfg)
}

// genericChoiceNested is an inline exact script (no .cards text is copied:
// the licensing rule) whose chosen body is itself a mid-resolution ask. Each
// opponent chooses DoDiscard (a TgtChoose discard) or DoGain (a life gain);
// the outer SubAbility$ DBTail must run once after the chooser sequence.
const genericChoiceNested = "Name:Trial of Choices\nManaCost:R\nTypes:Sorcery\n" +
	"A:SP$ GenericChoice | Defined$ Opponent | TempRemember$ Chooser | Choices$ DoDiscard,DoGain | SubAbility$ DBTail | SpellDescription$ Each opponent chooses.\n" +
	"SVar:DoDiscard:DB$ Discard | Defined$ Remembered | Mode$ TgtChoose | NumCards$ 1 | SpellDescription$ DoDiscard\n" +
	"SVar:DoGain:DB$ GainLife | Defined$ Remembered | LifeAmount$ 5 | SpellDescription$ DoGain\n" +
	"SVar:DBTail:DB$ LoseLife | Defined$ You | LifeAmount$ 1\n" +
	"Oracle:x\n"

// TestGenericChoiceNestedAskResumesRemainingChoosers pins the nested-ask
// continuation: opponent 1's chosen Discard body poses its own KChoose ask, so
// the GenericChoice must record the chooser cursor and still ask opponent 2
// once that nested ask's chain completes. Without SuspendGenericChoiceRest the
// cursor is lost and opponent 2 is never asked.
func TestGenericChoiceNestedAskResumesRemainingChoosers(t *testing.T) {
	t.Parallel()
	trial := card(t, genericChoiceNested)
	cfg := seatZeroStart(Config{Seed: 79, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{trial}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()
	id := placeInDeck(t, e, 0, trial, state.ZHand)
	if z := e.G.Obj(id).Zone; z != state.ZHand {
		t.Fatalf("Trial of Choices zone = %s, want Hand", z)
	}
	addMana(t, e, 0, "R")
	// Each opponent must hold at least two cards so the Discard body's
	// TgtChoose ask is a real choice (one eligible card is discarded with no
	// decision, which would hide the nested-ask path).
	for _, p := range []state.PlayerID{1, 2} {
		if n := len(e.G.Zone(state.ZHand, p)); n < 2 {
			t.Fatalf("opponent %d hand = %d cards, want >= 2 for the nested discard ask", p, n)
		}
	}
	hp1, hp2 := e.G.Players[1].Life, e.G.Players[2].Life

	// Opponent 1's ask; choose DoDiscard.
	d := castSeize(t, e, id)
	if d.Player != 1 || d.ResumeKind != "generic_players" {
		t.Fatalf("first chooser = %+v, want opponent 1 with generic_players", d)
	}
	choosers := []state.PlayerID{d.Player}
	submitChoices(t, e, 0) // DoDiscard

	// The chosen DoDiscard body's own Discard ask suspends the resolution.
	d = passUntilAskKind(t, e, decision.KModes, 40)
	if d.Player != 1 || d.ResumeKind == "generic_players" {
		t.Fatalf("nested discard ask = %+v, want opponent 1's own Discard KChoose (not a GenericChoice ask)", d)
	}
	submitChoices(t, e, 0)

	// Opponent 2 must STILL be asked after the nested ask completes.
	d = passUntilAskKind(t, e, decision.KModes, 40)
	if d.Player != 2 || d.ResumeKind != "generic_players" {
		t.Fatalf("second chooser after the nested ask = %+v, want opponent 2", d)
	}
	choosers = append(choosers, d.Player)
	submitChoices(t, e, 1) // DoGain

	passUntilStackEmpty(t, e, 40)

	// Chooser-bound effects: opponent 2's DoGain gained THEM 5 life, and the
	// controller (seat 0) lost exactly the single DBTail point.
	if got := e.G.Players[2].Life; got != hp2+5 {
		t.Fatalf("opponent 2 life = %d, want %d (its own DoGain body ran)", got, hp2+5)
	}
	if got := e.G.Players[1].Life; got != hp1 {
		t.Fatalf("opponent 1 life = %d, want %d (its chosen DoDiscard body gained nothing)", got, hp1)
	}
	var tail []int
	for _, ev := range e.L.Events {
		if ev.Kind == events.LifeChange && ev.Player == 0 && ev.Amount < 0 {
			tail = append(tail, int(ev.Amount))
		}
	}
	if len(tail) != 1 || tail[0] != -1 {
		t.Fatalf("controller life-loss events = %v, want exactly [-1] (DBTail ran once after both choosers)", tail)
	}
	// Both opponents were asked the GenericChoice: the two `generic_players`
	// asks this drive saw, in order, are the proof.
	if len(choosers) != 2 || choosers[0] != 1 || choosers[1] != 2 {
		t.Fatalf("GenericChoice choosers = %v, want [1 2]", choosers)
	}
	replayCheck(t, e, cfg)
}

// TestSeizeTheSpotlightCloneKeepsChooserCursor pins the clone contract: a
// clone taken while the SECOND chooser's ask is pending must carry the chooser
// cursor (Decision.ResumeGenericChoosers/Index) so answering on the clone
// completes both choosers exactly once instead of resetting to the first
// chooser or dropping the rest. This is the search/host snapshot path.
func TestSeizeTheSpotlightCloneKeepsChooserCursor(t *testing.T) {
	t.Parallel()
	e, _, id := seizeBoard(t)
	d := castSeize(t, e, id)
	if d.Player != 1 {
		t.Fatalf("first chooser = seat %d, want opponent 1", d.Player)
	}
	if d.ResumeGenericChooserIndex != 0 || len(d.ResumeGenericChoosers) != 2 {
		t.Fatalf("pending cursor = %d over %d choosers, want 0 over 2", d.ResumeGenericChooserIndex, len(d.ResumeGenericChoosers))
	}
	clone := e.Clone()
	if cp := clone.Pending(); cp == nil || cp.ResumeGenericChooserIndex != 0 || len(cp.ResumeGenericChoosers) != 2 {
		t.Fatalf("clone pending cursor = %+v, want the opponent cursor copied", cp)
	}
	// Complete BOTH choosers on the clone: if the cursor were lost, opponent 2
	// would be re-asked or never asked.
	submitChoices(t, clone, 0) // opponent 1 Fame
	d = passUntilAskKind(t, clone, decision.KModes, 40)
	if d.Player != 2 {
		t.Fatalf("clone second chooser = seat %d, want opponent 2", d.Player)
	}
	submitChoices(t, clone, 1) // opponent 2 Fortune
	passUntilStackEmpty(t, clone, 40)
	if got := clone.G.Players[1].Notes; len(got) != 1 || got[0] != "Fame" {
		t.Fatalf("clone opponent 1 notes = %v, want [Fame]", got)
	}
	if got := clone.G.Players[2].Notes; len(got) != 1 || got[0] != "Fortune" {
		t.Fatalf("clone opponent 2 notes = %v, want [Fortune]", got)
	}
}
