// Mentor (CR 702.134)'s target restriction.
//
// cards/kw_mentor.go expands printed K:Mentor into an ordinary Mode$ Attacks
// trigger whose body is a targeted PutCounter carrying the Mentor$ marker,
// and checkGrantedMentorTriggers synthesizes the same body for a layer-6
// AddKeyword$ Mentor grant. The restriction -- "target attacking creature
// with LESSER power" -- is the strict power comparison this file owns.
//
// It is deliberately NOT written as a powerLTX target spec. A target spec's
// numeric RHS resolves through targetSpecContext's Resolve, which reads the
// announced {X} (a cast's e.cast.x, or the stack object's o.X) and never the
// source face's SVar:X; Unliving Psychopath's own `Creature.powerLTX` with
// `SVar:X:Count$CardPower` therefore offers an EMPTY target list. Keeping the
// comparison in one shared helper -- called by the target offer and by the
// CR 608.2b resolution recheck -- means offer and recheck cannot disagree
// (Critical C2's one-definition rule).

package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// mentorAdmits reports whether obj may be chosen as the target of a target
// spec carrying the Mentor marker: the resolving source's power strictly
// exceeds obj's, both read as the derived value so a counter or a lord moves
// the comparison exactly as it moves the creature (CR 702.134 uses the
// target's current power). An absent source or candidate fails closed. The
// strict `<` is what excludes the Mentor source itself, since no creature's
// power is less than its own.
func (e *Engine) mentorAdmits(sa *cards.SA, source, obj state.ObjID) bool {
	if sa == nil || !strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKMentor)), "True") {
		return true
	}
	if e.G.Obj(source) == nil || e.G.Obj(obj) == nil {
		return false
	}
	return e.Power(obj) < e.Power(source)
}
