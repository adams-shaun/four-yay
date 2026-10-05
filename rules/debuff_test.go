package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestDebuffFearOfFallingRemovesFlyingUntilNextTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	fear := mustCorpusCard(t, reg, "Fear of Falling")
	target := card(t, "Name:Target with Flying\nTypes:Creature\nPT:2/2\nK:Flying\nOracle:x\n")
	e := newSeats(t, 2)
	sourceID := onBoardCard(t, e, 0, fear)
	targetID := onBoardCard(t, e, 1, target)
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZBattlefield || !e.HasKeyword(targetID, "Flying") {
		t.Fatal("precondition: target must be a battlefield creature with Flying")
	}
	face := e.G.Obj(sourceID).Face()
	trigger := cards.ResolveSVar(face.SVars, "TrigPump")
	if trigger == nil {
		t.Fatal("Fear of Falling has no TrigPump SVar")
	}
	effects.Resolve(e, &effects.Ctx{Source: sourceID, Controller: 0, SVars: face.SVars,
		Targets: []state.Target{{Obj: targetID}}}, trigger)
	if e.HasKeyword(targetID, "Flying") {
		t.Fatal("Fear of Falling's Debuff did not remove Flying")
	}
	if got := e.Power(targetID); got != 0 {
		t.Fatalf("Fear of Falling power = %d, want 0 after -2", got)
	}
	found := false
	for _, ce := range e.continuous {
		if ce.Source == targetID && len(ce.RemoveKeywords) == 1 && ce.RemoveKeywords[0] == "Flying" {
			found = true
			if ce.UntilTurn == 0 || ce.UntilEOT {
				t.Fatalf("Debuff expiry = turn %d UntilEOT=%v, want next-turn boundary", ce.UntilTurn, ce.UntilEOT)
			}
		}
	}
	if !found {
		t.Fatal("Fear of Falling did not register keyword removal")
	}
}

func TestDebuffJadedAnalystChainsPump(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	analyst := mustCorpusCard(t, reg, "Jaded Analyst")
	e := newSeats(t, 2)
	id := onBoardCard(t, e, 0, analyst)
	if !e.HasKeyword(id, "Defender") {
		t.Fatal("precondition: Jaded Analyst must have Defender before its trigger")
	}
	face := e.G.Obj(id).Face()
	trigger := cards.ResolveSVar(face.SVars, "TrigDebuff")
	if trigger == nil {
		t.Fatal("Jaded Analyst has no TrigDebuff SVar")
	}
	effects.Resolve(e, &effects.Ctx{Source: id, Controller: 0, SVars: face.SVars}, trigger)
	if e.HasKeyword(id, "Defender") {
		t.Fatal("Jaded Analyst still has Defender after Debuff")
	}
	if !e.HasKeyword(id, "Vigilance") {
		t.Fatal("Jaded Analyst's chained Pump did not grant Vigilance")
	}
}
