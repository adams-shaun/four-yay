package rules

// Kernel-era restorations of effects/sacrifice_targetedcard_test.go's
// Enchanter's Bane pin and effects/sacrificeall_test.go's Slow Motion pin
// (deleted with the W3 legacy removal).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestEnchantersBaneOptionalSacrifice: Enchanter's Bane's real DBSac
// (Sacrifice | Defined$ TargetedController | SacValid$ TargetedCard.Self |
// Optional$ True) offers the TARGET's controller a Min 0 / Max 1 choice over
// exactly the targeted enchantment; declining keeps it, choosing it
// sacrifices it. The corpus shape is pinned first; the resolving sorcery
// carries the same parameters inline.
func TestEnchantersBaneOptionalSacrifice(t *testing.T) {
	t.Parallel()
	bane := corpusAlternativeCard(t, "Enchanter's Bane")
	sac := cards.ResolveSVar(bane.Faces[0].SVars, "DBSac")
	if sac == nil || sac.API != "Sacrifice" || sac.Params["Defined"] != "TargetedController" ||
		sac.Params["SacValid"] != "TargetedCard.Self" || sac.Params["Optional"] != "True" {
		t.Fatalf("corpus pin moved: Enchanter's Bane DBSac = %+v", sac)
	}
	spell := "Name:Bane Sorcery\nManaCost:R\nTypes:Sorcery\n" +
		"A:SP$ Sacrifice | ValidTgts$ Enchantment | Defined$ TargetedController | SacValid$ TargetedCard.Self | Optional$ True\nOracle:x\n"
	ench := "Name:Target Enchantment\nManaCost:1\nTypes:Enchantment\nOracle:x\n"
	for i, answer := range []string{"decline", "sacrifice"} {
		t.Run(answer, func(t *testing.T) {
			e, cfg := kr3Game(t, uint64(101+i), kr3Cards(t, spell), kr3Cards(t, ench))
			kr3Move(t, e, 0, "Bane Sorcery", state.ZHand)
			target := kr3Move(t, e, 1, "Target Enchantment", state.ZBattlefield)
			d := kr3Cast(t, e, "Bane Sorcery", "R", -1)
			if d == nil || d.Kind != decision.KChoose || d.Player != 1 || d.Min != 0 || d.Max != 1 {
				t.Fatalf("choice = %+v, want the target controller's Min 0 / Max 1 KChoose", d)
			}
			if len(d.Options) != 1 || d.Options[0].Obj != target {
				t.Fatalf("options = %+v, want the targeted enchantment %d", d.Options, target)
			}
			var choices []int
			want := state.ZBattlefield
			if answer == "sacrifice" {
				choices, want = []int{d.Options[0].Index}, state.ZGraveyard
			}
			if d := kr3Answer(t, e, choices...); d != nil {
				t.Fatalf("unexpected follow-up %+v", d)
			}
			if got := kr3Zone(e, target); got != want {
				t.Fatalf("target zone = %v, want %v", got, want)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestSlowMotionPaymentPreventsSacrifice: the real Slow Motion, attached to
// seat 1's creature, triggers at seat 1's upkeep; its SacrificeAll offers
// the EnchantedController (seat 1) the unless_pay {2}. Paying keeps the
// creature; declining sacrifices it.
func TestSlowMotionPaymentPreventsSacrifice(t *testing.T) {
	t.Parallel()
	slow := corpusAlternativeCard(t, "Slow Motion")
	if sa := cards.ResolveSVar(slow.Faces[0].SVars, "TrigUpkeep"); sa == nil || sa.API != "SacrificeAll" {
		t.Fatalf("corpus pin moved: Slow Motion TrigUpkeep = %+v", sa)
	}
	for i, pay := range []bool{true, false} {
		name := "decline"
		if pay {
			name = "pay"
		}
		t.Run(name, func(t *testing.T) {
			e, cfg := kr3Game(t, uint64(111+i), []*cards.Card{slow}, kr3Cards(t, kr3Creature("Victim")))
			victim := kr3Move(t, e, 1, "Victim", state.ZBattlefield)
			src := kr3Move(t, e, 0, "Slow Motion", state.ZHand)
			stageAuraEntry(t, e, src, victim)
			e.pending = nil
			e.Advance()
			driveToStep(t, e, 2, 1, state.StepUpkeep)
			e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "C", Amount: 1})
			e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "C", Amount: 1})
			e.pending = nil
			e.priorityRound()
			d := kr3Next(t, e)
			if d == nil || d.ResumeKind != "unless_pay" || d.Player != 1 {
				t.Fatalf("Slow Motion payer decision = %+v, want seat 1's unless_pay", d)
			}
			choice := len(d.Options) - 1 // the decline is last
			if pay {
				choice = 0
			}
			if d := kr3Answer(t, e, d.Options[choice].Index); d != nil {
				t.Fatalf("unexpected follow-up %+v", d)
			}
			want := state.ZGraveyard
			if pay {
				want = state.ZBattlefield
			}
			if got := kr3Zone(e, victim); got != want {
				t.Fatalf("victim zone = %v after %s, want %v", got, name, want)
			}
			replayCheck(t, e, cfg)
		})
	}
}
