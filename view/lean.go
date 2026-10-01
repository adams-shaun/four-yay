package view

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Omit names optional parts of a Seat-visibility projection that a consumer
// declares it never reads. A lean projection (ProjectLeanInto) leaves every
// omitted part at its zero value and never asks Chars for the facts behind
// it; every part NOT omitted is exactly what ProjectInto writes. The zero
// Omit is ProjectInto.
type Omit uint32

const (
	// OmitLibrary skips PlayerView.Library (the unordered own-library list).
	OmitLibrary Omit = 1 << iota
	// OmitAvailable skips PlayerView.Available (Chars.AvailableMana).
	OmitAvailable
	// OmitArchetype skips PlayerView.Archetype (the opponent posterior). It
	// is inferred from full card lists, so OmitCardLists and
	// OmitDerivedChars skip it too.
	OmitArchetype
	// OmitAbilityCosts skips CardView.AbilityCosts (Chars.AbilityCosts).
	OmitAbilityCosts
	// OmitEffectiveCost skips CardView.EffectiveManaCost
	// (SpellEffectiveCost).
	OmitEffectiveCost
	// OmitDecision skips View.Decision (the copied pending decision).
	OmitDecision
	// OmitOwnDeck skips View.OwnDeck.
	OmitOwnDeck
	// OmitPotential skips PlayerView.PotentialActions.
	OmitPotential
	// OmitCardLists skips every CardView list but the viewer's own
	// Battlefield: hands, graveyards, exiles, command zones, planar decks,
	// commander rosters, and every other seat's battlefield.
	OmitCardLists
	// OmitDerivedChars projects every CardView without its Chars-derived
	// facts: the printed name, no keywords, zero power and toughness.
	OmitDerivedChars
)

// LeanReader is the optional seat capability a driver loop probes before it
// projects a decision's View: ViewOmit names the parts of the View the seat
// will not read when answering d. A seat implementing it also promises not
// to retain anything reachable from the View past its Decide call, so the
// driver may refill one View per seat (ProjectLeanInto's reuse contract,
// which is ProjectInto's).
type LeanReader interface {
	ViewOmit(d *decision.Decision) Omit
}

// ProjectLeanInto is ProjectInto with the parts named by omit left zero. It
// is a Seat-visibility projection: omit == 0 makes it exactly ProjectInto.
func ProjectLeanInto(dst *View, g *state.Game, ch Chars, viewer state.PlayerID, d *decision.Decision, omit Omit) {
	m := projectMode{ownLibrary: omit&OmitLibrary == 0, omit: omit}
	prevDeck := dst.OwnDeck
	projectInto(dst, g, ch, viewer, d, &m)
	dst.Visibility = Seat.String()
	if omit&OmitOwnDeck == 0 && g != nil && int(viewer) < len(g.Players) && ch != nil {
		dst.OwnDeck = ownDeckInto(prevDeck, ch, viewer)
	}
}

// RoundFold is RoundOf folded incrementally over an append-only event log:
// each Of call folds only the events appended since the previous call, so a
// driver that asks once per decision pays for every event once rather than
// once per decision. Of(g, evs) always equals RoundOf(g, evs) provided every
// evs a fold sees extends the previous one (a shorter log, or a different
// seat count, restarts the fold from the beginning). The zero RoundFold is
// ready to use.
type RoundFold struct {
	pos   int
	n     int
	start int
	round int32
	alive [16]bool
}

// Of returns RoundOf(g, evs).
func (f *RoundFold) Of(g *state.Game, evs []events.Event) int32 {
	if g == nil {
		return 1
	}
	n := len(g.Players)
	if n <= 0 {
		return 1
	}
	if n > len(f.alive) {
		return RoundOf(g, evs)
	}
	if f.n != n || len(evs) < f.pos {
		*f = RoundFold{n: n, start: -1, round: 1}
		for i := 0; i < n; i++ {
			f.alive[i] = true
		}
	}
	for _, ev := range evs[f.pos:] {
		switch ev.Kind {
		case events.PlayerLost:
			if int(ev.Player) < n {
				f.alive[ev.Player] = false
			}
		case events.TurnChange:
			cur := int(ev.Player)
			if f.start < 0 {
				f.start = cur
				continue
			}
			for i := 0; i < n; i++ {
				first := (f.start + i) % n
				if !f.alive[first] {
					continue
				}
				if cur == first {
					f.round++
				}
				break
			}
		}
	}
	f.pos = len(evs)
	return f.round
}
