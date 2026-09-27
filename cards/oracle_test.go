package cards

import (
	"strings"
	"testing"
)

// The packet is the whole of what an Oracle-audit author may read, so it must
// carry the printed card and nothing from the script: no ability lines, no
// Param$ syntax, no SVars, no keyword lines.
func TestOraclePacketCarriesNoScript(t *testing.T) {
	src := "Name:Test Push\nManaCost:B\nTypes:Instant\n" +
		"A:SP$ Destroy | ValidTgts$ Creature.cmcLE2 | SubAbility$ DBRevolt | SpellDescription$ Destroy it.\n" +
		"SVar:DBRevolt:DB$ Destroy | Defined$ Targeted | ConditionCheckSVar$ X\n" +
		"K:Flash\n" +
		`Oracle:Destroy target creature if it has mana value 2 or less.\nRevolt — More.` + "\n"
	c, d := ParseBytes("t.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	p := OraclePacket(c)
	for _, leak := range []string{"$", "SVar", "SP$", "DBRevolt", "K:", "cmcLE2"} {
		if strings.Contains(p, leak) {
			t.Errorf("packet leaks script token %q:\n%s", leak, p)
		}
	}
	for _, want := range []string{"Name: Test Push", "Mana cost: B", "Type: Instant",
		"Destroy target creature if it has mana value 2 or less.\n  Revolt — More.", "oracle_sha: "} {
		if !strings.Contains(p, want) {
			t.Errorf("packet lacks %q:\n%s", want, p)
		}
	}
	if a, b := OracleDigest(c), OracleDigest(c); a != b || len(a) != 12 {
		t.Errorf("digest %q/%q, want a stable 12-hex value", a, b)
	}
}
