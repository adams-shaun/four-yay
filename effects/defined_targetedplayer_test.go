package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func corpusAbility(t *testing.T, name, api string) (*cards.Card, *cards.SA) {
	t.Helper()
	card, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok || len(card.Faces) == 0 {
		t.Fatalf("corpus card %q missing", name)
	}
	for _, ability := range card.Faces[0].Abilities {
		for current := ability; current != nil; current = current.Sub {
			if current.API == api {
				return card, current
			}
		}
	}
	t.Fatalf("corpus card %q has no %s ability", name, api)
	return nil, nil
}

func TestCeaseDrawsForItsTargetedPlayer(t *testing.T) {
	card, life := corpusAbility(t, "Cease", "GainLife")
	h := newHost(t, 2)
	h.g.Players[1].Life = 20
	source := h.g.AddObject(card, 0)
	rootTarget := h.g.AddObject(mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	rootTarget.Zone = state.ZGraveyard
	libraryCard := h.g.AddObject(mkCard(t, "Name:Wastes\nTypes:Basic Land\nOracle:x\n"), 1)
	libraryCard.Zone = state.ZLibrary
	h.g.SetZone(state.ZLibrary, 1, []state.ObjID{libraryCard.ID})
	if source.ID == 0 || rootTarget.Zone != state.ZGraveyard || len(h.g.Zone(state.ZLibrary, 1)) != 1 {
		t.Fatal("precondition: Cease source, p0 graveyard target, and p1 library card must be present")
	}

	// The root's object target is inherited by the untargeted draw link; the
	// GainLife answer is captured by the resolver before its Draw sub-ability.
	ctx := &Ctx{Source: source.ID, Controller: 0,
		Targets:    []state.Target{{Obj: rootTarget.ID}},
		AllTargets: []state.Target{{Obj: rootTarget.ID}, {Player: 1, IsPlayer: true}},
		SubPreAsk:  map[string][]state.Target{life.Line: {{Player: 1, IsPlayer: true}}},
	}
	Resolve(h, ctx, life)
	if h.g.Players[1].Life != 22 {
		t.Fatalf("Cease target life = %d, want 22", h.g.Players[1].Life)
	}
	if len(h.g.Zone(state.ZHand, 1)) != 1 || h.g.Zone(state.ZHand, 1)[0] != libraryCard.ID {
		t.Fatalf("Cease draw gave p1 hand %v, want Wastes %d", h.g.Zone(state.ZHand, 1), libraryCard.ID)
	}
	if len(h.g.Zone(state.ZHand, 0)) != 0 {
		t.Fatalf("Cease draw changed caster hand: %v", h.g.Zone(state.ZHand, 0))
	}
	if got := Defined(h, ctx, sa(t, "DB$ Draw | Defined$ TargetedController")); len(got) != 1 || got[0] != (state.Target{Player: 0, IsPlayer: true}) {
		t.Fatalf("TargetedController changed: got %v, want root object's controller p0", got)
	}
	for _, event := range h.log {
		if event.Kind.String() == "Note" && event.Text == "unimplemented API Draw" {
			t.Fatalf("expected corpus Draw handler to run, log: %+v", h.log)
		}
	}
}

func TestBlightningTargetedPlayerFallsBackToPlaneswalkerController(t *testing.T) {
	_, discard := corpusAbility(t, "Blightning", "Discard")
	h := newHost(t, 2)
	pw := h.g.AddObject(mkCard(t, "Name:Test Planeswalker\nTypes:Planeswalker\nPT:4\nOracle:x\n"), 1)
	pw.Zone = state.ZBattlefield
	p0Card := h.g.AddObject(mkCard(t, "Name:Caster card\nTypes:Sorcery\nOracle:x\n"), 0)
	p1Card := h.g.AddObject(mkCard(t, "Name:Controller card\nTypes:Sorcery\nOracle:x\n"), 1)
	p0Card.Zone, p1Card.Zone = state.ZHand, state.ZHand
	h.g.SetZone(state.ZHand, 0, []state.ObjID{p0Card.ID})
	h.g.SetZone(state.ZHand, 1, []state.ObjID{p1Card.ID})
	if h.g.Obj(pw.ID).Controller != 1 || len(h.g.Zone(state.ZHand, 1)) != 1 {
		t.Fatal("precondition: target planeswalker and discard card must belong to p1")
	}
	ctx := &Ctx{Source: p0Card.ID, Controller: 0, Targets: []state.Target{{Obj: pw.ID}}}
	Resolve(h, ctx, discard)
	if len(h.g.Zone(state.ZHand, 1)) != 0 || len(h.g.Zone(state.ZHand, 0)) != 1 {
		t.Fatalf("Blightning hands after discard: p0=%v p1=%v, want p1's card discarded", h.g.Zone(state.ZHand, 0), h.g.Zone(state.ZHand, 1))
	}
	for _, event := range h.log {
		if event.Kind.String() == "Note" && event.Text == "unimplemented API Discard" {
			t.Fatalf("expected corpus Discard handler to run, log: %+v", h.log)
		}
	}
}
