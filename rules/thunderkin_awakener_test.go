package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestThunderkinAwakenerReturnsToughnessLessElementalTappedAndAttacking pins
// the reported card itself: Thunderkin Awakener's attack trigger is
//
//	T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigChange
//	SVar:TrigChange:DB$ ChangeZone | Origin$ Graveyard | Destination$ Battlefield
//	  | ValidTgts$ Creature.Elemental+YouCtrl+toughnessLTX | ... | Tapped$ True
//	  | Attacking$ True | AtEOT$ Sacrifice
//	SVar:X:Count$CardToughness
//
// The ValidTgts$ comparison's RHS is an SVAR-named X (toughnessLTX with
// SVar:X:Count$CardToughness), NOT the literal stack X. The rules-side target
// walk's SpecContext used to bind Resolve only for the literal X binding, so
// this comparison never resolved, numericPred returned its
// recognised-shape-but-unresolved no-match, and legalTargetCandidates offered
// zero candidates -- the trigger resolved silently with no target and the
// graveyard card never moved (agent-20260922T221917Z-aa93a144).
//
// 7454592e ("fix(rules): resolve trigger target X from source SVar") made
// SpecContext read the source face's SVar table, evaluating any non-xPaid
// body through effects.EvalCountOK. This test pins the reported card at HEAD:
// Thunderkin (1/2) attacks, the toughnessLTX ask offers the 1/1 Elemental in
// seat 0's graveyard (1 < 2), the chosen card returns tapped and attacking,
// and it deals its damage in that combat. It drives REAL combat from the
// attackers declaration, exactly like the sibling Yore-Tiller carrier.
func TestThunderkinAwakenerReturnsToughnessLessElementalTappedAndAttacking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Thunderkin Awakener"},
		[]string{"Name:SparkElemental\nManaCost:1 R\nTypes:Creature Elemental\nPT:1/1\nOracle:x\n"},
		nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	thunderkin := findBattlefield(t, e, 0, "Thunderkin Awakener", 0)

	// Preconditions the trigger depends on: Thunderkin is a 1/2 on the
	// battlefield, and the 1/1 Elemental is parked in seat 0's graveyard
	// (its toughness 1 is strictly less than Thunderkin's toughness 2, so it
	// is a legal toughnessLTX candidate).
	tk := e.G.Obj(thunderkin)
	if tk == nil || tk.Zone != state.ZBattlefield {
		t.Fatalf("Thunderkin = %+v, want it on the battlefield", tk)
	}
	if got := tk.Face().Toughness(); got != 2 {
		t.Fatalf("Thunderkin toughness = %d, want 2", got)
	}
	elemental := attackingEntryMove(t, e, 0, "SparkElemental", state.ZGraveyard)
	if o := e.G.Obj(elemental); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the Elemental = %+v, want it in seat 0's graveyard", o)
	}
	if got := e.G.Obj(elemental).Face().Toughness(); got != 1 {
		t.Fatalf("graveyard Elemental toughness = %d, want 1 (< Thunderkin's 2)", got)
	}

	e.askAttackers()
	submitAttackers(t, e, thunderkin)

	// The reported defect: the target ask offered zero candidates, so the
	// trigger resolved empty and the Elemental stayed in the graveyard. At
	// HEAD the toughnessLTX ask must offer the graveyard Elemental.
	sawTargetAsk := false
	entryDrive(t, e, func(d *decision.Decision) {
		if d.Kind == decision.KTarget {
			sawTargetAsk = true
			if len(d.Options) == 0 {
				t.Fatalf("toughnessLTX target ask offered zero candidates: %+v", d)
			}
			chooseObjOption(t, e, d, elemental)
			return
		}
		t.Fatalf("unexpected ask during Thunderkin's resolution: %+v", d)
	}, func() bool { return onBattlefield(e, elemental) })
	if !sawTargetAsk {
		t.Fatalf("no KTarget ask was ever posed for the toughnessLTX trigger")
	}
	assertEnteredAttacking(t, e, elemental, 1)

	// The entered 1/1 joins the attack: defender's life drops by Thunderkin's
	// 1 plus the returned Elemental's 1.
	entryDrive(t, e, func(d *decision.Decision) {
		t.Fatalf("unexpected ask after the entry: %+v", d)
	}, nil)
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("defender life = %d, want 18 (Thunderkin 1 + entered Elemental 1)", got)
	}
	replayCheck(t, e, cfg)
}
