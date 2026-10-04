package rules

// Kernel-era restorations of the W3 step 2 dual-run tests of
// resolve_convert_loops_test.go: one kernel run per scenario (kr7Run), the
// asks served from the tape, the named asks posed, the replay identical.

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestKernelAskLoops(t *testing.T) {
	cases := []struct {
		name, src string
		seats     int
		served    int64
	}{
		{"Tape Mid Charm", "A:SP$ GainLife | LifeAmount$ 1 | SubAbility$ DBCharm\n" +
			"SVar:DBCharm:DB$ Charm | Choices$ DBA,DBB\n" +
			"SVar:DBA:DB$ GainLife | LifeAmount$ 2 | SpellDescription$ gain\n" +
			"SVar:DBB:DB$ Draw | NumCards$ 1 | SpellDescription$ draw", 2, 1},
		{"Tape Charm Scry", "A:SP$ GainLife | LifeAmount$ 1 | SubAbility$ DBCharm\n" +
			"SVar:DBCharm:DB$ Charm | Choices$ DBA,DBB | CharmNum$ 2\n" +
			"SVar:DBA:DB$ Scry | ScryNum$ 2 | SpellDescription$ scry\n" +
			"SVar:DBB:DB$ Draw | NumCards$ 1 | SpellDescription$ draw", 2, 1},
		{"Tape Generic", "A:SP$ GenericChoice | Choices$ DBA,DBB\n" +
			"SVar:DBA:DB$ GainLife | LifeAmount$ 2 | SpellDescription$ gain\n" +
			"SVar:DBB:DB$ Draw | NumCards$ 1 | SpellDescription$ draw", 2, 1},
		{"Tape Generic Players", "A:SP$ GenericChoice | Defined$ Player | TempRemember$ Chooser | Choices$ DBA,DBB\n" +
			"SVar:DBA:DB$ GainLife | Defined$ Remembered | LifeAmount$ 2 | SpellDescription$ gain\n" +
			"SVar:DBB:DB$ Scry | Defined$ Remembered | ScryNum$ 1 | SpellDescription$ scry", 3, 3},
		{"Tape Generic Show", "A:SP$ GenericChoice | ShowChoice$ True | Choices$ DBA,DBB\n" +
			"SVar:DBA:DB$ GainLife | LifeAmount$ 2 | SpellDescription$ gain\n" +
			"SVar:DBB:DB$ Draw | NumCards$ 1 | SpellDescription$ draw", 2, 1},
		{"Tape Generic Players Show", "A:SP$ GenericChoice | Defined$ Player | ShowChoice$ True | Choices$ DBA,DBB\n" +
			"SVar:DBA:DB$ GainLife | Defined$ You | LifeAmount$ 2 | SpellDescription$ gain\n" +
			"SVar:DBB:DB$ Draw | Defined$ You | NumCards$ 1 | SpellDescription$ draw", 3, 3},
		{"Tape Generic Show Ask", "A:SP$ GenericChoice | ShowChoice$ True | Choices$ DBA,DBB\n" +
			"SVar:DBA:DB$ Scry | ScryNum$ 2 | SpellDescription$ scry\n" +
			"SVar:DBB:DB$ Surveil | Amount$ 1 | SpellDescription$ surveil", 2, 2},
		{"Tape Generic Players Show Ask", "A:SP$ GenericChoice | Defined$ Player | ShowChoice$ True | Choices$ DBA,DBB\n" +
			"SVar:DBA:DB$ Scry | Defined$ You | ScryNum$ 1 | SpellDescription$ scry\n" +
			"SVar:DBB:DB$ Surveil | Defined$ You | Amount$ 1 | SpellDescription$ surveil", 3, 6},
		{"Tape Villainous", "A:SP$ VillainousChoice | Defined$ Opponent | Choices$ DBA,DBB\n" +
			"SVar:DBA:DB$ LoseLife | Defined$ Remembered | LifeAmount$ 2 | SpellDescription$ lose\n" +
			"SVar:DBB:DB$ Draw | Defined$ You | NumCards$ 1 | SpellDescription$ draw", 3, 2},
		{"Tape Repeat Optional", "A:SP$ Repeat | RepeatSubAbility$ DBGain | RepeatOptional$ True\n" +
			"SVar:DBGain:DB$ GainLife | LifeAmount$ 1", 2, 1},
		{"Tape Repeat Body", "A:SP$ Repeat | RepeatSubAbility$ DBScry | MaxRepeat$ 3\n" +
			"SVar:DBScry:DB$ Scry | ScryNum$ 1", 2, 3},
		{"Tape Each Offer", "A:SP$ RepeatEach | RepeatPlayers$ Player | RepeatOptionalForEachPlayer$ True | RepeatSubAbility$ DBDraw\n" +
			"SVar:DBDraw:DB$ Draw | Defined$ Remembered | NumCards$ 1", 4, 4},
		{"Tape Each Body", "A:SP$ RepeatEach | RepeatPlayers$ Player | RepeatSubAbility$ DBScry\n" +
			"SVar:DBScry:DB$ Scry | Defined$ Remembered | ScryNum$ 1", 3, 3},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:" + tc.name + "\nManaCost:B\nTypes:Sorcery\n" + tc.src + "\nOracle:x\n"
			kr7Converted(t, tc.seats, 13000+uint64(i), tc.name, "B", tc.served, src)
		})
	}
}

