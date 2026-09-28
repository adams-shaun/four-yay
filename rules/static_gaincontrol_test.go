package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The static GainControl reader (S:Mode$ Continuous | ... | GainControl$),
// pinned end to end on REAL corpus cards: Mind Control's "You control
// enchanted creature" and Fealty to the Realm's "The monarch controls
// enchanted creature". Before the fix the statics scan built no effect for a
// GainControl$ parameter, so every control-stealing Aura in the corpus (42
// carriers) sat on its bearer doing nothing. The realization is the static
// control reconcile (rules/control_static.go): a real tracked control grant
// plus a real events.ControlChange, ended -- through the ordinary
// expireControl chain -- when the static stops being live.

const staticBearFixture = "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// staticGainControlGame builds a 2-seat game whose seat 0 deck starts with
// the named corpus cards over Mountains and whose seat 1 deck is nBears
// Grizzly Bears over Mountains, drives to seat 0's turn-1 Main1 and puts the
// first Bear on seat 1's battlefield through the logged library→battlefield
// MoveZone (an eventless onBoard placement would be invisible to the
// log-only replay every test here ends with).
func staticGainControlGame(t *testing.T, seed uint64, seat0 []*cards.Card, nBears int, seat1Extras ...*cards.Card) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, staticBearFixture)
	bears := make([]*cards.Card, nBears)
	for i := range bears {
		bears[i] = bear
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"aura", "victim"},
		Decks: [][]*cards.Card{
			append(append([]*cards.Card{}, seat0...), mountainDeck(t, 40-len(seat0))...),
			append(append(bears, seat1Extras...), mountainDeck(t, 40-nBears-len(seat1Extras))...),
		},
		Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 1) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				return e, cfg, id
			}
		}
	}
	t.Fatal("no Grizzly Bears dealt to seat 1")
	return nil, cfg, 0
}

// controlChangesFor returns every ControlChange event naming id.
func controlChangesFor(e *Engine, id state.ObjID) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.ControlChange && ev.Obj == id {
			out = append(out, ev)
		}
	}
	return out
}

// castControlAura moves the named corpus Aura to seat 0's hand, pays its cost
// from a fresh {5} pool and drives the cast to its target (bear), leaving the
// caller at priority with the resolution drained.
func castControlAura(t *testing.T, e *Engine, name string, bear state.ObjID) state.ObjID {
	t.Helper()
	id := findAndMoveToHand(t, e, 0, name)
	addMana(t, e, 0, "UUUUU")
	castFromPriority(t, e, id)
	answerKTarget(t, e, bear)
	passUntilStackEmpty(t, e, 60)
	if e.G.Obj(id).AttachedTo != bear {
		t.Fatalf("%s AttachedTo = %d, want the bearer %d", name, e.G.Obj(id).AttachedTo, bear)
	}
	return id
}

