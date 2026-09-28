// Tests for the `R:Event$ GameLoss` / `R:Event$ GameWin | Layer$ CantHappen`
// replacement class (task fdn-repl-cant-lose): "You can't lose the game and
// your opponents can't win the game" (Herald of Eternal Dawn, the Platinum
// Angel family, Abyssal Persecutor, Lich's Mastery, and the narrower
// ValidLoseReason$ carriers). The rule is CR 104.3 / 704.5a-c: while the
// replacement applies, the state-based loss simply does not happen (the
// condition stays true and the player loses as soon as the replacement is gone
// and state-based actions are next checked); an alternate win for a protected
// opponent does nothing.
//
// The driver is the REAL corpus script, not an inline fixture: the tests read
// Herald of Eternal Dawn from the corpus, place it on seat 0's battlefield with
// a logged MoveZone, and compare every scenario to config.Engine replay with
// replayCheck (a log-only rebuild) so the chain head is pinned too.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// cantLoseFixture builds a 2-seat game whose seat 0 deck leads with the real
// Herald of Eternal Dawn and moves it onto seat 0's battlefield with a logged
// MoveZone (so replayFromLog can rebuild the exact board). oppSrc, when
// non-empty, is an inline fixture card that additionally leads seat 1's deck
// and is moved onto seat 1's battlefield the same way; its id is returned as
// oppID (0 when oppSrc is empty).
func cantLoseFixture(t *testing.T, seed uint64, oppSrc string) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	herald := searchCorpusCard(t, reg, "Herald of Eternal Dawn")
	if d := herald.Link(); len(d) != 0 {
		t.Fatalf("link Herald of Eternal Dawn: %v", d)
	}
	seatDeck := append([]*cards.Card{herald}, mountainDeck(t, 39)...)
	oppDeck := mountainDeck(t, 40)
	var oppName string
	if oppSrc != "" {
		oppCard := card(t, oppSrc)
		oppName = oppCard.Faces[0].Name
		oppDeck = append([]*cards.Card{oppCard}, mountainDeck(t, 39)...)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{seatDeck, oppDeck},
		Tokens: reg.Tokens, NameUniverse: reg.Cards})
	e := New(cfg)
	e.Advance()
	heraldID := moveNamedToBattlefield(t, e, 0, "Herald of Eternal Dawn")
	var oppID state.ObjID
	if oppName != "" {
		oppID = moveNamedToBattlefield(t, e, 1, oppName)
	}
	return e, cfg, heraldID, oppID
}

// moveNamedToBattlefield finds the named card in p's hand or library and moves
// it onto p's battlefield with a logged MoveZone, returning its id. It fails
// if the card was not dealt.
func moveNamedToBattlefield(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	var id state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, cand := range e.G.Zone(z, p) {
			if o := e.G.Obj(cand); o != nil && o.Face() != nil && o.Face().Name == name {
				id = cand
			}
		}
	}
	if id == 0 {
		t.Fatalf("%s was not dealt to seat %d", name, p)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
	return id
}

// heraldOnBattlefield is the shared precondition: the object is on seat 0's
// battlefield, controlled by seat 0, and its parsed face carries the GameLoss
// CantHappen line this ticket implements. A test whose setup silently produced
// none of those must fail here instead of passing vacuously.
func heraldOnBattlefield(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Herald is not on seat 0's battlefield: %+v", o)
	}
	hasGameLoss := false
	for i := range o.Face().Repls {
		if o.Face().Repls[i].Event == "GameLoss" {
			hasGameLoss = true
		}
	}
	if !hasGameLoss {
		t.Fatalf("precondition: %s carries no parsed Event$ GameLoss line", o.Face().Name)
	}
}

// loseAllLife drops seat 0 to 0 life through one logged LifeChange and runs the
// state-based sweep, returning whether the log now records the loss.
func loseAllLife(t *testing.T, e *Engine) bool {
	t.Helper()
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -e.G.Players[0].Life})
	e.checkStateBased()
	return e.G.Players[0].Lost
}

