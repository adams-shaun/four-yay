package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// twopilesBoard builds a 2-seat game with five cards on seat 0's library
// (ids[0] on top), a source object, and the FoF-shaped SVar table the pile
// bodies resolve through: DBHand/DBGrave are exactly Fact or Fiction's real
// sub-abilities, DBBottom Jace, Architect of Thought's ChangeZoneAll tail.
// Returns the host, the five library ids, and the source id (ids[5]).
func twopilesBoard(t *testing.T) (*fakeHost, []state.ObjID) {
	t.Helper()
	h := newHost(t, 2)
	var ids []state.ObjID
	cards := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Source"}
	for i, n := range cards {
		_ = i
		c := mkCard(t, "Name:"+n+"\nTypes:Creature\nPT:1/1\nOracle:x\n")
		ids = append(ids, h.g.AddObject(c, 0).ID)
	}
	lib := append([]state.ObjID(nil), ids[:5]...)
	h.g.SetZone(state.ZLibrary, 0, lib)
	for _, id := range ids[:5] {
		h.g.Obj(id).Zone = state.ZLibrary
	}
	return h, ids
}

// twopilesSVars is the SVar table the FoF-shaped TwoPiles SA resolves its
// pile bodies through.
func twopilesSVars() map[string]string {
	return map[string]string{
		"DBHand":  "DB$ ChangeZone | Defined$ Remembered | Origin$ Library | Destination$ Hand",
		"DBGrave": "DB$ ChangeZone | Defined$ Remembered | Origin$ Library | Destination$ Graveyard",
	}
}

// twopilesSA parses the exact FoF TwoPiles sub-ability.
func twopilesSA(t *testing.T) *cards.SA {
	t.Helper()
	src := "Name:FoF\nTypes:Instant\nOracle:x\n" +
		"SVar:DBTwoPiles:DB$ TwoPiles | Defined$ You | DefinedCards$ Remembered | Separator$ Opponent | ChosenPile$ DBHand | UnchosenPile$ DBGrave\n" +
		"SVar:DBHand:DB$ ChangeZone | Defined$ Remembered | Origin$ Library | Destination$ Hand\n" +
		"SVar:DBGrave:DB$ ChangeZone | Defined$ Remembered | Origin$ Library | Destination$ Graveyard\n"
	c, d := cards.ParseBytes("twopiles.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	f := c.Faces[0]
	sa := cards.ResolveSVar(f.SVars, "DBTwoPiles")
	if sa == nil {
		t.Fatalf("DBTwoPiles did not resolve")
	}
	return sa
}

// twopilesRemembered is the ctx-level Remembered the upstream
// PeekAndReveal's RememberRevealed$ leaves (a fresh backing slice — the
// aliasing hazard the pile sub-Ctx must not write into).
func twopilesRemembered(ids []state.ObjID) []state.Target {
	out := make([]state.Target, 0, len(ids))
	for _, id := range ids {
		out = append(out, state.Target{Obj: id})
	}
	return out
}

// TestTwoPilesEmptyCardSetIsSilent pins the fail-to-find shape: an empty
// card set asks nothing, emits nothing, and the chain continues.
func TestTwoPilesEmptyCardSetIsSilent(t *testing.T) {
	h, ids := twopilesBoard(t)
	sh := &suspendHost{fakeHost: *h}
	ctx := &Ctx{Controller: 0, Source: ids[5], SVars: twopilesSVars()}
	sa := twopilesSA(t)
	Resolve(sh, ctx, sa)
	if sh.suspended || sh.asked != nil {
		t.Fatal("an empty card set posed a decision")
	}
	if len(sh.log) != 0 {
		t.Fatalf("an empty card set emitted %+v, want nothing", sh.log)
	}
}

// TestTwoPilesNoHostStandIn pins the R-9 stand-in: with a host that cannot
// ask, pile A is the FIRST card of the set, the chooser takes pile A, no
// Note records it, and nothing wedges.
func TestTwoPilesNoHostStandIn(t *testing.T) {
	h, ids := twopilesBoard(t)
	ctx := &Ctx{Controller: 0, Source: ids[5], Remembered: twopilesRemembered(ids[:5]), SVars: twopilesSVars()}
	sa := twopilesSA(t)
	Resolve(h, ctx, sa) // fakeHost.Ask returns false: no host
	if z := h.g.Obj(ids[0]).Zone; z != state.ZHand {
		t.Fatalf("pile-A card zone = %d, want hand (the stand-in takes pile A)", z)
	}
	for _, id := range ids[1:5] {
		if z := h.g.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("pile-B card %d zone = %d, want graveyard", id, z)
		}
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Fatalf("the no-host stand-in recorded a Note: %+v", e)
		}
	}
}

