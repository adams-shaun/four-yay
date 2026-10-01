package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestExpertLevelSafeActivatedAbilityIsOffered pins the REAL corpus card
// Expert-Level Safe's activated ability (A:AB$ Pump | Cost$ 1 T |
// ValidTgts$ Opponent | SubAbility$ ChooseNumber) through the priority walk,
// against triage report agent-20261001T025544Z-442a77e8, which claimed the
// ability "is not offered at all" -- a claim audited against an Oracle text
// that exists nowhere in the corpus (the real card's Oracle is the
// secret-choose-1-2-3 safe, not an exile-and-return {T} ability). On the
// real card the {1},{T} ability is an ordinary activated ability: seat 0's
// priority at its own main phase offers it as an "ability" option with cost
// "1 T".
//
// The card's one genuine gap -- the secret ChooseNumber ask (Secretly$ True,
// MatchedAbility$ DBSacrifice, UnmatchedAbility$ DBFillSafe) is never posed
// -- is ratcheted in rules/paramcensus_test.go's knownUnsupportedParams and
// is deliberately NOT touched here; closing it is M4 choice/cost-grammar
// work with its own ticket.
func TestExpertLevelSafeActivatedAbilityIsOffered(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 34, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	reg := testutil.CorpusRegistry(t)
	safe, ok := reg.Lookup("Expert-Level Safe")
	if !ok {
		t.Fatal("Expert-Level Safe missing from corpus")
	}
	if d := safe.Link(); len(d) != 0 {
		t.Fatalf("link Expert-Level Safe: %v", d)
	}
	id := onBoardCard(t, e, 0, safe)
	addMana(t, e, 0, "1")
	e.Advance()
	// Precondition chain: the offer only means something when the card is on
	// the battlefield, untapped (the {T} component), the generic {1} is
	// funded, and the pending ask is seat 0's priority at main 1.
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Expert-Level Safe not on the battlefield: %+v", o)
	} else if o.Tapped {
		t.Fatal("Expert-Level Safe is tapped; the {T} cost could not bind")
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool total = %d, want 1 funded for the {1} component", got)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("pending = %+v, want seat 0 priority", d)
	}
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("Expert-Level Safe's {1},{{T}} ability not offered: %+v", d.Options)
	}
	if opt.Cost != "1 T" {
		t.Fatalf("ability option cost = %q, want \"1 T\"", opt.Cost)
	}
	if !strings.HasPrefix(opt.Label, "Expert-Level Safe: You and target opponent") {
		t.Fatalf("ability option label = %q", opt.Label)
	}
}
