package scriptfacts

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
)

// ForetellCost is the cost of casting a card that was designated foretold by
// an effect rather than by its K:Foretell action. The explicit keyword cost
// wins; ForetoldCost$ True means printed mana cost less {2} generic.
func ForetellCost(f *cards.Face) (costvocab.Cost, bool) {
	if f == nil {
		return costvocab.Cost{}, false
	}
	if raw, ok := f.KeywordParam("Foretell"); ok && strings.TrimSpace(raw) != "" {
		c := costvocab.ParseCost(raw)
		return c, len(c.Unknown) == 0
	}
	if strings.TrimSpace(f.ManaCost) == "" || strings.EqualFold(strings.TrimSpace(f.ManaCost), "no cost") {
		return costvocab.Cost{}, false
	}
	c := costvocab.ParseCost(f.ManaCost)
	if len(c.Unknown) != 0 {
		return costvocab.Cost{}, false
	}
	if c.Generic >= 2 {
		c.Generic -= 2
	} else {
		c.Generic = 0
	}
	return c, true
}
