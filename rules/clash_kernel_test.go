package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestClashChainedInOneContextRevealsFreshKernel: a Clash reached through the
// outer Clash's WinSubAbility$ runs its OWN reveal and poses its OWN owner
// elections. Both clashes answer "top", so each library top is revealed once
// per clash (two reveal Notes per card, two markers per seat) and both
// libraries are untouched.
func TestClashChainedInOneContextRevealsFreshKernel(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	src, sa := clashChainSource(t, e)
	f0 := putTopOfLibrary(t, e, card(t, "Name:Seat Zero Filler\nManaCost:2\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"), 0)
	high := putTopOfLibrary(t, e, card(t, "Name:Outer Winner\nManaCost:5\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"), 0)
	f1 := putTopOfLibrary(t, e, card(t, "Name:Seat One Filler\nManaCost:3\nTypes:Creature Beast\nPT:3/3\nOracle:x\n"), 1)
	low := putTopOfLibrary(t, e, card(t, "Name:Outer Loser\nManaCost:0\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 1)
	if lib0 := e.G.Zone(state.ZLibrary, 0); len(lib0) < 2 || lib0[0] != high || lib0[1] != f0 {
		t.Fatalf("seat 0 library precondition: %v", lib0)
	}
	if lib1 := e.G.Zone(state.ZLibrary, 1); len(lib1) < 2 || lib1[0] != low || lib1[1] != f1 {
		t.Fatalf("seat 1 library precondition: %v", lib1)
	}
	svars := e.G.Obj(src).Face().SVars
	kr4Resolve(e, func() *effects.Ctx { return &effects.Ctx{Source: src, Controller: 0, SVars: svars} }, sa)
	for i := 0; i < 4; i++ {
		answerClashPlacement(t, e, state.PlayerID(i%2), "top")
	}
	revealCount := map[state.ObjID]int{}
	markerCount := map[state.PlayerID]int{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && len(ev.IDs) == 1 {
			revealCount[ev.IDs[0]]++
		}
		if ev.Kind == events.Clash {
			markerCount[ev.Player]++
		}
	}
	if revealCount[high] != 2 || revealCount[low] != 2 {
		t.Fatalf("reveal notes = high:%d low:%d, want 2 each (one per clash)", revealCount[high], revealCount[low])
	}
	if markerCount[0] != 2 || markerCount[1] != 2 {
		t.Fatalf("clash markers = %v, want 2 per seat", markerCount)
	}
	if hasNote(e, "unimplemented API Clash") {
		t.Fatal("Clash handler was not registered")
	}
	if lib0 := e.G.Zone(state.ZLibrary, 0); lib0[0] != high || lib0[1] != f0 {
		t.Fatalf("seat 0 library changed under top elections: %v", lib0)
	}
	if lib1 := e.G.Zone(state.ZLibrary, 1); lib1[0] != low || lib1[1] != f1 {
		t.Fatalf("seat 1 library changed under top elections: %v", lib1)
	}
}

// TestClashPlacementCloneIndependenceKernel clones a real Marvo clash at seat
// 1's outstanding placement election (seat 0 already answered bottom) and
// proves the clone is independent: answering bottom on the clone leaves the
// original's pending ask, library and log head untouched, and the original's
// own top answer ends with a different library and chain head.
func TestClashPlacementCloneIndependenceKernel(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	marvo := onBoardCard(t, e, 0, unblockedCorpusCard(t, "m/marvo_deep_operative.txt"))
	e.G.Obj(marvo).SummonSick = false
	clearLibrary := func(p state.PlayerID) {
		for _, id := range e.G.Zone(state.ZLibrary, p) {
			e.G.Obj(id).Zone = state.ZExile
		}
		e.G.SetZone(state.ZLibrary, p, nil)
	}
	clearLibrary(0)
	clearLibrary(1)
	high := e.G.AddObject(card(t, "Name:Controller clash\nManaCost:5\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"), 0)
	highf := e.G.AddObject(card(t, "Name:Controller filler\nManaCost:1\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 0)
	low := e.G.AddObject(card(t, "Name:Opponent clash\nManaCost:0\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 1)
	lowf := e.G.AddObject(card(t, "Name:Opponent filler\nManaCost:2\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"), 1)
	clashLibraryTwo(t, e, 0, high.ID, highf.ID)
	clashLibraryTwo(t, e, 1, low.ID, lowf.ID)

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{marvo}})
	e.putTriggersOnStack()
	kr4PassToDecision(t, e, 10)
	answerClashPlacement(t, e, 0, "bottom")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "clash_placement" || d.Player != 1 {
		t.Fatalf("expected seat 1's placement ask, got %+v", d)
	}
	if got := e.G.Zone(state.ZLibrary, 0); len(got) != 2 || got[0] != highf.ID || got[1] != high.ID {
		t.Fatalf("seat 0 bottom order = %v, want [%d %d]", got, highf.ID, high.ID)
	}
	origHead := e.L.Head()
	origLib0 := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	origLib1 := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 1)...)

	c := e.Clone()
	cd := c.Pending()
	if cd == nil || cd.ResumeKind != "clash_placement" || cd.Player != 1 {
		t.Fatalf("clone lost the pending placement ask: %+v", cd)
	}
	submitChoices(t, c, clashPlacementSelect(t, cd, "bottom"))
	if got := c.G.Zone(state.ZLibrary, 1); len(got) != 2 || got[0] != lowf.ID || got[1] != low.ID {
		t.Fatalf("clone seat 1 bottom order = %v, want [%d %d]", got, lowf.ID, low.ID)
	}
	od := e.Pending()
	if od == nil || od.ResumeKind != "clash_placement" || od.Player != 1 {
		t.Fatalf("original's pending ask was consumed/changed by the clone: %+v", od)
	}
	if got := e.L.Head(); got != origHead {
		t.Fatalf("original log head moved by answering the clone: %s, want %s", got, origHead)
	}
	if got := e.G.Zone(state.ZLibrary, 0); !reflect.DeepEqual(got, origLib0) {
		t.Fatalf("original seat 0 library changed by the clone: %v, want %v", got, origLib0)
	}
	if got := e.G.Zone(state.ZLibrary, 1); !reflect.DeepEqual(got, origLib1) {
		t.Fatalf("original seat 1 library changed by the clone: %v, want %v", got, origLib1)
	}
	submitChoices(t, e, clashPlacementSelect(t, od, "top"))
	if got := e.G.Zone(state.ZLibrary, 1); len(got) != 2 || got[0] != low.ID || got[1] != lowf.ID {
		t.Fatalf("original seat 1 top order = %v, want [%d %d]", got, low.ID, lowf.ID)
	}
	if e.L.Head() == c.L.Head() {
		t.Fatal("clone and original end with the same chain head despite opposite seat 1 answers")
	}
}

// TestMarvoDeepOperativeClashWinsDrawsAndOffersFreeCastKernel drives the full
// attack -> clash -> win -> draw -> free-cast chain on the real card: the
// placement asks go to seat 0 then seat 1 (bottom, top offered), each top is
// revealed once, seat 0 wins, the Won$ True trigger draws and offers the free
// cast of the MV-2 spell, which resolves onto the battlefield.
func TestMarvoDeepOperativeClashWinsDrawsAndOffersFreeCastKernel(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	marvo := onBoardCard(t, e, 0, unblockedCorpusCard(t, "m/marvo_deep_operative.txt"))
	e.G.Obj(marvo).SummonSick = false
	filler0 := putTopOfLibrary(t, e, card(t, "Name:Seat Zero Filler\nManaCost:2\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"), 0)
	filler1 := putTopOfLibrary(t, e, card(t, "Name:Seat One Filler\nManaCost:3\nTypes:Creature Beast\nPT:3/3\nOracle:x\n"), 1)
	high := putTopOfLibrary(t, e, card(t, "Name:Huge Beast\nManaCost:5 G\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"), 0)
	low := putTopOfLibrary(t, e, card(t, "Name:Tiny Beast\nManaCost:0\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 1)
	freeSpell := e.G.AddObject(card(t, "Name:Free Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	freeSpell.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), freeSpell.ID))
	hand0 := len(e.G.Zone(state.ZHand, 0))

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{marvo}})
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("Marvo's attack trigger was not queued by the declaration")
	}
	kr4PassToDecision(t, e, 10)
	for owner, want := range []string{"top", "bottom"} {
		d := e.Pending()
		if d == nil || d.Player != state.PlayerID(owner) || d.Kind != decision.KChoose || len(d.Options) != 2 || d.Options[0].Kind != "bottom" || d.Options[1].Kind != "top" {
			t.Fatalf("expected placement ask for owner %d with bottom then top, got %+v", owner, d)
		}
		submitChoices(t, e, clashPlacementSelect(t, d, want))
	}
	if hasNote(e, "unimplemented API Clash") {
		t.Fatal("api:Clash resolved through the unregistered fallback Note")
	}
	markerCount := map[state.PlayerID]int{}
	revealCount := map[state.ObjID]int{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Clash {
			markerCount[ev.Player]++
		}
		if ev.Kind == events.Note && len(ev.IDs) == 1 {
			revealCount[ev.IDs[0]]++
		}
	}
	if !hasClashEvent(e, 0, true) || markerCount[0] != 1 {
		t.Fatalf("seat 0 clash markers = %d, want exactly one win", markerCount[0])
	}
	if !hasClashEvent(e, 1, false) || markerCount[1] != 1 {
		t.Fatalf("seat 1 clash markers = %d, want exactly one loss", markerCount[1])
	}
	if revealCount[high] != 1 || revealCount[low] != 1 {
		t.Fatalf("clash reveal notes: high=%d low=%d, want exactly once each", revealCount[high], revealCount[low])
	}
	if lib0 := e.G.Zone(state.ZLibrary, 0); len(lib0) < 2 || lib0[0] != high || lib0[1] != filler0 {
		t.Fatalf("seat 0 top election order = %v", lib0)
	}
	if lib1 := e.G.Zone(state.ZLibrary, 1); len(lib1) < 2 || lib1[len(lib1)-1] != low || lib1[0] != filler1 {
		t.Fatalf("seat 1 bottom election order = %v", lib1)
	}
	d := kr4PassToDecision(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0+1 {
		t.Fatalf("seat 0 hand = %d after winning the clash, want %d", got, hand0+1)
	}
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("expected Marvo's optional free-cast ask, got %+v", d)
	}
	submitChoices(t, e, kr4Option(t, d, freeSpell.ID))
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(freeSpell.ID).Zone; got != state.ZBattlefield {
		t.Fatalf("accepted free-cast spell in %s, want battlefield", got)
	}
}
