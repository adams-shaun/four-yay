package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
)

func init() { Register("StoreSVar", effStoreSVar) }

// effStoreSVar implements Forge's ability-body SVar write (api:StoreSVar,
// Forge's `sa.setSVar(name, value)`): SVar$ names the variable, Type$ says how
// Expression$ is read, and the resolved integer is written onto the resolving
// body's SOURCE object through events.StoreSVar, where a later reader sees it
// through the object's runtime SVar store (effects.runtimeSVar, rules'
// cdaValue). The corpus shape this build targets is the ETB life payment
//
//	SVar:PayLife:AB$ StoreSVar | Cost$ Mandatory PayLife<X> | SVar$ LifePaidOnETB | Type$ Calculate | Expression$ X
//	SVar:X:Count$xPaid
//
// where the announced-and-paid X (Count$xPaid) is stored under LifePaidOnETB
// and read back by the card's characteristic-defining SetPower$/
// SetToughness$ (Minion of the Wastes, Nameless Race) or its token's
// TokenPower$/TokenToughness$ (Phyrexian Processor).
//
// Type$ resolves through the ONE shared evaluator, effects.NumResolved, whose
// grammar already covers every expression form the modelled types carry: a
// literal (Type$ Number | Expression$ 0), an SVar name (Type$ Calculate |
// Expression$ X, Type$ Number | Expression$ 1), and an SVar name with a /Op
// suffix (Type$ CountSVar | Expression$ X/Plus.1, X/Plus.Y). The Forge types
// this build does NOT evaluate (Type$ Triggered/Targeted, which read a
// trigger/target property through a separate ref-head evaluator) fail LOUDLY:
// no value is written and a Note names the shape, rather than a silent zero
// that would look like a real announcement of 0.
//
// The write is keyed and last-write-wins (Forge's setSVar), so a later
// StoreSVar of the same name on the same object replaces the value; the
// events.StoreSVar fold is a map insert, so no map range ever reaches an
// event, an option list or a view. A same-resolution sub that reads the name
// back (a StoreSVar whose SubAbility$ names another StoreSVar, the
// Spark-Fiend / Join-Forces family) resolves it through the object store the
// fold just populated, exactly as the printed table would have.
func effStoreSVar(h Host, c *Ctx, sa *cards.SA) {
	name := strings.TrimSpace(sa.Params["SVar"])
	if name == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "StoreSVar with no SVar$ name"})
		return
	}
	typ := strings.TrimSpace(sa.ParamStr(cards.PKType))
	switch typ {
	case "Number", "Calculate", "CountSVar":
		// The evaluated forms: NumResolved's grammar covers each.
	default:
		// Triggered/Targeted (and anything else) read a property this
		// primitive does not evaluate; no write, and a loud Note naming the
		// shape rather than a silent zero.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "StoreSVar Type$ " + typ + " is not evaluated (no value stored)"})
		return
	}
	v, ok := NumResolved(h, c, sa, "Expression", 0)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "StoreSVar Expression$ " + strings.TrimSpace(sa.Params["Expression"]) +
				" is not resolvable (no value stored)"})
		return
	}
	h.Emit(events.Event{Kind: events.StoreSVar, Obj: c.Source, Text: name, Amount: v})
}
