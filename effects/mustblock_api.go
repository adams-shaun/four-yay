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
	var attackers []state.Target
	if spec == "" {
		// Forge's default attacker is the resolving source (Vortex Elemental,
		// Auriok Siege Sled, and the other self-lure abilities).
		attackers = []state.Target{{Obj: c.Source}}
	} else {
		attackers = DefinedSpec(h, c, spec)
	}
	if len(attackers) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "MustBlock DefinedAttacker$ unresolved"})
		return
	}
	attackerIDs := make([]state.ObjID, 0, len(attackers))
	seenAttackers := make(map[state.ObjID]bool)
	for _, attacker := range attackers {
		if attacker.IsPlayer || attacker.Obj == 0 || seenAttackers[attacker.Obj] {
			continue
		}
		if o := h.Game().Obj(attacker.Obj); o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		seenAttackers[attacker.Obj] = true
		attackerIDs = append(attackerIDs, attacker.Obj)
	}
	if len(attackerIDs) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "MustBlock DefinedAttacker$ unresolved"})
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
		for _, attacker := range attackerIDs {
			h.AddContinuous(state.ContinuousEffect{
				Source: c.Source, Controller: c.Controller, UntilEOT: effectUntilEOT(h, c.Source, dur), Duration: dur,
				Restriction: "MustBlock", RestrictParams: map[string]string{"ValidCreature": "Card.IsRemembered"},
				Remembered: []state.ObjID{target.Obj}, MustBlockAttacker: attacker,
			})
		}
	}
}
