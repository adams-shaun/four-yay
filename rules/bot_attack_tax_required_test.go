package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
)

// TestBotRequiredAttackDeclarationFitsCombinedPhyrexianTax is the
// review-r1 MAJOR's reproduction: the combined Phyrexian attack tax must be
// priced on the REQUIRED path too, not just the optional one. With Norn's
// Annex out and three creatures that each MUST attack (CR 508.1d, their own
// `S:Mode$ MustAttack | ValidCreature$ Card.Self` static) at four life, the
// engine's RequiredQuota used to count all three required pairs -- it priced
// only Option.Value/MaxSum, never the wire's CostPhyrexian -- so the
// production policy's charge guard trimmed the declaration to two and
// FitRequired restored the third, and Submit rejected the whole declaration
// ("... 3 Phyrexian, unpriceable=false) is not payable"). host.runMatch turns that rejection into
// a crashed table. The quota and FitRequired now derive from the same
// charge-feasible required core, so the policy's declaration is accepted.
//
// The board is the review's exact variant; the test first asserts its
// preconditions (all three pairs are offered and Required, each carries one
// pip, seat 0 is at four life and has no white source), so a mis-set-up board
// fails loudly instead of passing vacuously.
func TestBotRequiredAttackDeclarationFitsCombinedPhyrexianTax(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.Advance()
	onBoardCard(t, e, 1, corpusCard(t, "Norn's Annex"))
	for i := 0; i < 3; i++ {
		id := onBoard(t, e, 0, "Name:Tax Bear\nManaCost:1\nTypes:Creature Bear\nPT:2/2\n"+
			"S:Mode$ MustAttack | ValidCreature$ Card.Self\nOracle:x\n")
		e.G.Obj(id).SummonSick = false
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 4 - e.G.Players[0].Life})
	passToKind(t, e, decision.KAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || d.Player != 0 {
		t.Fatalf("pending = %+v, want seat 0's attackers", d)
	}
	// Precondition: three individually-affordable pip-taxed pairs, each a
	// CR 508.1d requirement, are actually offered.
	if len(d.Options) != 3 {
		t.Fatalf("offered %d attacker options, want 3 (three pip-taxed required pairs)", len(d.Options))
	}
	for _, o := range d.Options {
		if o.CostPhyrexian != 1 {
			t.Fatalf("option %d CostPhyrexian = %d, want 1 (the wire must publish the pip)", o.Index, o.CostPhyrexian)
		}
		if !o.Required {
			t.Fatalf("option %d is not Required, want the MustAttack requirement on the wire", o.Index)
		}
	}
	if got := e.G.Players[0].Life; got != 4 {
		t.Fatalf("seat 0 life = %d, want 4 (the bound only binds there)", got)
	}
	// The quota itself must be charge-feasible: two pips x two life = 4.
	if d.PayerLife != 4 {
		t.Fatalf("decision.PayerLife = %d, want 4 (the engine must publish the bound)", d.PayerLife)
	}
	if q := d.RequiredQuota(); q != 2 {
		t.Fatalf("RequiredQuota = %d, want 2 (a third pip is unpayable at 4 life)", q)
	}
	in := newTestBot(7).answer(e, d)
	n := d.RequiredChosen(in.Choices)
	if err := e.Submit(in); err != nil {
		t.Fatalf("production policy declared %v (%d required covered, life %d): Submit rejected it: %v",
			in.Choices, n, e.G.Players[0].Life, err)
	}
	if n < d.RequiredQuota() {
		t.Fatalf("policy declared %v covering %d required creatures, the engine demands %d", in.Choices, n, d.RequiredQuota())
	}
	if len(in.Choices) != 2 {
		t.Fatalf("policy declared %v, want exactly 2 attackers (2 pips x 2 life = 4)", in.Choices)
	}
}
