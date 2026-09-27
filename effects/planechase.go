package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file holds the three planechase effect verbs Forge's card corpus uses
// on plane cards and the "Will of the Planeswalkers" cycle (Path of the
// Pyromancer/Animist/Schemer/Enigma/Ghosthunter). The planar deck and zone
// now exist (CR 901; task agent-20260924T183758Z-1ddc7625), so a seat with a
// planar deck planeswalks for real (the events.PlanarWalk fold rotates the
// deck and reveals the next plane) and DB$ ChaosEnsues erupts the current
// plane for real (the events.ChaosEnsues marker makes the plane's own
// Mode$ ChaosEnsues ability trigger). A seat with NO planar deck keeps the
// recorded no-op Note the verbs have always had, so a non-Planechase game
// that resolves one of these SVars is still visible and harmless. Each is a
// genuine registration rather than a RegisterNonAPI marker: they are DB$
// APIs the resolver dispatches, not keywords/triggers/statics.

func init() {
	Register("BlankLine", effBlankLine)
	Register("Planeswalk", effPlaneswalk)
	Register("ChaosEnsues", effChaosEnsues)
}

// effBlankLine is "DB$ BlankLine": a display-only spacer in Forge's card
// scripts. It resolves to nothing and has no rules meaning. It exists as a
// registered primitive only so the resolver does not emit its generic
// "unimplemented API BlankLine" Note while walking a chain that passes
// through it — the Path cycle's DBSpace SVar is a BlankLine whose
// SpellDescription$ is the literal artifact ",,,,,,". Deliberately SILENT:
// emitting a Note for a rules-free spacer would be noise in every log that
// carries one, and the point of the primitive is to disappear.
func effBlankLine(_ Host, _ *Ctx, _ *cards.SA) {}

// effPlaneswalk is CR 901.8's planeswalk action: move to the next plane of
// the planar deck and trigger "when you planeswalk away". A game with no
// planar deck has no next plane and no planeswalk-away trigger, so the
// correct degrade is a recorded no-op — the Note says the action resolved
// and why nothing moved. It keeps a defined$ planeswalk from wedging a
// resolution, and it is loud enough that a future planar-deck tier can find
// every call site by Text.
func effPlaneswalk(h Host, c *Ctx, sa *cards.SA) {
	// Optional$ True is the "you may planeswalk" election.  There is no
	// planar deck in this build, but the election still matters: it must be
	// visible to a host and a decline must still let Resolve walk the chained
	// SubAbility.  The answer is scoped to this SA and consumed on re-entry.
	if strings.EqualFold(strings.TrimSpace(sa.Params["Optional"]), "True") {
		answer := c.PlaneswalkOpt
		c.PlaneswalkOpt = ""
		if answer == "" {
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
				Source: c.Source, ResumeKind: "planeswalk_optional", ResumeSA: sa,
				Prompt: "Planeswalk?", Options: []decision.Option{
					{Index: 0, Kind: "yes", Label: "Yes — planeswalk", Player: c.Controller},
					{Index: 1, Kind: "no", Label: "No", Player: c.Controller},
				}}
			if Ask(h, d) == AskAsked {
				return
			}
			// R-9: a host without a decision channel deterministically declines.
			answer = "no"
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "planeswalk election: " + answer})
		if answer != "yes" {
			return
		}
	}
	if !hasPlanarDeck(h, c) {
		notePlanechaseNoDeck(h, c, "planeswalk")
		return
	}
	// CR 901.8: move to the next plane. The rotation itself is the
	// events.PlanarWalk fold (events/apply.go), the ONE state-mutating path,
	// so the effect only proposes the action -- the same shape
	// effRollPlanarDice uses for events.PlanarRoll.
	//
	// Defined$ names the destination plane(s) instead of the deck's next
	// plane: Norn's Seedcore's "Planeswalk to it" and Spatial Merging's
	// "simultaneously planeswalk to both of them" both ride
	// Defined$ Remembered. The corpus spelling is exactly "Remembered", so
	// any other Defined$ value is left to rules' ordinary destination fade
	// (this verb has no source-object default destination) and the walk
	// rotates as if no destination were given -- the loudest honest degrade
	// for a shape no carrier uses.
	if dests := planeswalkDestinations(h, c, sa); len(dests) > 0 {
		dont := strings.EqualFold(strings.TrimSpace(sa.Params["DontPlaneswalkAway"]), "True")
		h.Emit(events.Event{Kind: events.PlanarWalk, Player: c.Controller,
			Obj: departingPlane(h, c.Controller), IDs: dests,
			Amount: planeswalkAwayFlag(dont)})
		return
	}
	h.Emit(events.Event{Kind: events.PlanarWalk, Player: c.Controller,
		Obj: departingPlane(h, c.Controller)})
}

