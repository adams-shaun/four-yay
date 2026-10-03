package rules

// W3 step 2's dual-run tests for the "misc" group's converted ask sites
// (lasagna spec §7.7): each scenario resolves on the legacy resume machinery
// and on the tape kernel with the same deterministic driver, and the two
// logs must be identical while the converted asks are served from the tape.

import (
	"testing"

	"github.com/adams-shaun/gorge/rules/resolve"
)

func tapeMiscSorcery(name, body string) string {
	return "Name:" + name + "\nManaCost:U\nTypes:Sorcery\n" + body + "\nOracle:x\n"
}

// tapeMiscServed requires the tape path to have served at least minServed
// answers with no legacy ask ending a run.
func tapeMiscServed(t *testing.T, what string, st resolve.Stats, minServed int64) {
	t.Helper()
	if st.Served < minServed || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("%s: the converted asks were not served from the tape: %+v", what, st)
	}
}

const tapeMiscFrontSrc = "Name:Tape Front\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nAlternateMode:DoubleFaced\nOracle:x\n" +
	"\nALTERNATE\n\nName:Tape Back\nTypes:Creature Elf\nPT:2/2\nOracle:x\n"
