package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Extort", effExtort) }

// effExtort implements CR 702.100: "Whenever you cast a spell, you may pay
// {W/B}. If you do, each opponent loses 1 life and you gain that much life."
//
// The keyword expands (cards/keywords.go) to a SpellCast trigger whose body
// is DB$ Extort. The trigger fires once per spell the controller casts, so
// the caster — c.Controller, the controller of the Extort permanent at the
// moment the trigger resolves — is asked whether to pay the hybrid pip. The
// hybrid {W/B} is a single pip payable as either colour; the ask is a KModes
// yes/no and the answer is carried back through Ctx.Extort (the resume arm
// in rules/resolution.go records it), the same mid-resolution shape the
// unless-pay consumers use. Payment is charged from the caster's pool as
// one mana of either W or B if either colour is available; the drain is what
// actually happens on a pay.
func effExtort(h Host, c *Ctx, sa *cards.SA) {

	ans := string("")

	switch ans {
	case "pay":
		// Re-entry, paid: drain each opponent 1 life and gain that much.
	case "decline":
		return
	default:
		// First pass: pose the optional payment to the caster.
		d := &decision.Decision{Player: c.Controller, Kind: decision.KModes,
			Min: 1, Max: 1, Source: c.Source, ResumeKind: "extort",
			ResumeSA: sa, Prompt: "Extort: pay {W/B}?",
			Options: []decision.Option{
				{Index: 0, Kind: "mode", Label: "Pay {W/B} — each opponent loses 1", Obj: c.Source, Player: c.Controller},
				{Index: 1, Kind: "mode", Label: "Don't pay", Obj: c.Source, Player: c.Controller},
			}}
		g := h.Game()
		pool := extortPoolPips(g, c.Controller)
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand. Its record (the
			// "extort" answer record rules shares with the resume arm)
			// charged the pip on a "pay" when the pool held one; the drain
			// runs exactly when that charge was made.
			if len(ans) == 0 || ans[0].Index != 0 || extortPoolPips(g, c.Controller) >= pool {
				return
			}
			extortDrain(h, g, c.Controller)
			return
		}
		if h.Ask(d) {
			return // resolution suspended; the answer re-enters this effect.
		}
		// Fuzz/no-engine host: the deterministic decline (R-9).
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Extort declined (no engine host to ask)"})
		return
	}
	extortDrain(h, h.Game(), c.Controller)
}

// extortDrain is a paid Extort's drain: each opponent loses 1 life and the
// controller gains that much.
func extortDrain(h Host, g *state.Game, controller state.PlayerID) {
	n := int32(0)
	for _, p := range g.AliveFrom(controller) {
		if p == controller {
			continue
		}
		h.Emit(events.Event{Kind: events.LifeChange, Player: p, Amount: -1})
		n++
	}
	if n > 0 {
		h.Emit(events.Event{Kind: events.LifeChange, Player: controller, Amount: n})
	}
}

// extortPoolPips counts the units in p's pool that can pay the {W/B} pip.
func extortPoolPips(g *state.Game, p state.PlayerID) int32 {
	if int(p) >= len(g.Players) {
		return 0
	}
	return int32(g.Players[p].Pool[state.MW]) + int32(g.Players[p].Pool[state.MB])
}

// ManaPaysExtort reports whether p has at least one W or B in the pool to
// satisfy the {W/B} hybrid pip. Kept available for a test double that wants
// to verify the payment gate; effExtort itself charges through the caller's
// resumeResolution payment path when present.
func ManaPaysExtort(g *state.Game, p state.PlayerID) bool {
	if int(p) >= len(g.Players) {
		return false
	}
	return g.Players[p].Pool[state.MW] > 0 || g.Players[p].Pool[state.MB] > 0
}
