package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// K:Ascend (CR 702.131, task ascend1): "If you control ten or more
// permanents, you get the city's blessing for the rest of the game."
//
// The keyword is read DIRECTLY here rather than expanded in
// cards/keywords.go, exactly like kw:Start your engines (rules/speed.go's
// checkSpeedStart) and kw:MaxSpeed: Forge's own expansion (CardFactoryUtil)
// minted a Mode$ Always static trigger plus a static ability, machinery this
// engine has no representation for, and the equivalent read is the emit-side
// scan below plus the spell-resolution grant. Registering the primitive makes
// the coverage census count the 30 K:Ascend carriers as supported.
func init() {
	effects.RegisterNonAPI("kw:Ascend")
}

// checkBlessingGrants is the permanent half of the Ascend grant: called from
// Engine.emit's post-fold hook on every battlefield entry (MoveZone), token
// mint (TokenCreate, CardToken) and control transfer (ControlChange). For
// each still-alive unblessed seat it checks CR 702.131a's gate directly off
// the FOLDED state -- ten or more permanents on the battlefield under the
// seat's control, at least one of them carrying the Ascend keyword -- and
// emits the one-way BlessingChange latch. Running after the fold means a
// ten-permanent entry grants its own arrival, and the recursive emit the
// scan makes sees a now-blessed seat, so the scan terminates.
func (e *Engine) checkBlessingGrants() {
	if e.G.Over {
		return
	}
	if !e.ascendPossible() {
		return
	}
	// granted memoizes activeGrantsKeyword across the seats of this one
	// scan: 0 = not yet read, 1 = no active grant, 2 = some effect grants it.
	granted := 0
	for _, p := range e.G.AliveFrom(0) {
		if int(p) >= len(e.G.Players) || e.G.Players[p].Lost || e.G.Players[p].Blessing {
			continue
		}
		board := e.G.Zone(state.ZBattlefield, p)
		if len(board) < 10 {
			continue
		}
		if granted == 0 {
			granted = 1
			if e.activeGrantsKeyword("Ascend") {
				granted = 2
			}
		}
		ascend := false
		for _, id := range board {
			// The full derived walk is the expensive half (every mint stales
			// it), so an object that can provably not carry Ascend is
			// skipped: with no active layer-6 grant of it, the derived list
			// is the base list minus removals, so an object whose base list
			// lacks it never has it. Exact, not a heuristic.
			if granted == 1 && !baseMayHaveKeyword(e.G.Obj(id), "Ascend") {
				continue
			}
			if e.hasKeywordH(id, kwhAscend) {
				ascend = true
				break
			}
		}
		if ascend {
			e.emit(events.Event{Kind: events.BlessingChange, Player: p,
				Text: "city's blessing"})
		}
	}
}

// grantSpellBlessing is the spell half of the Ascend grant (CR 702.131a's
// non-permanent case, matching Forge's resolvePreAbilities "do blessing there
// before condition checks"): an instant or sorcery with K:Ascend grants its
// controller the blessing AS THE SPELL RESOLVES, before the spell's own body
// and any of its condition checks read the latch. Called from
// rules/stack.go's resolveTop spell branch right after the Resolve event and
// right before effects.Resolve. Permanent faces are excluded -- their grant
// is the continuous scan above -- and the stack object itself is never on the
// battlefield, so it does not count toward the ten.
func (e *Engine) grantSpellBlessing(o *state.Object, f *cards.Face) {
	if f == nil || f.IsPermanent() || !f.HasKeyword("Ascend") {
		return
	}
	p := o.Controller
	if int(p) >= len(e.G.Players) || e.G.Players[p].Lost || e.G.Players[p].Blessing {
		return
	}
	if len(e.G.Zone(state.ZBattlefield, p)) < 10 {
		return
	}
	e.emit(events.Event{Kind: events.BlessingChange, Player: p,
		Text: "city's blessing"})
}

// ascendScan is checkBlessingGrants' amortised pre-filter. The scan below
// reads every permanent's DERIVED keywords, and the post-fold hook runs it on
// every token mint: a Krenko, Mob Boss / Horn of Gondor batch of N tokens
// over a board of N permanents was N^2 derived recomputes (each mint stales
// the derived cache), which turned a long cardfuzz game into a multi-minute
// hang. No object can carry Ascend unless some card in the game's object
// arena mentions it -- printed K:Ascend, or an AddKeyword$/KW$ grant or any
// other parameter naming it -- so the arena is scanned once, incrementally
// (objects are only ever appended), and the per-seat walk runs only once
// such a card exists. It is a pure cache over state and emits nothing.
type ascendScan struct {
	game    *state.Game
	scanned int
	seen    bool
}

func (e *Engine) ascendPossible() bool {
	s := &e.ascend
	if s.game != e.G || s.scanned > len(e.G.Objs) {
		*s = ascendScan{game: e.G}
	}
	for ; !s.seen && s.scanned < len(e.G.Objs); s.scanned++ {
		if cardMentionsAscend(e.G.Objs[s.scanned].Card) {
			s.seen = true
		}
	}
	return s.seen
}

// cardMentionsAscend reports whether any face of c prints Ascend or names it
// in any parameter or SVar (a grant). Deliberately over-inclusive: a false
// positive only costs the full scan.
func cardMentionsAscend(c *cards.Card) bool {
	if c == nil {
		return false
	}
	params := func(m map[string]string) bool {
		for _, v := range m { // membership test only; order cannot matter.
			if strings.Contains(v, "Ascend") {
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
			if strings.Contains(k, "Ascend") {
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

// activeGrantsKeyword reports whether any active continuous effect's layer-6
// AddKeywords names kw (by keyword head) -- the only way derivedCompute adds
// a keyword to an object beyond its base list (baseMayHaveKeyword).
func (e *Engine) activeGrantsKeyword(kw string) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		for _, k := range ce.AddKeywords {
			if strings.EqualFold(cardsKeywordHead(k), kw) {
				return true
			}
		}
	}
	return false
}

// baseMayHaveKeyword reports whether kw is in the keyword list
// derivedCompute starts from before its layer walk: the (copy-aware) face's
// printed keywords, the object's intrinsic keywords, marker-counter grants,
// and the status keywords (cloak's ward, suspect's menace, a suspend grant).
// It over-approximates (a face-down object's printed face is hidden), so a
// false answer, together with no active grant of kw, means the derived list
// cannot contain kw.
func baseMayHaveKeyword(o *state.Object, kw string) bool {
	if o == nil {
		return false
	}
	has := func(list []string) bool {
		for _, k := range list {
			if strings.EqualFold(cardsKeywordHead(k), kw) {
				return true
			}
		}
		return false
	}
	if f := o.Face(); f != nil && has(f.Keywords) {
		return true
	}
	if has(o.IntrinsicKeywords) {
		return true
	}
	for _, c := range o.Counters {
		if name, ok := cards.CounterKeyword(c.Kind); ok && c.N > 0 && strings.EqualFold(cardsKeywordHead(name), kw) {
			return true
		}
	}
	for _, st := range []string{"Ward", "Menace", "Suspend"} {
		if strings.EqualFold(st, kw) {
			return true // a status keyword (cloak/suspect/suspend): stay exact by never skipping
		}
	}
	return false
}
