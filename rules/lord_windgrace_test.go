package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Destroy primitive's Ultimate$ read (task
// inbox-paramcensus-final-stragglers, Lord Windgrace entry). The census
// verified what Ultimate$ gates and the answer is: nothing rules-side -- in
// Forge the key is read only by the AI's ability ranking and the achievement
// tracker (forge-ai's ComputerUtilAbility/ComputerUtilCard and forge-game's
// AchievementTracker), so the engine's job is the census's ignoredParamKeys
// classification, and a REAL CARD PIN of the ability the flag decorates: a
// planeswalker's -11 destroy-everything ultimate must resolve exactly as its
// script says, with the Ultimate$ marker inert.

// windgraceSrc is Lord Windgrace's real script shape (the -11 ability and
// its token sub verbatim, no corpus dependency).
const windgraceSrc = "Name:Lord Windgrace\nManaCost:2 B R G\nTypes:Legendary Planeswalker Windgrace\nLoyalty:5\n" +
	"A:AB$ Destroy | Cost$ SubCounter<11/LOYALTY> | Planeswalker$ True | Ultimate$ True | ValidTgts$ Permanent.nonLand | TgtPrompt$ Select target nonland permanent | TargetMin$ 0 | TargetMax$ 6 | SubAbility$ DBToken | SpellDescription$ Destroy up to six target nonland permanents, then create six 2/2 green Cat Warrior creature tokens with forestwalk.\n" +
	"SVar:DBToken:DB$ Token | TokenAmount$ 6 | TokenOwner$ You | TokenScript$ g_2_2_cat_warrior_forestwalk\nOracle:x\n"

// windgraceTokenSrc is the g_2_2_cat_warrior_forestwalk token's shape; the
// fixture registers it under the exact TokenScript$ stem the ability names.
const windgraceTokenSrc = "Name:Cat Warrior\nManaCost:2 G\nTypes:Token Creature Cat Warrior\nPT:2/2\nK:Forestwalk\nOracle:x\n"

func windgraceEngine(t *testing.T) *Engine {
	t.Helper()
	cfg := seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{"g_2_2_cat_warrior_forestwalk": card(t, windgraceTokenSrc)}})
	e := New(cfg)
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	e.priorityRound()
	return e
}

// TestLordWindgraceUltimateDestroysNonlands pins the -11 end to end: the
// loyalty ability is offered once the walker holds enough loyalty, the
// 0..6-target ask admits nonland permanents but never a land, answering
// with two targets destroys exactly those two and creates the six Cat
// Warrior tokens, and the walker's own loyalty drops by the cost.
func TestLordWindgraceUltimateDestroysNonlands(t *testing.T) {
	t.Parallel()
	e := windgraceEngine(t)
	walker := onBoard(t, e, 0, windgraceSrc)
	// onBoard bypasses events, so the printed starting loyalty never applied:
	// +11 lands the walker on exactly the cost.
	e.emit(events.Event{Kind: events.CounterChange, Obj: walker, Counter: "LOYALTY", Amount: 11})
	bear0 := onBoard(t, e, 0, "Name:Bear\nTypes:Creature\nPT:1/1\nOracle:x\n")
	bear1 := onBoard(t, e, 1, "Name:Bear\nTypes:Creature\nPT:1/1\nOracle:x\n")
	mount := onBoard(t, e, 1, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	tokensBefore := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).IsToken {
			tokensBefore++
		}
	}

	e.askPriority(0)
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == walker {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the -11 loyalty ability was not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// The target ask: up to six nonland permanents (a KTarget, not a KChoose).
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("expected the 0..6-target ask, got %+v", td)
	}
	for _, o := range td.Options {
		if o.Obj == mount {
			t.Fatal("a land was offered to a nonland destroy")
		}
	}
	picks := []int{}
	for _, o := range td.Options {
		if o.Obj == bear0 || o.Obj == bear1 {
			picks = append(picks, o.Index)
		}
	}
	if len(picks) != 2 {
		t.Fatalf("expected both bears offered, got %d options: %+v", len(td.Options), td.Options)
	}
	submitChoices(t, e, picks...)
	passUntilStackEmpty(t, e, 20)

	if z := e.G.Obj(bear0).Zone; z != state.ZGraveyard {
		t.Fatalf("seat 0 bear zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(bear1).Zone; z != state.ZGraveyard {
		t.Fatalf("seat 1 bear zone = %s, want Graveyard", z)
	}
	if e.G.Obj(mount).Zone != state.ZBattlefield {
		t.Fatal("the land was destroyed by a nonland destroy")
	}
	tokens := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o.IsToken && o.Face() != nil && o.Face().Name == "Cat Warrior" {
			tokens++
		}
	}
	if tokens != tokensBefore+6 {
		t.Fatalf("created %d Cat Warrior tokens (had %d), want 6", tokens-tokensBefore, tokensBefore)
	}
	if got := e.G.Obj(walker).Counter("LOYALTY"); got != 0 {
		t.Fatalf("walker loyalty after the -11 = %d, want 0", got)
	}
}
