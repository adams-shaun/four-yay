package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
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
	if strings.Compare(strings.TrimSpace(sa.ParamStr(cards.PKChoices)), "Creature.untapped+DefenderCtrl") == 0 {
		effMustBlockChoicePool(h, c, sa)
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

// effMustBlockChoicePool implements Crashing Boars' exact choice-pool shape.
// Candidate iteration follows Game.Objs order, and the no-ask stand-in is the
// first eligible creature in that same deterministic order.
func effMustBlockChoicePool(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	attacker := g.Obj(c.Source)
	if attacker == nil || attacker.Zone != state.ZBattlefield {
		return
	}
	chooser := c.DefendingPlayer
	if !chooser.IsPlayer || int(chooser.Player) >= len(g.Players) {
		return
	}
	var opts []decision.Option
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone != state.ZBattlefield || o.Tapped || !MatchesObjectCtx(g, "Creature.untapped+DefenderCtrl", o, c.SpecContext(chooser.Player)) {
			continue
		}
		label := "Creature"
		if f := o.Face(); f != nil && f.Name != "" {
			label = f.Name
		}
		opts = append(opts, decision.Option{Index: len(opts), Kind: "object", Label: label, Obj: o.ID})
	}
	if len(opts) == 0 {
		return
	}
	d := &decision.Decision{Player: chooser.Player, Kind: decision.KChoose, Min: 1, Max: 1,
		ResumeKind: "mustblock", ResumeSA: sa, Prompt: "Choose a creature to block", Source: c.Source, Options: opts}
	ans, served := AskTape(h, d)
	picked := opts[0].Obj
	if served && len(ans) > 0 {
		picked = ans[0].Obj
	}
	if picked == 0 {
		return
	}
	dur := sa.ParamStr(cards.PKDuration)
	h.AddContinuous(state.ContinuousEffect{
		Source: c.Source, Controller: c.Controller, UntilEOT: effectUntilEOT(h, c.Source, dur), Duration: dur,
		Restriction: "MustBlock", RestrictParams: map[string]string{"ValidCreature": "Card.IsRemembered"},
		Remembered: []state.ObjID{picked}, MustBlockAttacker: c.Source,
	})
}
