package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effManifest implements Forge's Manifest primitive (Reality Shift's
// "its controller manifests the top card of their library", Whisperwood
// Elemental's bare `DB$ Manifest` trigger body): move the top card of each
// named player's library onto the battlefield FACE DOWN (CR 708.5). The move
// is the REAL card object -- never a token mint: the manifested 2/2 keeps
// the object's identity, so if it dies it reaches the graveyard as itself
// (CR 708.9's reveal is the FaceDown clear on leaving the battlefield, and
// the view's FaceDown redaction hides the face from non-controllers while it
// stays in play). Each move is one Secret MoveZone with Player set to the
// manifesting player: Secret is what keeps the event's Obj out of every
// other seat's projection (redaction rule 1) -- a library-to-battlefield
// move would otherwise stay public under rule 2 and leak the face through
// the transcript.
//
// Scope, measured over the corpus's 33 plain-Manifest lines: the default
// top-card shape (the 9 bare `DB$ Manifest` trigger bodies), a
// `DefinedPlayer$` selector through searchPlayers's grammar (Reality
// Shift's `TargetedController`) and a literal/SVar `Amount$` (default 1;
// a value resolving to <= 0 manifests nothing, no event) are implemented.
// Every other shape -- `Defined$` object manifests, the `Choices$`
// chooser forms, `RememberManifested$ True`, an unresolvable `Amount$`
// body (Y, or X outside a cast's own X-value) -- emits the SAME loud
// "unimplemented API Manifest" note the unimplemented-API fallback emits
// and moves nothing: fail loud, never silently move the wrong card.
// Turning a face-down permanent face up (CR 708.6) is not implemented
// anywhere (AGENTS.md's manifest row).
func effManifest(h Host, c *Ctx, sa *cards.SA) {
	if strings.TrimSpace(sa.ParamStr(cards.PKDefined)) != "" ||
		strings.TrimSpace(sa.ParamStr(cards.PKChoices)) != "" ||
		strings.EqualFold(strings.TrimSpace(sa.Params["RememberManifested"]), "True") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented API Manifest"})
		return
	}
	amount := int32(1)
	if raw, present := sa.Param(cards.PKAmount); present {
		// X/Y (and any body Num's grammar cannot resolve) are out of scope:
		// loud, never a degraded count silently moving a wrong number of
		// cards.
		if raw == "X" || raw == "Y" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented API Manifest"})
			return
		}
		n, ok := NumResolved(h, c, sa, "Amount", 1)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented API Manifest"})
			return
		}
		amount = n
	}
	if amount <= 0 {
		return
	}
	g := h.Game()
	for _, p := range searchPlayers(h, c, sa) {
		if int(p) >= len(g.Players) {
			continue
		}
		n := amount
		if l := int32(len(g.Zone(state.ZLibrary, p))); l < n {
			n = l
		}
		for i := int32(0); i < n; i++ {
			// Index 0 is the TOP of the library (the end a Draw takes). The
			// MoveZone fold removes the object as it lands, so the zone is
			// re-read each iteration.
			top := g.Zone(state.ZLibrary, p)[0]
			h.Emit(events.Event{Kind: events.MoveZone, Obj: top, Player: p,
				From: state.ZLibrary, To: state.ZBattlefield,
				Counter: "entered_face_down", Secret: true})
		}
	}
}

