package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Untap primitive's ETB$ read (task inbox-paramcensus-final-stragglers,
// Horizon Explorer entry): the DB$ Untap inside an "enters untapped"
// replacement body carries ETB$ True, the mirror of the ETB$ True the 804
// enters-tapped bodies carry on DB$ Tap (effTap already reads it there).
// The ETB untap is the plain Untap event -- no CR 122.1d stun-counter
// substitution, which is a rule for untapping a permanent already in play;
// an entering one carries no counters.
//
// horizonSrc is Horizon Explorer's real script shape (the R:/SVar pair
// verbatim, no corpus dependency).
const horizonExplorerSrc = "Name:Horizon Explorer\nManaCost:2 G\nTypes:Creature Insect Scout\nPT:2/4\n" +
	"R:Event$ Moved | ValidCard$ Land.YouCtrl | Destination$ Battlefield | ReplaceWith$ ETBUntapped | ReplacementResult$ Updated | ActiveZones$ Battlefield | Description$ Lands you control enter untapped.\n" +
	"SVar:ETBUntapped:DB$ Untap | ETB$ True | Defined$ ReplacedCard\nOracle:x\n"

// horizonTaplandSrc is Temple of Malady's enters-tapped replacement: the
// Updated + ReplaceWith(DB$ Tap | ETB$ True) shape every ETBTapped land uses.
const horizonTaplandSrc = "Name:Temple of Malady\nTypes:Land\n" +
	"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplacementResult$ Updated | ReplaceWith$ ETBTapped | Description$ CARDNAME enters tapped.\n" +
	"SVar:ETBTapped:DB$ Tap | Defined$ Self | ETB$ True\nOracle:x\n"

// horizonEngine is a 2-seat engine at seat 0's turn-1 main phase with empty
// hands, ready for direct placement and land drops.
func horizonEngine(t *testing.T) *Engine {
	t.Helper()
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	e.priorityRound()
	return e
}

// TestHorizonExplorerLandsEnterUntapped pins the composition end to end: a
// land whose own enters-tapped replacement (Temple of Malady's Updated
// DB$ Tap | ETB$ True body) competes with Horizon Explorer's enters-untapped
// replacement on the same Moved event. forEachObject's seat-local zone order
// scans the entering land's own zone (hand) before the battlefield, so the
// tap body composes first and the ETB untap composes after it: the land is
// on the battlefield and untapped.
func TestHorizonExplorerLandsEnterUntapped(t *testing.T) {
	t.Parallel()
	e := horizonEngine(t)
	onBoard(t, e, 0, horizonExplorerSrc)
	land := e.G.AddObject(card(t, horizonTaplandSrc), 0)
	land.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), land.ID))
	e.askPriority(0)
	if !playOneLand(t, e, 0, land.ID) {
		t.Fatal("the land drop was not offered")
	}
	o := e.G.Obj(land.ID)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("land zone = %s, want Battlefield", o.Zone)
	}
	if o.Tapped {
		t.Fatal("the land entered tapped despite Horizon Explorer's lands-enter-untapped replacement")
	}
}

// TestUntapETBClearsEntryStateWithoutStun pins the ETB read's one semantic
// difference from the ordinary untap path: an ETB$ True body untaps the
// entering permanent directly, never substituting a stun-counter removal
// (CR 122.1d's rule is about untapping a permanent already in play), while
// the same body without ETB$ goes through TryUntap and loses the untap to
// the stun counter instead.
func TestUntapETBClearsEntryStateWithoutStun(t *testing.T) {
	t.Parallel()
	e := horizonEngine(t)
	src := func(etb bool) state.ObjID {
		o := e.G.AddObject(card(t, "Name:Bear\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
		o.Zone = state.ZBattlefield
		o.Tapped = true
		e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
		e.emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: "STUN", Amount: 1})
		params := map[string]string{"Defined": "Self"}
		if etb {
			params["ETB"] = "True"
		}
		effects.Resolve(e, &effects.Ctx{Source: o.ID, Controller: 0},
			&cards.SA{Kind: "DB", API: "Untap", Params: params})
		return o.ID
	}
	etb := src(true)
	plain := src(false)
	if e.G.Obj(etb).Tapped {
		t.Fatal("the ETB untap did not clear the entry state")
	}
	if e.G.Obj(etb).Counter("STUN") != 1 {
		t.Fatal("the ETB untap consumed a stun counter; an entering permanent carries none")
	}
	// The ordinary path, by contrast, pays the stun substitution (CR 122.1d):
	// the counter goes and the permanent stays tapped.
	if o := e.G.Obj(plain); !o.Tapped || o.Counter("STUN") != 0 {
		t.Fatalf("ordinary untap: tapped=%t stun=%d, want true/0", o.Tapped, o.Counter("STUN"))
	}
}
