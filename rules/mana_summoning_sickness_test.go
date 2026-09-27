package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestAutopayFPSummoningSickRealFlow(t *testing.T) {
	elfSrc := "Name:Real Elf\nManaCost:G\nTypes:Creature Elf Druid\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add {G}.\nOracle:x\n"
	e, _, elf := newFixtureDeck(t, 9821, elfSrc)
	toMain1(t, e)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.pending = nil
	e.askPriority(0)
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == elf {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatal("fixture elf not offered for casting")
	}
	submitChoices(t, e, idx)
	resolveStack(t, e)
	if o := e.G.Obj(elf); o.Zone != state.ZBattlefield || !o.SummonSick {
		t.Fatalf("elf zone=%s sick=%v, want a summoning-sick battlefield creature", o.Zone, o.SummonSick)
	}
	probe := e.G.AddObject(card(t, "Name:Green Probe\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"), 0)
	probe.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), probe.ID))
	if a, ok := fpPlanUses(e.PlanCastPayment(0, paymentCast(probe.ID)), elf); ok {
		t.Errorf("V1 plan taps the elf cast this turn: %+v", a)
	}
	e.pending = nil
	e.askPriority(0)
	for _, o := range e.Pending().Options {
		if o.Kind == "activate" && o.Obj == elf {
			t.Error("priority offers the {T} mana ability of the elf cast this turn (CR 302.6)")
		}
	}
}

func TestAutopayFPSummoningSickCreatureIsNotASource(t *testing.T) {
	e := layerEngine(t)
	elf := onBoard(t, e, 0, "Name:Sick Elf\nTypes:Creature Elf\nA:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n")
	spell := e.G.AddObject(card(t, "Name:Green Probe\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"), 0)
	spell.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), spell.ID))
	if got := e.PlanCastPayment(0, paymentCast(spell.ID)); got.Plan != nil || got.Reason != "insufficient" {
		t.Fatalf("sick elf payment plan = %#v, want insufficient", got)
	}
	e.pending = nil
	e.askPriority(0)
	for _, o := range e.Pending().Options {
		if o.Kind == "activate" && o.Obj == elf {
			t.Error("priority offers a summoning-sick creature's tap mana ability")
		}
	}
}

func TestManaAbilitySummoningSickness(t *testing.T) {
	for _, tc := range []struct {
		name       string
		source     string
		wantSource bool
	}{
		{name: "sick creature", source: "Name:Sick Elf\nTypes:Creature Elf\nA:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n"},
		{name: "haste creature", source: "Name:Hasty Elf\nTypes:Creature Elf\nK:Haste\nA:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n", wantSource: true},
		{name: "new forest", source: "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n", wantSource: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			id := onBoard(t, e, 0, tc.source)
			if tc.name != "new forest" && !e.G.Obj(id).SummonSick {
				t.Fatal("precondition: creature must be summoning sick")
			}
			if got := len(e.availableManaAbilitiesForWindow(0, id, true)) != 0; got != tc.wantSource {
				t.Fatalf("priority mana membership = %v, want %v", got, tc.wantSource)
			}
			mana := e.AvailableMana(0)
			if (mana.Total() > 0) != tc.wantSource {
				t.Fatalf("AvailableMana = %v, want source present=%v", mana, tc.wantSource)
			}
			potential := e.PotentialMana(0)
			if (potential.Total() > 0) != tc.wantSource {
				t.Fatalf("PotentialMana = %v, want source present=%v", potential, tc.wantSource)
			}
			spell := e.G.AddObject(card(t, "Name:Green Probe\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"), 0)
			spell.Zone = state.ZHand
			e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), spell.ID))
			planned := e.PlanCastPayment(0, paymentCast(spell.ID))
			if (planned.Plan != nil) != tc.wantSource {
				t.Fatalf("payment plan = %#v, want source present=%v", planned, tc.wantSource)
			}
		})
	}

	e := layerEngine(t)
	elf := onBoard(t, e, 0, "Name:Turn Elf\nTypes:Creature Elf\nA:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n")
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	if e.G.Obj(elf).SummonSick {
		t.Fatal("creature remained summoning sick after its controller's turn began")
	}
	if got := len(e.availableManaAbilitiesForWindow(0, elf, true)); got == 0 {
		t.Fatal("creature's mana ability absent after controller's turn began")
	}

}