// TestHeraldOfEternalDawnPreventsLifeLoss is brief item (1): Herald's
// controller at 0 life does not lose while Herald is on the battlefield, and
// loses at the next state-based check after Herald leaves. The second half is
// also the negative control: the identical 0-life state DOES produce a loss
// once the replacement is gone, so the first half cannot pass because the
// sweep never ran.
func TestHeraldOfEternalDawnPreventsLifeLoss(t *testing.T) {
	t.Parallel()
	e, cfg, hid, _ := cantLoseFixture(t, 8301, "")
	heraldOnBattlefield(t, e, hid)
	lifeBefore := e.G.Players[0].Life
	if lost := loseAllLife(t, e); lost {
		t.Fatalf("Herald of Eternal Dawn did not prevent the 0-life loss (life %d -> %d)",
			lifeBefore, e.G.Players[0].Life)
	}
	if got := e.G.Players[0].Life; got > 0 {
		t.Fatalf("negative precondition: life must be at or below 0, got %d", got)
	}
	// Herald leaves: the condition is still true, so the very next state-based
	// check applies CR 704.5a.
	e.emit(events.Event{Kind: events.MoveZone, Obj: hid, From: state.ZBattlefield, To: state.ZGraveyard})
	e.checkStateBased()
	if !e.G.Players[0].Lost {
		t.Fatal("seat 0 must lose once Herald has left the battlefield and state-based actions are checked")
	}
	replayCheck(t, e, cfg)
}

// TestHeraldOfEternalDawnPreventsEmptyLibraryDraw is brief item (2): a draw
// from an empty library does not lose while Herald is out, and does once it is
// gone.
func TestHeraldOfEternalDawnPreventsEmptyLibraryDraw(t *testing.T) {
	t.Parallel()
	e, cfg, hid, _ := cantLoseFixture(t, 8302, "")
	heraldOnBattlefield(t, e, hid)
	cantLoseEmptyLibrary(t, e, 0)
	e.drawCard(0)
	if e.G.Players[0].Lost {
		t.Fatal("Herald of Eternal Dawn did not prevent the empty-library loss")
	}
	// The library is genuinely empty -- the draw really did fail, it was not
	// merely never attempted.
	if n := len(e.G.Zone(state.ZLibrary, 0)); n != 0 {
		t.Fatalf("negative precondition: library holds %d cards, want 0", n)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: hid, From: state.ZBattlefield, To: state.ZGraveyard})
	e.drawCard(0)
	if !e.G.Players[0].Lost {
		t.Fatal("seat 0 must lose to the empty library once Herald is gone")
	}
	replayCheck(t, e, cfg)
}

// TestHeraldOfEternalDawnPreventsOpponentWin is brief item (3): an opponent's
// "you win the game" effect does nothing while Herald is out, and does win once
// Herald is gone. The wins come from a real api:WinsGame body on a trigger the
// opponent controls.
func TestHeraldOfEternalDawnPreventsOpponentWin(t *testing.T) {
	t.Parallel()
	const winSrc = "Name:Synth Winnower\nTypes:Creature\nPT:1/1\n" +
		"T:Mode$ Phase | Phase$ Upkeep | Execute$ Win | TriggerDescription$ win the game\n" +
		"SVar:Win:DB$ WinsGame | Defined$ You\nOracle:x\n"
	e, cfg, hid, oid := cantLoseFixture(t, 8303, winSrc)
	heraldOnBattlefield(t, e, hid)
	if o := e.G.Obj(oid); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: the opponent's Winnower is not on seat 1's battlefield: %+v", o)
	}
	fireUpkeepTrigger(t, e)
	if e.G.Over || e.G.Winner == 1 {
		t.Fatalf("Herald did not stop the opponent's win: over=%v winner=%d", e.G.Over, e.G.Winner)
	}
	// The WinsGame body resolved through its registered handler, not the
	// loud unimplemented-API fallback -- a "nothing happened" assertion must
	// not pass with the whole feature unregistered.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "unimplemented API WinsGame" {
			t.Fatal("WinsGame resolved to the unimplemented-API fallback")
		}
	}
	// Negative control: with Herald gone the same trigger wins.
	e.emit(events.Event{Kind: events.MoveZone, Obj: hid, From: state.ZBattlefield, To: state.ZGraveyard})
	fireUpkeepTrigger(t, e)
	if !e.G.Over || e.G.Winner != 1 {
		t.Fatalf("with Herald gone the opponent must win: over=%v winner=%d", e.G.Over, e.G.Winner)
	}
	replayCheck(t, e, cfg)
}

