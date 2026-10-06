package rules

// resolve_mayask.go is the object half of the resolution kernel's ask-free
// predicate (lasagna spec §7, W3 step 0; spike S3b candidate 1): before the
// kernel checkpoints a resolution it asks whether the object about to resolve
// can pose a decision at all. The S3 spike measured 86.6% of per-resolution
// checkpoints dropped unused; skipping those is what brings the kernel's cost
// within noise.
//
// It must never say "ask-free" for a resolution that asks. Text cannot see
// board-dependent engine-posed asks (a CR 616.1 replacement-order choice, an
// as-enters choice on a permanent another effect moves, a CR 903.9
// commander-zone choice), so each of those needs a board gate
// (tapeBoardCompetes). A miss is caught at the one choke point every
// decision passes through (Engine.ask -> resolve.Kernel.OnAsk) and, with the
// legacy path gone, FAILS HARD: the exempted resolution has no checkpoint to
// pose the ask from, so Engine.ask panics with resolve.MissFailure. Misses
// are counted (resolve.Stats.Misses) and classed by the cardfuzz miss census.
//
// The text half is cards.SAChainMayAsk, a pure function of the ability's
// immutable text, memoised here on the ability's facts record (sa_facts.go),
// so the per-resolution cost is a few loads.

import (
	"math/bits"
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
	// Keep the conditional reasons above every replacement-event gate bit:
	// adding a new ReplEvent must not masquerade as a target-entry reason.
	mayAskCondShift = mayAskGateShift + cards.ReplEventCount
	mayAskGateMask  = uint32(1)<<cards.ReplEventCount - 1
)

// The highest conditional reason must still fit the uint32 cache state: a
// constant overflow here is a compile error, not a silent alias.
const _ uint32 = cards.MayAskCondParentSub << mayAskCondShift

// tapeMayAsk reports whether resolving the top of the stack may pose a
// decision, judged from text and the stack object. true is always safe.
func tapeMayAsk(e *Engine) bool {
	return tapeTextMayAsk(e) || tapeBoardCompetes(e)
}

