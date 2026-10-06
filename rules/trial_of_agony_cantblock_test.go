package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestTrialOfAgonyCantBlockRider(t *testing.T) {
	t.Parallel()
	for _, chosenName := range []string{"Wall of Stone", "Colossus of Sardia"} {
		t.Run(chosenName, func(t *testing.T) {
			reg := searchTestRegistry(t)
			spell := lookup(t, reg, "Trial of Agony")
			wall := lookup(t, reg, "Wall of Stone")
			colossus := lookup(t, reg, "Colossus of Sardia")
			attackerCard := lookup(t, reg, "Grizzly Bears")
			e, _ := corpusEngineCfg(t, reg, []*cards.Card{spell, attackerCard}, []*cards.Card{wall, colossus})
			wallID := moveByName(t, e, 1, "Wall of Stone", state.ZBattlefield)
			colossusID := moveByName(t, e, 1, "Colossus of Sardia", state.ZBattlefield)
			attacker := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
			spellID := moveByName(t, e, 0, "Trial of Agony", state.ZHand)
			chosenID := wallID
			if chosenName == "Colossus of Sardia" {
				chosenID = colossusID
			}
			otherID := wallID
			if chosenID == wallID {
				otherID = colossusID
			}
			for _, id := range []state.ObjID{wallID, colossusID} {
				o := e.G.Obj(id)
				if o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 || e.Derived(id).Toughness <= 5 {
					t.Fatalf("precondition: target %d must be a seat-1 battlefield creature with toughness >5: object=%+v toughness=%d", id, o, e.Derived(id).Toughness)
				}
			}
			if chosenID == otherID {
				t.Fatal("precondition: chosen and unchosen targets must differ")
			}

			// Prove both creatures can block the same real ground attacker before
			// Trial grants its restriction.
			e.G.Active = 0
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
			e.G.Step = state.StepDeclareBlockers
			if !e.canBlock(wallID, attacker) || !e.canBlock(colossusID, attacker) {
				t.Fatalf("precondition: both targets must be legal blockers before Trial; wall=%v colossus=%v", e.canBlock(wallID, attacker), e.canBlock(colossusID, attacker))
			}
			// Return to the main phase to cast the instant, retaining the declared
			// attacker so the resulting blocker decision exercises the same pair.
			e.G.Step = state.StepMain1
			addMana(t, e, 0, "R")
			d := e.Pending()
			if d == nil || d.Kind != decision.KPriority {
				t.Fatalf("before cast pending = %+v, want priority", d)
			}
			cast := -1
			for _, o := range d.Options {
				if o.Kind == "cast" && o.Obj == spellID {
					cast = o.Index
				}
			}
			if cast < 0 {
				t.Fatalf("Trial of Agony cast option absent from %+v", d.Options)
			}
			submitChoices(t, e, cast)
			targetAsk := e.Pending()
			if targetAsk == nil || targetAsk.Kind != decision.KTarget || targetOptionIndex(targetAsk, wallID) < 0 || targetOptionIndex(targetAsk, colossusID) < 0 {
				t.Fatalf("precondition: both target creatures must be offered: %+v", targetAsk)
			}
			submitChoices(t, e, targetOptionIndex(targetAsk, wallID), targetOptionIndex(targetAsk, colossusID))
			choose := passUntilAskKind(t, e, decision.KChoose, 200)
			if choose.Player != 1 || choose.ResumeKind != "choice" || len(choose.Options) != 2 {
				t.Fatalf("Trial of Agony choice = %+v, want opponent choosing one of two", choose)
			}
			var choiceIndex = -1
			for _, option := range choose.Options {
				if option.Obj == chosenID {
					choiceIndex = option.Index
				}
			}
			if choiceIndex < 0 {
				t.Fatalf("precondition: chosen %d absent from choice options %+v", chosenID, choose.Options)
			}
			submitChoices(t, e, choiceIndex)
			passUntilStackEmpty(t, e, 200)
			for _, id := range []state.ObjID{chosenID, otherID} {
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: both creatures must survive 5 damage for rider assertions; %d = %+v", id, o)
				}
			}
			damage := 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.Damage && ev.Amount == 5 {
					damage++
					if ev.Obj != chosenID {
						t.Errorf("5 damage landed on %d, chose %d", ev.Obj, chosenID)
					}
				}
			}
			if damage != 1 {
				t.Fatalf("5-damage events = %d, want exactly one", damage)
			}
			if _, found := cantBlockKeywordLine(t, e, otherID); !found {
				t.Fatalf("unchosen creature %d lacks derived can't-block grant: %v", otherID, e.Derived(otherID).Keywords)
			}
			if _, found := cantBlockKeywordLine(t, e, chosenID); found {
				t.Fatalf("chosen creature %d unexpectedly has derived can't-block grant", chosenID)
			}
			if !e.hasCantBlockKeyword(otherID) || e.hasCantBlockKeyword(chosenID) {
				t.Fatalf("semantic can't-block mismatch: unchosen=%v chosen=%v", e.hasCantBlockKeyword(otherID), e.hasCantBlockKeyword(chosenID))
			}
			if !e.blockRestricted(otherID, attacker) || e.blockRestricted(chosenID, attacker) || !e.canBlock(chosenID, attacker) {
				t.Fatalf("block semantics mismatch: unchosen restricted=%v chosen restricted=%v chosen can block=%v", e.blockRestricted(otherID, attacker), e.blockRestricted(chosenID, attacker), e.canBlock(chosenID, attacker))
			}
			control := onBoard(t, e, 1, "Name:Trial Block Control\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			blockAsk := openBlockAsk(t, e)
			if findBlockOption(blockAsk, control, attacker) == nil || findBlockOption(blockAsk, chosenID, attacker) == nil || findBlockOption(blockAsk, otherID, attacker) != nil {
				t.Fatalf("Trial blocker offers must include control and chosen only: %+v", blockAsk.Options)
			}
			e.EndOfTurnCleanup()
			if _, found := cantBlockKeywordLine(t, e, otherID); found || e.hasCantBlockKeyword(otherID) || !e.canBlock(otherID, attacker) {
				t.Fatalf("unchosen restriction did not expire at cleanup: keywords=%v", e.Derived(otherID).Keywords)
			}
			blockAsk = openBlockAsk(t, e)
			if findBlockOption(blockAsk, otherID, attacker) == nil {
				t.Fatalf("unchosen blocker not re-offered after cleanup: %+v", blockAsk.Options)
			}
		})
	}
}
