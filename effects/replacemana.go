package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// effReplaceMana is api:ReplaceMana: it rewrites one in-flight ManaAdd event
// for a ProduceMana replacement (its own API, so it lives beside, not in,
// api:Mana's resolution file).
func effReplaceMana(_ Host, c *Ctx, sa *cards.SA) {
	if c == nil {
		return
	}
	if only := strings.TrimSpace(sa.ParamStr(cards.PKReplaceOnly)); only != "" && only != c.ManaType {
		return
	}
	if n := Num(nil, c, sa, "ReplaceAmount", 1); n > 0 {
		c.ManaAmount *= n
	}
	kind := strings.TrimSpace(sa.ParamStr(cards.PKReplaceMana))
	if kind != "" {
		c.ManaAmount = 1
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
	switch strings.ToLower(kind) {
	case "white":
		kind = "W"
	case "blue":
		kind = "U"
	case "black":
		kind = "B"
	case "red":
		kind = "R"
	case "green":
		kind = "G"
	case "any", "chosen":
		kind = c.ManaChoice
	}
	if len(kind) == 1 && strings.ContainsRune(ManaSymbols, rune(kind[0])) {
		c.ManaType = kind
	}
}