// RepeatEach ChooseOrder$ over two creatures: the order ask, then the loop.
func TestKernelAskRepeatEachChooseOrder(t *testing.T) {
	const src = "Name:Tape Order\nManaCost:G\nTypes:Sorcery\n" +
		"A:SP$ RepeatEach | RepeatCards$ Creature.YouCtrl | ChooseOrder$ True | RepeatSubAbility$ DBScry\n" +
		"SVar:DBScry:DB$ Scry | ScryNum$ 1\nOracle:x\n"
	for _, seed := range []uint64{13101, 13102} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			_, st := kr7Dual(t, 2, seed, func(t *testing.T, e *Engine) {
				moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
				moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
				tapeCastAndResolve(t, e, "Tape Order", "G")
			}, src, ptResumeBearSrc, ptResumeAngelSrc)
			if st.Served < 3 || st.LegacySwitch != 0 || st.Aborts != 0 {
				t.Fatalf("the order ask and the loop bodies were not served: %+v", st)
			}
		})
	}
}

// The RepeatOptional$ election's "repeat" answer: across seeds the
// deterministic policy answers both ways; at least one run repeats.
func TestKernelAskRepeatOptionalRepeats(t *testing.T) {
	const src = "Name:Tape Again\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Repeat | RepeatSubAbility$ DBGain | RepeatOptional$ True\n" +
		"SVar:DBGain:DB$ GainLife | LifeAmount$ 1\nOracle:x\n"
	repeated := false
	for seed := uint64(13200); seed < 13206; seed++ {
		if st := kr7Converted(t, 2, seed, "Tape Again", "B", 1, src); st.Served >= 2 {
			repeated = true
		}
	}
	if !repeated {
		t.Fatal("no seed answered the election with a repeat")
	}
}

// CR 603.5's optional trigger at resolution (an engine-posed ask, step 2d):
// an ETB "you may" trigger is answered from the tape, yes (then its Scry is
// served too) and no.
func TestKernelAskOptionalTrigger(t *testing.T) {
	const src = "Name:Tape Maybe Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | OptionalDecider$ You | Execute$ TrigScry | TriggerDescription$ x\n" +
		"SVar:TrigScry:DB$ Scry | ScryNum$ 2 | SubAbility$ DBGain\n" +
		"SVar:DBGain:DB$ GainLife | LifeAmount$ 1\nOracle:x\n"
	for _, yes := range []bool{true, false} {
		t.Run(fmt.Sprint("yes=", yes), func(t *testing.T) {
			_, st := kr7Dual(t, 2, 13300, func(t *testing.T, e *Engine) {
				addMana(t, e, 0, "G")
				submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, "Tape Maybe Bear")).Index)
				for i := 0; i < 60; i++ {
					d := e.Pending()
					if d == nil || (d.Kind == decision.KPriority && len(e.G.Stack) == 0) {
						break
					}
					switch {
					case d.Kind == decision.KPriority:
						submitChoices(t, e, tapePassIndex(d))
					case d.Kind == decision.KTriggerOptional && yes:
						submitChoices(t, e, 0)
					case d.Kind == decision.KTriggerOptional:
						submitChoices(t, e, 1)
					default:
						submitChoices(t, e, tapePick(d)...)
					}
				}
			}, src)
			want := int64(1)
			if yes {
				want = 2
			}
			if st.Served < want || st.LegacySwitch != 0 || st.Aborts != 0 {
				t.Fatalf("the optional trigger was not served from the tape: %+v", st)
			}
		})
	}
}
