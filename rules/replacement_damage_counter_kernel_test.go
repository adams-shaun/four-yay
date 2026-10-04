package rules

// Kernel-era restorations of the tests W3 removed from replacement_damage_counter_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDamageAllParksEveryRecipientBeforeTheChainResumes pins the
// multi-recipient settlement order (CR 616.1): a DamageAll over two creatures
// with two competing amount modifiers parks one order choice per recipient
// event, and the spell's chained rider (and its departure from the stack)
// waits for the LAST answer. Resuming after the first answer would let the
// rider fire and the spell leave the stack while the second recipient's
// damage is still awaiting its order choice.
func TestDamageAllParksEveryRecipientBeforeTheChainResumesKernel(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	fiery := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	onBoard(t, e, 0, "Name:Plus Two\nTypes:Enchantment\n"+
		"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Creature | ReplaceWith$ D\n"+
		"SVar:D:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n"+
		"SVar:X:ReplaceCount$DamageAmount/Plus.2\nOracle:x\n")
	t1 := onBoard(t, e, 1, "Name:Victim One\nTypes:Creature\nPT:2/2\nOracle:x\n")
	t2 := onBoard(t, e, 1, "Name:Victim Two\nTypes:Creature\nPT:2/2\nOracle:x\n")
	spell := e.G.AddObject(card(t, "Name:Damage All\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ DamageAll | ValidCards$ Creature | NumDmg$ 2 | SubAbility$ Gain\n"+
		"SVar:Gain:DB$ GainLife | Defined$ You | LifeAmount$ 5\nOracle:x\n"), 0)
	spell.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID})
	e.G.Stack = []state.ObjID{spell.ID}
	// Resolve through real priority passes (a Submit-driven resolution).
	e.priorityRound()
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		submitChoices(t, e, passIndex(t, d))
	}
	answers := 0
	for {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		if d.Kind != decision.KReplacement {
			t.Fatalf("answer %d: pending kind %v, want KReplacement (%+v)", answers, d.Kind, d)
		}
		answers++
		// After the first answer the rider must NOT have run and the spell
		// must still be on the stack: the second recipient's choice is owed.
		if answers == 2 {
			if got := e.G.Players[0].Life; got != 20 {
				t.Fatalf("rider ran before the second recipient's answer: life = %d, want 20", got)
			}
			if got := e.G.Obj(spell.ID).Zone; got != state.ZStack {
				t.Fatalf("spell zone after first answer = %s, want stack", got)
			}
		}
		if answers > 4 {
			t.Fatalf("too many replacement asks: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
	}
	kr7Settle(e)
	if answers != 2 {
		t.Fatalf("replacement asks = %d, want one per recipient (2)", answers)
	}
	// Both recipients' damage settled before the rider: each victim took the
	// answered (2+2)*3 = 12 damage and died from it, and the rider ran once.
	for _, id := range []state.ObjID{t1, t2} {
		if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
			t.Fatalf("victim zone = %s (damage %d, pending %+v), want graveyard after lethal settled damage", got, e.G.Obj(id).Damage, e.Pending())
		}
	}
	if got := e.G.Players[0].Life; got != 25 {
		t.Fatalf("rider life = %d, want 25 from exactly one GainLife after both answers", got)
	}
	if got := e.G.Obj(spell.ID).Zone; got != state.ZGraveyard {
		t.Fatalf("spell zone = %s, want graveyard after the chain resumed", got)
	}
	_ = fiery
}

