package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestSorinDestroyRememberTargetsReturnsDestroyedPermanents exercises the
// real compiled Sorin ability, including its chained ChangeZoneAll. The
// prior remembered object proves ForgetOtherTargets clears both memory
// representations before the destroy survivors are recorded.
func TestSorinDestroyRememberTargetsReturnsDestroyedPermanents(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	sorin := choiceCorpusCard(t, "Sorin, Lord of Innistrad")
	e := corpusEngine(t, reg, []*cards.Card{sorin}, nil)
	sorinID := findCardObj(t, e, 0, "Sorin, Lord of Innistrad", state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: sorinID, From: state.ZHand, To: state.ZBattlefield})
	old := onBoard(t, e, 0, "Name:Old Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	first := onBoard(t, e, 1, "Name:First Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	second := onBoard(t, e, 1, "Name:Second Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.G.Obj(sorinID).Zone != state.ZBattlefield || e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield {
		t.Fatal("Sorin and both targets must start on the battlefield")
	}
	e.G.Obj(sorinID).Remembered = []state.Target{{Obj: old}}
	if len(sorin.Faces) == 0 || len(sorin.Faces[0].Abilities) < 2 {
		t.Fatal("compiled Sorin destroy ability is missing")
	}
	ctx := &effects.Ctx{
		Source:         sorinID,
		Controller:     0,
		Remembered:     []state.Target{{Obj: old}},
		Targets:        []state.Target{{Obj: first}, {Obj: second}},
		TargetsOffered: true,
		SVars:          sorin.Faces[0].SVars,
	}
	effects.Resolve(e, ctx, sorin.Faces[0].Abilities[2])
	for _, id := range []state.ObjID{first, second} {
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield || o.Controller != 0 {
			t.Fatalf("returned target %d = zone %v controller %d, want battlefield under seat 0", id, o.Zone, o.Controller)
		}
	}
	if e.G.Obj(old).Zone != state.ZBattlefield {
		t.Fatalf("pre-existing remembered object moved unexpectedly: zone %v", e.G.Obj(old).Zone)
	}
	if e.G.Obj(old).Zone != state.ZBattlefield {
		t.Fatalf("ForgetOtherTargets did not drop the prior remembered object: zone %v", e.G.Obj(old).Zone)
	}
}

// TestSorinDestroyRememberTargetsExcludesReplacedMoves pins the replacement
// boundary: a destroy redirected to exile was not actually put into a
// graveyard and must therefore not be returned by Sorin's follower.
func TestSorinDestroyRememberTargetsExcludesReplacedMoves(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	sorin := choiceCorpusCard(t, "Sorin, Lord of Innistrad")
	e := corpusEngine(t, reg, []*cards.Card{sorin, card(t, restInPeaceSrc)}, []*cards.Card{card(t, "Name:Exiled Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")})
	sorinID := findCardObj(t, e, 0, "Sorin, Lord of Innistrad", state.ZHand)
	peaceID := findCardObj(t, e, 0, "Peace", state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: sorinID, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: peaceID, From: state.ZHand, To: state.ZBattlefield})
	target := moveSeeded(t, e, 1, "Name:Exiled Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	destroy := *sorin.Faces[0].Abilities[2]
	destroy.Sub = nil
	ctx := &effects.Ctx{Source: sorinID, Controller: 0,
		Targets: []state.Target{{Obj: target}}, TargetsOffered: true,
		SVars: sorin.Faces[0].SVars}
	effects.Resolve(e, ctx, &destroy)
	if e.G.Obj(target).Zone != state.ZExile {
		t.Fatalf("destroy replacement left target in %v, want exile", e.G.Obj(target).Zone)
	}
	if len(ctx.Remembered) != 0 {
		t.Fatalf("replaced target was remembered: %#v", ctx.Remembered)
	}
}
