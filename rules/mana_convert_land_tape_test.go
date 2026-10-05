package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A land play starts a tape resolution but is not casting a spell (CR 601.2).
// North Star's Optional$ conversion applies to a spell's payment, not to
// playing a land; posing its election here also defeats the tape predicate.
func TestLandPlayIgnoresOptionalSpellManaConversion(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{manaConvertCard(t, reg, "North Star"), manaConvertCard(t, reg, "Plains")}, nil)
	star := moveByName(t, e, 0, "North Star", state.ZBattlefield)
	land := moveByName(t, e, 0, "Plains", state.ZHand)
	face := e.G.Obj(star).Face()
	effects.Resolve(e, &effects.Ctx{Source: star, Controller: 0, SVars: face.SVars}, face.Abilities[0])
	if _, optional := e.manaConversionParts(0, land, false); optional.Empty() {
		t.Fatal("precondition: North Star's optional spell conversion did not reach the unfiltered cast payment path")
	}
	e.priorityRound()
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || optionKinds(d)["play_land"] == 0 {
		t.Fatalf("precondition: land not playable: %+v", d)
	}
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "play_land" && o.Obj == land {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("precondition: chosen Plains not in land options: %+v", e.Pending())
	}
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatal(err)
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.Prompt == "Use optional mana conversion?" {
		t.Fatalf("land play offered a spell-payment election: %+v", d)
	}
	if got := e.G.Obj(land).Zone; got != state.ZBattlefield {
		t.Fatalf("land play did not finish: zone %s, pending=%+v", got, e.Pending())
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.LandPlayed && ev.Player == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("land played no LandPlayed event (test would pass if cast handler were skipped)")
	}
}
