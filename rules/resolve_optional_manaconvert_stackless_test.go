package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A modal land selects its back face before continueCast asks for optional
// conversions. A front-face filter read at the checkpoint can miss that ask.
func TestOptionalManaConvertMayAskOnStacklessCast(t *testing.T) {
	t.Parallel()
	const modal = "Name:Quiet Front\nManaCost:0\nTypes:Creature\nPT:1/1\nAlternateMode:Modal\nOracle:x\nALTERNATE\nName:Quiet Back\nTypes:Land\nOracle:x\n"
	e := corpusEngine(t, testutil.CorpusRegistry(t), []*cards.Card{card(t, modal)}, nil)
	land := moveByName(t, e, 0, "Quiet Front", state.ZHand)
	holder := onBoard(t, e, 0, "Name:Conversion Holder\nTypes:Enchantment\nS:Mode$ ManaConvert | ValidPlayer$ You | ValidCard$ Land | ValidSA$ Spell | Optional$ True | ManaConversion$ AnyType->AnyType | Description$ x\nOracle:x\n")
	if e.G.Obj(holder).Zone != state.ZBattlefield || e.G.Obj(land).Zone != state.ZHand || e.G.Obj(land).Face().IsLand() || !e.G.Obj(land).Card.Faces[1].IsLand() {
		t.Fatal("precondition: holder on battlefield and modal land front in hand, back is land")
	}
	if _, before := e.manaConversionParts(0, land, false); !before.Empty() {
		t.Fatal("precondition: the front face already admits conversion")
	}
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 || len(e.G.Stack) != 0 {
		t.Fatalf("precondition: stackless priority for player 0: %+v", d)
	}
	var opt *decision.Option
	for i := range d.Options {
		if d.Options[i].Kind == "play_land" && d.Options[i].Obj == land && d.Options[i].Mode == "modal_land" {
			opt = &d.Options[i]
			break
		}
	}
	if opt == nil {
		t.Fatalf("precondition: modal land not offered: %+v", d.Options)
	}
	in := decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{opt.Index}}
	if !asResolve(e).StartsResolution(d, in) {
		t.Fatal("precondition: modal land does not start a tape run")
	}
	if !asResolve(e).MayAsk(d, in) {
		t.Fatal("modal land's optional mana conversion must checkpoint the stackless land play")
	}
	if err := e.Submit(in); err != nil {
		t.Fatal(err)
	}
	ask := e.Pending()
	if ask == nil || ask.Kind != decision.KChoose || ask.Prompt != "Use optional mana conversion?" {
		t.Fatalf("modal land did not ask for optional conversion: %+v", ask)
	}
	if e.G.Obj(land).FaceIdx != 1 || e.G.Obj(land).Face().Name != "Quiet Back" {
		t.Fatalf("precondition: selected face was not Quiet Back: %+v", e.G.Obj(land))
	}
	if _, after := e.manaConversionParts(0, land, false); after.Empty() {
		t.Fatal("precondition: the back face must admit a conversion that the front face did not")
	}
}
