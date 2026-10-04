package effects

import "github.com/adams-shaun/gorge/effects/params"

// ManaSymbols is the set of single-letter mana symbols the engine recognises
// in a Produced$ value (params.ManaSymbols).
const ManaSymbols = params.ManaSymbols

// ComboColours parses a Produced$ "Combo <colour> ..." value into the colours
// it names (params.ComboColours, the one classifier rules' colour ask and
// effMana's fail-closed walk share).
func ComboColours(produced string) (cols []string, ok bool) { return params.ComboColours(produced) }