func TestDamageReplacementSupportedBodyFamiliesKernel(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)

	t.Run("ChangeZone Weeping Angel", func(t *testing.T) {
		e := newSeats(t, 2)
		angel := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Weeping Angel"))
		target := onBoard(t, e, 1, "Name:Target\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.damaging, e.combatDamaging = angel, true
		e.emit(events.Event{Kind: events.Damage, Obj: target, Amount: 2})
		e.damaging, e.combatDamaging = 0, false
		if got := e.G.Obj(target).Zone; got != state.ZLibrary {
			t.Fatalf("target zone = %s, want library", got)
		}
	})

	t.Run("Dig Crumbling Sanctuary", func(t *testing.T) {
		e := newSeats(t, 2)
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Crumbling Sanctuary"))
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:2/2\nOracle:x\n")
		before := len(e.G.Zone(state.ZLibrary, 0))
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
		e.damaging = 0
		if got := len(e.G.Zone(state.ZLibrary, 0)); got != before-2 {
			t.Fatalf("library size = %d, want %d", got, before-2)
		}
		if got := len(e.G.Zone(state.ZExile, 0)); got != 2 {
			t.Fatalf("exile size = %d, want 2", got)
		}
	})

	t.Run("Draw Swans of Bryn Argoll", func(t *testing.T) {
		e := newSeats(t, 2)
		swans := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Swans of Bryn Argoll"))
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:3/3\nOracle:x\n")
		before := len(e.G.Zone(state.ZHand, 1))
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Obj: swans, Amount: 3})
		e.damaging = 0
		if got := len(e.G.Zone(state.ZHand, 1)); got != before+3 {
			t.Fatalf("source controller hand = %d, want %d", got, before+3)
		}
	})

	t.Run("GainLife Purity", func(t *testing.T) {
		e := newSeats(t, 2)
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Purity"))
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:3/3\nOracle:x\n")
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
		e.damaging = 0
		if got := e.G.Players[0].Life; got != 23 {
			t.Fatalf("life = %d, want 23", got)
		}
	})

	t.Run("Mill Angel of Suffering", func(t *testing.T) {
		e := newSeats(t, 2)
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Angel of Suffering"))
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:3/3\nOracle:x\n")
		before := len(e.G.Zone(state.ZLibrary, 0))
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
		e.damaging = 0
		if got := len(e.G.Zone(state.ZLibrary, 0)); got != before-6 {
			t.Fatalf("library size = %d, want %d", got, before-6)
		}
	})

	t.Run("Sacrifice Dralnu", func(t *testing.T) {
		e := newSeats(t, 2)
		dralnu := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Dralnu, Lich Lord"))
		one := onBoard(t, e, 0, "Name:One\nTypes:Creature\nPT:1/1\nOracle:x\n")
		two := onBoard(t, e, 0, "Name:Two\nTypes:Creature\nPT:1/1\nOracle:x\n")
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:2/2\nOracle:x\n")
		before := len(e.G.Zone(state.ZGraveyard, 0))
		e.damaging = source
		e.pending = nil
		e.probe(func() { e.emit(events.Event{Kind: events.Damage, Obj: dralnu, Amount: 2}) })
		e.damaging = 0
		// The merge's approved sacrifice semantics ask the player to choose
		// the exact batch whenever more eligible permanents exist than the
		// amount (three creatures, sacrifice two), so the replacement body
		// suspends on the real KChoose and the answer applies the batch.
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 || len(d.Options) != 3 {
			t.Fatalf("Dralnu did not ask its controller to choose two sacrifices: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1, 2}}); err != nil {
			t.Fatal(err)
		}
		if got := len(e.G.Zone(state.ZGraveyard, 0)); got != before+2 {
			t.Fatalf("graveyard size = %d, want %d after sacrificing two permanents", got, before+2)
		}
		if e.G.Obj(one).Zone != state.ZGraveyard || e.G.Obj(two).Zone != state.ZGraveyard {
			t.Fatalf("the chosen permanents did not move: %s %s", e.G.Obj(one).Zone, e.G.Obj(two).Zone)
		}
	})

	t.Run("Token Hostility", func(t *testing.T) {
		e := newSeats(t, 2)
		e.G.Tokens = reg.Tokens
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Hostility"))
		source := e.G.AddObject(mustCorpusCard(t, reg, "Lightning Bolt"), 0)
		source.Zone = state.ZStack
		before := len(e.G.Zone(state.ZBattlefield, 0))
		e.damaging = source.ID
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
		e.damaging = 0
		if got := len(e.G.Zone(state.ZBattlefield, 0)); got != before+2 {
			t.Fatalf("battlefield size = %d, want %d after two Hostility tokens", got, before+2)
		}
	})
}
