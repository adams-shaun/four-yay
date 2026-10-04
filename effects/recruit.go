package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Recruit", effRecruit) }

// humanSoldierTokenKey is the corpus token script Recruit mints
// (.cards/tokenscripts/w_1_1_human_soldier.txt: a 1/1 white Human Soldier
// creature token).
const humanSoldierTokenKey = "w_1_1_human_soldier"

// effRecruit implements the Recruit keyword action (the named action The
// Hobbit's cards carry -- 10 raw corpus files, all `DB$ Recruit` bodies):
//
//	"Draw a card, then discard a card. If you discarded a nonland card,
//	 create a 1/1 white Human Soldier creature token."
//
// The action is one resolution over the resolving ability's controller:
//
//   - The draw comes first, through drawFor with cursor 0 and this SA as the
//     asking SA, so a Dredge replacement over the draw is a real choice whose
//     answer is applied in place before THIS recruit continues. A Ctx with
//     Draw.Done set skips the draw.
//   - The discard follows: one card from the controller's hand. A hand with
//     EXACTLY one card discards it with no ask (the strict-supersets rule --
//     nobody could answer differently); a hand with more poses a real
//     KChoose (Min = Max = 1, options in hand order). The kernel's answer
//     is applied in place under the shared "discard" decision kind, so no
//     new kind is introduced. An unserved ask takes the front-of-hand card, byte-identical to botpolicy's KChoose clamp.
//   - The token comes last: one events.TokenCreate for
//     humanSoldierTokenKey, owned by the controller, but ONLY when at least
//     one NONLAND card was actually discarded.
func effRecruit(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	ctrl := c.Controller
	// The draw. A non-zero DrawDone is a Dredge resume: the draw already
	// completed and was replaced (CR 701.9 orders draw before discard).
	drawDone := c.Draw.Done
	c.Draw.Done = 0
	if drawDone == 0 {
		drawFor(h, ctrl, 0, sa, drawUptoRider{})
		if h.Suspended() {
			return
		}
	}
	// The discard: exactly one card from the controller's hand.
	hand := zoneOf(g, state.ZHand, ctrl)
	picks := make([]state.ObjID, 0, 1)
	switch {
	case len(hand) == 0:
		// Nothing to discard and therefore no token.
	case len(hand) == 1:
		// The only legal discard: no choice to ask.
		picks = append(picks, hand[0])
	default:
		opts := make([]decision.Option, 0, len(hand))
		for _, id := range hand {
			name := "a card"
			if o := g.Obj(id); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			opts = append(opts, decision.Option{Index: len(opts), Kind: "discard",
				Label: "Discard " + name, Obj: id, Player: ctrl})
		}
		d := &decision.Decision{Player: ctrl, Kind: decision.KChoose, Min: 1, Max: 1,
			Source: c.Source, ResumeKind: "discard", ResumeSA: sa, ResumeTarget: 0,
			Prompt: "Recruit: choose a card to discard", Options: opts}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand, filtered to cards
			// still in this hand.
			picks = recruitPicks(g, ctrl, answerObjs(ans), picks)
			break
		}

		// No host: the deterministic front-of-hand card (the R-9 stand-in),
		// byte-identical to botpolicy's KChoose clamp.
		picks = append(picks, hand[0])
	}
	nonland := false
	for _, id := range picks {
		o := g.Obj(id)
		if o == nil || o.Zone != state.ZHand || o.Owner != ctrl {
			continue
		}
		h.Emit(events.Discard(id, ctrl))
		if o.Face() != nil && !o.Face().IsLand() {
			nonland = true
		}
	}
	if !nonland {
		return
	}
	if _, ok := g.Tokens[humanSoldierTokenKey]; !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Recruit: unknown token script " + humanSoldierTokenKey})
		return
	}
	h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: ctrl, Text: humanSoldierTokenKey})
}

// recruitPicks appends the answered discard picks still in ctrl's hand: the
// one home of Recruit's "discard" answer.
func recruitPicks(g *state.Game, ctrl state.PlayerID, answered, picks []state.ObjID) []state.ObjID {
	for _, id := range answered {
		if o := g.Obj(id); o != nil && o.Zone == state.ZHand && o.Owner == ctrl {
			picks = append(picks, id)
		}
	}
	return picks
}
