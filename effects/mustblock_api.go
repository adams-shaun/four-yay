package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("MustBlock", effMustBlock) }

// effMustBlock creates one scoped duty per chosen blocker. The attacker is
// frozen at resolution, not re-resolved when the defender declares blocks.
func effMustBlock(h Host, c *Ctx, sa *cards.SA) {
	if !cards.MustBlockNamedTargetShape(sa) {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "MustBlock selector shape unimplemented"})
		return
	}
	spec := strings.TrimSpace(sa.ParamStr(cards.PKDefinedAttacker))
	attackers := DefinedSpec(h, c, spec)
	if spec == "" || len(attackers) != 1 || attackers[0].IsPlayer || attackers[0].Obj == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "MustBlock DefinedAttacker$ unresolved"})
		return
	}
	attacker := attackers[0].Obj
	if o := h.Game().Obj(attacker); o == nil || o.Zone != state.ZBattlefield {
		return
	}
	dur := sa.ParamStr(cards.PKDuration)
	for _, target := range Defined(h, c, sa) {
		if target.IsPlayer {
			continue
		}
		o := h.Game().Obj(target.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		h.AddContinuous(state.ContinuousEffect{
			Source: c.Source, Controller: c.Controller, UntilEOT: effectUntilEOT(h, c.Source, dur), Duration: dur,
			Restriction: "MustBlock", RestrictParams: map[string]string{"ValidCreature": "Card.IsRemembered"},
			Remembered: []state.ObjID{target.Obj}, MustBlockAttacker: attacker,
		})
	}
}
