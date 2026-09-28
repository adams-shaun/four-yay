package rules

// Produced$ Special EachColorAmong_ExiledWith (feedback
// fb-20260928T161639Z-081a09f0, the creature mana-ability audit's one
// unmodelled selector). Sunbird Effigy -- the creature face of the corpus's
// only carrier, sunbird_standard_sunbird_effigy.txt -- prints "{T}: For each
// color among the exiled cards used to craft CARDNAME, add one mana of that
// color." The selector previously fell through to the rune gate's loud
// "unhandled Produced$" Note and added nothing.
//
// The executor now unions the colours among the cards exiled WITH the
// resolving source (the same exiledWithSet the Defined$ ExiledWith referent
// reads), rendered in fixed WUBRG order; an empty set is a deterministic
// no-op (no Note, no mana). These tests drive it through the real mana path
// using an inline fixture that mirrors the printed shape -- never the corpus
// .txt (GPL) -- plus a registry anchor proving the real card carries the
// token.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// moveToExileWith moves the named seeded deck card to a player's exile zone
// through a LOGGED MoveZone whose IDs payload names the exiling source, which
// events.Apply's exile branch folds into Object.ExiledWith (the
// event-derived, replay-safe association). It is only valid for a card seeded
// via eachColorGame/colourIdentityGame into the seat's deck.
func moveToExileWith(t *testing.T, e *Engine, p state.PlayerID, name string, src state.ObjID) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZExile, IDs: []state.ObjID{src}})
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("card %q not found in seat %d's library or hand", name, p)
	return 0
}

// TestEachColorAmongExiledWithProducesExiledColours is the positive case:
// two cards exiled WITH the source (a mono-blue and a mono-red creature) make
// the {T} ability add exactly {U}{R}, no Note, and the board replays from the
// log.
func TestEachColorAmongExiledWithProducesExiledColours(t *testing.T) {
	t.Parallel()
	src := card(t, colorlessFixture("Special EachColorAmong_ExiledWith"))
	monoU := card(t, monoFixtureSrc("U"))
	monoR := card(t, monoFixtureSrc("R"))
	e, cfg := eachColorGame(t, 497, []*cards.Card{src, monoU, monoR})
	sid := moveToBattlefieldByName(t, e, 0, "Colorless Sifter")
	uid := moveToExileWith(t, e, 0, "MonoU Testee", sid)
	rid := moveToExileWith(t, e, 0, "MonoR Testee", sid)
	// PRECONDITION: the source is an untapped battlefield creature and the two
	// exiled cards really name it as their exiling source -- without the
	// ExiledWith association the empty set would make this a silent no-op and
	// the assertion below would pass vacuously.
	if o := e.G.Obj(sid); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: source = %+v, want an untapped battlefield creature", o)
	}
	for _, tc := range []struct {
		name string
		id   state.ObjID
	}{{"MonoU Testee", uid}, {"MonoR Testee", rid}} {
		o := e.G.Obj(tc.id)
		if o == nil || o.Zone != state.ZExile || o.ExiledWith != sid {
			t.Fatalf("precondition: %s = %+v, want exiled with %d", tc.name, o, sid)
		}
	}
	// CR 302.6: the Sifter entered this turn, so clear summoning sickness with
	// a logged TurnChange (the tapForManaAfterTurnClears discipline) and
	// activate its {T} ability.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.priorityRound()
	activateMana(t, e, sid)
	poolIs(t, e, [state.MC + 1]int32{state.MU: 1, state.MR: 1})
	if noteContaining(t, e, "unhandled Produced$") {
		t.Fatalf("resolved selector still emitted a loud Note: %v", notes(t, e))
	}
	replayCheck(t, e, cfg)
}

// TestEachColorAmongExiledWithEmptySetIsSilentNoOp pins the empty-set shape:
// with NOTHING exiled with the source, the batch adds nothing and emits NO
// Note, exactly like EachColorAmong_Valid's empty set -- "for each color"
// over none is a deterministic no-op, not a failure.
func TestEachColorAmongExiledWithEmptySetIsSilentNoOp(t *testing.T) {
	t.Parallel()
	src := card(t, colorlessFixture("Special EachColorAmong_ExiledWith"))
	e, cfg := eachColorGame(t, 498, []*cards.Card{src})
	tapForManaAfterTurnClears(t, e, "Colorless Sifter")
	poolIs(t, e, [state.MC + 1]int32{})
	if noteContaining(t, e, "unhandled Produced$") {
		t.Fatalf("empty exiled-with set emitted a loud Note: %v", notes(t, e))
	}
	replayCheck(t, e, cfg)
}

// TestSunbirdEffigyCarriesEachColorAmongExiledWith is the real-corpus anchor:
// the compiled Sunbird Standard card's creature face carries the exact
// Produced$ token this selector handles, so the executor shape tested above
// is the one the real card requests (never a paraphrase).
func TestSunbirdEffigyCarriesEachColorAmongExiledWith(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Sunbird Standard")
	if !ok {
		t.Fatalf("Sunbird Standard not in the compiled corpus")
	}
	found := false
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		creature := false
		for _, ty := range f.Types {
			if ty == "Creature" {
				creature = true
			}
		}
		if !creature {
			continue
		}
		for _, a := range f.Abilities {
			if a.Params["Produced"] == "Special EachColorAmong_ExiledWith" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no Sunbird Effigy creature face carries Produced$ Special EachColorAmong_ExiledWith")
	}
}
