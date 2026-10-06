package rules

// The as-cast battlefield snapshot behind Count$LastStateBattlefieldWithFallback
// ("if you controlled a Mount as you cast this spell"): the head must read the
// battlefield the spell found when it was cast, not the one that exists when it
// resolves. Real corpus spells (never committed .txt), with synthetic
// surrounding permanents.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// holdCast casts id (answering an X ask with x and a target ask with the
// option naming target) and stops with the spell on the stack and its caster
// holding priority, i.e. in the response window. It returns the spell object.
func holdCast(t *testing.T, e *Engine, id state.ObjID, target state.ObjID, x int) *state.Object {
	t.Helper()
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	for i := 0; i < 12; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while casting %d", id)
		}
		if o := e.G.Obj(id); o.Zone == state.ZStack && d.Kind == decision.KPriority {
			return o
		}
		switch {
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "x":
			submitChoices(t, e, x)
		case d.Kind == decision.KTarget:
			picked := -1
			for _, o := range d.Options {
				if o.Obj == target || (target == 0 && o.Kind == "player" && o.Player != e.G.Obj(id).Controller) {
					picked = o.Index
				}
			}
			if picked < 0 {
				t.Fatalf("target %d not offered: %+v", target, d.Options)
			}
			submitChoices(t, e, picked)
		default:
			t.Fatalf("unexpected decision while casting: %+v", d)
		}
	}
	t.Fatalf("spell %d never reached the response window", id)
	return nil
}

// markAttacking declares id attacking (Steer Clear targets an attacking or
// blocking creature).
func markAttacking(t *testing.T, e *Engine, controller state.PlayerID, id state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: controller, IDs: []state.ObjID{id}})
	if !e.G.Obj(id).IsAttacking {
		t.Fatalf("precondition: %d is not attacking", id)
	}
}

// snapshotConfig is corpusCardConfig with extra corpus cards in BOTH decks
// (one copy each per seat, the rest Mountains): the spell is moved to the
// active seat's hand, and the extras stay in their libraries until a test
// brings them to the battlefield with toBattlefield. Every move is a logged
// event, so replayCheck can rebuild the game from the log alone.
func snapshotConfig(t *testing.T, seed uint64, spell string, extras ...string) (*Engine, Config, state.ObjID, state.PlayerID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	deck := func() []*cards.Card {
		d := []*cards.Card{mustCorpusCard(t, reg, spell)}
		for _, name := range extras {
			d = append(d, mustCorpusCard(t, reg, name))
		}
		return append(d, mountainDeck(t, 40-len(d))...)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{deck(), deck()}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	caster := e.G.Active
	id := findByName(e, spell, caster)
	if id == 0 {
		t.Fatalf("corpus %q not found for seat %d", spell, caster)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZHand})
	e.pending = nil
	e.priorityRound()
	return e, cfg, id, caster
}

// toBattlefield moves seat p's copy of the named corpus card from its library
// to the battlefield by a logged event, and asserts it got there.
func toBattlefield(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	id := findByName(e, name, p)
	if id == 0 {
		t.Fatalf("%q not in seat %d's deck", name, p)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("precondition: %q did not reach the battlefield", name)
	}
	return id
}

// steerClearFixture seats a 6/6 attacking opposing creature and a Steer Clear
// in seat 0's hand with W available, optionally with a Mount already in play.
// It returns the engine, config, spell, victim, the in-play Mount (0 when
// absent) and the caster.
func steerClearFixture(t *testing.T, seed uint64, mount bool) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID, state.PlayerID) {
	t.Helper()
	e, cfg, spell, caster := snapshotConfig(t, seed, "Steer Clear", "Colossal Dreadmaw", "Venomsac Lagac")
	victim := toBattlefield(t, e, 1-caster, "Colossal Dreadmaw")
	markAttacking(t, e, 1-caster, victim)
	var m state.ObjID
	if mount {
		m = toBattlefield(t, e, caster, "Venomsac Lagac")
	}
	addMana(t, e, caster, "W")
	return e, cfg, spell, victim, m, caster
}

// TestCastBattlefieldSnapshotSteerClearMountLeavesInResponse: the Mount dies
// in response, yet "you controlled a Mount as you cast this spell" stays true.
func TestCastBattlefieldSnapshotSteerClearMountLeavesInResponse(t *testing.T) {
	t.Parallel()
	e, cfg, spell, victim, mount, _ := steerClearFixture(t, 211, true)
	so := holdCast(t, e, spell, victim, 0)
	if so.CastBattlefield == nil {
		t.Fatal("precondition: Steer Clear carries no as-cast battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: mount, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(mount).Zone != state.ZGraveyard {
		t.Fatal("precondition: the Mount did not leave the battlefield")
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(victim).Damage; got != 4 {
		t.Fatalf("Steer Clear dealt %d, want 4 (a Mount was controlled as it was cast)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCastBattlefieldSnapshotSteerClearMountArrivesAfterCast: a Mount that
// arrives in response was not controlled as the spell was cast.
func TestCastBattlefieldSnapshotSteerClearMountArrivesAfterCast(t *testing.T) {
	t.Parallel()
	e, cfg, spell, victim, _, caster := steerClearFixture(t, 212, false)
	holdCast(t, e, spell, victim, 0)
	toBattlefield(t, e, caster, "Venomsac Lagac")
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(victim).Damage; got != 2 {
		t.Fatalf("Steer Clear dealt %d, want 2 (the Mount arrived after the cast)", got)
	}
	replayCheck(t, e, cfg)
}
