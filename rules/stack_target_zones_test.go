package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const validTgtsSpellNoTargetTypeSrc = "Name:Spell Filter\nManaCost:U\nTypes:Instant\n" +
	"A:SP$ Counter | ValidTgts$ Spell | TgtPrompt$ Select target spell | SpellDescription$ Counter target spell.\nOracle:x\n"

// TestValidTgtsSpellWithoutTargetTypeSearchesStack covers the legacy shape
// whose ValidTgts$ names the stack object kind, without TargetType$ or
// TgtZone$. The stack spell and battlefield permanent are both present so a
// battlefield-default implementation cannot pass vacuously.
func TestValidTgtsSpellWithoutTargetTypeSearchesStack(t *testing.T) {
	t.Parallel()
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	boardSrc := "Name:Board Creature\nTypes:Creature\nPT:2/2\nOracle:x\n"
	e := handEngine(t, card(t, validTgtsSpellNoTargetTypeSrc), card(t, bearSrc))
	board := e.G.AddObject(card(t, boardSrc), 1)
	board.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{board.ID})

	var filterID, bearID state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Spell Filter" {
			filterID = id
		} else if e.G.Obj(id).Face().Name == "Bear" {
			bearID = id
		}
	}
	if filterID == 0 || bearID == 0 {
		t.Fatalf("fixture hand missing cards: filter=%d bear=%d", filterID, bearID)
	}
	sa := e.G.Obj(filterID).Face().Abilities[0]
	if sa.Params["TargetType"] != "" || sa.Params["TgtZone"] != "" {
		t.Fatalf("fixture no longer has the unqualified target shape: params=%v", sa.Params)
	}
	zones := targetZones(sa)
	if len(zones) != 1 || zones[0] != state.ZStack || zones[0] == state.ZBattlefield {
		t.Fatalf("targetZones(%q) = %v, want the distinct stack zone", sa.Params["ValidTgts"], zones)
	}
	if board.Zone != state.ZBattlefield {
		t.Fatalf("battlefield precondition lost: board is in %s", board.Zone)
	}

	e.G.Players[0].Pool[state.MU] = 5
	e.G.Players[0].Pool[state.MG] = 5
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, bearID))
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Zone != state.ZStack {
		t.Fatalf("stack-spell precondition failed: stack=%v", e.G.Stack)
	}
	bearOnStack := e.G.Stack[0]

	submitChoices(t, e, passToCast(t, e, filterID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected target decision, got %+v", d)
	}
	stackIdx := -1
	for _, option := range d.Options {
		if option.Obj == board.ID {
			t.Fatalf("battlefield object offered for ValidTgts$ Spell: %+v", d.Options)
		}
		if option.Obj == bearOnStack {
			stackIdx = option.Index
		}
	}
	if stackIdx < 0 {
		t.Fatalf("stack spell not offered for ValidTgts$ Spell: %+v", d.Options)
	}
}
