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
)

// tapeCheckpointAll (tests only) makes the predicate say "may ask" for every
// resolution, so every resolution takes a checkpoint; tapeExemptAll makes it
// exempt everything, to exercise the miss fallback.
var tapeCheckpointAll, tapeExemptAll bool

// SAFacts.MayAsk cache states.
const (
	mayAskUnknown uint32 = iota
	mayAskNo
	mayAskYes
)

// tapeMayAsk reports whether resolving the top of the stack may pose a
// decision, judged from text and the stack object. true is always safe.
func tapeMayAsk(e *Engine) bool {
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
			return cards.SAChainMayAsk(ab, owned, self, true)
		}
		if self == nil || !cards.FaceOwnsSA(self, ab) {
			// A granted ability or a mutated pile's under-card: Self is not
			// the face that owns the text, so judge it uncached, with no Self
			// face (conservative for a Self return to the battlefield).
			var svars map[string]string
			if self != nil {
				svars = self.SVars
			}
			return cards.SAChainMayAsk(ab, svars, nil, true)
		}
		return saMayAskCached(e, ab, self)
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
	}
	if sa == nil {
		return true
	}
	return saMayAskCached(e, sa, f)
}

// saMayAskCached is cards.SAChainMayAsk over a face-owned root ability,
// memoised on its facts record. An ability with no record (runtime-built) is
// judged every time; a root's answer covers its whole SubAbility$ chain.
func saMayAskCached(e *Engine, sa *cards.SA, self *cards.Face) bool {
	f := e.compiledText.factsOf(sa)
	if f == nil {
		return cards.SAChainMayAsk(sa, self.SVars, self, true)
	}
	switch atomic.LoadUint32(&f.MayAsk) {
	case mayAskNo:
		return false
	case mayAskYes:
		return true
	}
	v := cards.SAChainMayAsk(sa, self.SVars, self, true)
	st := mayAskNo
	if v {
		st = mayAskYes
	}
	atomic.StoreUint32(&f.MayAsk, st)
	return v
}
