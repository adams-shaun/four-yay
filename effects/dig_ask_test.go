package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The dig1 look-and-take tests. The pinned primitives_test.go Dig tests pin
// the NO-CHOICE and no-host behaviour; these pin the ask itself, the look
// Note, the Optional$ Min, the re-entry contract and the strict-supersets
// gate, using the askHost double (primitives_test.go) whose Ask captures the
// posed decision and suspends -- the effects-package stand-in for
// rules.Engine.

// digAskFixture builds seat 0's library as [c0 bear, c1 land, c2 land, c3
// bear] and returns (host, ids).
func digAskFixture(t *testing.T) (*askHost, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(bear, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(bear, 0).ID,
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	return h, ids
}

const digAskSA = "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand"

// TestDigNoHostFallbackKeepsTheFirstEligibleWithANote pins the R-9 fallback:
// a host that cannot answer keeps today's behaviour -- the first ChangeNum
// eligible cards in zone order -- with the Note that records why the richer
// path did not run (after the look Note the ask itself emitted).
func TestDigNoHostFallbackKeepsTheFirstEligibleWithANote(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(bear, 0).ID,
		h.g.AddObject(land, 0).ID,
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	Resolve(h, &Ctx{Controller: 0}, sa(t, digAskSA))
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 1 || hand[0] != ids[0] {
		t.Fatalf("hand = %v, want [%d] (the FIRST eligible card, the deterministic stand-in)", hand, ids[0])
	}
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 2 || lib[0] != ids[1] || lib[1] != ids[2] {
		t.Fatalf("library = %v, want [%d %d]", lib, ids[1], ids[2])
	}
	found := false
	for _, e := range h.log {
		if e.Kind == events.Note && strings.Contains(e.Text, "no engine host to ask") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no fallback Note in %+v", h.log)
	}
}

// TestDigNoHostFallbackNoteNamesTheLibraryOwner is the r2 minor finding: the
// fallback Note is Secret, so it must carry Player = the library's owner;
// without it a Dig of another player's library routed that private note to
// seat 0 (the zero value), the one reader the note must NOT reach.
func TestDigNoHostFallbackNoteNamesTheLibraryOwner(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(land, 1).ID,
		h.g.AddObject(bear, 1).ID,
		h.g.AddObject(land, 1).ID,
	}
	h.g.SetZone(state.ZLibrary, 1, ids)
	Resolve(h, &Ctx{Controller: 1}, sa(t, digAskSA))
	if hand := h.g.Zone(state.ZHand, 1); len(hand) != 1 || hand[0] != ids[0] {
		t.Fatalf("seat 1 hand = %v, want [%d] (the deterministic fallback take)", hand, ids[0])
	}
	var fallback *events.Event
	for i := range h.log {
		e := &h.log[i]
		if e.Kind == events.Note && strings.Contains(e.Text, "no engine host to ask") {
			fallback = e
			break
		}
	}
	if fallback == nil {
		t.Fatalf("no fallback Note in %+v", h.log)
	}
	if !fallback.Secret || fallback.Player != 1 {
		t.Fatalf("fallback Note = %+v, want Secret to the library's owner (seat 1)", fallback)
	}
}

// TestDigLookNoteIsSecretToItsOwner is the multi-seat leaf: a Dig resolved
// against seat 1's library records its look (when it asks) and its take as
// Secret events naming seat 1, never seat 0.
func TestDigLookNoteIsSecretToItsOwner(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(land, 1).ID,
		h.g.AddObject(bear, 1).ID,
		h.g.AddObject(land, 1).ID,
	}
	h.g.SetZone(state.ZLibrary, 1, ids)
	Resolve(h, &Ctx{Controller: 1},
		sa(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand"))
	if h.asked == nil || h.asked.Player != 1 {
		t.Fatalf("decision = %+v, want one posed to seat 1, the library's owner", h.asked)
	}
	for _, e := range h.log {
		switch e.Kind {
		case events.Note:
			if !e.Secret {
				continue
			}
			if e.Player != 1 {
				t.Fatalf("Secret Note has Player %d, want the library's owner (1): %+v", e.Player, e)
			}
		case events.MoveZone:
			if !e.Secret {
				t.Fatalf("Dig MoveZone not Secret: %+v", e)
			}
			if e.Player != 1 {
				t.Fatalf("Secret MoveZone has Player %d, want the library's owner (1): %+v", e.Player, e)
			}
		}
	}
}