// effManifestDread implements CR 701.61's two-card library operation. The
// private look is recorded before the choice; the offered identities are
// visible only to the library's player. If the host cannot ask, choose the
// top card deterministically, matching the engine's R-9 fallback contract.
//
// Scope, measured over the corpus's 37 ManifestDread lines: the plain top-two
// body (Zimone, Mystery Unraveler and 26 others) and `Amount$ 2` (identical
// to the default). Every other parameter family -- `Amount$ 1` (a count this
// build does not implement), `DefinedPlayer$` (only the resolving
// controller's library is supported) and `RememberManifested$ True` (the
// DBAttach/DBPutCounter rider family needs the manifested object remembered)
// -- emits the SAME loud "unimplemented API ManifestDread" note the
// unimplemented-API fallback emits and moves nothing: fail loud, never
// silently look at the wrong count, the wrong player's library, or lose the
// remembered card a rider needs.
func effManifestDread(h Host, c *Ctx, sa *cards.SA) {
	if strings.TrimSpace(sa.ParamStr(cards.PKDefinedPlayer)) != "" ||
		strings.EqualFold(strings.TrimSpace(sa.Params["RememberManifested"]), "True") ||
		sa.ParamStr(cards.PKChoices) != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented API ManifestDread"})
		return
	}
	if raw, present := sa.Param(cards.PKAmount); present && strings.TrimSpace(raw) != "2" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented API ManifestDread"})
		return
	}
	g := h.Game()
	p := c.ManifestDreadPlayer
	picked := c.ManifestDreadPick
	done := c.ManifestDreadDone
	c.ManifestDreadPick, c.ManifestDreadDone = 0, false
	if int(p) >= len(g.Players) {
		p = c.Controller
	}
	if done {
		manifestDreadAnswered(h, c, p, picked)
		return
	}
	p = c.Controller
	lib := g.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		return
	}
	window := append([]state.ObjID(nil), lib[:min(2, len(lib))]...)
	emitLook(h, []state.PlayerID{p}, state.ZLibrary, window, "looks at the top two cards of the library")
	if len(window) == 1 {
		manifestDreadMove(h, c, p, window, window[0])
		return
	}
	d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "manifest_dread", ResumeSA: sa,
		Prompt: "Choose a card to manifest dread"}
	for _, id := range window {
		label := "a card"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			label = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "manifest_dread", Label: label, Obj: id, Player: p})
	}
	if ans, ok := AskTape(h, d); ok {
		// The resolution kernel's answer in hand: the "manifest_dread"
		// re-entry's answered move.
		var picked state.ObjID
		if len(ans) > 0 {
			picked = ans[0].Obj
		}
		manifestDreadAnswered(h, c, p, picked)
		return
	}
	c.ManifestDreadPlayer = p
	if Ask(h, d) == AskAsked {
		return
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
		Text: "manifests the top card (no engine host to ask)", Secret: true})
	manifestDreadMove(h, c, p, window, window[0])
}

// manifestDreadAnswered applies player p's answered pick: the picked card
// (still on top of p's library) and the next card form the window it
// splits. The answer re-entry's branch, and the resolution kernel's served
// answer alike.
func manifestDreadAnswered(h Host, c *Ctx, p state.PlayerID, picked state.ObjID) {
	g := h.Game()
	if o := g.Obj(picked); o != nil && o.Zone == state.ZLibrary && o.Owner == p {
		window := []state.ObjID{picked}
		for _, id := range g.Zone(state.ZLibrary, p) {
			if id != picked && len(window) < 2 {
				window = append(window, id)
			}
		}
		manifestDreadMove(h, c, p, window, picked)
	}
}

// manifestDreadMove applies CR 701.61's two destinations. The chosen card's
// move onto the battlefield face down is Secret (the private look must not
// leak which card was manifested); the unchosen card's move to the graveyard
// is PUBLIC -- a graveyard is a public zone, so every seat and spectator
// learns which card went there, exactly as applyNonlandExplore's
// library-to-graveyard move does. Marking it Secret would strip Obj from
// every non-owner projection, leaving the transcript a nameless move even
// though the card's identity is public the moment it lands.
func manifestDreadMove(h Host, c *Ctx, p state.PlayerID, window []state.ObjID, chosen state.ObjID) {
	for _, id := range window {
		if id == chosen {
			h.Emit(events.Event{Kind: events.MoveZone, Obj: id, Player: p, From: state.ZLibrary,
				To: state.ZBattlefield, Counter: "entered_face_down", Secret: true})
		} else {
			o := h.Game().Obj(id)
			if o != nil {
				h.Emit(events.Event{Kind: events.MoveZone, Obj: id, Player: o.Owner, From: state.ZLibrary, To: state.ZGraveyard})
			}
		}
	}
}

