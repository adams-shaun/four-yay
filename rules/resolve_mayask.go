package rules

// resolve_mayask.go is the object half of the resolution kernel's ask-free
// predicate (lasagna spec §7, W3 step 0; spike S3b candidate 1): before the
// kernel checkpoints a resolution it asks whether the object about to resolve
// can pose a decision at all. The S3 spike measured 86.6% of per-resolution
// checkpoints dropped unused; skipping those is what brings the kernel's cost
// within noise.
//
// It is a PERFORMANCE HINT, never a correctness input. Text cannot see
// board-dependent engine-posed asks (a CR 616.1 replacement-order choice, an
// as-enters choice on a permanent another effect moves, a CR 903.9
// commander-zone choice), and its per-API rules can be wrong. Either way the
// miss is caught at the one choke point every decision passes through
// (Engine.ask -> resolve.Kernel.OnAsk): the resolution has no checkpoint, so
// while the legacy path exists it continues on it in place -- sound because
// nothing has been served from a tape yet. Misses are counted
// (resolve.Stats.Misses) and classed by the cardfuzz miss census.
//
// The text half is cards.SAChainMayAsk, a pure function of the ability's
// immutable text, memoised here on the ability's facts record (sa_facts.go),
// so the per-resolution cost is a few loads.

import (
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// tapeCheckpointAll (tests only) makes the predicate say "may ask" for every
// resolution, so every resolution takes a checkpoint; tapeExemptAll makes it
// exempt everything, to exercise the miss fallback.
var tapeCheckpointAll, tapeExemptAll bool

// SAFacts.MayAsk cache states: 0 is unknown; a known state carries
// mayAskKnown, plus mayAskText when the text may ask, plus the chain's board
// gates (cards.SAChainBoardGates) shifted by mayAskGateShift.
const (
	mayAskUnknown   uint32 = 0
	mayAskKnown     uint32 = 1
	mayAskText      uint32 = 2
	mayAskGateShift        = 2
)

// tapeMayAsk reports whether resolving the top of the stack may pose a
// decision, judged from text and the stack object. true is always safe.
func tapeMayAsk(e *Engine) bool {
	return tapeTextMayAsk(e) || tapeBoardCompetes(e)
}

// tapeBoardCompetes is the board gate every otherwise ask-free resolution
// meets: counters-put replacements competing for one event (CR 616.1's order
// choice: a permanent entering with counters under Doubling Season and
// Hardened Scales) and any replacement that elects or whose body asks. Each
// test prunes on the replacement arena's event mask, so it costs a load when
// no such line is in play.
func tapeBoardCompetes(e *Engine) bool {
	return tapeReplMayAsk(e, "AddCounter") || tapeAnyReplBodyMayAsk(e)
}

func tapeTextMayAsk(e *Engine) bool {
	switch {
	case tapeCheckpointAll:
		return true
	case tapeExemptAll:
		return false
	}
	id := e.G.Stack[len(e.G.Stack)-1]
	o := e.G.Obj(id)
	if o == nil || o.IsCopy {
		return true // a copy: copy-target elections, copied modal riders
	}
	if ab := o.Ability; ab != nil {
		if ab.API == "Charm" && len(o.ChosenModes) == 0 {
			return true // modes not announced on the stack: picked at resolution
		}
		// CumulativeUpkeep, Echo and CopySpellAbility bodies are outside the
		// text allowlist, so the chain walk already says "may ask".
		if t, ok := e.triggerForAbilityObject(id, o); ok {
			if e.triggerBodyNeedsCostWindow(ab) || cards.TriggerLineMayAsk(t) {
				return true
			}
		}
		var self *cards.Face
		if src := e.G.Obj(o.Source); src != nil {
			self = src.Face()
		}
		if owned, ok := e.triggerLineSVars[id]; ok {
			// A granted, delayed or reflexive body: a freshly parsed SA with
			// no facts record, read against the grant's own SVar table.
			return mayAskOnBoard(e, saMayAskState(ab, owned, self))
		}
		if self == nil || !cards.FaceOwnsSA(self, ab) {
			// A granted ability or a mutated pile's under-card: Self is not
			// the face that owns the text, so judge it uncached, with no Self
			// face (conservative for a Self return to the battlefield).
			var svars map[string]string
			if self != nil {
				svars = self.SVars
			}
			return mayAskOnBoard(e, saMayAskState(ab, svars, nil))
		}
		return mayAskOnBoard(e, saMayAskCached(e, ab, self))
	}
	f := o.Face()
	if f == nil {
		return true
	}
	sa := f.SpellAbility()
	if f.IsPermanent() {
		if cards.FaceEntryMayAsk(f) {
			return true
		}
		if sa == nil {
			return false
		}
	} else if f.HasKeyword("Cipher") {
		return true // the resolving spell's CR 702.99a encode election
	}
	if sa == nil {
		return true
	}
	return mayAskOnBoard(e, saMayAskCached(e, sa, f))
}

// saMayAskState is the text judgement of sa's chain as a cache state.
func saMayAskState(sa *cards.SA, svars map[string]string, self *cards.Face) uint32 {
	if cards.SAChainMayAsk(sa, svars, self, true) {
		return mayAskKnown | mayAskText
	}
	return mayAskKnown | uint32(cards.SAChainBoardGates(sa, svars))<<mayAskGateShift
}

// mayAskOnBoard resolves a cache state against the board: an ask-free text
// asks only through a board gate it opens.
func mayAskOnBoard(e *Engine, st uint32) bool {
	if st&mayAskText != 0 {
		return true
	}
	g := uint8(st >> mayAskGateShift)
	return (g&cards.GateTokens != 0 && tapeReplMayAsk(e, "CreateToken")) ||
		(g&cards.GateDamage != 0 && tapeReplMayAsk(e, "DamageDone")) ||
		(g&cards.GateDraw != 0 && tapeDredgeMayAsk(e))
}

// tapeDredgeMayAsk is the draw gate: a card with Dredge in any graveyard
// (CR 702.55) can replace a draw with its election. Read only for an
// otherwise ask-free drawing resolution.
func tapeDredgeMayAsk(e *Engine) bool {
	for p := range e.G.Players {
		for _, id := range e.G.Zone(state.ZGraveyard, state.PlayerID(p)) {
			if o := e.G.Obj(id); o != nil {
				if f := o.Face(); f != nil {
					if _, ok := f.KeywordParam("Dredge"); ok {
						return true
					}
				}
			}
		}
	}
	return false
}

// tapeReplMayAsk is a board gate: a replacement on event that elects
// (cards.ReplMayElect), or two of them at once (a CR 616.1 order choice),
// can ask as the event happens. An Effect-created CreateToken replacement
// always counts as electing. Read only for an
// otherwise ask-free resolution that opens the gate, through the
// replacement-source zone masks.
func tapeReplMayAsk(e *Engine, event string) bool {
	n, ask := 0, false
	// Effect-created replacements (an Effect's ReplacementEffects$: Soul
	// Echo's per-upkeep damage shield) live on the continuous effects.
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		if ce := &ceL[ceI]; ce.ReplacementEvent == event {
			if n++; n > 1 || event == "CreateToken" || cards.ReplParamsMayElect(ce.ReplacementParams) {
				return true
			}
		}
	}
	e.forEachReplacementSourceFor(replEventBit(event), func(id state.ObjID) {
		o := e.G.Obj(id)
		if ask || o == nil {
			return
		}
		f := o.Face()
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			if r.Event != event {
				continue
			}
			if n++; n > 1 || cards.ReplMayElect(r) {
				ask = true
				return
			}
		}
	})
	return ask
}

// saMayAskCached is cards.SAChainMayAsk over a face-owned root ability,
// memoised on its facts record as a cache state. An ability with no record
// (runtime-built) is judged every time; a root's answer covers its whole
// SubAbility$ chain.
func saMayAskCached(e *Engine, sa *cards.SA, self *cards.Face) uint32 {
	f := e.compiledText.factsOf(sa)
	if f == nil {
		return saMayAskState(sa, self.SVars, self)
	}
	if st := atomic.LoadUint32(&f.MayAsk); st != mayAskUnknown {
		return st
	}
	st := saMayAskState(sa, self.SVars, self)
	atomic.StoreUint32(&f.MayAsk, st)
	return st
}
