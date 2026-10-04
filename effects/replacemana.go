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
	switch effReplaceManaCodes.Code(string(strings.ToLower(kind))) {
	case effReplaceManaWhite:
		kind = "W"
	case effReplaceManaBlue:
		kind = "U"
	case effReplaceManaBlack:
		kind = "B"
	case effReplaceManaRed:
		kind = "R"
	case effReplaceManaGreen:
		kind = "G"
	case effReplaceManaChoice:
		kind = c.Mana.Choice
	}
	if len(kind) == 1 && strings.ContainsRune(ManaSymbols, rune(kind[0])) {
		c.Mana.Type = kind
	}
}

type effReplaceManaCode uint16

const (
	effReplaceManaWhite effReplaceManaCode = iota + 1
	effReplaceManaBlue
	effReplaceManaBlack
	effReplaceManaRed
	effReplaceManaGreen
	effReplaceManaChoice
)

var effReplaceManaCodes = state.NewStrCodes(
	state.StrEntry[effReplaceManaCode]{Key: "white", Val: effReplaceManaWhite},
	state.StrEntry[effReplaceManaCode]{Key: "blue", Val: effReplaceManaBlue},
	state.StrEntry[effReplaceManaCode]{Key: "black", Val: effReplaceManaBlack},
	state.StrEntry[effReplaceManaCode]{Key: "red", Val: effReplaceManaRed},
	state.StrEntry[effReplaceManaCode]{Key: "green", Val: effReplaceManaGreen},
	state.StrEntry[effReplaceManaCode]{Key: "any", Val: effReplaceManaChoice},
	state.StrEntry[effReplaceManaCode]{Key: "chosen", Val: effReplaceManaChoice},
)
