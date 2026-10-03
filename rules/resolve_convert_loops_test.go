package rules

// W3 step 2b's dual-run tests: the loop-shaped asks (a mid-resolution
// Charm, the per-player GenericChoice, VillainousChoice, the RepeatOptional$
// election, RepeatEach's ChooseOrder$ and per-subject offers) and loops
// whose bodies ask a converted site, all served from the tape.

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestTapeConvertLoops(t *testing.T) {
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
			tapeConverted(t, tc.seats, 13000+uint64(i), tc.name, "B", tc.served, src)
		})
	}
}

// RepeatEach ChooseOrder$ over two creatures: the order ask, then the loop.
func TestTapeConvertRepeatEachChooseOrder(t *testing.T) {
	const src = "Name:Tape Order\nManaCost:G\nTypes:Sorcery\n" +
		"A:SP$ RepeatEach | RepeatCards$ Creature.YouCtrl | ChooseOrder$ True | RepeatSubAbility$ DBScry\n" +
		"SVar:DBScry:DB$ Scry | ScryNum$ 1\nOracle:x\n"
	for _, seed := range []uint64{13101, 13102} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			_, st := tapeDual(t, 2, seed, func(t *testing.T, e *Engine) {
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
func TestTapeConvertRepeatOptionalRepeats(t *testing.T) {
	const src = "Name:Tape Again\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Repeat | RepeatSubAbility$ DBGain | RepeatOptional$ True\n" +
		"SVar:DBGain:DB$ GainLife | LifeAmount$ 1\nOracle:x\n"
	repeated := false
	for seed := uint64(13200); seed < 13206; seed++ {
		if st := tapeConverted(t, 2, seed, "Tape Again", "B", 1, src); st.Served >= 2 {
			repeated = true
		}
	}
	if !repeated {
		t.Fatal("no seed answered the election with a repeat")
	}
}
