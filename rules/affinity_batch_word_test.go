package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestHellspurBruteAffinityOutlaw(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hellspur := lookup(t, reg, "Hellspur Brute")
	pirateCard := lookup(t, reg, "Kitesail Freebooter")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{hellspur, pirateCard}, nil)

	brute := moveByName(t, e, 0, "Hellspur Brute", state.ZHand)
	bruteObj := e.G.Obj(brute)
	if bruteObj == nil || bruteObj.Zone != state.ZHand || bruteObj.Face() == nil || !bruteObj.Face().HasKeyword("Affinity") {
		t.Fatalf("precondition: real Hellspur Brute must be in hand and have Affinity: %+v", bruteObj)
	}
	if got := reduceOf(t, e, 0, brute); got != 0 {
		t.Fatalf("precondition: no outlaw on battlefield should reduce by 0, got %d", got)
	}

	pirate := moveByName(t, e, 0, "Kitesail Freebooter", state.ZBattlefield)
	pirateObj := e.G.Obj(pirate)
	if pirateObj == nil || pirateObj.Zone != state.ZBattlefield || !slices.Contains(pirateObj.Face().Types, "Pirate") {
		t.Fatalf("precondition: Kitesail Freebooter must be a battlefield Pirate: %+v", pirateObj)
	}
	if got := reduceOf(t, e, 0, brute); got != 1 {
		t.Fatalf("one outlaw should reduce Hellspur Brute by 1, got %d", got)
	}
	replayCheck(t, e, cfg)
}
