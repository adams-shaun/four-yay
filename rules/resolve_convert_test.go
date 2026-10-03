package rules

// W3 step 2's dual-run tests (lasagna spec §7.7): each converted ask site,
// driven on legacy and on the tape kernel by the same deterministic driver
// (tapeDual), must stay event-, intent-, head- and RNG-identical, and its
// asks must be served from the tape with no legacy switch.

import (
	"testing"

	"github.com/adams-shaun/gorge/rules/resolve"
)

// tapeConverted runs a single-caster scenario (cast name for mana, resolve
// everything) through tapeDual and requires the tape path to have served
// at least minServed answers with no legacy ask ending the run.
func tapeConverted(t *testing.T, seats int, seed uint64, name, mana string, minServed int64, srcs ...string) resolve.Stats {
	t.Helper()
	_, st := tapeDual(t, seats, seed, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, name, mana)
	}, srcs...)
	if st.Served < minServed || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("%s: the converted asks were not served from the tape: %+v", name, st)
	}
	return st
}

func TestTapeConvertArrange(t *testing.T) {
	cases := []struct {
		name, src string
		served    int64
	}{
		{"Tape Scry", "A:SP$ Scry | ScryNum$ 3 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		{"Tape Surveil", "A:SP$ Surveil | Amount$ 2 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1},
		{"Tape Each Scry", "A:SP$ Scry | ScryNum$ 2 | Defined$ Player", 2},
		{"Tape Maybe Scry", "A:SP$ Scry | ScryNum$ 2 | Optional$ True | Defined$ Player", 2},
		{"Tape Ponder", "A:SP$ RearrangeTopOfLibrary | Defined$ You | NumCards$ 3 | MayShuffle$ True | SubAbility$ DBDraw\nSVar:DBDraw:DB$ Draw | NumCards$ 1", 2},
		{"Tape Rearrange", "A:SP$ RearrangeTopOfLibrary | Defined$ Player | NumCards$ 2", 2},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:" + tc.name + "\nManaCost:U\nTypes:Sorcery\n" + tc.src + "\nOracle:x\n"
			tapeConverted(t, 2, 12000+uint64(i), tc.name, "U", tc.served, src)
		})
	}
}