// TestHeraldOfEternalDawnDoesNotPreventConcede is brief item (4): conceding is
// CR 104.3a's "a player may concede at any time", not a loss a GameLoss
// replacement can stop, so Herald's controller may concede and the game ends.
func TestHeraldOfEternalDawnDoesNotPreventConcede(t *testing.T) {
	t.Parallel()
	e, cfg, hid, _ := cantLoseFixture(t, 8304, "")
	heraldOnBattlefield(t, e, hid)
	e.pending = nil
	e.priorityRound()
	opt := highestPriorityOptionWithKind(t, e, "concede")
	submitChoices(t, e, opt.Index)
	if !e.G.Players[0].Lost {
		t.Fatal("seat 0 conceded but is not Lost -- the GameLoss replacement swallowed a concession")
	}
	if !e.G.Over || e.G.Winner != 1 {
		t.Fatalf("after the concede: over=%v winner=%d, want seat 1 to win", e.G.Over, e.G.Winner)
	}
	replayCheck(t, e, cfg)
}

// cantLoseEmptyLibrary moves every card in p's library to the graveyard with
// logged MoveZones, leaving the library empty.
func cantLoseEmptyLibrary(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, p)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
}

// fireUpkeepTrigger clears any pending decision, announces an upkeep step and
// resolves every trigger that queues for it.
func fireUpkeepTrigger(t *testing.T, e *Engine) {
	t.Helper()
	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("no upkeep trigger queued")
	}
	e.putTriggersOnStack()
	e.resolveTop()
}

// TestGameLossAndGameWinPrimitivesAreRegistered is the coverage half of the
// brief: both heads are registered, and for every corpus script that carries
// an Event$ GameLoss / Event$ GameWin line the head itself is no longer
// reported missing (any other missing primitive is allowed and named in the
// log, since the Lich ReplaceWith$ bodies and the Command-zone Platinum Angel
// Avatar remain out of scope).
func TestGameLossAndGameWinPrimitivesAreRegistered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	for _, head := range []string{"repl:GameLoss", "repl:GameWin"} {
		if !supported[head] {
			t.Fatalf("effects.Supported() lacks %q -- the registration was reverted", head)
		}
	}
	carriers := 0
	for _, c := range reg.Cards {
		carries := map[string]bool{}
		for _, f := range c.Faces {
			for i := range f.Repls {
				if f.Repls[i].Event == "GameLoss" || f.Repls[i].Event == "GameWin" {
					carries[f.Repls[i].Event] = true
				}
			}
		}
		if len(carries) == 0 {
			continue
		}
		carriers++
		for head := range carries {
			for _, miss := range reg.Unsupported(c, supported) {
				if miss == "repl:"+head {
					t.Errorf("%s still reports %q missing (all misses: %v)",
						c.Faces[0].Name, "repl:"+head, reg.Unsupported(c, supported))
				}
			}
		}
	}
	if carriers < 15 {
		t.Fatalf("precondition: corpus walk found only %d GameLoss/GameWin carriers, want >= 15", carriers)
	}
	t.Logf("corpus: %d cards carry an Event$ GameLoss/GameWin replacement", carriers)
}