// TestTwoPilesExoticShapeIsLoudAndInert pins the scope boundary: a line
// carrying an unread parameter (Raging River's LeftRightPile$) emits exactly
// ONE loud Note naming it and moves nothing.
func TestTwoPilesExoticShapeIsLoudAndInert(t *testing.T) {
	h, ids := twopilesBoard(t)
	sh := &suspendHost{fakeHost: *h}
	svars := twopilesSVars()
	svars["DBDefLeftPile"] = "DB$ ChangeZone | Defined$ Remembered | Destination$ Graveyard"
	svars["DBDefRightPile"] = "DB$ ChangeZone | Defined$ Remembered | Destination$ Hand"
	ctx := &Ctx{Controller: 0, Source: ids[5], Remembered: twopilesRemembered(ids[:5]), SVars: svars}
	sa := sa(t, "DB$ TwoPiles | Defined$ Remembered | Separator$ Remembered | Zone$ Battlefield | LeftRightPile$ True | ChosenPile$ DBDefLeftPile | UnchosenPile$ DBDefRightPile")
	Resolve(sh, ctx, sa)
	if sh.suspended {
		t.Fatal("an exotic shape posed a decision")
	}
	notes := 0
	for _, e := range sh.log {
		if e.Kind == events.Note {
			notes++
			if e.Text == "" || !(strings.Contains(e.Text, "Zone$") || strings.Contains(e.Text, "LeftRightPile")) {
				t.Fatalf("the Note names the wrong param: %q", e.Text)
			}
		}
		if e.Kind == events.MoveZone {
			t.Fatalf("the exotic shape moved a card: %+v", e)
		}
	}
	if notes != 1 {
		t.Fatalf("notes = %d, want exactly one naming LeftRightPile$", notes)
	}
	for _, id := range ids[:5] {
		if z := h.g.Obj(id).Zone; z != state.ZLibrary {
			t.Fatalf("card %d moved (zone %d), want the library", id, z)
		}
	}
}

// TestTwoPilesNonSVarPileValueIsLoud pins the other exotic one-off: a
// ChosenPile$ value that is not an SVar name (TurnFaceUp/ToHand/ToGrave)
// emits one Note and moves nothing.
func TestTwoPilesNonSVarPileValueIsLoud(t *testing.T) {
	h, ids := twopilesBoard(t)
	sh := &suspendHost{fakeHost: *h}
	ctx := &Ctx{Controller: 0, Source: ids[5], Remembered: twopilesRemembered(ids[:5]), SVars: twopilesSVars()}
	sa := sa(t, "DB$ TwoPiles | Defined$ You | DefinedCards$ Remembered | ChosenPile$ ToHand | UnchosenPile$ ToGrave")
	Resolve(sh, ctx, sa)
	if sh.suspended {
		t.Fatal("a non-SVar pile value posed a decision")
	}
	notes := 0
	for _, e := range sh.log {
		if e.Kind == events.Note {
			notes++
			if !strings.Contains(e.Text, "ChosenPile$ ToHand") {
				t.Fatalf("the Note names the wrong param: %q", e.Text)
			}
		}
	}
	if notes != 1 {
		t.Fatalf("notes = %d, want exactly one", notes)
	}
}
