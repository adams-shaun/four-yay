package rules

// stat:IgnorePlaneswalkerZeroLoyaltyRule -- Sanctum Lurker: "Planeswalkers
// you control aren't put into their owners' graveyards for having 0
// loyalty." (S:Mode$ IgnorePlaneswalkerZeroLoyaltyRule | ValidCard$
// Planeswalker.YouCtrl.) The CR 704.5i sweep skips an exempt walker; an
// opponent's walker is not exempt, and the exemption ends with the Lurker.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const zeroWalker = "Name:Zero walker\nLoyalty:0\nTypes:Planeswalker Jace\nOracle:x\n"

func TestSanctumLurkerKeepsYourZeroLoyaltyPlaneswalkers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	lurker := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Sanctum Lurker"))
	mine := onBoard(t, e, 0, zeroWalker)
	theirs := onBoard(t, e, 1, zeroWalker)
	e.checkStateBased()
	if z := e.G.Obj(mine).Zone; z != state.ZBattlefield {
		t.Fatalf("seat 0's zero-loyalty walker went to %v under its controller's Sanctum Lurker", z)
	}
	if z := e.G.Obj(theirs).Zone; z != state.ZGraveyard {
		t.Fatalf("the opponent's zero-loyalty walker stayed in %v; the Lurker covers only its controller's", z)
	}
	// The exemption ends when the Lurker leaves.
	e.emit(events.Event{Kind: events.MoveZone, Obj: lurker, From: state.ZBattlefield, To: state.ZGraveyard})
	e.checkStateBased()
	if z := e.G.Obj(mine).Zone; z != state.ZGraveyard {
		t.Fatalf("the zero-loyalty walker stayed in %v after Sanctum Lurker left", z)
	}
}

// TestSanctumLurkerJaceTokenSurvivesItsMinusOne drives the card end to end:
// cast from hand, its ETB empowers Jace 1 (a 1-loyalty Jace token), the
// token's [-1] Surveil 1 takes it to 0 loyalty and it stays on the
// battlefield; on the next turn the Lurker-granted [+2] ("deals 1 damage to
// each opponent and you gain 1 life") is activated from 0 loyalty.
func TestSanctumLurkerJaceTokenSurvivesItsMinusOne(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Sanctum Lurker")}, nil)
	moveByName(t, e, 0, "Sanctum Lurker", state.ZHand)
	addMana(t, e, 0, "BBB")
	castNamed(t, e, "Sanctum Lurker")
	passUntilStackEmpty(t, e, 40)
	toks := jaceTokens(e, 0)
	if len(toks) != 1 || e.G.Obj(toks[0]).Counter("LOYALTY") != 1 {
		t.Fatalf("precondition: Sanctum Lurker's ETB did not make a 1-loyalty Jace token: %v", toks)
	}
	tok := toks[0]
	opt, ok := findAbilityOption(e, tok, 0)
	if !ok {
		t.Fatalf("the Jace token's [-1] is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	drainKeepTop(t, e, "the Jace token's [-1]")
	o := e.G.Obj(tok)
	if o.Counter("LOYALTY") != 0 {
		t.Fatalf("precondition: Jace token loyalty = %d after its -1, want 0", o.Counter("LOYALTY"))
	}
	if o.Zone != state.ZBattlefield {
		t.Fatalf("the 0-loyalty Jace token left the battlefield (zone %v) under Sanctum Lurker", o.Zone)
	}

	driveToTurn(t, e, e.G.Turn+2, 0)
	if e.G.Obj(tok).Zone != state.ZBattlefield {
		t.Fatal("the 0-loyalty Jace token did not survive to its controller's next turn")
	}
	plus := -1
	for _, op := range e.Pending().Options {
		if op.Kind == "ability" && op.Obj == tok && op.SVar == "PWLurker" {
			plus = op.Index
		}
	}
	if plus < 0 {
		t.Fatalf("the Lurker-granted [+2] is not offered on the Jace token: %+v", e.Pending().Options)
	}
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	submitChoices(t, e, plus)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(tok).Counter("LOYALTY"); got != 2 {
		t.Fatalf("Jace token loyalty after the granted [+2] = %d, want 2", got)
	}
	if e.G.Players[1].Life != life1-1 || e.G.Players[0].Life != life0+1 {
		t.Fatalf("granted [+2]: lives %d/%d -> %d/%d, want 1 damage to the opponent and 1 life gained",
			life0, life1, e.G.Players[0].Life, e.G.Players[1].Life)
	}
	replayCheck(t, e, cfg)
}
