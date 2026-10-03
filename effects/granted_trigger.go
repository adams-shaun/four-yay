package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// linkGrantedTrigger resolves a granted trigger line's Execute$ body against
// the SAME source SVar table the line came from, the way cards/link.go links
// a printed trigger to its body. Without this the granted trigger carries a
// nil Effect, so the placement gate cannot read the body's ValidTgts$ (the
// fight's "up to one target creature you don't control" ask is never posed)
// and rules' target dispatch has no SA. CopyPermanent's and Clone's
// AddTriggers$ grants share it, so neither API's own resolution file reads a
// parameter of the granted line.
func linkGrantedTrigger(tr *cards.Trigger, table map[string]string) {
	if ex := strings.TrimSpace(tr.ParamStr(cards.PKExecute)); ex != "" {
		tr.Effect = cards.ResolveSVar(table, ex)
	}
}
