package rules

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// derivedScalar returns only an object's derived power and toughness — the
// subset of Derived that combat, legal, cast, trigger and bot predicates read
// constantly (and, through the Chars interface, every projected view). It
// never builds the keyword/type slices Derived carries, and its effect list
// comes from active()'s cached, buffer-reused build. Its P/T is identical to
// what the full Derived computes because it first derives layer-4 types and
// hands them to every filter used by a layer-7 effect. A layer-6 KEYWORD
// grant can affect P/T applicability (an `Affected$ ...+with<Keyword>`
// pump like Windstorm Drake's), so when any layer-7 effect's Affected$
// spec actually reads the keyword list the walk delegates to derivedWith --
// the one place that builds the finished layer-6 list exactly -- instead
// of duplicating the keyword-evolution logic. The probe is a cheap
// strings.Contains short-circuit per layer-7 effect, so boards without a
// keyword-gated pump keep the old no-keyword-slice cost; layer-4 type
// changes are handled by typeCharacteristics below regardless.

func (e *Engine) derivedScalar(id state.ObjID) (power, toughness int32) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return 0, 0
	}
	f := o.Face()
	active := e.active()
	if p, t, ok := e.printedPT(o, f, active); ok {
		return p, t
	}
	for i := range active {
		if ce := &active[i]; ce.Layer == LPT && effects.SpecReadsKeywords(ce.Affects) {
			d := e.derivedWith(id, 0)
			return d.Power, d.Toughness
		}
	}
	p, t, _, _ := chars.PT(asChars(e), &e.charsWalk, id, o, f, active, nil, nil, false)
	return p, t
}

// Derived computes an object's current characteristics: printed values from
// its face, then every applicable continuous effect in layer order, then
// layer 7d counters last. A malformed or missing object degrades to the
// zero Derived rather than panicking — layer inputs ultimately come from
// parsed card text, and a nonexistent ObjID or an ability/token object with
// no Face() must never crash the match goroutine.
//
// The Keywords and Types slices alias the Engine's scratch buffers
// (charsWalk.KW / charsWalk.Types, engine_scratch.go) and are reused across calls: after
// the first call's buffers grow to size they are rewritten, never
// reallocated, so repeated Derived builds are allocation-free. That is only
// sound because every caller treats the returned slices as read-only and
// does not retain them past building its own view — view.Project and
// botpolicy both copy (append([]string(nil), ...)) synchronously, and the
// loops in HasKeyword and protectedFrom only range. Sharing would be wrong
// if a caller held one Derived's slices while calling Derived again (the
// next call would rewrite the shared buffers), so the discipline is
// load-bearing; charsWalk.Depth guards re-entry the way active()'s activeDepth
// guards its cache (a nested Derived mid-build gets private owned buffers
// instead of clobbering the outer build's).
//
// Inside a legal-actions walk (legalActionsPriced) a top-level Derived is
// memoized per object for that walk only (rules/derivedmemo.go); a memo hit's
// slices are owned by the memo entry rather than the scratch, and the same
// read-only, do-not-retain discipline applies to them.
func (e *Engine) Derived(id state.ObjID) Derived {
	return e.derivedWith(id, 0)
}

// inProgressDerivedPT returns only an active layer-7 frame; unlike
// FilterDerivedPT it never starts a fresh derivation when no frame exists.
func (e *Engine) inProgressDerivedPT(id state.ObjID) (chars.PTFrame, bool) {
	return e.charsWalk.InProgress(id)
}

// InProgressDerivedPT exposes the current layer-7 value to effects-side P/T
// references. A reference made by the static currently deriving this object
// must observe the value before that effect, rather than recursively deriving
// the same object. It intentionally has no fallback derivation, and it reads
// the PRE-COUNTER pair: the running value excludes layer 7d counters (CR
// 613.4 orders counters after every 7c modify), so a 7c amount sized from
// this object's P/T does not depend on its own +1/+1 counters.
func (e *Engine) InProgressDerivedPT(id state.ObjID) (power, toughness int32, ok bool) {
	frame, ok := e.inProgressDerivedPT(id)
	if !ok {
		return 0, 0, false
	}
	return frame.PreCounterPower, frame.PreCounterToughness, true
}

