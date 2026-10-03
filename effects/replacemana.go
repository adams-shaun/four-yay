package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// effReplaceMana is api:ReplaceMana: it rewrites one in-flight ManaAdd event
// for a ProduceMana replacement (its own API, so it lives beside, not in,
// api:Mana's resolution file).
func effReplaceMana(_ Host, c *Ctx, sa *cards.SA) {
	if c == nil {
		return
	}
	if only := strings.TrimSpace(sa.ParamStr(cards.PKReplaceOnly)); only != "" && only != c.Mana.Type {
		return
	}
	if n := Num(nil, c, sa, "ReplaceAmount", 1); n > 0 {
		c.Mana.Amount *= n
	}
	kind := strings.TrimSpace(sa.ParamStr(cards.PKReplaceMana))
	if kind != "" {
		c.Mana.Amount = 1
	}
	if kind == "" {
		kind = strings.TrimSpace(sa.ParamStr(cards.PKReplaceType))
	}
	if kind == "" {
		kind = strings.TrimSpace(sa.ParamStr(cards.PKReplaceColor))
	}
	if kind == "" {
		return
	}
	switch effReplaceMana1b71Codes.Code(string(strings.ToLower(kind))) {
	case effReplaceMana1b71White:
		kind = "W"
	case effReplaceMana1b71Blue:
		kind = "U"
	case effReplaceMana1b71Black:
		kind = "B"
	case effReplaceMana1b71Red:
		kind = "R"
	case effReplaceMana1b71Green:
		kind = "G"
	case effReplaceMana1b71Any:
		kind = c.Mana.Choice
	}
	if len(kind) == 1 && strings.ContainsRune(ManaSymbols, rune(kind[0])) {
		c.Mana.Type = kind
	}
}

const (
	effReplaceMana1b71White uint16 = 1 // "white"
	effReplaceMana1b71Blue  uint16 = 2 // "blue"
	effReplaceMana1b71Black uint16 = 3 // "black"
	effReplaceMana1b71Red   uint16 = 4 // "red"
	effReplaceMana1b71Green uint16 = 5 // "green"
	effReplaceMana1b71Any   uint16 = 6 // "any", "chosen"
)

var effReplaceMana1b71Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "white", Val: effReplaceMana1b71White},
	state.StrEntry[uint16]{Key: "blue", Val: effReplaceMana1b71Blue},
	state.StrEntry[uint16]{Key: "black", Val: effReplaceMana1b71Black},
	state.StrEntry[uint16]{Key: "red", Val: effReplaceMana1b71Red},
	state.StrEntry[uint16]{Key: "green", Val: effReplaceMana1b71Green},
	state.StrEntry[uint16]{Key: "any", Val: effReplaceMana1b71Any},
	state.StrEntry[uint16]{Key: "chosen", Val: effReplaceMana1b71Any},
)
