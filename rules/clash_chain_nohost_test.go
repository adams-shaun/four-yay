package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// clashChainSource is a real battlefield permanent whose face defines the
// chained clash SVars. A fixture must live on the object's face (not in a
// hand-built Ctx.SVars): a suspension rebuilds the resumed Ctx from the
// resolving object's face table (rules/resolution.go's SetSVars), so an
// in-memory table would not survive the first answer and the nested clash
// would never resolve.
func clashChainSource(t *testing.T, e *Engine) (state.ObjID, *cards.SA) {
	t.Helper()
	const src = "Name:Clash Chain Fixture\nManaCost:1 G\nTypes:Creature Beast\nPT:1/1\n" +
		"SVar:Chain:DB$ Clash | WinSubAbility$ Second\nSVar:Second:DB$ Clash\nOracle:x\n"
	id := onBoardCard(t, e, 0, card(t, src))
	sa := resolveSourceFaceSA(t, e, id, "Chain")
	if sa == nil || sa.API != "Clash" || sa.Params["WinSubAbility"] != "Second" {
		t.Fatalf("chain fixture precondition: SA=%+v, want Clash with WinSubAbility Second", sa)
	}
	return id, sa
}

// answerClashPlacement answers exactly one pending clash placement ask,
// asserting it routes to wantPlayer with the bottom-then-top option shape.
func answerClashPlacement(t *testing.T, e *Engine, wantPlayer state.PlayerID, want string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.ResumeKind != "clash_placement" || d.Kind != decision.KChoose || d.Player != wantPlayer || len(d.Options) != 2 ||
		d.Options[0].Kind != "bottom" || d.Options[1].Kind != "top" {
		t.Fatalf("expected clash placement ask for seat %d with bottom then top, got %+v", wantPlayer, d)
	}
	selected := -1
	for _, option := range d.Options {
		if option.Kind == want {
			selected = option.Index
		}
	}
	if selected < 0 {
		t.Fatalf("placement options = %+v, missing %q", d.Options, want)
	}
	submitChoices(t, e, selected)
}

// clashSoloSA is a single parsed `DB$ Clash` ability with no outcome branch,
// for fixtures that need exactly one clash and no suspension.
func clashSoloSA() *cards.SA {
	sa := cards.ResolveSVar(map[string]string{"Solo": "DB$ Clash"}, "Solo")
	if sa == nil || sa.API != "Clash" {
		panic("clash solo fixture failed to compile")
	}
	return sa
}

// TestClashNoHostPutsBothCardsOnBottom pins the R-9 no-host degradation: a
// host that refuses every ask puts each revealed card on the BOTTOM
// deterministically and records an explicit no-host Note per participant.
// The feature's handler really ran (no unimplemented-API Note), and the
// stand-in is event-backed (one LibraryOrder per library), never a direct
// game write.
func TestClashNoHostPutsBothCardsOnBottom(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	marvo := onBoardCard(t, e, 0, unblockedCorpusCard(t, "m/marvo_deep_operative.txt"))
	e.G.Obj(marvo).SummonSick = false

	f0 := putTopOfLibrary(t, e, card(t, "Name:Seat Zero Filler\nManaCost:2\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"), 0)
	high := putTopOfLibrary(t, e, card(t, "Name:No Host High\nManaCost:5\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"), 0)
	f1 := putTopOfLibrary(t, e, card(t, "Name:Seat One Filler\nManaCost:3\nTypes:Creature Beast\nPT:3/3\nOracle:x\n"), 1)
	low := putTopOfLibrary(t, e, card(t, "Name:No Host Low\nManaCost:0\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 1)

	if lib0 := e.G.Zone(state.ZLibrary, 0); len(lib0) < 2 || lib0[0] != high || lib0[1] != f0 {
		t.Fatalf("seat 0 library precondition: %v", lib0)
	}
	if lib1 := e.G.Zone(state.ZLibrary, 1); len(lib1) < 2 || lib1[0] != low || lib1[1] != f1 {
		t.Fatalf("seat 1 library precondition: %v", lib1)
	}

	effects.Resolve(noAskHost{e}, &effects.Ctx{Source: marvo, Controller: 0}, clashSoloSA())

	if d := e.Pending(); d != nil && d.ResumeKind == "clash_placement" {
		t.Fatalf("no-host run posed a placement ask: %+v", d)
	}
	noteCount := 0
	orderCount := map[state.PlayerID]int{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Clash placement: no decision host") {
			noteCount++
		}
		if ev.Kind == events.LibraryOrder {
			orderCount[ev.Player]++
		}
	}
	if noteCount != 2 {
		t.Fatalf("no-host placement notes = %d, want one per participant", noteCount)
	}
	if orderCount[0] != 1 || orderCount[1] != 1 {
		t.Fatalf("no-host LibraryOrders = %v, want one per library", orderCount)
	}
	if lib0 := e.G.Zone(state.ZLibrary, 0); lib0[len(lib0)-1] != high || lib0[0] != f0 {
		t.Fatalf("seat 0 no-host order = %v, want revealed %d on the bottom", lib0, high)
	}
	if lib1 := e.G.Zone(state.ZLibrary, 1); lib1[len(lib1)-1] != low || lib1[0] != f1 {
		t.Fatalf("seat 1 no-host order = %v, want revealed %d on the bottom", lib1, low)
	}
	if hasNote(e, "unimplemented API Clash") {
		t.Fatal("Clash handler was not registered")
	}
}
