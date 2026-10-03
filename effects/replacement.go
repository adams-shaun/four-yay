package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

func init() { Register("ReplaceEffect", effReplaceEffect) }

// effReplaceEffect changes the event currently being replaced. Forge names
// the event field in VarName$ and supplies its replacement value in VarValue$.
// A ReplaceCount$ body -- directly (Torture Pit: VarValue$
// ReplaceCount$DamageAmount/Plus.2) or behind an SVar name (Fiery
// Emancipation: VarValue$ X, SVar:X:ReplaceCount$DamageAmount/Thrice) --
// expresses its new amount in terms of the HELD event's own amount, which
// only the host reading the in-flight event can resolve, so it is handed over
// unresolved. Every other value form is a plain number in the replacement
// source's context, which Num evaluates (an unresolvable expression degrades
// to zero, and the host's ReplaceEvent then leaves the event untouched rather
// than erasing the damage).
func effReplaceEffect(h Host, c *Ctx, sa *cards.SA) {
	p := ReplaceEffectOf(sa)
	noteUnreadParams(h, c, "ReplaceEffect", p.Unread)
	name := p.VarName
	if name == "" {
		return
	}
	raw := strings.TrimSpace(p.VarValue.Text)
	if c != nil && c.SVars != nil {
		if body, ok := c.SVars[raw]; ok && strings.HasPrefix(body, "ReplaceCount$") {
			raw = body
		}
	}
	if strings.HasPrefix(raw, "ReplaceCount$") {
		h.ReplaceEvent(name, rewriteReplaceCountOperand(h, c, raw), 0)
		return
	}
	h.ReplaceEvent(name, raw, numText(h, c, p.VarValue, 0))
}

// rewriteReplaceCountOperand resolves a ReplaceCount$ operand the host's
// arithmetic cannot parse, so the bound value survives the handover (task
// wildgrowth1 stretch): replCountOp prices only numeric operands
// (Plus.3/Minus.4/Times.2) and the word ops, so Taii Wakeen's
// ReplaceCount$DamageAmount/Plus.Y -- the Plus operand behind the SVar
// Y:Count$ChosenNumber, the Effect's frozen SetChosenNumber$ binding -- used
// to degrade to "base unchanged" and the activation did nothing. A resolvable
// non-numeric operand is rewritten to its number in THIS body's context (the
// face SVar table the printed R: lines need, plus Ctx.ChosenNumber for the
// effect-created binding); an unresolvable one returns raw unchanged, which
// the host reads as base unchanged -- the fail-closed direction, never an
// erased amount.
func rewriteReplaceCountOperand(h Host, c *Ctx, raw string) string {
	_, body, _ := strings.Cut(raw, "ReplaceCount$")
	field, op, hasOp := strings.Cut(body, "/")
	if !hasOp {
		return raw
	}
	verb, operand, hasOperand := strings.Cut(op, ".")
	if !hasOperand {
		return raw // the word ops (Twice, Thrice, HalfDown, ...) carry no operand
	}
	if _, err := strconv.Atoi(operand); err == nil {
		return raw // already numeric
	}
	n, ok := resolveCountOperand(h, c, operand, 0)
	if !ok {
		return raw
	}
	return "ReplaceCount$" + field + "/" + verb + "." + strconv.FormatInt(int64(n), 10)
}
