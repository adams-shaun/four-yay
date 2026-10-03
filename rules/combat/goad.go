package combat

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// GoadMayAttack implements the defender half of CR 701.38b. Every goad is
// a separate requirement: a goaded creature attacks a player other than EACH
// player who goaded it if one is available. If all possible defenders are
// goaders, no declaration can satisfy every requirement, so each remains
// legal and the creature still has to attack if able.
func GoadMayAttack(b Board, id state.ObjID, defender state.PlayerID) bool {
	g := b.Game()
	o := g.Obj(id)
	if o == nil || !GoadedBy(b, o, defender) {
		return true
	}
	for _, p := range g.AliveFrom(0) {
		if p != o.Controller && !GoadedBy(b, o, p) {
			return false
		}
	}
	return true
}

// staticGoadLine is one live Goad$ True static: a printed S: line on a
// battlefield permanent (Board.Statics' walk) or a granted one — the
// DB$ Effect StaticAbilities$ registrations (Hot Pursuit's IsGoaded body,
// Immortal Obligation's Static) and the DB$ Clone AddStaticAbilities$ grant
// (Mocking Doppelganger's FamilyTease) effEffect/effClone register into the
// continuous registry. The Affected$ spec is matched against the BEARER; a
// granted line carries its Effect's Remembered set for the
// `Creature.IsRemembered` spelling.
type staticGoadLine struct {
	source     state.ObjID
	controller state.PlayerID
	spec       string
	remembered []state.ObjID
}

// staticGoadLines collects every live Goad$ True static, both delivery
// routes, in one deterministic pass: printed statics in Board.Statics' APNAP
// order, then the continuous registry's Goad restrictions in registry order.
// The goad is a requirement, not a layer effect: like every other S:
// restriction read by Board.Statics it is re-derived on demand from the
// current board (rebuilding on replay), so the goad ends when the source
// leaves the battlefield, moves to another bearer, or an "as long as" gate
// flips -- no lifetime bookkeeping. A granted static's lifetime is the
// registration's own (the registry machinery expires it), so this reader
// needs none.
func staticGoadLines(b Board) []staticGoadLine {
	var out []staticGoadLine
	for _, sv := range b.Statics("Continuous") {
		if !strings.EqualFold(strings.TrimSpace(sv.ParamStr(cards.PKGoad)), "True") {
			continue
		}
		spec := sv.Params["Affected"]
		if spec == "" {
			spec = "Card.Self"
		}
		out = append(out, staticGoadLine{source: sv.Source, controller: sv.Controller, spec: spec})
	}
	for ceI, ceL := 0, b.Active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "Goad" {
			continue
		}
		spec := strings.TrimSpace(ce.RestrictParams["Affected"])
		if spec == "" {
			spec = "Card.Self"
		}
		out = append(out, staticGoadLine{source: ce.Source, controller: ce.Controller,
			spec: spec, remembered: ce.Remembered})
	}
	return out
}

// goadLineMatches reports whether one live Goad$ True static goads o: its
// Affected$ spec matched with the static's own source, controller and
// remembered set bound (the Affected$ default is Card.Self, mirroring
// staticEffects, so a Goad$ line without Affected$ fails closed to its own
// source rather than to every creature). Board.GoadMatches holds the
// engine's re-entry guard so an IsGoaded-conditioned Affected$ spec cannot
// re-enter the derivation.
func goadLineMatches(b Board, l staticGoadLine, o *state.Object) bool {
	if o == nil {
		return false
	}
	return b.GoadMatches(l.spec, o.ID, l.source, l.controller, l.remembered)
}

// staticGoaders returns the controllers of every live Goad$ True static
// whose Affected$ spec matches o (CR 701.38b: a goad's goader is the
// permanent's controller, so a static goad's goader is the static's own
// controller), printed or granted alike.
func staticGoaders(b Board, o *state.Object) []state.PlayerID {
	var out []state.PlayerID
	for _, l := range staticGoadLines(b) {
		if goadLineMatches(b, l, o) {
			out = append(out, l.controller)
		}
	}
	return out
}

// StaticallyGoaded derives the static-goad SET the effects tier's IsGoaded
// predicate reads (SpecContext.Layers.StaticGoads, published through the
// layerTablesHost seam and bound in rules' matchesSpec): every battlefield
// object any live Goad$ True static currently goads, printed or granted. One
// board walk, AliveFrom(0) order, so the table is deterministic; nil when no
// goad static is live, which keeps the per-Resolve publication free for
// every board without one.
//
// When lki is supplied it also evaluates the just-departed battlefield
// object's LKI against those same live static sources. Trigger ValidCard$
// filters run after the zone move, so deriving only from the current
// battlefield would lose a static goad that applied immediately before the
// object left.
func StaticallyGoaded(b Board, lki *state.Object) map[state.ObjID]bool {
	lines := staticGoadLines(b)
	if len(lines) == 0 {
		return nil
	}
	g := b.Game()
	out := map[state.ObjID]bool{}
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			o := g.Obj(id)
			if o == nil {
				continue
			}
			for _, l := range lines {
				if goadLineMatches(b, l, o) {
					out[id] = true
					break
				}
			}
		}
	}
	if lki != nil && lki.Zone == state.ZBattlefield {
		for _, l := range lines {
			if goadLineMatches(b, l, lki) {
				out[lki.ID] = true
				break
			}
		}
	}
	return out
}

// HasActiveGoad reports whether o carries a live goad: an event-backed goad
// designation still in force, or a live Goad$ True static that matches it.
func HasActiveGoad(b Board, o *state.Object) bool {
	for _, ge := range o.Goads {
		if activeGoad(b, o, ge) {
			return true
		}
	}
	return len(staticGoaders(b, o)) > 0
}

// GoadedBy reports whether player p goads o, through a live goad
// designation or a matching Goad$ True static p controls.
func GoadedBy(b Board, o *state.Object, p state.PlayerID) bool {
	for _, ge := range o.Goads {
		if ge.Player == p && activeGoad(b, o, ge) {
			return true
		}
	}
	for _, goader := range staticGoaders(b, o) {
		if goader == p {
			return true
		}
	}
	return false
}

func activeGoad(b Board, o *state.Object, ge state.GoadEffect) bool {
	if o.Zone != state.ZBattlefield {
		return false
	}
	switch ge.Duration {
	case "AsLongAsInPlay":
		src := b.Game().Obj(ge.Source)
		return src != nil && src.Zone == state.ZBattlefield
	case "AsLongAsControl":
		return o.Controller == ge.Controller
	default:
		return true
	}
}
