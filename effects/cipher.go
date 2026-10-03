package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Cipher", effCipher) }

// effCipher implements DB$ Cipher, the body cards/kw_cipher.go mints as the
// SVar rules/stack.go splices onto the TAIL of a Cipher spell's resolution
// chain: "Then you may exile this spell card encoded on a creature you
// control." (CR 702.99a.) It is the ONE home for the encode ask and the
// association -- the combat-damage copy trigger the association grants lives
// rules-side (checkCipherTriggers), because only rules can queue a trigger.
//
// Because the encode is the resolution's last instruction, the spell card is
// still ON THE STACK here, not in the graveyard: the effect must not depend on
// the spell first settling in a resting zone. It exiles the card from
// wherever it is (the stack), which is what makes a replacement that would
// have sent the resolved spell elsewhere irrelevant to whether it may be
// encoded. A declined encode leaves the card on the stack, so the ordinary
// resolution tail (rules/resolution.go's moveResolvedOffStack) sends it to its
// resting zone as usual.
//
// The offer is a single mid-resolution KModes ask over the resolving
// controller's creatures: Min 0 (an empty answer declines -- "you may"), Max
// 1 (exactly one creature is encoded). The choice is carried across the
// suspension in Ctx.CipherDone/CipherPick (the demonstrate transport shape),
// and a host that cannot ask takes the deterministic decline -- the R-9
// no-host contract, matching every other optional keyword the engine poses.
//
// The performing half moves the card to exile and writes the association as
// an ordinary events.Imprint with the "encoded" Text discriminator onto the
// chosen creature (state.Object.EncodedCards), so replay rebuilds it and
// events.Move prunes/clears it when either side leaves its zone.
func effCipher(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	// The encoded card is the resolving spell's object (Ctx.Source is the
	// stack object for a spell resolution). An absent or departed object is a
	// no-op -- the totality stance every effect takes.
	card := c.Source
	co := g.Obj(card)
	if co == nil {
		return
	}
	controller := c.Controller
	if int(controller) < len(g.Players) && g.Players[controller].Lost {
		return
	}

	answered := c.CipherDone
	pick := c.CipherPick
	// fx42 scoping: consume and clear the answered transport before anything
	// below, so a nested Cipher never inherits this answer.
	c.CipherDone = false
	c.CipherPick = nil

	if answered {
		cipherEncode(h, c, card, controller, pick)
		return
	}

	// The encode instruction is the resolution's tail, so the spell card must
	// still be on the stack. A card that is not (a copy that already ceased to
	// exist, or one a replacement relocated) is nothing to encode.
	if co.Zone != state.ZStack {
		return
	}
	// Candidates: the resolving controller's creatures, in deterministic
	// battlefield-zone order (the zone list is insertion-ordered).
	sc := c.SpecContext(controller)
	var candidates []state.ObjID
	for _, id := range g.Zone(state.ZBattlefield, controller) {
		if MatchesSpecCtx(g, "Creature.YouCtrl", id, sc) {
			candidates = append(candidates, id)
		}
	}
	if len(candidates) == 0 {
		// No legal host: make the no-ask path replay-visible so it cannot
		// pass a negative test simply because the handler was unregistered.
		h.Emit(events.Event{Kind: events.Note, Obj: card,
			Text: "cipher encode has no creature you control"})
		return
	}

	// The optional encode: a single-pick KModes over the candidates, Min 0
	// (decline). Naming the card keeps the prompt readable.
	name := ""
	if f := co.Face(); f != nil {
		name = f.Name
	}
	d := &decision.Decision{Player: controller, Kind: decision.KModes,
		Min: 0, Max: 1, Source: card, ResumeKind: "cipher", ResumeSA: sa,
		Prompt: "Encode " + name + " on a creature you control?", ResumeRemembered: copyTargets(c.Remembered)}
	for _, id := range candidates {
		label := "Encode"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			label = "Encode on " + o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mode",
			Label: label, Obj: id, Player: controller})
	}
	if ans, ok := AskTape(h, d); ok {
		// The resolution kernel's answer in hand: the "cipher" re-entry's
		// encode (an empty answer declines).
		var picked []state.Target
		for _, o := range ans {
			if o.Obj != 0 {
				picked = append(picked, state.Target{Obj: o.Obj})
			}
		}
		cipherEncode(h, c, card, controller, picked)
		return
	}
	if h.Ask(d) {
		return // suspended; rules' "cipher" arm re-enters this walk with the answer.
	}
	// No host to ask (the R-9 fuzz/test contract): the deterministic decline.
	h.Emit(events.Event{Kind: events.Note, Obj: card,
		Text: "cipher encode resolved as the decline (no engine host to ask)"})
}

// cipherEncode applies an answered encode pick (the "cipher" answer: the
// re-entry's and the resolution kernel's one home). An empty pick is the
// decline; a pick naming an object that has since left the battlefield (or
// is not a legal creature any more) is a stale answer and encodes nothing.
func cipherEncode(h Host, c *Ctx, card state.ObjID, controller state.PlayerID, pick []state.Target) {
	g := h.Game()
	co := g.Obj(card)
	if co == nil {
		return
	}
	// The answer is settled. An empty pick is the decline; a pick naming
	// an object that has since left the battlefield (or is not a legal
	// creature any more) is a stale answer and encodes nothing.
	if len(pick) == 0 {
		return
	}
	// The spell must still be resolving (on the stack) for its encode to
	// apply; a card that already left resolved elsewhere and encodes
	// nothing.
	if co.Zone != state.ZStack {
		return
	}
	creature := pick[0].Obj
	target := g.Obj(creature)
	if target == nil || target.Zone != state.ZBattlefield ||
		!MatchesSpecCtx(g, "Creature.YouCtrl", creature, c.SpecContext(controller)) {
		h.Emit(events.Event{Kind: events.Note, Obj: card,
			Text: "cipher encode found its chosen creature no longer on the battlefield"})
		return
	}
	// "Encoded" means exiled AND associated (CR 702.99a). Move the card
	// to exile from wherever it currently sits (the stack), then write the
	// link. The Imprint event's IDs[0] is the encoded card; Apply appends
	// it to the creature's EncodedCards.
	h.Emit(events.Event{Kind: events.MoveZone, Obj: card, From: co.Zone, To: state.ZExile,
		Text: "encoded"})
	if co = g.Obj(card); co == nil || co.Zone != state.ZExile {
		return // a replacement prevented the exile; nothing was encoded.
	}
	h.Emit(events.Event{Kind: events.Imprint, Obj: creature, IDs: []state.ObjID{card},
		Text: "encoded"})
}
