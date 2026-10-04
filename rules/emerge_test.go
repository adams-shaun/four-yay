// emerge_test.go proves CR 702.118a: a spell with Emerge is cast by
// sacrificing a creature and paying the printed emerge cost reduced by that
// creature's mana value. Elder Deep-Fiend is the real corpus carrier (printed
// {8}, Emerge {5}{U}{U}); its companions are freely authored fixtures (never
// corpus .txt, per the licensing rule).
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

const (
	// emergeFuelSrc is a vanilla 2/2 for {2}{G}: mana value 3, so an emerge
	// cast of Elder Deep-Fiend ({5}{U}{U}) is reduced by exactly 3 to
	// {2}{U}{U}.
	emergeFuelSrc = "Name:Emerge Fuel\nManaCost:2 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	// emergeColossusSrc has mana value 10, larger than the {5}{U}{U} emerge
	// cost's generic component of 5: the reduction must floor the generic at
	// zero and still leave the {U}{U} pips (a mana-value reduction cannot
	// touch coloured mana).
	emergeColossusSrc = "Name:Emerge Colossus\nManaCost:10\nTypes:Creature Giant\nPT:10/10\nOracle:x\n"
)

// castEmergeSacrificing submits the "emerged" cast option for id, answers the
// mandatory sacrifice ask with sacObj, and drains the cast (the 601.2g mana
// window, Elder Deep-Fiend's own "when you cast" target ask, and priority)
// until the spell has resolved onto the battlefield. It fails on any decision
// shape it does not recognise, so a vacuous setup cannot slip through.
func castEmergeSacrificing(t *testing.T, e *Engine, id, sacObj state.ObjID) {
	t.Helper()
	submitChoices(t, e, castModeOption(t, e, id, "emerged"))
	for i := 0; i < 80; i++ {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield && len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("emerge cast stalled with no pending decision (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KChoose:
			idx := -1
			// The mandatory sacrifice ask: pick exactly the creature under
			// test.
			for _, o := range d.Options {
				if o.Kind == "sacrifice" && o.Obj == sacObj {
					idx = o.Index
				}
			}
			// The 601.2g mana window: the pool already covers the reduced
			// cost, so take "done".
			if idx < 0 {
				for _, o := range d.Options {
					if o.Kind == "done" {
						idx = o.Index
					}
				}
			}
			if idx < 0 {
				t.Fatalf("unexpected KChoose during emerge cast: %+v", d)
			}
			submitChoices(t, e, idx)
		case decision.KTarget:
			// Elder Deep-Fiend's cast trigger ("tap up to four target
			// permanents") is TargetMin 0: an empty answer taps none.
			if d.Min != 0 {
				t.Fatalf("emerge cast target ask with Min %d: %+v", d.Min, d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
				t.Fatalf("submit empty target answer: %v", err)
			}
		case decision.KPriority:
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected decision during emerge cast: %+v", d)
		}
	}
	t.Fatal("emerge cast never resolved")
}

// TestEmergeCastPaysReducedCost is the CR 702.118a proof: Elder Deep-Fiend,
// held in hand, is cast for its emerge cost by sacrificing a controlled
// battlefield creature of mana value 3, and only {2}{U}{U} (the printed
// {5}{U}{U} reduced by 3) is taken from a pool funded with the full printed
// cost.
func TestEmergeCastPaysReducedCost(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 4242, []string{"Elder Deep-Fiend"}, []string{emergeFuelSrc}, nil)
	// The primitive must be registered (Ruling W1): without it the coverage
	// census rejects every Emerge card as unsupported, so reverting the
	// registration alone fails this test.
	if !effects.Supported()["kw:Emerge"] {
		t.Fatal(`effects.Supported() is missing "kw:Emerge"`)
	}
	deep := findCardObj(t, e, 0, "Elder Deep-Fiend", state.ZHand)
	fuel := findCardObj(t, e, 0, "Emerge Fuel", state.ZBattlefield)

	// Preconditions: the spell is in hand, the sacrifice is a controlled
	// battlefield creature, and its mana value is actually non-zero so the
	// reduction under test is a real change.
	dface := e.G.Obj(deep)
	if dface == nil || dface.Zone != state.ZHand || dface.Controller != 0 {
		t.Fatalf("Elder Deep-Fiend not in seat 0's hand: %+v", dface)
	}
	fobj := e.G.Obj(fuel)
	if fobj == nil || fobj.Zone != state.ZBattlefield || fobj.Controller != 0 {
		t.Fatalf("Emerge Fuel not a controlled battlefield creature: %+v", fobj)
	}
	if mv := fobj.Face().ManaValue(); mv != 3 {
		t.Fatalf("Emerge Fuel mana value %d, want 3", mv)
	}
	// Sanity on the printed costs so the arithmetic assertion is grounded.
	ec, ok := emergeCost(dface.Face())
	if !ok || ec.Generic != 5 || ec.Colored[state.ManaIndex('U')] != 2 {
		t.Fatalf("emerge cost %+v (ok=%v), want {{5}{U}{U}}", ec, ok)
	}
	if base := pay.RawBaseCost(asPayer(e), 0, deep); base.Generic != 8 {
		t.Fatalf("printed cost %+v, want {8}", base)
	}

	// Fund the pool with the full printed emerge cost {5}{U}{U}: 2 blue + 5
	// generic. If the reduction is applied, the two blue pips are consumed and
	// 3 generic remain after payment.
	addMana(t, e, 0, "UUCCCCC")
	pool := e.G.Players[0].Pool
	if pool[state.MU] != 2 || pool[state.MC] != 5 || pool.Total() != 7 {
		t.Fatalf("pool funded %+v, want 2U 5C", pool)
	}

	castEmergeSacrificing(t, e, deep, fuel)

	if o := e.G.Obj(fuel); o.Zone != state.ZGraveyard {
		t.Fatalf("sacrificed creature in %s, want graveyard", o.Zone)
	}
	// The remaining pool's composition, not just its total: the two blue pips
	// of the reduced {2}{U}{U} must actually have been paid.
	pool = e.G.Players[0].Pool
	if pool[state.MU] != 0 || pool[state.MC] != 3 || pool.Total() != 3 {
		t.Fatalf("mana remaining %+v, want 0U 3C ({5}{U}{U} reduced by mana value 3)", pool)
	}
	sacrificed := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == fuel && ev.From == state.ZBattlefield &&
			ev.To == state.ZGraveyard && ev.Text == "sacrificed" {
			sacrificed = true
		}
	}
	if !sacrificed {
		t.Fatal("no sacrifice MoveZone for the emerge cost in the log")
	}
	replayCheck(t, e, cfg)
}