// FilterDerivedPT exposes a value snapshot for effects-side zone counts. The
// effects package cannot depend on rules, so its Count$Valid fold discovers
// this bridge through an optional interface and binds the values into the
// candidate's SpecContext.
func (e *Engine) FilterDerivedPT(id state.ObjID) (power, toughness, basePower, baseToughness int32, ok bool) {
	if frame, found := e.inProgressDerivedPT(id); found {
		return frame.Power, frame.Toughness, frame.BasePower, frame.BaseToughness, true
	}
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return 0, 0, 0, 0, false
	}
	d := e.Derived(id)
	return d.Power, d.Toughness, d.BasePower, d.BaseToughness, true
}

// Characteristics returns the three derived facts botpolicy projects in one
// pass. Keywords aliases Engine scratch storage exactly as Derived does; a
// caller that keeps it across another characteristics query must copy it.
func (e *Engine) Characteristics(id state.ObjID) (power, toughness int32, keywords []string) {
	if p, t, kw, ok := e.printedCharacteristics(id, true); ok {
		return p, t, kw
	}
	// Derived(id), read in place: the memo entry by pointer when derivedWith
	// would serve the memo, else one uncached build.
	if e.derivedMemoDepth > 0 && e.derivedMemoUsable() {
		if d := e.derivedMemoRef(id, 0); d != nil {
			return d.Power, d.Toughness, d.Keywords
		}
	}
	d := e.derivedCompute(id, 0)
	return d.Power, d.Toughness, d.Keywords
}

// derivedWith is Derived with an optional ZONE OVERRIDE for the AffectedZone$
// gate: atStack != 0 evaluates the grants against that zone instead of the
// object's live one. The convoke announcement (CR 601.2b) happens while the
// announced spell is still in hand -- the engine pushes it to the stack only
// later in its own cast flow -- so convokeCost/hasCastConvoke evaluate an
// AffectedZone$ Stack grant against ZStack via this override; everything
// else reads the live zone.
func (e *Engine) derivedWith(id state.ObjID, atStack state.Zone) Derived {
	if (atStack == 0 || atStack == state.ZStack) && e.derivedMemoDepth > 0 && e.derivedMemoUsable() {
		return e.derivedMemoizedAt(id, atStack)
	}
	return e.derivedCompute(id, atStack)
}

// derivedCompute is derivedWith's uncached build: chars.Compute, the full
// layer walk, over the engine's Board and its charsWalk scratch, its
// Keywords/Types aliasing that scratch as documented on Derived.
// derivedmemo.go's walk-scoped memo calls it on a miss (and on every hit in
// verify mode) and copies the result into owned storage.
func (e *Engine) derivedCompute(id state.ObjID, atStack state.Zone) Derived {
	return chars.Compute(asChars(e), &e.charsWalk, id, atStack)
}

// derivedName is Derived(id).Name without the rest of the walk (chars.Name):
// it evaluates exactly the layer-3 SetName$ effects the full walk would, with
// the same bindings, so it names what derivedCompute names. In the rules test
// binary derivedMemoVerify recomputes the full walk and panics on a
// difference.
func (e *Engine) derivedName(id state.ObjID) string {
	name := chars.Name(asChars(e), &e.charsWalk, id)
	if derivedMemoVerify {
		if want := e.derivedCompute(id, 0).Name; want != name {
			panic(fmt.Sprintf("rules: derivedName(%d) = %q, full walk %q", id, name, want))
		}
	}
	return name
}

// Name returns the current layer-3 name of an object. Callers that render or
// compare characteristics must use this rather than the printed face name.
func (e *Engine) Name(id state.ObjID) string { return e.Derived(id).Name }

