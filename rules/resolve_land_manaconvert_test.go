package rules

// The land-play ask-free predicate's optional-mana-conversion gate. A land
// play drives the ordinary cast flow (legal_priority.go optPlayLand ->
// continueCast -> manaConvertAsk -> pay.ManaConvertAsk), so a board with an
// active Optional$ ManaConvert static (North Star) makes the play pose a real
// election. tapeLandMayAsk must say "may ask" for it; otherwise the cardfuzz
// predicate-miss census (cmd/cardfuzz TestTapeMissCensus) catches a land play
// exempted with no checkpoint and reports
// `nostack -> choose "Use optional mana conversion?"`.

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

// TestLandPlayMayAskUnderOptionalManaConvert pins the predicate and the real
// decision it protects: a land play under North Star's Optional$ conversion
// must be a may-ask (checkpointed) and must pose the ManaConvert election.
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
	if !tapeLandMayAsk(e, 0, land) {
		t.Fatal("tapeLandMayAsk = false under an active Optional$ ManaConvert static; the land play reaches ManaConvertAsk through continueCast, so the predicate must say may-ask")
	}

	// The predicate claims a decision; prove the claim. Playing the land must
	// pose the ManaConvert election synchronously at priority.
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
	ask := e.Pending()
	if ask == nil || ask.Kind != decision.KChoose || ask.Prompt != "Use optional mana conversion?" {
		t.Fatalf("land play did not pose the Optional$ ManaConvert election: %+v", ask)
	}
}
