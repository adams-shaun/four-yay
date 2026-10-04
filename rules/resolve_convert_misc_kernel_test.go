package rules

// Kernel-era restorations of the W3 step 2 dual-run tests of
// resolve_convert_misc_test.go: one kernel run per scenario (kr7Run), the
// asks served from the tape, the named asks posed, the replay identical.

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The sorcery-only shapes: each resolves its ask on the synthetic spell
// itself. Two seeds per case so the deterministic driver takes both sides of
// a yes/no (tapePick answers by decision sequence number).
func TestKernelAskMiscSorceries(t *testing.T) {
	cases := []struct {
		name, body string
		served     int64
	}{
		// mana_color: one colour, then an allocation of Combo units.
		{"Tape Any Mana", "A:SP$ Mana | Produced$ Any | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		{"Tape Combo Mana", "A:SP$ Mana | Produced$ Combo W U B | Amount$ 2 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		// endturn_optional and planeswalk_optional.
		{"Tape Time Stop", "A:SP$ EndTurn | Optional$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		{"Tape Walk", "A:SP$ Planeswalk | Optional$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
	}
	for i, tc := range cases {
		for _, seed := range []uint64{0, 1} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, seed), func(t *testing.T) {
				kr7Converted(t, 2, 13000+uint64(i)*2+seed, tc.name, "U", tc.served, tapeMiscSorcery(tc.name, tc.body))
			})
		}
	}
}

// The battlefield shapes: the spell reads (or targets) permanents put onto
// seat 0's battlefield first.
func TestKernelAskMiscBattlefield(t *testing.T) {
	cases := []struct {
		name, body string
		served     int64
	}{
		{"Tape Ring", "A:SP$ RingTemptsYou | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		{"Tape Reflect", "A:SP$ ManaReflected | ColorOrType$ Color | ReflectProperty$ Is | Valid$ Creature.YouCtrl | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		{"Tape Untap", "A:SP$ Untap | UntapType$ Creature.YouCtrl | Amount$ 1 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		{"Tape Untap Upto", "A:SP$ Untap | UntapType$ Creature.YouCtrl | Amount$ 2 | UntapUpTo$ True", 1},
		{"Tape Twiddle", "A:SP$ TapOrUntap | ValidTgts$ Creature | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		{"Tape Twiddle Two", "A:SP$ TapOrUntap | ValidTgts$ Creature | SubAbility$ DBTwo\nSVar:DBTwo:DB$ TapOrUntap | ValidTgts$ Creature | TargetUnique$ True", 2},
		{"Tape Skirmish", "A:SP$ Pump | ValidTgts$ Creature | KWChoice$ Flying,Vigilance,Trample | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		// An unread parameter's Note and a NoteCards$ notation precede the
		// ask; the legacy re-entry repeats both, so the tape branch must.
		{"Tape Noted Skirmish", "A:SP$ Pump | ValidTgts$ Creature | TapeUnread$ 1 | NoteCards$ TriggeredSource | NoteCardsFor$ x | KWChoice$ Flying,Vigilance", 1},
	}
	for i, tc := range cases {
		for _, seed := range []uint64{0, 1} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, seed), func(t *testing.T) {
				_, st := kr7Dual(t, 2, 13100+uint64(i)*2+seed, func(t *testing.T, e *Engine) {
					moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
					moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
					tapeCastAndResolve(t, e, tc.name, "U")
				}, tapeMiscSorcery(tc.name, tc.body), ptResumeBearSrc, ptResumeAngelSrc)
				tapeMiscServed(t, tc.name, st, tc.served)
			})
		}
	}
}

// setstate_optional: an Optional$ Transform of a two-faced creature.
func TestKernelAskMiscSetState(t *testing.T) {
	const name = "Tape Flip"
	src := tapeMiscSorcery(name, "A:SP$ SetState | ValidTgts$ Creature | Mode$ Transform | Optional$ True | SubAbility$ DBGain\n"+
		"SVar:DBGain:DB$ GainLife | LifeAmount$ 2")
	for _, seed := range []uint64{0, 1} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			_, st := kr7Dual(t, 2, 13200+seed, func(t *testing.T, e *Engine) {
				moveByName(t, e, 0, "Tape Front", state.ZBattlefield)
				tapeCastAndResolve(t, e, name, "U")
			}, src, tapeMiscFrontSrc)
			tapeMiscServed(t, name, st, 1)
		})
	}
}