// Text returns the object's current layer-3 text (CR 613.1d / CR 612): its
// printed Oracle text after every applicable text-changing effect. Callers
// that render or compare an object's rules text must use this rather than
// o.Face().Oracle.
func (e *Engine) Text(id state.ObjID) string { return e.Derived(id).Text }

func (e *Engine) Power(id state.ObjID) int32 {
	p, _ := e.derivedScalar(id)
	return p
}

func (e *Engine) Toughness(id state.ObjID) int32 {
	_, t := e.derivedScalar(id)
	return t
}

// HasKeyword matches case-insensitively, like its sibling cards.Face.HasKeyword
// (Ruling T19-b) — every existing call site already goes through that
// case-insensitive comparison, so an exact-match Engine.HasKeyword would have
// been a silent trap for the first caller with non-canonical-cased input.
func (e *Engine) HasKeyword(id state.ObjID, kw string) bool {
	return e.hasKeywordH(id, kwHeadOf(kw))
}

// hasKeywordH is HasKeyword for a precompiled head (rules/keyword_heads.go).
func (e *Engine) hasKeywordH(id state.ObjID, h kwHead) bool {
	kw := h.s
	if !e.mayHaveDerivedKeywordH(id, h) {
		if derivedMemoVerify {
			e.verifyKeywordPrecheck(id, kw)
		}
		return false
	}
	for _, k := range e.Derived(id).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), kw) {
			return true
		}
	}
	return false
}

// derivedKeywordParam is Face.KeywordParam over the object's CURRENT derived
// keyword list (printed plus layer-6 granted), so a keyword a continuous
// effect delivered (Underworld Breach's AddKeyword$ Escape grant, Snapcaster
// Mage's Flashback) is readable exactly where the printed one would be.
func (e *Engine) derivedKeywordParam(id state.ObjID, head string) (string, bool) {
	return e.derivedKeywordParamH(id, kwHeadOf(head))
}

// derivedKeywordParamH is derivedKeywordParam for a precompiled head.
func (e *Engine) derivedKeywordParamH(id state.ObjID, h kwHead) (string, bool) {
	head := h.s
	if !e.mayHaveDerivedKeywordH(id, h) {
		if derivedMemoVerify {
			e.verifyKeywordPrecheck(id, head)
		}
		return "", false
	}
	for _, k := range e.Derived(id).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), head) {
			if i := strings.IndexByte(k, ':'); i >= 0 {
				return strings.TrimSpace(k[i+1:]), true
			}
			return "", true
		}
	}
	return "", false
}

// ToxicValue is the object's total toxic N (CR 702.164), SUMMED over every
// `Toxic:<N>` entry on its CURRENT derived keyword list -- CR 702.164c makes
// multiple toxic instances cumulative (a printed Toxic 2 Ixhel equipped by
// Prosthetic Injector's AddKeyword$ Toxic:1 is toxic 3), so the read cannot
// stop at the first entry the way the singleton derivedKeywordParam helper
// does. A layer-6 grant (the Rat lord, an Aura, an Equipment) is readable
// exactly where the printed K:Toxic line is, the same derived read
// HasKeyword/derivedKeywordParam give every other keyword. Each entry whose
// parameter is absent or not a positive integer contributes 0 (a non-numeric
// N can only be a malformed script, so failing closed to no poison from that
// entry is the conservative direction); the whole read reports 0 when the
// object has no toxic at all.
func (e *Engine) ToxicValue(id state.ObjID) int {
	total := 0
	for _, k := range e.Derived(id).Keywords {
		if !strings.EqualFold(cardsKeywordHead(k), "Toxic") {
			continue
		}
		raw := ""
		if i := strings.IndexByte(k, ':'); i >= 0 {
			raw = strings.TrimSpace(k[i+1:])
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			continue
		}
		total += n
	}
	return total
}