// TestEmergeCastReductionExceedsGenericKeepsColored proves the boundary of
// the mana-value reduction: it reduces ONLY the generic component. Elder
// Deep-Fiend's {5}{U}{U} emerge cost reduced by a mana-value-10 sacrifice is
// {U}{U}, not free -- the two blue pips must still be paid from the pool, and
// an emerge cast is not even OFFERED while the pool holds no blue mana.
func TestEmergeCastReductionExceedsGenericKeepsColored(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 4243, []string{"Elder Deep-Fiend"}, []string{emergeColossusSrc}, nil)
	deep := findCardObj(t, e, 0, "Elder Deep-Fiend", state.ZHand)
	colossus := findCardObj(t, e, 0, "Emerge Colossus", state.ZBattlefield)

	dface := e.G.Obj(deep)
	if dface == nil || dface.Zone != state.ZHand || dface.Controller != 0 {
		t.Fatalf("Elder Deep-Fiend not in seat 0's hand: %+v", dface)
	}
	fobj := e.G.Obj(colossus)
	if fobj == nil || fobj.Zone != state.ZBattlefield || fobj.Controller != 0 {
		t.Fatalf("Emerge Colossus not a controlled battlefield creature: %+v", fobj)
	}
	if mv := fobj.Face().ManaValue(); mv != 10 {
		t.Fatalf("Emerge Colossus mana value %d, want 10", mv)
	}
	// The offer price itself: generic floored at zero, both blue pips intact.
	if ec, ok := e.emergeOfferCost(0, deep, dface.Face(), func(Cost) bool { return true }); !ok || ec.Generic != 0 ||
		ec.Colored[state.MU] != 2 {
		t.Fatalf("emerge offer cost %+v (ok=%v), want {U}{U} with the generic floored to 0", ec, ok)
	}

	// Fund 2 blue + 2 generic: MORE than the {U}{U} reduced cost, and the
	// payment must take exactly the two blue pips, leaving the generic alone.
	// A coloured-reducing implementation would charge nothing and leave all 4.
	addMana(t, e, 0, "UUCC")
	pool := e.G.Players[0].Pool
	if pool[state.MU] != 2 || pool[state.MC] != 2 || pool.Total() != 4 {
		t.Fatalf("pool funded %+v, want 2U 2C", pool)
	}

	castEmergeSacrificing(t, e, deep, colossus)

	if o := e.G.Obj(colossus); o.Zone != state.ZGraveyard {
		t.Fatalf("sacrificed creature in %s, want graveyard", o.Zone)
	}
	pool = e.G.Players[0].Pool
	if pool[state.MU] != 0 || pool[state.MC] != 2 || pool.Total() != 2 {
		t.Fatalf("mana remaining %+v, want the two blue pips paid and the 2 generic left ({U}{U}, not free)", pool)
	}
	replayCheck(t, e, cfg)

	// The coloured requirement is also the OFFER gate: with only generic mana
	// in the pool the reduced {U}{U} is unpayable, so the emerged mode is not
	// offered at all (a coloured-erasing reduction would offer a free emerge
	// here). The plain {8} cast stays offered, so the decision exists and the
	// absence is about the emerge mode specifically.
	e2, _, _ := altCostEngine(t, 4244, []string{"Elder Deep-Fiend"}, []string{emergeColossusSrc}, nil)
	deep2 := findCardObj(t, e2, 0, "Elder Deep-Fiend", state.ZHand)
	addMana(t, e2, 0, "CCCCCCCC") // pays the plain {8}, never the emerged {U}{U}
	d2 := e2.Pending()
	if d2 == nil || d2.Kind != decision.KPriority {
		t.Fatalf("no priority decision on the generic-only engine: %+v", d2)
	}
	plain, emerged := false, false
	for _, o := range d2.Options {
		if o.Kind == "cast" && o.Obj == deep2 {
			switch o.Mode {
			case "":
				plain = true
			case "emerged":
				emerged = true
			}
		}
	}
	if emerged {
		t.Fatal("emerged cast offered with a generic-only pool: the {U}{U} requirement was erased")
	}
	if !plain {
		t.Fatalf("plain {8} cast not offered from a generic-only pool; options %+v", d2.Options)
	}
}
