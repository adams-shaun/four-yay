package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
)

// TestBotAttackDeclarationFitsCombinedPhyrexianTax (general; cardfuzz sig
// "error: seed #, intent #: declaration's attack cost (# mana, # life, # taps,
// # sacrifices, # returns, # Phyrexian, unpriceable=#) is not payable", off and
// auto-pay lanes alike): with Norn's Annex out, each attacking creature costs {W/P}.
// The offer walk prices every (attacker, defender) pair on its own, so at 4
// life with no white source each of three attackers is offered (one pip = 2
// life). The production policy used to declare all three (3 x 2 life = 6 > 4)
// and validateAttackers (rules/combat.go) rejected the whole declaration at
// Submit -- which host.runMatch turns into a crashed table (r.crash on
// "intent N rejected"). The fix publishes Option.CostPhyrexian and folds the
// pip count into the shared decision.ChargeOptionConstraints life bound, so
// the policy's declaration is a payable subset Submit accepts.
func TestBotAttackDeclarationFitsCombinedPhyrexianTax(t *testing.T) {
	e := layerEngine(t)
	e.Advance()
	onBoardCard(t, e, 1, corpusCard(t, "Norn's Annex"))
	for i := 0; i < 3; i++ {
		id := onBoard(t, e, 0, "Name:Tax Attacker\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		e.G.Obj(id).SummonSick = false
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 4 - e.G.Players[0].Life})
	passToKind(t, e, decision.KAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || d.Player != 0 {
		t.Fatalf("pending = %+v, want seat 0's attackers", d)
	}
	// Precondition the assertion depends on: three individually-affordable
	// pip-taxed pairs are actually offered, so a pass cannot come from the
	// board being set up wrong (no Annex, attackers not able to attack).
	if len(d.Options) != 3 {
		t.Fatalf("offered %d attacker options, want 3 (three pip-taxed pairs)", len(d.Options))
	}
	for _, o := range d.Options {
		if o.CostPhyrexian != 1 {
			t.Fatalf("option %d CostPhyrexian = %d, want 1 (the wire must publish the pip)", o.Index, o.CostPhyrexian)
		}
	}
	if got := e.G.Players[0].Life; got != 4 {
		t.Fatalf("seat 0 life = %d, want 4 (the bound only binds at 4)", got)
	}
	in := newTestBot(7).answer(e, d)
	if err := e.Submit(in); err != nil {
		t.Fatalf("production policy declared %v (%d options, life %d): Submit rejected it: %v", in.Choices, len(d.Options), e.G.Players[0].Life, err)
	}
	if len(in.Choices) != 2 {
		t.Fatalf("policy declared %v, want exactly 2 attackers (2 pips x 2 life = 4)", in.Choices)
	}
}
