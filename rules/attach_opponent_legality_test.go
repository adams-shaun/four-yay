package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestOrdinaryAttachRechecksEnchantOpponentForEachSeat pins the ordinary
// Defined$ path: an Enchant:Opponent Aura may attach to an opponent, but not
// its controller, even when a malformed or redirected target supplies that
// controller as the destination.
func TestOrdinaryAttachRechecksEnchantOpponentForEachSeat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	archnemesis := mustCorpusCard(t, reg, "Archnemesis")
	cfg := Config{Seed: 71, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{
		append([]*cards.Card{archnemesis}, mountainDeck(t, 39)...),
		mountainDeck(t, 40),
		mountainDeck(t, 40),
	}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	var id state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == archnemesis {
			id = o.ID
			if o.Zone != state.ZBattlefield {
				stageAuraEntry(t, e, id, state.PlayerRef(1))
			}
			break
		}
	}
	if id == 0 {
		t.Fatal("setup: no seat-0 Archnemesis copy")
	}
	aura := e.G.Obj(id)
	if aura.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Aura zone = %s, want battlefield", aura.Zone)
	}
	if param, ok := aura.Face().KeywordParam("Enchant"); !ok || param != "Opponent" {
		t.Fatalf("precondition: Enchant spec = %q, want Opponent", param)
	}
	e.emit(events.Event{Kind: events.Attach, Obj: id, Player: 1, Text: "attach to player"})
	if !e.G.Obj(id).HasAttachedPlayer || e.G.Obj(id).AttachedPlayer != 1 {
		t.Fatal("precondition: Aura must start attached to opponent seat 1")
	}

	attachTo := func(seat state.PlayerID) {
		effects.Resolve(e, &effects.Ctx{Source: id, Controller: 0,
			Targets: []state.Target{{Player: seat, IsPlayer: true}}},
			&cards.SA{Kind: "DB", API: "Attach", Params: map[string]string{
				"Object": "Self", "Defined": "Targeted",
			}})
	}
	attachTo(0)
	if got := e.G.Obj(id); !got.HasAttachedPlayer || got.AttachedPlayer != 1 {
		t.Fatalf("Enchant:Opponent Aura attached to its controller: %+v", got)
	}
	attachTo(2)
	if got := e.G.Obj(id); !got.HasAttachedPlayer || got.AttachedPlayer != 2 {
		t.Fatalf("Enchant:Opponent Aura failed to attach to valid opponent seat 2: %+v", got)
	}
}