// tapeBoardCompetes is the board gate every otherwise ask-free resolution
// meets: counters-put or PayLife cost replacements competing for one event
// (CR 616.1's order choice), and any replacement that elects or whose body
// asks. Each test prunes on the replacement arena's event mask, so it costs a
// load when no such line is in play.
//
// The last gate is an Oblivion Ring style return (ChangeZone Duration$
// UntilHostLeavesPlay, sweepExileReturn): any resolution that moves the
// holder off the battlefield -- a bounce, a destroy, a sacrifice -- returns
// its exiled cards inside the same emit, and a card coming back onto the
// battlefield asks as it enters exactly as a targeted entry would (an Aura
// choosing what it enchants, CR 303.4f: Mischievous Pup bouncing a Banishing
// Light that holds Imprisoned in the Moon). The exile zones are read first,
// so the battlefield is walked only when an exiled card could ask.
func tapeBoardCompetes(e *Engine) bool {
	if tapeReplMayAsk(e, "AddCounter") || tapeReplMayAsk(e, "PayLife") || tapeAnyReplBodyMayAsk(e) {
		return true
	}
	cand := false
	for p := 0; p < len(e.G.Players) && !cand; p++ {
		for _, id := range e.G.Zone(state.ZExile, state.PlayerID(p)) {
			if tapeEntryMayAsk(e, id) {
				cand = true
				break
			}
		}
	}
	if !cand {
		return false
	}
	for p := range e.G.Players {
		for _, hid := range e.G.Zone(state.ZBattlefield, state.PlayerID(p)) {
			h := e.G.Obj(hid)
			if h == nil {
				continue
			}
			for _, en := range h.ExileReturn {
				if o := e.G.Obj(en.Obj); en.From == state.ZBattlefield && o != nil && o.Zone == state.ZExile &&
					tapeEntryMayAsk(e, en.Obj) {
					return true
				}
			}
		}
	}
	return false
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
	if o.CastFlags&state.FlagFused != 0 {
		// A fused split spell (CR 702.102c) runs BOTH halves; the face read
		// below is only the front half, and resolveFused poses each half's
		// sub-ability target asks (and the alternate half's own asks, Away's
		// sacrifice) at resolution.
		return true
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
			return mayAskObjOnBoard(e, saMayAskState(e, ab, owned, self), id, o, ab)
		}
		if self == nil || !cards.FaceOwnsSA(self, ab) {
			// A granted ability or a mutated pile's under-card: Self is not
			// the face that owns the text, so judge it uncached, with no Self
			// face (conservative for a Self return to the battlefield).
			var svars map[string]string
			if self != nil {
				svars = self.SVars
			}
			return mayAskObjOnBoard(e, saMayAskState(e, ab, svars, nil), id, o, ab)
		}
		return mayAskObjOnBoard(e, saMayAskCached(e, ab, self), id, o, ab)
	}
	f := o.Face()
	if f == nil {
		return true
	}
	sa := f.SpellAbility()
	if f.IsPermanent() {
		// The spell's own entry text, and the board's entry replacements:
		// two Moved replacements competing for the entering permanent pose
		// a CR 616.1 order choice. The land play's narrowed gate: a line
		// that cannot apply to this object's own battlefield entry (an
		// ETB-tapped land's self-only line in a library or hand) neither
		// elects nor competes; counting those checkpointed every permanent
		// spell of a deck holding two such lands.
		if cards.FaceEntryMayAsk(f) || tapeLandReplMayAsk(e, id) {
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
	return mayAskObjOnBoard(e, saMayAskCached(e, sa, f), id, o, sa)
}

// saMayAskState is the text judgement of sa's chain as a cache state.
// A Token body's minted face is the match's (e.G.Tokens): its entry asks
// are judged here, with the text (cards.SAChainTokenEntryMayAsk).
func saMayAskState(e *Engine, sa *cards.SA, svars map[string]string, self *cards.Face) uint32 {
	may, cond := cards.SAChainMayAskCond(sa, svars, self, true)
	if may || cards.SAChainTokenEntryMayAsk(sa, svars, e.G.Tokens) {
		return mayAskKnown | mayAskText
	}
	return mayAskKnown | uint32(cards.SAChainBoardGates(sa, svars))<<mayAskGateShift | cond<<mayAskCondShift
}

// mayAskObjOnBoard is mayAskOnBoard for the resolving stack object o whose
// root ability is sa: a conditional reason of the chain (cards.MayAskCond*)
// is settled from o's chosen targets and the board. MayAskCondTargetEntry:
// the root moves its chosen targets onto the battlefield, so it asks only if
// one of them asks as it enters (tapeEntryMayAsk).
func mayAskObjOnBoard(e *Engine, st uint32, id state.ObjID, o *state.Object, sa *cards.SA) bool {
	if mayAskOnBoard(e, st) {
		return true
	}
	cond := st >> mayAskCondShift
	if cond&cards.MayAskCondTargetEntry != 0 {
		for _, t := range o.Targets {
			if !t.IsPlayer && tapeEntryMayAsk(e, t.Obj) {
				return true
			}
		}
	}
	return cond&cards.MayAskCondParentSub != 0 && tapeParentSubMayAsk(e, id, o, sa)
}

// tapeEntryMayAsk reports whether the object id asks as it enters the
// battlefield: its own entry text on any face it could enter as, or the
// entry replacements on the board that can apply to it (tapeLandReplMayAsk:
// an election, or two competing).
func tapeEntryMayAsk(e *Engine, id state.ObjID) bool {
	obj := e.G.Obj(id)
	if obj == nil {
		return false
	}
	if f := obj.CopyFace; f != nil && cards.FaceEntryOrEnchantMayAsk(f) {
		return true
	}
	if obj.Card != nil {
		for _, f := range obj.Card.Faces {
			if f != nil && cards.FaceEntryOrEnchantMayAsk(f) {
				return true
			}
		}
	}
	return tapeLandReplMayAsk(e, id)
}

// tapeParentSubMayAsk settles cards.MayAskCondParentSub for a spell: a
// link targeting relative to its parent's target asks only with a candidate
// (cards.ParentSubLinksMayAsk), which must be a battlefield object matching
// one of its ValidTgts$ heads.
func tapeParentSubMayAsk(e *Engine, id state.ObjID, o *state.Object, sa *cards.SA) bool {
	return cards.ParentSubLinksMayAsk(sa, func(head string) bool {
		for p := range e.G.Players {
			for _, bf := range e.G.Zone(state.ZBattlefield, state.PlayerID(p)) {
				if e.matchesSpecFrom(head, bf, o.Controller, id) {
					return true
				}
			}
		}
		return false
	})
}

// mayAskOnBoard resolves a cache state against the board: an ask-free text
// asks only through a board gate it opens (replEventGates).
func mayAskOnBoard(e *Engine, st uint32) bool {
	if st&mayAskText != 0 {
		return true
	}
	g := cards.ReplEventMask(st >> mayAskGateShift & mayAskGateMask)
	for g != 0 {
		k := cards.ReplEvent(bits.TrailingZeros32(uint32(g)))
		g &^= 1 << k
		if ask := replEventGates[k].ask; ask == nil || ask(e) {
			return true // an ungated event is "may ask" (the census holds this empty)
		}
	}
	return false
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
		if ce := &ceL[ceI]; replacementEventNameMatches(ce.ReplacementEvent, event) {
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
			if !replacementEventNameMatches(r.Event, event) {
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
		return saMayAskState(e, sa, self.SVars, self)
	}
	if st := atomic.LoadUint32(&f.MayAsk); st != mayAskUnknown {
		return st
	}
	st := saMayAskState(e, sa, self.SVars, self)
	atomic.StoreUint32(&f.MayAsk, st)
	return st
}