// effCloak implements Forge's Cloak primitive (veiled_ascension's upkeep
// trigger, unexplained_absence's per-player cloak, cryptic_coat's ETB cloak):
// CR 708.5's cloak variant -- the named card objects move onto the
// battlefield FACE DOWN as 2/2 creatures with ward {2}. The move is the REAL
// card object, the effManifest shape with a different Counter value (one
// Secret MoveZone per card, "entered_cloaked" instead of
// "entered_face_down" -- the marker events/apply.go folds into
// state.Object.Cloaked, which rules/layers.go and rules/trigger_match.go
// read for the ward {2}; the view's FaceDown redaction covers both
// variants). A cloak's Defined$ names card objects that may sit in the
// library, exile or hand (the Remembered carriers move cards that just
// arrived there), so unlike effManifest the object shapes resolve directly.
//
// Scope, measured over the corpus's 11 Cloak lines: the per-player top-card
// shapes (DefinedPlayer$, or Defined$ TopOfLibrary whose listed player's OWN
// library is taken -- never the resolver's), the ctx-Remembered object
// shapes (become_anonymous, hide_in_plain_sight, expose_the_culprit), a
// literal/SVar Amount$ (default 1; a value resolving to <= 0 cloaks
// nothing, no event) and the riders Tapped$ (enter tapped, the
// MoveZone-then-Tap pair), Shuffle$ (the standard Secret shuffle of each
// affected player's library afterwards) and RememberCloaked$ (each cloaked
// object joins the resolution's Remembered -- cryptic_coat's chained attach)
// are implemented. SubAbility$ is free (the ordinary chain). Every other
// shape -- the Choices$ cloak-from-hand chooser (vannifar), Defined$
// ValidLibrary (etrata), an unresolvable Amount$ body -- emits the SAME loud
// "unimplemented API Cloak" note the unimplemented fallback emits and moves
// nothing: fail loud, never silently move the wrong card. Turning a cloaked
// card face up (CR 708.6) is not implemented anywhere (the
// Morph/Megamorph/Disguise ticket owns the shared turn-face-up path).
func effCloak(h Host, c *Ctx, sa *cards.SA) {
	loud := func() {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented API Cloak"})
	}
	defined := strings.TrimSpace(sa.ParamStr(cards.PKDefined))
	if strings.TrimSpace(sa.ParamStr(cards.PKChoices)) != "" ||
		strings.Contains(defined, "ValidLibrary") {
		loud()
		return
	}
	amount := int32(1)
	if raw, present := sa.Param(cards.PKAmount); present {
		// X/Y (and any body Num's grammar cannot resolve) are out of scope:
		// loud, never a degraded count silently moving a wrong number of
		// cards.
		if raw == "X" || raw == "Y" {
			loud()
			return
		}
		n, ok := NumResolved(h, c, sa, "Amount", 1)
		if !ok {
			loud()
			return
		}
		amount = n
	}
	if amount <= 0 {
		return
	}
	tapped := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKTapped)), "True")
	shuffle := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKShuffle)), "True")
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberCloaked"]), "True")
	g := h.Game()
	shuffled := make(map[state.PlayerID]bool)
	cloak := func(id state.ObjID) {
		o := g.Obj(id)
		if o == nil || o.Zone == state.ZBattlefield {
			// A missing object moves nothing; a card already on the
			// battlefield is not a cloak candidate (every corpus shape sources
			// from library/exile/hand).
			return
		}
		from := o.Zone
		// Player rides the CLOAKED card's controller: Secret is what keeps
		// the event's Obj out of every other seat's projection (redaction
		// rule 1), and the seat that may look at a face-down card is its
		// controller (CR 708.5).
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, Player: o.Controller,
			From: from, To: state.ZBattlefield,
			Counter: "entered_cloaked", Secret: true})
		if remember {
			// RememberCloaked$ is ctx level (the cryptic_coat attach chain
			// reads it within the same resolution); the persistent list is
			// left alone, the RememberChanged$ convention.
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
		}
		if tapped {
			h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: o.Controller,
				Text: "entered tapped"})
		}
		shuffled[o.Owner] = true
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKDefinedPlayer)) != "" {
		// The per-player top-card shape (unexplained_absence's
		// "Defined$ TopOfLibrary | DefinedPlayer$ RememberedController"):
		// each listed player's OWN top Amount$ cards -- searchPlayers's
		// DefinedPlayer$ precedence, the effManifest loop's move shape.
		for _, p := range searchPlayers(h, c, sa) {
			if int(p) >= len(g.Players) {
				continue
			}
			n := amount
			if l := int32(len(g.Zone(state.ZLibrary, p))); l < n {
				n = l
			}
			for i := int32(0); i < n; i++ {
				// Index 0 is the TOP of the library (the end a Draw takes).
				// The MoveZone fold removes the object as it lands, so the
				// zone is re-read each iteration.
				top := g.Zone(state.ZLibrary, p)[0]
				cloak(top)
			}
		}
	} else {
		switch defined {
		case "", "TopOfLibrary":
			// The bare top-card shape (veiled_ascension, ransom_note,
			// cryptic_coat): the resolving controller's top card, the
			// TopOfLibrary selector's own anchor (effects/context.go).
			targets, ok := definedSpec(h, c, "TopOfLibrary")
			if !ok {
				loud()
				return
			}
			for _, t := range targets {
				if t.IsPlayer {
					continue
				}
				cloak(t.Obj)
			}
		case "Remembered":
			// The Remembered-object shape (become_anonymous,
			// hide_in_plain_sight, expose_the_culprit): each remembered card
			// object cloaks from wherever it sits now (library top, exile,
			// hand).
			for _, t := range objectsOf(copyTargets(c.Remembered)) {
				cloak(t.Obj)
			}
		default:
			loud()
			return
		}
	}
	if shuffle {
		// Shuffle$ True: the standard Secret events.Shuffle for each player
		// whose library lost a card (become_anonymous and expose_the_culprit
		// carry it; the deterministic order comes from the host's own rng
		// path every other library shuffle uses).
		for _, p := range g.AliveFrom(c.Controller) {
			if shuffled[p] {
				shuffleLibraryOrder(h, p)
			}
		}
	}
}
