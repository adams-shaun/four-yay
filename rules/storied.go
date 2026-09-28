package rules

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// K:Storied (CR 702.175, task storied1): "If you control three or more
// artifacts, legendaries, and/or Sagas, you have an enduring story for the
// rest of the game."
//
// The keyword is read DIRECTLY here rather than expanded in
// cards/keywords.go, exactly like kw:Ascend (rules/ascend.go) and kw:Start
// your engines (rules/speed.go): Forge's own expansion mints a static
// ability this engine has no representation for, and the equivalent read is
// the emit-side scan below plus state.Player.EnduringStory. Registering the
// primitive makes the coverage census count the 9 K:Storied carriers as
// supported.
func init() {
	effects.RegisterNonAPI("kw:Storied")
}

// checkEnduringStoryGrants is the continuous half of CR 702.175a's grant:
// called from Engine.emit's post-fold hook on every battlefield entry
// (MoveZone), token mint (TokenCreate, CardToken) and control transfer
// (ControlChange) -- the same hook shape checkBlessingGrants uses. For each
// still-alive seat without the designation it checks the gate directly off
// the FOLDED state: the seat controls at least three permanents that are
// artifacts, legendaries and/or Sagas (each permanent counted ONCE, even if
// it matches several), and controls at least one permanent whose derived
// keywords include Storied. It then emits the one-way EnduringStoryChange
// latch. Running after the fold means a third qualifying permanent's own
// arrival grants it, and the recursive emit the scan makes sees a now-set
// seat, so the scan terminates.
func (e *Engine) checkEnduringStoryGrants() {
	if e.G.Over {
		return
	}
	if !e.storiedPossible() {
		return
	}
	for _, p := range e.G.AliveFrom(0) {
		if int(p) >= len(e.G.Players) || e.G.Players[p].Lost || e.G.Players[p].EnduringStory {
			continue
		}
		board := e.G.Zone(state.ZBattlefield, p)
		storied, count := false, 0
		for _, id := range board {
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield {
				continue
			}
			if e.hasKeywordH(id, kwhStoried) {
				storied = true
			}
			d := e.Derived(id)
			if slices.Contains(d.Types, "Artifact") ||
				slices.Contains(d.Types, "Saga") ||
				(o.Face() != nil && o.Face().IsLegendary()) {
				count++
			}
		}
		if storied && count >= 3 {
			e.emit(events.Event{Kind: events.EnduringStoryChange, Player: p,
				Text: "enduring story"})
		}
	}
}

// storiedPossible is checkEnduringStoryGrants' amortised pre-filter, the
// exact shape of ascendPossible (rules/ascend.go): the scan reads every
// permanent's DERIVED keywords, and the post-fold hook runs it on every
// battlefield entry, so a game with no Storied card in its arena pays a
// full derived walk per entry for nothing. No object can carry Storied
// unless some card in the game's object arena mentions it (printed
// K:Storied, or an AddKeyword$/KW$ grant naming it), so the arena is scanned
// once, incrementally (objects are only ever appended), and the per-seat
// walk runs only once such a card exists. It is a pure cache over state and
// emits nothing.
type storiedScan struct {
	game    *state.Game
	scanned int
	seen    bool
}

func (e *Engine) storiedPossible() bool {
	s := &e.storied
	if s.game != e.G || s.scanned > len(e.G.Objs) {
		*s = storiedScan{game: e.G}
	}
	for ; !s.seen && s.scanned < len(e.G.Objs); s.scanned++ {
		if cardMentionsStoried(e.G.Objs[s.scanned].Card) {
			s.seen = true
		}
	}
	return s.seen
}

// cardMentionsStoried reports whether any face of c prints Storied or names
// it in any parameter or SVar (a grant). Deliberately over-inclusive: a false
// positive only costs the full scan.
func cardMentionsStoried(c *cards.Card) bool {
	if c == nil {
		return false
	}
	params := func(m map[string]string) bool {
		for _, v := range m { // membership test only; order cannot matter.
			if strings.Contains(v, "Storied") {
				return true
			}
		}
		return false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, k := range f.Keywords {
			if strings.Contains(k, "Storied") {
				return true
			}
		}
		if params(f.SVars) {
			return true
		}
		for _, st := range f.Statics {
			if params(st.Params) {
				return true
			}
		}
		for _, a := range f.Abilities {
			if a != nil && params(a.Params) {
				return true
			}
		}
		for _, tr := range f.Triggers {
			if params(tr.Params) || (tr.Effect != nil && params(tr.Effect.Params)) {
				return true
			}
		}
	}
	return false
}
