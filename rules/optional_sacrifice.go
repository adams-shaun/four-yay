package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// optional_sacrifice.go is the ONE path for the optional additional costs
// that are "you may sacrifice <a permanent of a kind> as you cast this
// spell": Casualty N (CR 702.153, a creature with power N or greater) and
// Bargain (CR 702.166, an artifact, enchantment, or token). Each is a cast
// mode the walk offers beside the plain cast (never a substitute for the
// mana cost); the election is posed mid-cast before payment, and the chosen
// permanent is sacrificed by payCast with every other cost part. A new
// keyword of the shape is one row here.

// optionalSacrifice is one row.
type optionalSacrifice struct {
	// mode is the cast mode (decision.Option.Mode) that elects the cost;
	// answer is the Option.Kind of the election's choices (castAnswerCodes).
	mode, answer string
	// spec is the eligible permanents' filter, matched from the spell;
	// minPower requires power >= the cast's casualtyN (Casualty's N).
	spec     string
	minPower bool
	prompt   string
	// scope is the spellScope mode the offer prices under: Casualty prices
	// the plain spell (""), Bargain its own mode so the Spell.Bargain
	// ReduceCost statics (Hamlet Glutton, Ice Out, Johann's Stopgap) price
	// the discounted cast and the charge agrees.
	scope string
	// offered reports whether the spell carries the keyword (printed or
	// granted) and the power floor the candidates must meet.
	offered func(e *Engine, id state.ObjID) (int32, bool)
	// stale is the Note when no candidate remains by the time the
	// election is posed: the cast degrades to the plain cast.
	stale string
}

const (
	optSacCasualty = iota
	optSacBargain
)

var optionalSacrifices = [...]optionalSacrifice{
	optSacCasualty: {mode: "casualty", answer: "casualty", spec: "Creature.YouCtrl", minPower: true,
		// The variable form (Casualty:X, Ob Nixilis, the Adversary) has no
		// threshold: the sacrificed creature's own power names the amount,
		// so any creature qualifies and the ask's power gate reads 0.
		offered: func(e *Engine, id state.ObjID) (int32, bool) {
			info, ok := e.casualtySpec(id)
			if !ok || info.variable {
				return 0, ok
			}
			return info.threshold, true
		},
		prompt: "Choose a creature to sacrifice for casualty",
		stale:  "casualty no longer payable; casting without casualty"},
	optSacBargain: {mode: "bargained", answer: "bargain", spec: "Artifact,Enchantment,token", scope: "bargained",
		offered: func(e *Engine, id state.ObjID) (int32, bool) {
			return 0, e.stackKeywordPossibleH(id, kwhBargain)
		},
		prompt: "Choose an artifact, enchantment, or token to sacrifice for bargain",
		stale:  "bargain no longer payable; casting without bargain"},
}

// optionalSacrificeFor is the row whose mode is mode, or nil.
func optionalSacrificeFor(mode string) *optionalSacrifice {
	for i := range optionalSacrifices {
		if optionalSacrifices[i].mode == mode {
			return &optionalSacrifices[i]
		}
	}
	return nil
}

// optionalSacrificeCandidates returns the permanents p may sacrifice for row
// r while casting spell, in stable battlefield order (a replay derives the
// identical option list). n is Casualty's power floor (ignored otherwise). A
// permanent the cast already committed to another sacrifice cost is
// excluded.
func (e *Engine) optionalSacrificeCandidates(r *optionalSacrifice, p state.PlayerID, spell state.ObjID, n int32) []state.ObjID {
	var committed []state.ObjID
	if e.cast != nil && e.cast.card == spell {
		committed = e.cast.Sacs
	}
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if containsObjID(committed, id) || !e.matchesSpecFrom(r.spec, id, p, spell) {
			continue
		}
		if r.minPower && e.Power(id) < n {
			continue
		}
		out = append(out, id)
	}
	return out
}

// optionalSacrificeAsk announces the cast mode's optional sacrifice before
// payment. The chosen permanent remains on the battlefield until payCast,
// after target selection.
func (e *Engine) optionalSacrificeAsk() bool {
	pc := e.cast
	r := optionalSacrificeFor(pc.mode)
	if r == nil || pc.casualtyDone {
		return false
	}
	pc.casualtyDone = true
	candidates := e.optionalSacrificeCandidates(r, pc.player, pc.card, pc.casualtyN)
	if (r.minPower && pc.casualtyN < 0) || len(candidates) == 0 {
		if !r.minPower {
			pc.mode = ""
		}
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card, Text: r.stale})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: r.prompt, Source: pc.card}
	for _, id := range candidates {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: r.answer, Obj: id, Label: e.targetName(id)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}
