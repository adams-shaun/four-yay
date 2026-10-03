package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Demonstrate", effDemonstrate) }

// effDemonstrate implements DB$ Demonstrate (CR 702.152): the resolution of
// the demonstrate trigger both the printed K:Demonstrate expansion
// (cards/kw_demonstrate.go) and the granted-keyword synthesis
// (rules/trigger_granted.go's checkGrantedDemonstrateTriggers) queue. The
// oracle text the body serves: "Whenever you cast this spell, you may copy
// it. If you do, choose an opponent to also copy it. Each copy becomes a
// token."
//
// The two "may/choose" halves are real mid-resolution asks (the fx42
// transport: Ctx.Demonstrate* fields, rebuilt by rules' "demonstrate" resume
// arm and consumed and cleared at the top of this walk):
//
//   - Stage 0, the election: "you may copy it" -- a yes/no KChoose to the
//     caster (the trigger's controller). A decline ends the trigger having
//     copied nothing. The no-host deterministic answer (an effects-package
//     test double, a fuzz run) is the decline under one R-9 Note.
//
//   - Stage 1, the opponent: "choose an opponent to also copy it" -- a
//     one-pick KChoose over the caster's living opponents in AliveFrom
//     order. Zero living opponents means the clause has no candidate (only
//     the caster's copy exists); ONE means a decision nobody could answer
//     differently, so the copies are recorded without an ask (the
//     strict-supersets convention, the battle-protector precedent). The
//     no-host deterministic answer is the first opponent under one R-9 Note.
//
// Both copies are emitted only once the opponent clause is settled (the
// caster's copy first, then the chosen opponent's): an emission between the
// two asks would put a stack spell on top of the demonstrate wrapper, and the
// opponent ask's resume point -- the top of the stack -- would then re-enter
// the COPY's resolution instead of this body's. Each copy is an ordinary
// StackCopy mint (the same event effCopySpellAbility emits) keeping the
// original's targets -- the standing copy-keeps-targets stand-in the M4
// copy-target task owns (the printed carriers' oracle adds "Players may
// choose new targets for their copies", a choice this build cannot pose). A
// creature-spell copy becomes a token when it RESOLVES, through the standing
// CR 707.10g fold in events.Apply's Move case (enteredFrom ZStack + IsCopy ->
// IsToken) -- the reminder text's "each copy becomes a token", no new mint
// machinery. A copy is never a cast (it emits StackCopy, never PutOnStack),
// so a demonstrate copy cannot re-fire a demonstrate trigger.
func effDemonstrate(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	// The spell: Defined$ TriggeredSpellAbility, the same read
	// effCopySpellAbility's TriggeredSpellAbility arm makes -- the cast
	// spell rides Remembered (the TriggerPush's IDs). The ability-object
	// role stays first for symmetry, though a SpellCast trigger never
	// carries one.
	var spell state.ObjID
	if id := c.TriggerAbility; id != 0 {
		spell = id
	} else {
		for _, t := range c.Remembered {
			if !t.IsPlayer && t.Obj != 0 {
				spell = t.Obj
				break
			}
		}
	}
	if spell == 0 {
		return
	}
	// The source spell must actually be on the stack; a copy of something
	// that already left it is a no-op, the same totality stance as every
	// other effect primitive.
	so := g.Obj(spell)
	if so == nil || so.Zone != state.ZStack || !spellCopyAllowed(h, spell) {
		return
	}
	name := ""
	if f := so.Face(); f != nil {
		name = f.Name
	}

	answered := false
	stage := int(0)

	yes := false
	opp := demonstratePlayer(([]state.Target)(nil))

	if answered {
		if stage != 1 {
			// The election was answered. A decline ends the trigger having
			// copied nothing; an acceptance moves on to the opponent
			// clause below. Any other stage is a malformed resume and
			// copies nothing.
			if !yes {
				return
			}
		} else {
			demonstrateCopies(h, spell, c.Controller, opp)
			return
		}
	} else {
		// Stage 0: the may-copy election.
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
			Source: c.Source, Min: 1, Max: 1, ResumeKind: "demonstrate",
			ResumeSA: sa, ResumeTarget: 0,
			Prompt: "Demonstrate: copy " + name + "?"}
		d.Options = append(d.Options,
			decision.Option{Index: 0, Kind: "yes", Label: "Yes — copy", Player: c.Controller},
			decision.Option{Index: 1, Kind: "no", Label: "No", Player: c.Controller})
		if ans, ok := AskTape(h, d); ok {
			// The election's answer in hand (the "demonstrate" arm's
			// stage 0): a decline ends the trigger, a yes goes on to the
			// opponent clause below, as the re-entry does.
			if len(ans) == 0 || ans[0].Kind != "yes" {
				return
			}
		} else {
			_ = Ask(h, d)

			// No host to ask (the R-9 fuzz/test contract): the deterministic
			// decline -- a may-copy the engine cannot ask is never copied.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "demonstrate copy resolved as the decline (no engine host to ask)"})
			return
		}

	}

	// The opponent choice: "choose an opponent to also copy it". The
	// candidate list is the caster's living opponents in AliveFrom scan
	// order, the deterministic order every player scan here uses.
	var opps []state.PlayerID
	for _, p := range g.AliveFrom(c.Controller) {
		if p != c.Controller {
			opps = append(opps, p)
		}
	}
	if len(opps) == 0 {
		// No living opponent: the choice has no candidate, so only the
		// caster's copy exists.
		h.Emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: c.Controller})
		return
	}
	if len(opps) == 1 {
		// A decision nobody could answer differently is never posted (the
		// strict-supersets convention, the battle-protector precedent): one
		// living opponent means both copies are recorded without an ask.
		h.Emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: c.Controller})
		h.Emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: opps[0]})
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
		Source: c.Source, Min: 1, Max: 1, ResumeKind: "demonstrate",
		ResumeSA: sa, ResumeTarget: 1,
		Prompt: "Choose an opponent to also copy " + name}
	for j, p := range opps {
		d.Options = append(d.Options, decision.Option{Index: j, Kind: "player",
			Player: p, Label: g.Players[p].Name})
	}
	if ans, ok := AskTape(h, d); ok {
		// The opponent pick in hand (the "demonstrate" arm's stage 1).
		var picked []state.Target
		for _, o := range ans {
			if o.Kind == "player" {
				picked = append(picked, state.Target{Player: o.Player, IsPlayer: true})
			}
		}
		demonstrateCopies(h, spell, c.Controller, demonstratePlayer(picked))
		return
	}
	_ = Ask(h, d)

	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "demonstrate opponent resolved as the first opponent (no engine host to ask)"})
	h.Emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: c.Controller})
	h.Emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: opps[0]})
}

// demonstrateCopies emits the answered opponent clause's copies, the
// caster's first. An unanswered pick (a malformed resume) keeps only the
// caster's copy.
func demonstrateCopies(h Host, spell state.ObjID, caster, opp state.PlayerID) {
	h.Emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: caster})
	if opp != 0 {
		h.Emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: opp})
	}
}

// demonstratePlayer is the first player Target of an answered pick list
// (the demonstrate opponent ask's answer), or 0.
func demonstratePlayer(ts []state.Target) state.PlayerID {
	for _, t := range ts {
		if t.IsPlayer {
			return t.Player
		}
	}
	return 0
}
