package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The Motherlode, Excavator's DBEffect remembers
// `RememberObjects$ TargetedController` -- the controller of the nonbasic land
// its Destroy half targeted, i.e. the defending player its registered
// CantBlockBy restriction's ValidBlocker$ Creature.RememberedPlayerCtrl
// clause reads. Before the case existed, effectRememberedPlayers switched
// only over TargetedPlayer/Targeted, TargetedOrController and the Remembered
// family, so the capture yielded NOTHING and the registered restriction bound
// nobody (rules pinned the dead blocker half end to end on the real card).
// The Black Gate is why ChosenPlayer joins: its DBEffect remembers
// `ChosenPlayer & Targeted`.

func TestEffectRememberedPlayersTargetedController(t *testing.T) {
	h := newHost(t, 3)
	land := mkCard(t, "Name:Reliquary Tower\nTypes:Land\nOracle:x\n")
	landObj := h.g.AddObject(land, 1) // a nonbasic land controlled by seat 1
	if h.g.Obj(landObj.ID) == nil {
		t.Fatal("precondition: targeted land is not in the game")
	}
	if landObj.Controller != 1 {
		t.Fatalf("precondition: land controller = %d, want 1", landObj.Controller)
	}
	c := &Ctx{Source: landObj.ID, Controller: 0}
	sa := sa(t, "DB$ Effect | RememberObjects$ TargetedController")

	// An object target contributes its controller.
	c.PickedTargets = []state.Target{{Obj: landObj.ID}}
	got := effectRememberedPlayers(h, c, sa)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("TargetedController (object target) = %v, want [1] (the land's controller)", got)
	}

	// A player target contributes itself, not nothing.
	c.PickedTargets = []state.Target{{Player: 2, IsPlayer: true}}
	got = effectRememberedPlayers(h, c, sa)
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("TargetedController (player target) = %v, want [2] (the targeted player)", got)
	}

	// A mixed set deduplicates in first-capture order, like the sibling
	// spellings.
	c.PickedTargets = []state.Target{
		{Obj: landObj.ID}, {Player: 2, IsPlayer: true}, {Obj: landObj.ID},
	}
	got = effectRememberedPlayers(h, c, sa)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("TargetedController (mixed) = %v, want [1 2]", got)
	}
}

func TestEffectRememberedPlayersChosenPlayer(t *testing.T) {
	h := newHost(t, 3)
	c := &Ctx{Controller: 0, ChosenValid: true, Chosen: []state.Target{{Player: 2, IsPlayer: true}}}
	sa := sa(t, "DB$ Effect | RememberObjects$ ChosenPlayer")
	got := effectRememberedPlayers(h, c, sa)
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("ChosenPlayer = %v, want [2]", got)
	}
}

func TestEffectRememberedPlayersUnknownSpellingStaysEmpty(t *testing.T) {
	// The fail-closed contract: a remember spelling this helper cannot read
	// contributes no player, so the registered restriction binds nobody
	// rather than guessing.
	h := newHost(t, 2)
	card := mkCard(t, "Name:Remembered Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	obj := h.g.AddObject(card, 1)
	c := &Ctx{Source: obj.ID, Controller: 0, PickedTargets: []state.Target{{Obj: obj.ID}}}
	sa := sa(t, "DB$ Effect | RememberObjects$ ChosenCard")
	if got := effectRememberedPlayers(h, c, sa); len(got) != 0 {
		t.Fatalf("ChosenCard contributed players %v, want none (fail closed)", got)
	}
}
