package cards

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// OracleText returns a face's printed rules text with the corpus's escaped
// line breaks ("\n" written as two characters) turned into real newlines.
func (f *Face) OracleText() string {
	return strings.ReplaceAll(f.Oracle, `\n`, "\n")
}

// OracleDigest is the first 12 hex digits of the sha256 of a card's Oracle
// text, faces joined by "\n//\n". The Oracle-text audit
// (rules/testdata/oracle) records the digest its author read, so a corpus pin
// bump that changes the printed text (an erratum) marks the scenarios stale
// instead of silently re-judging them against text nobody read.
func OracleDigest(c *Card) string {
	parts := make([]string, 0, len(c.Faces))
	for _, f := range c.Faces {
		parts = append(parts, f.OracleText())
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n//\n")))
	return hex.EncodeToString(sum[:])[:12]
}

// OraclePacket renders what a player can read on the printed card -- name,
// mana cost, type line, P/T (or loyalty/defense) and Oracle text, per face --
// and nothing from the Forge script. It is the only card text an Oracle-audit
// scenario author is shown (cmd/oraclepacket).
func OraclePacket(c *Card) string {
	var b strings.Builder
	for i, f := range c.Faces {
		if i > 0 {
			b.WriteString("--- other face ---\n")
		}
		b.WriteString("Name: " + f.Name + "\n")
		if f.ManaCost != "" && f.ManaCost != "no cost" {
			b.WriteString("Mana cost: " + f.ManaCost + "\n")
		}
		b.WriteString("Type: " + strings.Join(f.Types, " ") + "\n")
		switch {
		case f.PT != "":
			b.WriteString("P/T: " + f.PT + "\n")
		case f.Loyalty != "":
			b.WriteString("Loyalty: " + f.Loyalty + "\n")
		case f.Defense != "":
			b.WriteString("Defense: " + f.Defense + "\n")
		}
		if t := f.OracleText(); t != "" {
			b.WriteString("Oracle:\n  " + strings.ReplaceAll(t, "\n", "\n  ") + "\n")
		}
	}
	b.WriteString("oracle_sha: " + OracleDigest(c) + "\n")
	return b.String()
}
