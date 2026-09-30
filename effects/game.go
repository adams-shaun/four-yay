package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func effBecomeMonarch(h Host, c *Ctx, sa *cards.SA) {
	targets := Defined(h, c, sa)
	if len(targets) == 0 {
		return
	}
	players := definedPlayers(h, c, sa)
	if len(players) == 0 {
		return
	}
	p := players[0]
	if g := h.Game(); g != nil && g.IsMonarch(p) {
		return
	}
	h.Emit(events.Event{Kind: events.MonarchChange, Player: p})
}

// effRestartGame ends the game as a draw. Actually restarting (leaving
// exiled permanents in play under the restarting player's control, per the
// real card text) is out of M1's scope; ending the match honestly rather
// than hanging or silently no-op-ing is the closest correct degradation.
//
// Ruling T22-k (fix round 2): Amount: 1 is required, not cosmetic --
// rules/sba.go's checkGameOver is not the only GameOver emitter in this
// tree, and Amount is the shape discriminator events.Apply's GameOver case
// reads (0 = win, 1 = draw; Task 22 fix round 1). Left at its zero value,
// this event's own Amount reads as "Amount 0", a win -- and Player is also
// left at its zero value, which validates as seat 0 -- so despite this
// function's name, its own comment and its own Text all saying "draw", the
// event it actually emitted a win for seat 0. Every other RestartGame-style
// primitive in this file already carries no Player of its own, so seat 0
// winning was never a deliberate choice anywhere in this file; it was
// simply the one call site nobody had reason to re-examine once Amount
// became meaningful.
func effRestartGame(h Host, c *Ctx, sa *cards.SA) {
	// RestrictFromZone$/RestrictFromValid$ (Karn Liberated's [-14]): the
	// objects the restart would KEEP. Forge's RestartGame discards everything
	// in RestrictFromZone that matches RestrictFromValid and carries the rest
	// into the restarted game — Karn keeps exactly the non-Aura permanents
	// exiled with him (his ReturnFromExile sub-ability acts on that keep-set).
	// A full restart needs game-loop machinery this engine does not have (see
	// the draw degradation below), but the keep-set is real game state this
	// build can name, so the log records it instead of leaving both keys
	// silently inert.
	if zonesRaw := strings.TrimSpace(sa.Params["RestrictFromZone"]); zonesRaw != "" {
		if spec := strings.TrimSpace(sa.Params["RestrictFromValid"]); spec != "" {
			zones, all, valid := ParseZones(zonesRaw)
			g := h.Game()
			if !valid {
				all = true
			}
			if all {
				zones = []state.Zone{state.ZLibrary, state.ZHand, state.ZBattlefield,
					state.ZGraveyard, state.ZExile, state.ZStack, state.ZCommand}
			}
			var kept []string
			for _, z := range zones {
				// The shared stack is named once, under the first alive seat
				// (state/game.go Zone), so a stack card cannot appear in the
				// keep-set note multiple times on an N-seat table.
				for si, p := range g.AliveFrom(0) {
					if z == state.ZStack && si > 0 {
						continue
					}
					for _, id := range append([]state.ObjID(nil), g.Zone(z, p)...) {
						// RestrictFromValid$ names what the restart DISCARDS; the
						// complement inside the named zone is what it keeps.
						if !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
							if o := g.Obj(id); o != nil && o.Face() != nil {
								kept = append(kept, o.Face().Name)
							}
						}
					}
				}
			}
			if len(kept) > 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "restart would keep in " + zonesRaw + ": " + strings.Join(kept, ", ")})
			}
		}
	}
	h.Emit(events.Event{Kind: events.GameOver, Amount: 1, Text: "game restarted: ended as a draw"})
}

// effMana implements "AB$ Mana": add Amount mana of Produced's colour(s) to
// the pool of each ManaRecipients player (the activating player unless
// Defined$ names another). Absorbed from Task 14's stopgap: the
// negative-Amount clamp is Ruling T14-f, kept verbatim for the same reason as
// DealDamage's -- events.Apply's ManaAdd case is a plain "+=", so an
// unclamped negative would drop the pool below zero instead of doing
// nothing.
//
// A resolution-time host now gets the same CR 106.6 colour choice as the
// activation path. A host without a decision channel keeps the R-9 fallback:
// Any becomes colourless and an unasked Combo list retains its old full-listed
// output. A dual-producing ability such as "Add {R}{R}" is walked one symbol
// at a time rather than split on whitespace, since Produced$ carries no spaces
// of its own.
//
// The one thing this primitive must NEVER do is walk a value it does not
// understand. A "Combo R G" reaches effMana from a path with no colour
// chooser (the activation path substitutes the chosen colour in first), and
// walking it one rune at a time turned "Combo R G" into five stray
// colourless plus a red and a green -- o, m, b are not mana symbols. An
// unrecognised Produced$ value therefore emits nothing and records a Note
// naming it, following this repo's fail-closed convention (an unknown token
// never invents a value).
// effReplaceMana rewrites one in-flight ManaAdd event for a ProduceMana
// replacement. The surrounding rules code supplies the amount and colour in
// Ctx, then logs the rewritten ManaAdd; this effect itself has no game-state
// mutation to emit. ReplaceAmount multiplies the whole production.
// ReplaceType/ReplaceColor preserve its amount and replace only its colour;
// ReplaceMana is Forge's "one mana instead of any other type and amount"
// form (Damping Sphere, Contamination), so it sets the amount to exactly one
// as well as replacing the colour. For a choice-valued replacement
// (Any/Chosen), rules parks the ManaAdd and supplies the player's W/U/B/R/G
// answer in Ctx.ManaChoice; without a valid answer this pure effect fails
// closed rather than inventing colourless mana.
