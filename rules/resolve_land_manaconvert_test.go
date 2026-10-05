package rules

// Land plays drive the ordinary cast flow but pay no spell mana cost. An
// Optional$ ManaConvert static must neither offer an election nor make the
// land-play may-ask predicate report a possible ask.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// activateNorthStarConvert resolves North Star's AB$ Effect so its
// Mode$ ManaConvert | Optional$ True static is on the board, asserting the
// real SVar text the predicate reads.
func activateNorthStarConvert(t *testing.T, e *Engine, star state.ObjID) {
	t.Helper()
	face := e.G.Obj(star).Face()
	if face == nil || len(face.Abilities) == 0 {
		t.Fatal("precondition: North Star has no ability to resolve")
	}
	effects.Resolve(e, &effects.Ctx{Source: star, Controller: 0, SVars: face.SVars}, face.Abilities[0])
	if !strings.Contains(e.G.Obj(star).Face().SVars["Convert"], "ManaConversion$ AnyType->AnyType") {
		t.Fatal("precondition: North Star's Convert SVar changed")
	}
}

// TestLandPlayMayAskUnderOptionalManaConvert pins both the predicate and the
// real land-play flow under North Star's Optional$ conversion: no election,
// and the land enters the battlefield.
func TestLandPlayMayAskUnderOptionalManaConvert(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{manaConvertCard(t, reg, "North Star"), manaConvertCard(t, reg, "Mountain")}, nil)
	star := moveByName(t, e, 0, "North Star", state.ZBattlefield)
	land := moveByName(t, e, 0, "Mountain", state.ZHand)
	if e.G.Obj(star).Zone != state.ZBattlefield || e.G.Obj(land).Zone != state.ZHand {
		t.Fatalf("precondition: North Star in %s, Mountain in %s", e.G.Obj(star).Zone, e.G.Obj(land).Zone)
	}

	// Control: a plain Mountain (no entry ask, no Moved replacement on the
	// board) is ask-free, so the gate below is what moves the predicate.
	if tapeLandMayAsk(e, 0, land) {
		t.Fatal("precondition: a plain Mountain's land play already may ask")
	}

	activateNorthStarConvert(t, e, star)
	if _, opt := e.manaConversionParts(0, land, false); opt.Empty() {
		t.Fatal("precondition: North Star grants no Optional$ conversion for the land play")
	}
	if tapeLandMayAsk(e, 0, land) {
		t.Fatal("tapeLandMayAsk = true solely because of an Optional$ ManaConvert static")
	}

	// Even though the conversion matches the land, land play pays no spell
	// mana cost and must complete without presenting its inert election.
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("precondition: expected seat 0's priority, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == land {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("precondition: no play_land option for the Mountain: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	if e.G.Obj(land).Zone != state.ZBattlefield {
		t.Fatalf("land play did not complete: Mountain remains in %s", e.G.Obj(land).Zone)
	}
	if ask := e.Pending(); ask != nil && ask.Kind == decision.KChoose && ask.Prompt == "Use optional mana conversion?" {
		t.Fatalf("land play posed the Optional$ ManaConvert election: %+v", ask)
	}
}

// TestLandPlayMayAskReadsTheIntentsPlayer confirms the land-play predicate
// stays ask-free for a seat-1 land even when only that seat controls North Star.
func TestLandPlayMayAskReadsTheIntentsPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, nil,
		[]*cards.Card{manaConvertCard(t, reg, "North Star"), manaConvertCard(t, reg, "Mountain")})
	star := moveByName(t, e, 1, "North Star", state.ZBattlefield)
	land := moveByName(t, e, 1, "Mountain", state.ZHand)
	face := e.G.Obj(star).Face()
	effects.Resolve(e, &effects.Ctx{Source: star, Controller: 1, SVars: face.SVars}, face.Abilities[0])
	if _, opt := e.manaConversionParts(1, land, false); opt.Empty() {
		t.Fatal("precondition: North Star grants seat 1 no Optional$ conversion")
	}
	d := &decision.Decision{Player: 1, Kind: decision.KPriority,
		Options: []decision.Option{{Index: 0, Kind: "play_land", Obj: land}}}
	if (*resolveBoard)(e).MayAsk(d, decision.Intent{Player: 1, Choices: []int{0}}) {
		t.Fatal("MayAsk = true for seat 1's land play solely under its own Optional$ ManaConvert static")
	}
}
