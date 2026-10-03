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
//     resume SA, so a Dredge replacement over the draw is a real choice that
//     resumes THIS recruit (rules' "dredge" arm sets ctx.DrawDone = 1) rather
//     than orphaning it. A re-entry with DrawDone set skips the draw.
//   - The discard follows: one card from the controller's hand. A hand with
//     EXACTLY one card discards it with no ask (the strict-supersets rule --
//     nobody could answer differently); a hand with more poses a real
//     KChoose (Min = Max = 1, options in hand order). The answer travels
//     through the shared "discard" resume arm and Ctx.Discard, so no new
//     resume kind or Ctx field is introduced. A host that cannot ask takes
//     the front-of-hand card, byte-identical to botpolicy's KChoose clamp.
//   - The token comes last: one events.TokenCreate for
//     humanSoldierTokenKey, owned by the controller, but ONLY when at least
//     one NONLAND card was actually discarded.
//
// The mint is the resolution's final action, so a CreateToken
// replacement-order park needs only a re-entry marker: the parked mint
// suspends a bare TokenRest and the answer re-enters through resumingMint,
// which returns immediately without replaying the draw or the discard.
func effRecruit(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	// A parked token mint's answer re-entered here: the mint already landed
	// (nothing follows it in this action), so stop rather than replaying the
	// draw and the discard.
	if resumingMint(c, sa) != nil {
		return
	}
	ctrl := c.Controller
	// The answered discard (re-entered through rules' generic "discard"
	// resume arm with ResumeKind "discard"): captured and cleared before any
	// further ask this walk poses (fx42 scoping).
	answered := c.Discard
	c.Discard = nil
	c.DiscardTarget = 0
	c.DiscardVote = ""
	// The draw. A non-zero DrawDone is a Dredge resume: the draw already
	// completed and was replaced (CR 701.9 orders draw before discard).
	drawDone := c.DrawDone
	c.DrawDone = 0
	if answered == nil && drawDone == 0 {
		drawFor(h, ctrl, 0, sa, drawUptoRider{})
		if h.Suspended() {
			return
		}
	}
	// The discard: exactly one card from the controller's hand.
	hand := zoneOf(g, state.ZHand, ctrl)
	picks := make([]state.ObjID, 0, 1)
	switch {
	case answered != nil:
		// The answered pick, filtered to cards still in this hand.
		picks = recruitPicks(g, ctrl, answered, picks)
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
			// The resolution kernel's answer in hand: the same pick the
			// "discard" re-entry filters above.
			picks = recruitPicks(g, ctrl, answerObjs(ans), picks)
			break
		}
		if Ask(h, d) == AskAsked {
			return
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
	wasSuspended := h.Suspended()
	h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: ctrl, Text: humanSoldierTokenKey})
	if !wasSuspended && h.Suspended() {
		// The mint parked the resolution behind a CR 616.1 replacement-order
		// ask. Record a continuation so the answer's re-entry does not replay
		// the draw and discard; nothing follows the mint, so the restart is
		// the whole of what remains.
		suspendMint(h, c, TokenRest{SA: sa})
	}
}

// recruitPicks appends the answered discard picks still in ctrl's hand: the
// one home of Recruit's "discard" answer, shared by the re-entry and the
// resolution kernel's tape answer.
func recruitPicks(g *state.Game, ctrl state.PlayerID, answered, picks []state.ObjID) []state.ObjID {
	for _, id := range answered {
		if o := g.Obj(id); o != nil && o.Zone == state.ZHand && o.Owner == ctrl {
			picks = append(picks, id)
		}
	}
	return picks
}