// derivedTypesOf is Derived(id).Types' content -- the layer-4 result
// typeCharacteristics computes, which is exactly what derivedCompute copies
// into Types -- without the layer-3/5/6/7 walks the rest of Derived runs.
// It keeps derivedCompute's framing (the faceless-object guard and the
// charsWalk.Depth bump a nested read sees), so every read is the same answer.
// Only a read-only caller that tests membership may use it: the result may
// alias the printed face's slice, and its nil-ness is not Derived's (an
// empty list may be nil here, where Derived binds []string{}), so a
// SpecContext ExtraTypes binding must still read Derived.
func (e *Engine) derivedTypesOf(id state.ObjID) []string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	e.charsWalk.Depth++
	ty := e.typeCharacteristics(id, 0)
	e.charsWalk.Depth--
	if derivedMemoVerify {
		if want := e.derivedCompute(id, 0).Types; !slices.Equal(ty, want) {
			panic(fmt.Sprintf("rules: derivedTypesOf(%d) = %v, full walk %v", id, ty, want))
		}
	}
	return ty
}

// IsCreature reads the current layer-derived type list. In particular, a
// planeswalker animated by a layer-4 effect is a creature for damage marking,
// even though its printed face is not.
func (e *Engine) IsCreature(id state.ObjID) bool {
	for _, typ := range e.derivedTypesOf(id) {
		if typ == "Creature" {
			return true
		}
	}
	return false
}

// IsLand reads the current layer-derived type list -- IsCreature's twin for
// the CR 704.5n Fortification legality SBA: a bearer that LOST its Land type
// to a layer-4 static is no longer a legal Fortification bearer, and one that
// gained a type (an animated manland) still is.
func (e *Engine) IsLand(id state.ObjID) bool {
	for _, typ := range e.derivedTypesOf(id) {
		if typ == "Land" {
			return true
		}
	}
	return false
}

// Colors is the object's current layer-5 colour set as WUBRG letters (see
// Derived.Colors); "" is a colourless object. Every rules-side colour read
// about a live object goes through this (objColors below for callers that
// already hold the *state.Object) rather than effects.ColorsOf's face read,
// so an animated manland's granted colours are real everywhere the engine
// consults them -- protection qualities, Fear's black-blocker test, convoke's
// colour contributions, the Count$...$Colors heads.
func (e *Engine) Colors(id state.ObjID) string {
	// A read of an object whose own characteristics are being derived right
	// now (its layer-7 pump expression counting its own colours) is served
	// from the stash derivedWith set before its layer-7 walk — re-entering
	// Derived here would recurse forever. CR 613's layers are ordered: no
	// layer-7 result feeds layer 5, so the stashed answer is the finished
	// layer-5 result, exact rather than an approximation.
	if e.charsWalk.ColorsSet && e.charsWalk.ColorsID == id {
		return e.charsWalk.Colors
	}
	return e.Derived(id).Colors
}

// objColors is Colors for a caller holding the object rather than the id:
// a battlefield permanent reads its derived (layer-5) colours; anything off
// the battlefield has no continuous characteristics (CR 613.6 -- a spell on
// the stack shows its face's colours) and falls back to the face read,
// which also covers LKI snapshots keyed by an id that may no longer resolve.
func (e *Engine) objColors(o *state.Object) string {
	if o != nil && o.Zone == state.ZBattlefield {
		return e.Colors(o.ID)
	}
	return effects.ColorsOf(o)
}

// Keywords exists for Ruling F2: Task 23's view.Chars interface needs a
// Keywords(state.ObjID) []string method, and Engine.Derived already returns
// a Derived struct — a method of the same name on Engine could not satisfy
// an interface expecting a slice. This is that method; Derived(id).Keywords
// remains the field other engine-internal code should read when it also
// wants Power/Toughness/Types in the same call.
func (e *Engine) Keywords(id state.ObjID) []string {
	// The printed fast path (derived_printed.go) answers with the face's own
	// list, which no later derivation rewrites.
	if kw, ok := e.printedKeywordsOnly(id); ok {
		return kw
	}
	return e.Derived(id).Keywords
}
