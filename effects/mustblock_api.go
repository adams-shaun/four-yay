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
	// BlockAllDefined names all attackers in THIS resolution. A single
	// Fighter Class trigger names only one attacker: two independent triggers
	// cannot combine to grant a blocker permission to block both (CR 509.1a).
	// Blaze of Glory defines the entire attacking set in one resolution.
	blockAll := isTrue(sa.ParamStr(cards.PKBlockAllDefined)) && len(attackerIDs) > 1
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
				MustBlockAllAttackers: blockAll,
			})
		}
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