// TestMindControlStaticTakesAndReturnsControl: the archetype. Attach Mind
// Control to the opponent's creature -> the bearer's controller becomes the
// Aura's controller through a real ControlChange; the Aura leaves the
// battlefield -> the bearer returns to its owner.
func TestMindControlStaticTakesAndReturnsControl(t *testing.T) {
	t.Parallel()
	mc, ok := testutil.CorpusRegistry(t).Lookup("Mind Control")
	if !ok {
		t.Fatal("corpus missing Mind Control")
	}
	e, cfg, bear := staticGainControlGame(t, 6101, []*cards.Card{mc}, 1)
	mcID := castControlAura(t, e, "Mind Control", bear)
	if got := e.G.Obj(bear).Controller; got != 0 {
		t.Fatalf("bearer controller = %d, want 0 (the Aura's controller)", got)
	}
	ch := controlChangesFor(e, bear)
	if len(ch) != 1 || ch[0].Player != 0 {
		t.Fatalf("ControlChange events for the bearer = %+v, want exactly one to seat 0", ch)
	}
	replayCheck(t, e, cfg)

	// The Aura leaves the battlefield: the static stops being live, the
	// tracked grant ends, and the bearer returns to its owner (CR 611.3b
	// through the ordinary expireControl Previous chain).
	e.emit(events.Event{Kind: events.MoveZone, Obj: mcID, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(bear).Controller; got != 1 {
		t.Fatalf("bearer controller after the Aura left = %d, want 1 (its owner)", got)
	}
	if ch = controlChangesFor(e, bear); len(ch) != 2 || ch[1].Player != 1 {
		t.Fatalf("ControlChange events after the Aura left = %+v, want a second one back to seat 1", ch)
	}
	replayCheck(t, e, cfg)
}

// TestFealtyToTheRealmMonarchControlsEnchantedCreature: the deck-carrier
// shape. Fealty to the Realm enters (its own ETB trigger makes its
// controller the monarch), the bearer's controller becomes THE MONARCH --
// Player.isMonarch resolved through the shared player-spec grammar -- and
// when the monarch designation moves away the static grant ends and the
// bearer returns to its owner.
func TestFealtyToTheRealmMonarchControlsEnchantedCreature(t *testing.T) {
	t.Parallel()
	ft, ok := testutil.CorpusRegistry(t).Lookup("Fealty to the Realm")
	if !ok {
		t.Fatal("corpus missing Fealty to the Realm")
	}
	e, cfg, bear := staticGainControlGame(t, 6102, []*cards.Card{ft}, 1)
	castControlAura(t, e, "Fealty to the Realm", bear)
	if !e.G.HasMonarch || e.G.Monarch != 0 {
		t.Fatalf("monarch = %d/%v, want seat 0 (Fealty's own ETB trigger)", e.G.Monarch, e.G.HasMonarch)
	}
	if got := e.G.Obj(bear).Controller; got != 0 {
		t.Fatalf("bearer controller = %d, want 0 (the monarch)", got)
	}
	if ch := controlChangesFor(e, bear); len(ch) != 1 || ch[0].Player != 0 {
		t.Fatalf("ControlChange events for the bearer = %+v, want exactly one to seat 0", ch)
	}
	replayCheck(t, e, cfg)

	// The monarch designation moves away: the resolved controller changed,
	// so the static grant ends and the bearer returns to its owner.
	e.emit(events.Event{Kind: events.MonarchChange, Player: 1})
	if got := e.G.Obj(bear).Controller; got != 1 {
		t.Fatalf("bearer controller after the monarch moved = %d, want 1 (its owner)", got)
	}
	if ch := controlChangesFor(e, bear); len(ch) != 2 || ch[1].Player != 1 {
		t.Fatalf("ControlChange events after the monarch moved = %+v, want a second one back to seat 1", ch)
	}
	replayCheck(t, e, cfg)
}

// TestStaticGainControlStacksWithTemporarySteal: the CR 613.7 begin-order
// contract. With Mind Control (seat 0) already holding the bearer, seat 1's
// Act of Treason (until end of turn) stacks on top; when the temporary grant
// expires the bearer returns to the STATIC's controller, not its owner --
// and only after the Mind Control itself ends does the bearer go home.
func TestStaticGainControlStacksWithTemporarySteal(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mc, ok := reg.Lookup("Mind Control")
	if !ok {
		t.Fatal("corpus missing Mind Control")
	}
	treason, ok := reg.Lookup("Act of Treason")
	if !ok {
		t.Fatal("corpus missing Act of Treason")
	}
	e, cfg, bear := staticGainControlGame(t, 6103, []*cards.Card{mc}, 1, treason)
	mcID := castControlAura(t, e, "Mind Control", bear)
	if got := e.G.Obj(bear).Controller; got != 0 {
		t.Fatalf("bearer controller after Mind Control = %d, want 0", got)
	}

	// Seat 1's temporary steal resolves on top of the standing static (the
	// choose_control_regression_test's own Act-of-Treason convention: the
	// SA resolved directly off the real face with the target bound).
	trID := findAndMoveToHand(t, e, 1, "Act of Treason")
	addMana(t, e, 1, "RRR")
	trObj := e.G.Obj(trID)
	effects.Resolve(e, &effects.Ctx{Source: trID, Controller: 1,
		Targets: []state.Target{{Obj: bear}}, TargetsOffered: true,
		SVars: trObj.Face().SVars}, trObj.Face().SpellAbility())
	if got := e.G.Obj(bear).Controller; got != 1 {
		t.Fatalf("bearer controller after Act of Treason = %d, want 1", got)
	}
	if ch := controlChangesFor(e, bear); len(ch) != 2 || ch[1].Player != 1 {
		t.Fatalf("ControlChange events after the steal = %+v, want the steal's own to seat 1", ch)
	}
	replayCheck(t, e, cfg)

	// The temporary grant expires at cleanup: the bearer returns to the
	// static's controller (seat 0), NOT its owner (seat 1).
	e.EndOfTurnCleanup()
	if got := e.G.Obj(bear).Controller; got != 0 {
		t.Fatalf("bearer controller after the temporary grant expired = %d, want 0 (the Mind Control static's controller), not the owner", got)
	}
	replayCheck(t, e, cfg)

	// And when the Mind Control itself ends the bearer finally goes home.
	e.emit(events.Event{Kind: events.MoveZone, Obj: mcID, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(bear).Controller; got != 1 {
		t.Fatalf("bearer controller after Mind Control left = %d, want 1 (its owner)", got)
	}
	replayCheck(t, e, cfg)
}

// TestStaticGainControlFailsClosedOnUnresolvableValues: a GainControl$ value
// that names nobody or several players yields NO grant -- the fail-closed
// direction, never a wrong-wide steal. Pinned at the resolver the reconcile
// runs (staticGainControlController): no monarch on the board, and a bare
// unqualified Player/Any that would match every seat.
func TestStaticGainControlFailsClosedOnUnresolvableValues(t *testing.T) {
	t.Parallel()
	ft, ok := testutil.CorpusRegistry(t).Lookup("Fealty to the Realm")
	if !ok {
		t.Fatal("corpus missing Fealty to the Realm")
	}
	e, _, bear := staticGainControlGame(t, 6104, []*cards.Card{ft}, 1)
	if e.G.HasMonarch {
		t.Fatalf("fixture requires no monarch; got %d", e.G.Monarch)
	}
	if _, ok := staticGainControlController(e.G, "Player.isMonarch", 0, bear); ok {
		t.Fatal("Player.isMonarch with no monarch must resolve to nobody")
	}
	if _, ok := staticGainControlController(e.G, "Player", 0, bear); ok {
		t.Fatal("a bare unqualified Player/Any (matches every seat) must fail closed")
	}
	if _, ok := staticGainControlController(e.G, "Any", 0, bear); ok {
		t.Fatal("a bare Any must fail closed")
	}
	// You always resolves, to the static's controller.
	if p, ok := staticGainControlController(e.G, "You", 0, bear); !ok || p != 0 {
		t.Fatalf("You resolved to %d/%v, want seat 0", p, ok)
	}
}

// TestStaticGainControlFollowsBearerMoves: an Aura that moves bearers
// releases the old bearer to its owner and takes the new one in the same
// pass -- the wanted set is re-derived per event, so both legs land.
func TestStaticGainControlFollowsBearerMoves(t *testing.T) {
	t.Parallel()
	mc, ok := testutil.CorpusRegistry(t).Lookup("Mind Control")
	if !ok {
		t.Fatal("corpus missing Mind Control")
	}
	e, cfg, bear := staticGainControlGame(t, 6105, []*cards.Card{mc}, 2)
	mcID := castControlAura(t, e, "Mind Control", bear)
	if got := e.G.Obj(bear).Controller; got != 0 {
		t.Fatalf("bearer controller = %d, want 0", got)
	}
	var other state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
			other = id
			break
		}
	}
	if other == 0 {
		t.Fatal("no second bear dealt to seat 1")
	}
	// The Aura re-attaches to the other Bear through the ordinary Attach
	// event: the reconcile then releases bear and takes other.
	e.emit(events.Event{Kind: events.Attach, Obj: mcID, IDs: []state.ObjID{other}})
	if got := e.G.Obj(bear).Controller; got != 1 {
		t.Fatalf("released bearer controller = %d, want 1 (its owner)", got)
	}
	if got := e.G.Obj(other).Controller; got != 0 {
		t.Fatalf("new bearer controller = %d, want 0 (the Aura's controller)", got)
	}
	replayCheck(t, e, cfg)
}