// effChaosEnsues is CR 901.9's chaos action: when the planar die rolls the
// chaos symbol, the current plane's chaos ability triggers. With no planar
// deck there is no current plane and no chaos ability to trigger, so this
// records the same recorded no-op as Planeswalk.
//
// Defined$ names the plane(s) chaos ensues on instead of the current plane
// (The Fertile Lands of Saulvinia's "Chaos ensues on that plane", riding
// Defined$ Remembered). The corpus spelling is exactly "Remembered"; any
// other value falls through to the controller's current plane.
func effChaosEnsues(h Host, c *Ctx, sa *cards.SA) {
	if !hasPlanarDeck(h, c) {
		notePlanechaseNoDeck(h, c, "chaos ensues")
		return
	}
	// CR 901.9: the current plane's chaos ability triggers. Propose the
	// events.ChaosEnsues marker naming the plane; rules' synthetic plane
	// scan (checkChaosEnsuesTriggers) queues that plane's Mode$ ChaosEnsues
	// ability. With a deck but no revealed plane (all planes walked and the
	// zone empty) there is nothing to erupt: fall through to the no-deck
	// Note so the action stays visible.
	planes := chaosDestinations(h, c, sa)
	if len(planes) == 0 {
		if plane := currentPlaneOf(h, c.Controller); plane != 0 {
			planes = []state.ObjID{plane}
		}
	}
	if len(planes) == 0 {
		notePlanechaseNoDeck(h, c, "chaos ensues")
		return
	}
	for _, plane := range planes {
		h.Emit(events.Event{Kind: events.ChaosEnsues, Player: c.Controller, Obj: plane})
	}
}

// planeswalkDestinations resolves a Planeswalk SA's Defined$ Remembered into
// the planar-deck object ids the walk should land on (CR 901.8). It returns
// nil for an absent or non-Remembered Defined$ (the ordinary rotation) and
// for a remembered set with no object still in the controller's planar deck.
// Remembered order is preserved, which is what Spatial Merging's
// "simultaneously planeswalk to both of them" needs.
func planeswalkDestinations(h Host, c *Ctx, sa *cards.SA) []state.ObjID {
	if !strings.EqualFold(strings.TrimSpace(sa.Params["Defined"]), "Remembered") {
		return nil
	}
	return planarRememberedIDs(h, c)
}

// chaosDestinations is planeswalkDestinations for DB$ ChaosEnsues' own
// Defined$ Remembered rider (The Fertile Lands of Saulvinia): the remembered
// planes chaos ensues on.
func chaosDestinations(h Host, c *Ctx, sa *cards.SA) []state.ObjID {
	if !strings.EqualFold(strings.TrimSpace(sa.Params["Defined"]), "Remembered") {
		return nil
	}
	return planarRememberedIDs(h, c)
}

// planarRememberedIDs returns the resolving controller's remembered objects
// that are still face-up-eligible cards of their planar deck, in Remembered
// order. A remembered card that has left the planar deck (or belongs to
// another seat) is dropped: the walk can only land on a plane of this seat's
// own planar deck.
func planarRememberedIDs(h Host, c *Ctx) []state.ObjID {
	g := h.Game()
	var out []state.ObjID
	seen := map[state.ObjID]bool{}
	for _, t := range resolvedRemembered(h, c) {
		if t.IsPlayer || t.Obj == 0 || seen[t.Obj] {
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil || o.Zone != state.ZPlanarDeck || o.Controller != c.Controller {
			continue
		}
		seen[t.Obj] = true
		out = append(out, t.Obj)
	}
	return out
}

// departingPlane is the object id of the seat's current plane (the plane the
// walk leaves), or 0 when there is none. It is the source of the walk's
// PlaneswalkedFrom ability.
func departingPlane(h Host, p state.PlayerID) state.ObjID {
	return currentPlaneOf(h, p)
}

// planeswalkAwayFlag maps DontPlaneswalkAway$ True to the PlanarWalk event's
// Amount flag (events.PlanarWalkDontPlaneswalkAway).
func planeswalkAwayFlag(dont bool) int32 {
	if dont {
		return events.PlanarWalkDontPlaneswalkAway
	}
	return 0
}

// hasPlanarDeck reports whether the resolving controller has a planar deck
// (CR 901). A nil Game or an empty ZPlanarDeck zone means the seat is not
// playing Planechase, so the verbs keep their recorded no-op degrade.
func hasPlanarDeck(h Host, c *Ctx) bool {
	if c == nil {
		return false
	}
	return len(h.Game().Zone(state.ZPlanarDeck, c.Controller)) > 0
}

// currentPlaneOf returns the controller's face-up current plane (the top of
// their ZPlanarDeck zone), or 0 when the seat has no planar deck or the
// plane is still face down. It is the effects-side read of the same fact
// rules/planar.go's currentPlane resolves for the engine.
func currentPlaneOf(h Host, p state.PlayerID) state.ObjID {
	ids := h.Game().Zone(state.ZPlanarDeck, p)
	if len(ids) == 0 {
		return 0
	}
	o := h.Game().Obj(ids[0])
	if o == nil || o.FaceDown || o.Zone != state.ZPlanarDeck {
		return 0
	}
	return o.ID
}

// notePlanechaseNoDeck records the shared "resolved, but there is no planar
// deck" Note. Optional$ elections are handled by effPlaneswalk before this
// helper; non-optional callers reach it directly.
func notePlanechaseNoDeck(h Host, c *Ctx, action string) {
	text := action + " (no planar deck)"
	if c != nil && c.Source != 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: text})
		return
	}
	h.Emit(events.Event{Kind: events.Note, Text: text})
}
