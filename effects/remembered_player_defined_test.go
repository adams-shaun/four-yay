package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// rmpl1: `Defined$ RememberedPlayer`/`RememberedPlayers` must name the
// resolution's remembered PLAYERS only, in remembered order. Before the
// definedSpec case the spellings were unknown to the shared grammar:
// knownDefinedTargets fell through and Defined's source fallback handed back
// the SOURCE object, which the plain-Remembered player mapping then dropped --
// so a carrier like The Toymaker's Trap charged NOBODY, not the remembered
// opponent. A remembered CARD contributes no player for these spellings (the
// plain Remembered family's getDefinedPlayers rule); `Defined$ Remembered`
// keeps resolving BOTH kinds.

func TestDefinedRememberedPlayerSelectors(t *testing.T) {
	h, c, cardID := mixedRememberedHost(t)

	// Singular: exactly the remembered player, in remembered order.
	ts, ok := knownDefinedTargets(h, c, "RememberedPlayer")
	if !ok {
		t.Fatal("RememberedPlayer must be a known selector, not a source fallback")
	}
	if len(ts) != 1 || !ts[0].IsPlayer || ts[0].Player != 2 {
		t.Fatalf("RememberedPlayer = %+v, want [{Player:2 IsPlayer:true}]", ts)
	}
	// Plural is the same set: the grammar alias, not a separate pool.
	ts, ok = knownDefinedTargets(h, c, "RememberedPlayers")
	if !ok {
		t.Fatal("RememberedPlayers must be a known selector, not a source fallback")
	}
	if len(ts) != 1 || !ts[0].IsPlayer || ts[0].Player != 2 {
		t.Fatalf("RememberedPlayers = %+v, want [{Player:2 IsPlayer:true}]", ts)
	}

	// The old spelling still resolves BOTH kinds, card first (order kept).
	tsAll, okAll := knownDefinedTargets(h, c, "Remembered")
	if !okAll || len(tsAll) != 2 || tsAll[0].Obj != cardID || tsAll[0].IsPlayer || !tsAll[1].IsPlayer || tsAll[1].Player != 2 {
		t.Fatalf("Remembered = %+v, want [{Obj:%d} {Player:2}]", tsAll, cardID)
	}

	// Order and duplicates survive: remembered entries [seat 0, card, seat 2,
	// seat 0] resolve to [0, 2, 0].
	c2 := &Ctx{Source: cardID, Controller: 0, Remembered: []state.Target{
		{Player: 0, IsPlayer: true}, {Obj: cardID}, {Player: 2, IsPlayer: true}, {Player: 0, IsPlayer: true},
	}}
	if h.g.Obj(cardID) == nil {
		t.Fatal("precondition: remembered card is not in the game")
	}
	ts, ok = knownDefinedTargets(h, c2, "RememberedPlayers")
	if !ok {
		t.Fatal("RememberedPlayers must be a known selector with a mixed set")
	}
	want := []state.Target{{Player: 0, IsPlayer: true}, {Player: 2, IsPlayer: true}, {Player: 0, IsPlayer: true}}
	if len(ts) != len(want) {
		t.Fatalf("RememberedPlayers = %+v, want %+v", ts, want)
	}
	for i := range want {
		if ts[i] != want[i] {
			t.Fatalf("RememberedPlayers = %+v, want %+v", ts, want)
		}
	}

	// A remembered CARD alone contributes NO player, and the spelling stays
	// KNOWN (ok=true, known-empty) -- it must not become Defined's source
	// fallback.
	c3 := &Ctx{Source: cardID, Controller: 0, Remembered: []state.Target{{Obj: cardID}}}
	ts, ok = knownDefinedTargets(h, c3, "RememberedPlayer")
	if !ok {
		t.Fatal("RememberedPlayer must be known even with only a remembered card")
	}
	if len(ts) != 0 {
		t.Fatalf("RememberedPlayer over a card-only memory = %+v, want empty (a remembered card contributes no player)", ts)
	}
	if got := Defined(h, c3, &cards.SA{Params: map[string]string{"Defined": "RememberedPlayer"}}); len(got) != 0 {
		t.Fatalf("Defined(RememberedPlayer) over a card-only memory = %+v, want empty (not the source object %d)", got, cardID)
	}
	ps, pok := knownDefinedTargets(h, c, "RememberedPlayer")
	if !pok || len(ps) != 1 || !ps[0].IsPlayer || ps[0].Player != 2 {
		t.Fatalf("precondition drift: mixed host RememberedPlayer = %+v ok=%v, want [{Player:2}]", ps, pok)
	}
}

// TestToymakersTrapRememberedPlayerLoseLife resolves the real corpus script
// (`DB$ LoseLife | Defined$ RememberedPlayer | LifeAmount$ Count$ChosenNumber
// | SubAbility$ DBDraw`) with a bound chosen number and asserts the LifeChange
// recipient: the remembered opponent who guessed wrong, never the source's
// controller.
func TestToymakersTrapRememberedPlayerLoseLife(t *testing.T) {
	card, sa := corpusSA(t, "The Toymaker's Trap", "DBLoseLife")
	if sa.API != "LoseLife" || sa.Params["Defined"] != "RememberedPlayer" {
		t.Fatalf("precondition: corpus script changed: API=%q Defined=%q", sa.API, sa.Params["Defined"])
	}
	h := newHost(t, 3)
	for i := range h.g.Players {
		h.g.Players[i].Life = 20
	}
	trap := h.g.AddObject(card, 1)
	if trap == nil || h.g.Obj(trap.ID) == nil {
		t.Fatal("precondition: the Trap source is not in the game")
	}
	if trap.Controller != 1 {
		t.Fatalf("precondition: Trap controller = %d, want 1 (must differ from the remembered opponent)", trap.Controller)
	}
	c := &Ctx{
		Source:     trap.ID,
		Controller: 1,
		Remembered: []state.Target{{Player: 2, IsPlayer: true}},
		// The bound chosen number (Ctx.ChosenNumberBound, rules'
		// seedEffectReplCtx shape): the guess that resolved this branch.
		ChosenNumber:      3,
		ChosenNumberBound: true,
	}
	if h.g.Players[2].Life != 20 || h.g.Players[1].Life != 20 {
		t.Fatal("precondition: starting lives are not equal")
	}
	Resolve(h, c, sa)
	if h.g.Players[2].Life != 17 {
		t.Fatalf("remembered opponent's life = %d, want 17 (loses the chosen number 3)", h.g.Players[2].Life)
	}
	if h.g.Players[1].Life != 20 {
		t.Fatalf("source controller's life = %d, want unchanged at 20", h.g.Players[1].Life)
	}
	if h.g.Players[0].Life != 20 {
		t.Fatalf("unrelated seat's life = %d, want unchanged at 20", h.g.Players[0].Life)
	}
}
