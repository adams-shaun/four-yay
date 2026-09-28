package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// An Any production is a real CR 601.2g mana-window source; unlike an
// indeterminate amount, its one-unit output is provable even though the
// payer chooses its colour. It must keep the weak target at {7}+{1}.
func TestBeltOfGiantStrengthEquipChoiceManaWindow(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	belt, ok := reg.Lookup("Belt of Giant Strength")
	if !ok {
		t.Fatal("real Belt of Giant Strength absent from corpus")
	}
	const prismSrc = "Name:Prism Land\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Any | SpellDescription$ Add one mana of any color.\nOracle:x\n"
	cfg := Config{Seed: 517, Names: []string{"a", "b"}, Decks: [][]*cards.Card{
		append([]*cards.Card{belt, card(t, equipReduceBruteSrc), card(t, equipReduceSmallSrc), card(t, prismSrc)}, mountainDeck(t, 36)...),
		mountainDeck(t, 40),
	}, Tokens: reg.Tokens}
	e := New(seatZeroStart(cfg))
	e.Advance()
	beltID := findAndMoveToHand(t, e, 0, "Belt of Giant Strength")
	moveToBattlefield(t, e, beltID)
	bruteID := moveSeeded(t, e, 0, equipReduceBruteSrc, state.ZBattlefield)
	smallID := moveSeeded(t, e, 0, equipReduceSmallSrc, state.ZBattlefield)
	prismID := moveSeeded(t, e, 0, prismSrc, state.ZBattlefield)
	e.pending = nil
	e.Advance()
	driveToStep(t, e, 3, 0, state.StepMain1)
	if e.Power(bruteID) != 4 || e.Power(smallID) != 2 {
		t.Fatalf("fixture power must differ 4 vs 2: %d vs %d", e.Power(bruteID), e.Power(smallID))
	}
	if prism := e.G.Obj(prismID); prism.Zone != state.ZBattlefield || prism.Tapped || !e.untappedManaSource(0, prismID) {
		t.Fatalf("Prism must be an untapped mana source on battlefield: %+v", prism)
	}
	if got := e.attackBudget(0); got != 1 {
		t.Fatalf("Prism generic production = %d, want one unit", got)
	}
	addMana(t, e, 0, "CCCCCCC")
	if got := e.attackBudget(0); got != 8 {
		t.Fatalf("pool plus Prism = %d, want exactly the weak target's {8}", got)
	}
	opt, ok := findAbilityOption(e, beltID, 0)
	if !ok {
		t.Fatal("Belt equip not offered at {7}")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want equip target decision, got %+v", d)
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == smallID {
			found = true
		}
	}
	if !found {
		t.Fatalf("2/2 not offered at {7} plus Any source: %+v", d.Options)
	}
	targetObject(t, e, smallID)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want mana window, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == prismID {
			submitChoices(t, e, o.Index)
			goto activated
		}
	}
	t.Fatalf("Prism not available in payment window: %+v", d.Options)
activated:
	// The choice-shaped source may ask which colour it produces.
	if d = e.Pending(); d != nil && d.Kind == decision.KChoose {
		for _, o := range d.Options {
			if o.Kind == "mana" || o.Kind == "R" || o.Kind == "W" {
				submitChoices(t, e, o.Index)
				break
			}
		}
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(beltID).AttachedTo != smallID {
		t.Fatalf("Belt not attached to 2/2 after paying {8}; target=%d", e.G.Obj(beltID).AttachedTo)
	}
	replayCheck(t, e, cfg)
}