// soulbond: the Soulbond creature's own entry trigger pairs it with a
// creature already on the battlefield (Min 0: the empty answer declines).
func TestKernelAskMiscSoulbond(t *testing.T) {
	const src = "Name:Tape Bonder\nManaCost:G\nTypes:Creature Human\nPT:1/1\nK:Soulbond\nOracle:x\n"
	for _, seed := range []uint64{0, 1} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			_, st := kr7Dual(t, 2, 13300+seed, func(t *testing.T, e *Engine) {
				moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
				tapeCastAndResolve(t, e, "Tape Bonder", "G")
			}, src, ptResumeBearSrc)
			tapeMiscServed(t, "soulbond", st, 1)
		})
	}
}

// extort: the trigger's KModes pay/decline, the pip charged by the shared
// answer record from the mana left floating after the cast.
func TestKernelAskMiscExtort(t *testing.T) {
	const src = "Name:Tape Extorter\nManaCost:B\nTypes:Creature Spirit\nPT:1/1\nK:Extort\nOracle:x\n"
	drained := 0
	for _, tc := range []struct {
		mana string
		seed uint64
	}{{"WB", 0}, {"WB", 1}, {"WB", 4}, {"W", 2}, {"W", 3}} {
		t.Run(fmt.Sprint(tc.mana, tc.seed), func(t *testing.T) {
			e, st := kr7Dual(t, 2, 13400+tc.seed, func(t *testing.T, e *Engine) {
				moveByName(t, e, 0, "Tape Extorter", state.ZBattlefield)
				tapeCastAndResolve(t, e, "Tape Gain", tc.mana)
			}, src, tapeNoAskSrc)
			tapeMiscServed(t, "extort", st, 1)
			for _, ev := range e.L.Events {
				if ev.Kind == events.LifeChange && ev.Player == 1 && ev.Amount == -1 {
					drained++
				}
			}
		})
	}
	if drained == 0 {
		t.Fatal("no scenario paid the Extort pip: the paid drain is untested")
	}
}

// myriad: one may choice per non-defending opponent, the walk continuing
// locally at the next opponent.
func TestKernelAskMiscMyriad(t *testing.T) {
	const src = "Name:Tape Swarm\nManaCost:1 R\nTypes:Creature Elf\nPT:1/1\nK:Myriad\nOracle:x\n"
	for _, seats := range []int{3, 4} {
		for _, seed := range []uint64{0, 1} {
			t.Run(fmt.Sprintf("seats%d/%d", seats, seed), func(t *testing.T) {
				_, st := kr7Dual(t, seats, 13500+uint64(seats)*2+seed, func(t *testing.T, e *Engine) {
					id := moveByName(t, e, 0, "Tape Swarm", state.ZBattlefield)
					e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
					e.priorityRound()
					tapeResolveAll(t, e)
				}, src)
				tapeMiscServed(t, "myriad", st, int64(seats-2))
			})
		}
	}
}

// venture: the dungeon choice (and, venturing again, a two-arrow room
// choice) for every player the Defined$ walk names.
func TestKernelAskMiscVenture(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Tape Delve"
	src := tapeMiscSorcery(name, "A:SP$ Venture | Defined$ Player | SubAbility$ DBAgain\n"+
		"SVar:DBAgain:DB$ Venture | Defined$ Player | SubAbility$ DBThird\n"+
		"SVar:DBThird:DB$ Venture | Defined$ Player")
	for _, seed := range []uint64{0, 1, 2} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			_, st := kr7DualTokens(t, 2, 13600+seed, reg.Tokens, func(t *testing.T, e *Engine) {
				tapeCastAndResolve(t, e, name, "U")
			}, src)
			tapeMiscServed(t, "venture", st, 2)
		})
	}
}
