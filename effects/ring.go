package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effRingTemptsYou performs one "the Ring tempts you" action (CR 701.54a):
// the tempted player's count rises by one and a creature they control
// becomes (or stays) their Ring-bearer. The designation and the count fold
// through events.Apply's RingTemptsYou case, so a replay derives both from
// the log alone.
//
// CR 701.54a makes the creature a real choice ("choose a creature you
// control"), so when the player controls TWO OR MORE eligible creatures the
// choice is a real KChoose — the strict-supersets rule every asking primitive
// here shares (effBlight, effSacrifice's player branch): with exactly one
// creature the deterministic answer IS the only legal answer, so it is
// designated silently; with none there is nothing to choose and the event
// still counts (CR 701.54d — "even if some [of the actions] were
// impossible").
//
// The existing Ring-bearer, when still controlled and on the battlefield,
// is the deterministic default AND one of the offered options: CR 701.54a
// keeps the designation "until another creature becomes your Ring-bearer",
// so a player may keep it, and the no-host R-9 fallback does exactly that
// (falling back to the first eligible creature in zone order when the
// existing bearer is stale). The choice is answered in place (ResumeKind
// "ring_bearer"), and every path emits exactly one RingTemptsYou event.
func effRingTemptsYou(h Host, c *Ctx, sa *cards.SA) {
	// Measured corpus: none of the 49 raw RingTemptsYou SA lines carries
	// Defined$/ValidTgts$, so the tempted player is always the resolving
	// controller. A Defined$-driven path would be untested dead code whose
	// PlayerOf behaviour on a non-player object reference is undefined for
	// this shape -- it stays out deliberately.
	p := c.Controller
	g := h.Game()

	emit := func(bearer state.ObjID) {
		h.Emit(events.Event{
			Kind:   events.RingTemptsYou,
			Player: p,
			Obj:    bearer,
			Amount: g.Players[p].RingTempted + 1,
		})
	}

	// The eligible creatures, in battlefield zone order (the ordered list,
	// never a map range). pending is the deterministic default the no-host
	// fallback and the single/none cases designate.
	eligible := make([]state.ObjID, 0, 4)
	for _, id := range g.Zone(state.ZBattlefield, p) {
		if h.IsCreature(id) {
			eligible = append(eligible, id)
		}
	}
	defaultBearer := ringDefaultBearer(h, p, g)
	switch len(eligible) {
	case 0:
		// CR 701.54d: no creature controlled -- the temptation still counts
		// and the trigger still fires; the event carries Obj 0.
		emit(0)
		return
	case 1:
		// Strict-supersets: one eligible creature is no choice. Designate it
		// silently -- no decision, no stand-in Note.
		emit(eligible[0])
		return
	}

	d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "ring_bearer", ResumeSA: sa,
		Prompt: "Choose your Ring-bearer"}
	// The deterministic default (the existing Ring-bearer when still
	// controlled, else first-in-zone-order) is offered FIRST when it is one
	// of the eligible creatures. CR 701.54a's choice is over every
	// controlled creature, so the ordering is free; but this makes the
	// shared KChoose first-option policy (botpolicy, and every R-9 clamp
	// fallback) answer "keep the existing Ring-bearer" exactly as the
	// no-host stand-in and the pre-choice engine did. Ordering the offered
	// list any other way would silently switch a bot's bearer off the
	// deterministic default -- a regression the brief forbids.
	ordered := make([]state.ObjID, 0, len(eligible))
	defaultEligible := false
	for _, id := range eligible {
		if id == defaultBearer {
			defaultEligible = true
			break
		}
	}
	if defaultEligible {
		ordered = append(ordered, defaultBearer)
	}
	for _, id := range eligible {
		if id != defaultBearer {
			ordered = append(ordered, id)
		}
	}
	for _, id := range ordered {
		name := "a creature"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "ring_bearer", Label: name, Obj: id, Player: p})
	}
	if ans, ok := AskTape(h, d); ok {
		// The answered pick. A stale or stray answer (the object left the
		// battlefield, changed controller, or was never the player's) falls
		// back to the deterministic default (ringAnsweredBearer).
		pick := state.ObjID(0)
		if len(ans) > 0 {
			pick = ans[0].Obj
		}
		emit(ringAnsweredBearer(h, g, p, pick))
		return
	}

	// R-9 no-host stand-in: the existing Ring-bearer when still controlled,
	// otherwise the first eligible creature in zone order -- the same pick
	// botpolicy's clamp fallback answers, so a bot-answered ask emits the
	// same event this silent path does.
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
		Text: "designates the default Ring-bearer (no engine host to ask)", Secret: true})
	emit(defaultBearer)
}

// ringAnsweredBearer is the bearer an answered Ring-bearer choice
// designates: the pick when it is still a creature p controls on the
// battlefield, else (a stale or stray answer) the deterministic default.
func ringAnsweredBearer(h Host, g *state.Game, p state.PlayerID, pick state.ObjID) state.ObjID {
	if o := g.Obj(pick); o != nil && o.Zone == state.ZBattlefield && o.Controller == p && h.IsCreature(pick) {
		return pick
	}
	return ringDefaultBearer(h, p, g)
}

// ringDefaultBearer is the deterministic bearer choice: the existing
// Ring-bearer when the player still controls it on the battlefield (CR
// 701.54a keeps the designation), otherwise the first eligible creature in
// battlefield zone order (0 when none). The existing-bearer read requires
// only zone and control, NOT current creature-ness: CR 701.54a's designation
// persists until another creature becomes the bearer or control changes, so
// a bearer that has stopped being a creature (a Crewed Vehicle at end of
// turn, an animation that ended) is still the player's Ring-bearer.
func ringDefaultBearer(h Host, p state.PlayerID, g *state.Game) state.ObjID {
	if cur := g.Players[p].RingBearer; cur != 0 {
		if o := g.Obj(cur); o != nil && o.Zone == state.ZBattlefield && o.Controller == p {
			return cur
		}
	}
	for _, id := range g.Zone(state.ZBattlefield, p) {
		if h.IsCreature(id) {
			return id
		}
	}
	return 0
}
