package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Only the colossus can pay {5}{U}{U} from two blue mana. The cheaper
// sacrifice must not remain an answer to an offer priced with the colossus.
func TestEmergeSacrificeAskPricesEachCreature(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 4250, []string{"Elder Deep-Fiend"}, []string{emergeFuelSrc, emergeColossusSrc}, nil)
	deep := findCardObj(t, e, 0, "Elder Deep-Fiend", state.ZHand)
	fuel := findCardObj(t, e, 0, "Emerge Fuel", state.ZBattlefield)
	large := findCardObj(t, e, 0, "Emerge Colossus", state.ZBattlefield)
	if e.G.Obj(deep).Zone != state.ZHand || e.G.Obj(fuel).Controller != 0 || e.G.Obj(large).Controller != 0 ||
		e.G.Obj(fuel).Zone != state.ZBattlefield || e.G.Obj(large).Zone != state.ZBattlefield ||
		e.G.Obj(fuel).Face().ManaValue() >= e.G.Obj(large).Face().ManaValue() {
		t.Fatal("the hand spell and two distinct-value controlled sacrifices are required")
	}
	addMana(t, e, 0, "UU")
	if e.G.Players[0].Pool.Total() != 2 {
		t.Fatal("test pool must pay only the large-creature reduction")
	}
	submitChoices(t, e, castModeOption(t, e, deep, "emerged"))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected sacrifice ask, got %+v", d)
	}
	largeOffered, smallOffered := false, false
	for _, o := range d.Options {
		if o.Kind == "sacrifice" && o.Obj == large {
			largeOffered = true
		}
		if o.Kind == "sacrifice" && o.Obj == fuel {
			smallOffered = true
		}
	}
	if !largeOffered || smallOffered {
		t.Fatalf("sacrifice choices: large=%v small=%v; want true false", largeOffered, smallOffered)
	}
	// Finish this same offer; the earlier test helper includes the choice and
	// would submit a second cast instead, so drive the pending stages here.
	for i := 0; i < 80; i++ {
		if e.G.Obj(deep).Zone == state.ZBattlefield && len(e.G.Stack) == 0 {
			break
		}
		d = e.Pending()
		if d == nil {
			t.Fatal("cast stalled")
		}
		switch d.Kind {
		case decision.KChoose:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "sacrifice" && o.Obj == large || o.Kind == "done" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("unexpected choose: %+v", d)
			}
			submitChoices(t, e, idx)
		case decision.KTarget:
			if d.Min != 0 {
				t.Fatalf("unexpected mandatory target: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
				t.Fatal(err)
			}
		case decision.KPriority:
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected decision: %+v", d)
		}
	}
	if e.G.Obj(deep).Zone != state.ZBattlefield || e.G.Obj(large).Zone != state.ZGraveyard ||
		e.G.Obj(fuel).Zone != state.ZBattlefield || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("emerge payment: spell=%s large=%s small=%s pool=%+v",
			e.G.Obj(deep).Zone, e.G.Obj(large).Zone, e.G.Obj(fuel).Zone, e.G.Players[0].Pool)
	}
	replayCheck(t, e, cfg)
}

const emergeExtraSacSrc = "Name:Emerge Double Sac\nManaCost:7\nTypes:Creature Eldrazi\nPT:1/1\nK:Emerge:3 U\n" +
	"A:SP$ Draw | Cost$ 7 Sac<1/Creature> | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n"
const emergeTinySrc = "Name:Emerge Tiny\nManaCost:1\nTypes:Creature Eldrazi\nPT:1/1\nOracle:x\n"

// SpellAbility costs follow the mandatory Emerge sacrifice. Their mana values
// must never contribute to the Emerge reduction, even though both are paid.
func TestEmergeAdditionalSacDoesNotIncreaseReduction(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 4251, nil, []string{emergeExtraSacSrc, emergeTinySrc, emergeColossusSrc}, nil)
	spell := findCardObj(t, e, 0, "Emerge Double Sac", state.ZHand)
	small := findCardObj(t, e, 0, "Emerge Tiny", state.ZBattlefield)
	big := findCardObj(t, e, 0, "Emerge Colossus", state.ZBattlefield)
	if e.G.Obj(spell).Zone != state.ZHand || e.G.Obj(small).Zone != state.ZBattlefield ||
		e.G.Obj(big).Zone != state.ZBattlefield || e.G.Obj(small).Controller != 0 ||
		e.G.Obj(big).Controller != 0 || e.G.Obj(small).Face().ManaValue() != 1 ||
		e.G.Obj(big).Face().ManaValue() != 10 {
		t.Fatal("hand spell and two controlled sacrifices of mana values 1 and 10 required")
	}
	if c, ok := emergeBase(e.G.Obj(spell).Face()); !ok || c.Generic != 3 || c.Colored[state.MU] != 1 || len(c.Sac) != 2 {
		t.Fatalf("expected {3}{U} and two Sac parts; got %+v, ok=%v", c, ok)
	}
	addMana(t, e, 0, "UCCC")
	if e.G.Players[0].Pool.Total() != 4 {
		t.Fatal("expected four mana pre-payment")
	}
	submitChoices(t, e, castModeOption(t, e, spell, "emerged"))
	for i := 0; i < 80; i++ {
		if e.G.Obj(spell).Zone == state.ZBattlefield && len(e.G.Stack) == 0 {
			break
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("cast stalled")
		}
		switch d.Kind {
		case decision.KChoose:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "sacrifice" && d.Source == spell {
					want := small
					if e.G.Obj(small).Zone != state.ZBattlefield || e.cast != nil && e.cast.sacPart == 1 {
						want = big
					}
					if o.Obj == want {
						idx = o.Index
					}
				}
				if o.Kind == "done" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("unexpected choose: %+v", d)
			}
			submitChoices(t, e, idx)
		case decision.KPriority:
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected decision: %+v", d)
		}
	}
	if e.G.Obj(spell).Zone != state.ZBattlefield || e.G.Obj(small).Zone != state.ZGraveyard ||
		e.G.Obj(big).Zone != state.ZGraveyard || e.G.Players[0].Pool.Total() != 1 ||
		e.G.Players[0].Pool[state.MU] != 0 {
		t.Fatalf("additional sacrifice charged wrong reduction: spell=%s small=%s big=%s pool=%+v",
			e.G.Obj(spell).Zone, e.G.Obj(small).Zone, e.G.Obj(big).Zone, e.G.Players[0].Pool)
	}
	replayCheck(t, e, cfg)
}

func TestEmergeWithholdsUnsupportedCostShapes(t *testing.T) {
	t.Parallel()
	for _, cost := range []string{"5 WU", "5 UP", "5 2U", "5 S", "5 PayLife<2>", "5 Sac<1/Creature>",
		"5 Draw<1/You>", "5 X", "5 Mandatory", "5 gibberish", "5 {U", ""} {
		t.Run(cost, func(t *testing.T) {
			face := &cards.Face{Keywords: []string{"Emerge:" + cost}}
			if c, ok := emergeCost(face); ok {
				t.Fatalf("unsupported emerge cost %q accepted as %+v", cost, c)
			}
		})
	}
	face := &cards.Face{Keywords: []string{"Emerge:5 U U:Artifact"}}
	if c, ok := emergeCost(face); !ok || c.Generic != 5 || c.Colored[state.MU] != 2 {
		t.Fatalf("valid corpus cost not accepted: %+v, ok=%v", c, ok)
	}
}
