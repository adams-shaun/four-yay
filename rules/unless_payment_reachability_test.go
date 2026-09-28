package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpusUnlessCost returns the first UnlessCost$ raw value found anywhere in
// the card's ability tree, so the tests below read the REAL script parameter
// (never a hand-trimmed literal).
func corpusUnlessCost(c *cards.Card) string {
	var walk func(*cards.SA) string
	walk = func(sa *cards.SA) string {
		if sa == nil {
			return ""
		}
		if raw := sa.Params["UnlessCost"]; raw != "" {
			return raw
		}
		return walk(sa.Sub)
	}
	for _, f := range c.Faces {
		for _, sa := range f.Abilities {
			if raw := walk(sa); raw != "" {
				return raw
			}
		}
	}
	return ""
}

// TestUnlessCostPayableNeedsEnoughAndRightColourFromRealCards pins the offer
// gate against the real corpus cards the brief names. Mana Leak's {3} is
// reachable from three untapped Islands but not from one; Chain Lightning's
// {R}{R} needs two red sources and an Island cannot pay it at all. Before the
// fix the gate was pool-only, so every pool-empty payer looked unable to pay
// (the pay branch vanished); a gate that merely counted "any source" would
// offer {3} with one Island -- both directions are asserted here.
func TestUnlessCostPayableNeedsEnoughAndRightColourFromRealCards(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	leak := mustCorpusCard(t, reg, "Mana Leak")
	chain := mustCorpusCard(t, reg, "Chain Lightning")
	leakCost := corpusUnlessCost(leak)
	chainCost := corpusUnlessCost(chain)
	if strings.TrimSpace(leakCost) == "" || strings.TrimSpace(chainCost) == "" {
		t.Fatalf("corpus scripts carry no UnlessCost$ (leak=%q chain=%q) -- premise false", leakCost, chainCost)
	}

	island := mustCorpusCard(t, reg, "Island")
	mountain := mustCorpusCard(t, reg, "Mountain")

	e := handEngine(t, leak, chain, island, mountain)
	e.G.Players[0].Pool = state.Mana{}
	// Precondition: the engine's own window membership counts an ordinary
	// untapped land, so the gate is not simply failing closed.
	islandID := onBoardCard(t, e, 0, island)
	if !e.untappedManaSource(0, islandID) {
		t.Fatal("ordinary Island is not a payment-window source -- premise false")
	}

	// One Island cannot reach Mana Leak's {3} nor Chain Lightning's {R}{R}.
	if e.UnlessCostPayable(0, leakCost) {
		t.Fatalf("%q payable with a single Island; the window cannot reach it", leakCost)
	}
	if e.UnlessCostPayable(0, chainCost) {
		t.Fatalf("%q payable with an Island; wrong colour must not count", chainCost)
	}

	// Two more Islands reach the {3} but still not the {R}{R}.
	onBoardCard(t, e, 0, island)
	onBoardCard(t, e, 0, island)
	if !e.UnlessCostPayable(0, leakCost) {
		t.Fatalf("%q not payable with three untapped Islands", leakCost)
	}
	if e.UnlessCostPayable(0, chainCost) {
		t.Fatalf("%q payable with Islands; wrong colour must not count", chainCost)
	}

	// Two Mountains reach the {R}{R}.
	onBoardCard(t, e, 0, mountain)
	if e.UnlessCostPayable(0, chainCost) {
		t.Fatalf("%q payable with one Mountain; needs two red", chainCost)
	}
	onBoardCard(t, e, 0, mountain)
	if !e.UnlessCostPayable(0, chainCost) {
		t.Fatalf("%q not payable with two untapped Mountains", chainCost)
	}
}

// TestCounterUnlessCostManaLeakTapsRealManaSources is the end-to-end pin: a
// real Mana Leak counters a real creature spell, the payer (empty pool) is
// offered the pay branch, taps three real Islands one at a time through the
// mana window, and the creature survives. This is the brief's headline
// defect -- before the fix Mana Leak was a hard counter against a
// pool-empty payer because no window existed.
func TestCounterUnlessCostManaLeakTapsRealManaSources(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, bearID := counterFixture(t, reg, "Mana Leak", "Grizzly Bears")
	// The caster spent its pool on the Bear, so nothing floats -- the normal
	// case the window must serve.
	e.G.Players[0].Pool = state.Mana{}

	land := mustCorpusCard(t, reg, "Island")
	lands := []state.ObjID{
		onBoardCard(t, e, 0, land),
		onBoardCard(t, e, 0, land),
		onBoardCard(t, e, 0, land),
	}
	for _, id := range lands {
		if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Tapped {
			t.Fatalf("payment source precondition failed for %d: %+v", id, e.G.Obj(id))
		}
	}

	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Mana Leak")
	}
	// The pay branch must be offered (the window can reach the {3}).
	if len(pay.Options) != 2 || pay.Options[0].Index != 0 {
		t.Fatalf("Mana Leak did not offer a payable pay/decline election: %+v", pay.Options)
	}
	submitChoices(t, e, pay.Options[0].Index)
	// Tap one Island per window step until the engine closes the source list.
	for i := 0; i < len(lands); i++ {
		d := e.Pending()
		if d == nil || d.ResumeKind != "unless_mana" {
			t.Fatalf("mana window step %d = %+v", i, d)
		}
		chosen := -1
		for _, o := range d.Options {
			if o.Obj == lands[i] {
				chosen = o.Index
			}
		}
		if chosen < 0 {
			t.Fatalf("mana window omitted untapped source %d: %+v", lands[i], d.Options)
		}
		submitChoices(t, e, chosen)
	}
	// Every selected Island reopens the unless window. Complete it explicitly
	// once the third source has produced the {3}; the generic drain only
	// handles priority decisions and must not silently choose this payment.
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_mana" || len(d.Options) != 1 || d.Options[0].Kind != "done" {
		t.Fatalf("expected the final unless_mana Done window, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bearID).Zone != state.ZBattlefield {
		t.Fatalf("Mana Leak paid from tapped lands but bear zone = %s", e.G.Obj(bearID).Zone)
	}
	for _, id := range lands {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("payment source %d was not tapped", id)
		}
	}
}

// TestCounterUnlessCostManaLeakWithOneIslandPayNotOffered is the t1 review's
// regression: with an empty pool and ONE Island the window cannot reach the
// {3}, so the pay branch must not be offered at all (offering it, tapping the
// Island and still countering the spell strands the payer). The ask is a
// single decline option and the Bear is countered.
func TestCounterUnlessCostManaLeakWithOneIslandPayNotOffered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, bearID := counterFixture(t, reg, "Mana Leak", "Grizzly Bears")
	e.G.Players[0].Pool = state.Mana{}

	islandID := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Island"))
	if !e.untappedManaSource(0, islandID) {
		t.Fatal("Island is not a payment-window source -- premise false")
	}

	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Mana Leak")
	}
	if len(pay.Options) != 1 || pay.Options[0].Kind != "mode" {
		t.Fatalf("unreachable pay was still offered: %+v", pay.Options)
	}
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(bearID).Zone; z != state.ZGraveyard {
		t.Fatalf("unpayable Mana Leak should counter the spell: Bear zone = %s", z)
	}
	if e.G.Obj(islandID).Tapped {
		t.Fatal("the Island was tapped even though pay was never offered")
	}
}
