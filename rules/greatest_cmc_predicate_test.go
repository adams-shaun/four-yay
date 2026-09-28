package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// valeforBoard seats Summon: Valefor under seat 0 as a 3-seat game whose two
// opponents each hold a distinctly costed creature and a cheaper one, so
// "the greatest mana value among creatures they control" is exactly the
// dearer creature. Valefor is placed on the battlefield with a real MoveZone,
// which grants its first LORE counter and queues the Sonic Wings chapter.
func valeforBoard(t *testing.T, reg *cards.Registry) (*Engine, map[state.PlayerID]struct{ big, small state.ObjID }) {
	t.Helper()
	e := New(Config{Seed: 41, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(mustCorpusCard(t, reg, "Summon: Valefor"), 0)
	// Craw Wurm (4GG, CMC 6) and Grizzly Bears (1G, CMC 2) for each opponent.
	craw := mustCorpusCard(t, reg, "Craw Wurm")
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	held := map[state.PlayerID]struct{ big, small state.ObjID }{}
	for _, p := range []state.PlayerID{1, 2} {
		big := e.G.AddObject(craw, p)
		small := e.G.AddObject(bears, p)
		e.emit(events.Event{Kind: events.MoveZone, Obj: big.ID, From: state.ZLibrary, To: state.ZBattlefield})
		e.emit(events.Event{Kind: events.MoveZone, Obj: small.ID, From: state.ZLibrary, To: state.ZBattlefield})
		held[p] = struct{ big, small state.ObjID }{big.ID, small.ID}
	}
	// The entry grants the first LORE counter and queues chapter I (Sonic
	// Wings), exactly as a real Saga entering the battlefield does.
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("Summon: Valefor's entry queued no chapter trigger")
	}
	e.putTriggersOnStack()
	e.resolveTop()
	return e, held
}

// TestSummonValeforEachOpponentReturnsTheirGreatestManaValueCreature runs
// Summon: Valefor's Sonic Wings chapter (chapter I, the entry-triggered lore
// counter): RepeatEach per opponent asks each opponent to choose a creature
// with the greatest mana value among creatures THEY control
// (greatestCMC_CreatureControlledByRemembered), so each is offered only their
// own 6-CMC Craw Wurm and never the cheaper Grizzly Bears nor the other
// opponent's creature; the chosen creatures return to their owners' hands.
func TestSummonValeforEachOpponentReturnsTheirGreatestManaValueCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, held := valeforBoard(t, reg)
	// Precondition: each opponent really holds a dearer and a cheaper
	// creature, so "the greatest" is a narrowing, not the whole set.
	for p, h := range held {
		if e.G.Obj(h.big).Face().Cmc() <= e.G.Obj(h.small).Face().Cmc() {
			t.Fatalf("seat %d setup: big CMC %d must exceed small CMC %d",
				p, e.G.Obj(h.big).Face().Cmc(), e.G.Obj(h.small).Face().Cmc())
		}
		if e.G.Obj(h.big).Zone != state.ZBattlefield || e.G.Obj(h.small).Zone != state.ZBattlefield {
			t.Fatalf("seat %d creatures are not on the battlefield", p)
		}
	}

	for _, p := range []state.PlayerID{1, 2} {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("expected the ChooseCard ask for opponent %d, got %+v", p, d)
		}
		if d.Player != p {
			t.Fatalf("chooser = seat %d, want the remembered opponent seat %d", d.Player, p)
		}
		if len(d.Options) != 1 || d.Options[0].Obj != held[p].big {
			t.Fatalf("opponent %d greatest-CMC pool = %+v, want only Craw Wurm", p, d.Options)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 20)

	// Both chosen 6-CMC creatures returned to their owners' hands; the
	// cheaper creatures each stayed put.
	for _, p := range []state.PlayerID{1, 2} {
		if z := e.G.Obj(held[p].big).Zone; z != state.ZHand {
			t.Fatalf("seat %d greatest-CMC creature zone = %s, want Hand", p, z)
		}
		if z := e.G.Obj(held[p].small).Zone; z != state.ZBattlefield {
			t.Fatalf("seat %d lesser creature must be untouched, zone %s", p, z)
		}
	}
}

// TestSummonValeforGreatestCMCTiesAreAllOffered pins the tie half of Forge's
// getCardsWithHighestCMC: an opponent holding TWO creatures tied for the
// highest mana value is offered BOTH, and only the answered one returns.
func TestSummonValeforGreatestCMCTiesAreAllOffered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := New(Config{Seed: 42, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(mustCorpusCard(t, reg, "Summon: Valefor"), 0)
	craw := mustCorpusCard(t, reg, "Craw Wurm")
	// Seat 1 holds two tied 6-CMC creatures; seat 2 holds only a 2-CMC one.
	first := e.G.AddObject(craw, 1)
	second := e.G.AddObject(craw, 1)
	other := e.G.AddObject(mustCorpusCard(t, reg, "Grizzly Bears"), 2)
	for _, id := range []state.ObjID{first.ID, second.ID, other.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	// Precondition: the two tied creatures really share the maximum CMC and
	// the third is strictly below it.
	if a, b, c := int(first.Face().Cmc()), int(second.Face().Cmc()), int(other.Face().Cmc()); a != b || c >= a {
		t.Fatalf("tie setup: CMCs = %d/%d/%d, want the first two equal and the third below", a, b, c)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("Summon: Valefor's entry queued no chapter trigger")
	}
	e.putTriggersOnStack()
	e.resolveTop()

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 1 {
		t.Fatalf("expected seat 1's ChooseCard ask, got %+v", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("two tied greatest-CMC creatures must both be offered, got %+v", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)

	// Seat 2's ask follows: its only creature is its own maximum.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 2 {
		t.Fatalf("expected seat 2's ChooseCard ask, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != other.ID {
		t.Fatalf("seat 2 pool = %+v, want only its own creature", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)

	// Exactly one of the tied pair returned; the other stays.
	returned := 0
	for _, id := range []state.ObjID{first.ID, second.ID} {
		if e.G.Obj(id).Zone == state.ZHand {
			returned++
		}
	}
	if returned != 1 {
		t.Fatalf("%d of the tied pair returned, want exactly the answered one", returned)
	}
	if e.G.Obj(other.ID).Zone != state.ZHand {
		t.Fatal("seat 2's chosen creature must return to hand too")
	}
}
