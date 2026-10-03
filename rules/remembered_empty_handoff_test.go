package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// emptyHandoffSrc is a synthetic ETB trigger whose stack object's Remembered
// is the trigger capture (the entering card itself). Its coin-flip branch --
// the same body either way -- clears the chain's Remembered and then asks
// (ChoosePlayer), so the resolution suspends with two continuation frames on
// one Ctx: the branch's rest and the FlipCoin's own SubAbility$. The second
// reads Defined$ Remembered. No corpus card is needed to reach the shape;
// the defect is the handoff, not a card.
const emptyHandoffSrc = `Name:Empty Handoff Probe
ManaCost:2
Types:Artifact Creature
PT:1/1
T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigFlip | TriggerDescription$ x
SVar:TrigFlip:DB$ FlipCoin | WinSubAbility$ DBClear | LoseSubAbility$ DBClear | SubAbility$ DBCounter
SVar:DBClear:DB$ Cleanup | ClearRemembered$ True | SubAbility$ DBAsk
SVar:DBAsk:DB$ ChoosePlayer
SVar:DBCounter:DB$ PutCounter | Defined$ Remembered | CounterType$ P1P1 | CounterNum$ 1
Oracle:x
`

// TestEmptyRememberedHandoffIsAHandoff: an EMPTY Remembered is still a
// Remembered, at both halves of a suspension. The ask's resume point must
// carry the chain's cleared set (resolution_ask.go copied it to nil, so the
// answered frame rebuilt the stack object's seed), and a completed frame must
// hand its empty set to the next frame on its Ctx (resolution.go handed nil
// to every frame but a Repeat loop frame). Before the fix the FlipCoin's Sub
// fell back to the stale trigger capture -- the probe itself -- and put a
// +1/+1 counter on it although the chain had cleared it.
func TestEmptyRememberedHandoffIsAHandoff(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	probe := card(t, emptyHandoffSrc)
	e, cfg := flipEngine(t, reg, 5, []*cards.Card{probe}, []*cards.Card{})
	id := moveByName(t, e, 0, "Empty Handoff Probe", state.ZBattlefield)
	e.priorityRound()
	asks := 0
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			castFirst(t, e, "pass")
			continue
		}
		asks++
		submitChoices(t, e, d.Options[0].Index)
	}
	if asks != 1 {
		t.Fatalf("the ChoosePlayer body asked %d times, want 1 (the suspension this test needs)", asks)
	}
	if n := len(e.G.Obj(id).Counters); n != 0 {
		t.Fatalf("the probe has counters %+v: Defined$ Remembered read the stale trigger capture after the chain cleared it", e.G.Obj(id).Counters)
	}
	replayCheck(t, e, cfg)
}
