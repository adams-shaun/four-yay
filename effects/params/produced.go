package params

import "strings"

// ManaSymbols is the set of single-letter mana symbols the engine recognises
// in a Produced$ value: the five colours plus colourless. A Produced$ value
// whose (brace/space-stripped) runes all lie in this set is a plain literal
// the rune walk may add one mana per symbol ("R" adds one red, "RR" two red,
// "W U B R G" one of each); a value containing any other rune must not be
// walked.
const ManaSymbols = "WUBRGC"

// ComboColours parses a Produced$ value of the form "Combo <colour> ..." into
// the colours it names. ok is true only when every token after the "Combo "
// prefix is a single one of the five coloured-mana letters and there is at
// least one, so "Combo R G" returns (["R","G"], true) and "Combo Any" (which
// keeps its five-colour meaning), "Combo Chosen", "Combo ColorIdentity" and
// "Combo W B G C" (colourless is not a colour this engine asks for) all
// return (_, false) so the caller falls back to its own handling rather than
// inventing a colour list. rules' mana-activation colour ask and effMana's
// fail-closed walk share this one classifier so the two cannot disagree.
func ComboColours(produced string) (cols []string, ok bool) {
	p := strings.TrimSpace(produced)
	if !strings.HasPrefix(p, "Combo ") {
		return nil, false
	}
	for tok := range strings.FieldsSeq(p[len("Combo "):]) {
		if len(tok) != 1 || !strings.Contains("WUBRG", tok) {
			return nil, false
		}
		cols = append(cols, tok)
	}
	return cols, len(cols) > 0
}
