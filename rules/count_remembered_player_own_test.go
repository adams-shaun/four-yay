package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestSingularityRuptureRememberedPlayerOwn(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	card := searchCorpusCard(t, reg, "Singularity Rupture")
	var spell *cards.SA
	for _, face := range card.Faces {
		for _, ability := range face.Abilities {
			for node := ability; node != nil; node = node.Sub {
				if node.API == "RepeatEach" {
					spell = ability
				}
			}
		}
	}
	if spell == nil {
		t.Fatal("corpus precondition: Singularity Rupture spell chain does not contain RepeatEach")
	}
	e := corpusEngineThree(t, reg, nil, nil, nil)
	for _, seat := range []struct {
		player state.PlayerID
		size   int
	}{{1, 5}, {2, 3}} {
		ids := e.G.Zone(state.ZLibrary, seat.player)
		if len(ids) < seat.size {
			t.Fatalf("precondition: seat %d library has %d cards, need %d", seat.player, len(ids), seat.size)
		}
		for _, id := range ids[seat.size:] {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
		}
	}
	// A seat-1-owned card controlled by seat 0 distinguishes ownership from
	// control while still being in the library scanned by Count$ValidLibrary.
	seat1Lib := e.G.Zone(state.ZLibrary, 1)
	stolen := e.G.Obj(seat1Lib[0])
	e.emit(events.Event{Kind: events.ControlChange, Obj: stolen.ID, Player: 0})
	if stolen.Zone != state.ZLibrary || stolen.Owner != 1 || stolen.Controller != 0 {
		t.Fatalf("precondition: distinguishing library card = %+v", stolen)
	}
	remembered := effects.SpecContext{Resolving: true,
		Remembered: []state.Target{{IsPlayer: true, Player: 1}}}
	if unknown := effects.UnknownPredicates("Card.RememberedPlayerOwn"); len(unknown) != 0 {
		t.Fatalf("RememberedPlayerOwn unexpectedly unknown: %v", unknown)
	}
	if !effects.MatchesObjectCtx(e.G, "Card.RememberedPlayerOwn", stolen, remembered) {
		t.Fatal("RememberedPlayerOwn must match owner despite a different controller")
	}
	if effects.MatchesObjectCtx(e.G, "Card.RememberedPlayerOwn", stolen, effects.SpecContext{Resolving: true}) {
		t.Fatal("RememberedPlayerOwn must fail closed with no remembered player")
	}
	if effects.MatchesObjectCtx(e.G, "Card.!RememberedPlayerOwn", stolen, remembered) {
		t.Fatal("negated RememberedPlayerOwn must reject a remembered player's owner")
	}
	for _, seat := range []struct {
		player state.PlayerID
		size   int
	}{{1, 5}, {2, 3}} {
		if got := len(e.G.Zone(state.ZLibrary, seat.player)); got != seat.size {
			t.Fatalf("precondition: seat %d library size=%d, want %d", seat.player, got, seat.size)
		}
	}
	source := e.G.AddObject(card, 0)
	ctx := &effects.Ctx{Host: e, Source: source.ID, Controller: 0,
		Targets:        []state.Target{{IsPlayer: true, Player: 1}, {IsPlayer: true, Player: 2}},
		TargetsOffered: true, SVars: card.Faces[0].SVars}
	// Resolve the corpus spell ability through the normal effects kernel.
	// Its RepeatEach loop binds each target as the remembered player.
	effects.Resolve(e, ctx, spell)
	milledByOwner := map[state.PlayerID]int{}
	for _, ev := range e.L.Events {
		if events.IsMill(ev) {
			o := e.G.Obj(ev.Obj)
			if o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("milled object %d is not in its owner's graveyard: %+v", ev.Obj, o)
			}
			milledByOwner[o.Owner]++
		}
	}
	// Five and three cards produce distinct floor-half outcomes, 2 and 1;
	// the old unrecognised filter produces zero for both players.
	if milledByOwner[1] != 2 || milledByOwner[2] != 1 || milledByOwner[0] != 0 {
		t.Fatalf("Singularity Rupture mills by owner = %v, want seat 1:2, seat 2:1, seat 0:0", milledByOwner)
	}
}
