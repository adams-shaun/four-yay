package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Otherworldly Escort's death trigger (StaticEffect$ Animate, task
// static-effect) pinned end to end: the card returns with its four charge
// counters AND the named Continuous static registered as a real layer effect
// -- the returned permanent derives as a [Creature Spirit Detective], the
// Human subtype stripped (RemoveCreatureTypes$ True) before the grant's own
// Spirit & Detective types append (strip-before-add, the layer-4 walk's
// reading). No repo deck carries the card, so the golden heads cannot move.

// escortEngine deals seat 0 a hand holding Otherworldly Escort; seat 1's deck
// is bears so nothing else interacts. All corpus cards.
func escortEngine(t *testing.T, reg *cards.Registry) (*Engine, Config) { //nolint:revive
	t.Helper()
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{searchCorpusCard(t, reg, "Otherworldly Escort")}
	for len(deck) < 40 {
		deck = append(deck, searchCorpusCard(t, reg, "Forest"))
	}
	opp := make([]*cards.Card, 0, 40)
	for i := 0; i < 10; i++ {
		opp = append(opp, bear)
	}
	for len(opp) < 40 {
		opp = append(opp, searchCorpusCard(t, reg, "Forest"))
	}
	cfg := seatZeroStart(Config{Seed: 3301, Names: []string{"escort", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// escortOnBattlefield moves the Escort into seat 0's battlefield from wherever
// the deal put it and returns its id.
func escortOnBattlefield(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == "Otherworldly Escort" {
				if z != state.ZBattlefield {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatal("Otherworldly Escort absent from hand/library")
	return 0
}

func TestOtherworldlyEscortReturnsAsSpiritDetective(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := escortEngine(t, reg)
	id := escortOnBattlefield(t, e)

	// Before the death the printed face stands: a Creature Human Detective.
	der := e.Derived(id)
	if got := strings.Join(der.Types, " "); got != "Creature Human Detective" {
		t.Fatalf("pre-death types = %q, want %q", got, "Creature Human Detective")
	}

	// Lethal damage: the death trigger fires through the ordinary drain, and
	// its resolution is the DB$ ChangeZone return -- no ask (Defined$
	// TriggeredNewCardLKICopy).
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 5, Player: 0})
	e.checkStateBased()
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 20)

	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Otherworldly Escort zone = %+v, want battlefield", o)
	}
	if got := o.Counter("CHARGE"); got != 4 {
		t.Fatalf("charge counters = %d, want 4 (WithCountersType$/WithCountersAmount$)", got)
	}
	der = e.Derived(id)
	if got := strings.Join(der.Types, " "); got != "Creature Spirit Detective" {
		t.Fatalf("returned types = %q, want %q (Human stripped, Spirit & Detective added)", got, "Creature Spirit Detective")
	}

	// The body is fully readable (AddType$ + RemoveCreatureTypes$ only), so
	// the application is silent: no unread-remainder Note on the carrier path.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "StaticEffect") {
			t.Fatalf("unexpected StaticEffect Note on the carrier path: %+v", ev)
		}
	}

	// The registration is event-backed: a log-only replay re-executes the
	// trigger and derives the identical type grant.
	replayCheck(t, e, cfg)
}
