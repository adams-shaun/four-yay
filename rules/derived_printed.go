package rules

import (
	"fmt"
	"os"
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Printed characteristics: Characteristics' fast path.
//
// botpolicy's board build asks Characteristics (power, toughness, keywords)
// for every battlefield permanent and every card in the deciding seat's hand,
// graveyard and command zone, on every decision the bot or a searcher's
// rollout answers -- and almost none of those objects is touched by any
// continuous effect. Measured on the search bench (az leg): of 102M
// Characteristics calls, 70M were battlefield and 32M hand/graveyard, and
// fewer than one effect in the P/T or keyword layers per off-battlefield call
// was even present.
//
// The full derivation (derivedCompute) produces those three facts from:
//
//   - the base keyword list (derivedBaseKeywords: printed, intrinsic,
//     marker-counter and status keywords), then every LAYER-6 effect and
//     every CantHaveKeywords$ prohibition that matches the object;
//   - the printed P/T, overridden by the face's own characteristic-defining
//     P/T (cdaSetPT), then every LAYER-7 effect that matches, then the 7d
//     counter totals.
//
// Layers 3, 4 and 5 (name/text, types, colours) feed those two outputs only
// through the Affected$ bindings of a layer-6/7 effect that is evaluated
// against the object -- they never write P/T or keywords themselves. So when
// every layer-6/7/CantHaveKeywords entry of active() is an exact `Card.Self`
// effect whose source is ANOTHER object, no match can admit the object:
// matchesWithCharsPT rejects that shape before any binding is read (the
// Card.Self early-out, held to the full match by layer4PrecheckVerify).
// Nothing then applies in either walk, whatever the bindings, and the result
// is the base keyword list and printed P/T plus counters. The two remaining
// inputs are excluded explicitly: a face-down battlefield object (CR 708.5's
// synthetic 2/2 basis) and a face carrying a characteristic-defining static
// (cdaSetPT evaluates an arbitrary amount) take the full derivation.
//
// The fast path runs only on a top-level read (no derivation, active() build
// or memo-bypassing probe in progress: derivedMemoUsable) and only when active() is already
// exactly current, so it never builds active() at a moment the full path
// would not have -- it reads the same list the full path would read, and
// writes nothing but the derivedKW scratch the full path rewrites too.
//
// printedCharsVerify (the rules test binary, or derivedMemoVerifyFlag at
// link time for a botbench run) recomputes every fast answer through
// derivedCompute and panics on any difference.

// printedCharsVerify: see derivedMemoVerify.
var printedCharsVerify = derivedMemoVerifyFlag != ""

// charsSummary is a per-build digest of active() for the fast path: whether
// any P/T- or keyword-bearing entry (layer 6, layer 7, or a CantHaveKeywords$
// prohibition) is anything but an exact Card.Self effect, and the sources of
// the exact Card.Self ones. It is keyed like activeSummary: active()'s build
// count plus the list's backing pointer and length.
type charsSummary struct {
	valid   bool
	seq     uint64
	base    *ContinuousEffect
	n       int
	nonSelf bool
	selfSrc []state.ObjID
	// renames: some entry is a layer-3 SetName$ effect, so a derived name
	// may differ from the printed one (ViewCharacteristics' fast path).
	renames bool
}

// charsSummaryOf returns the digest of ces, which must be active()'s
// current list.
func (e *Engine) charsSummaryOf(ces []ContinuousEffect) *charsSummary {
	var base *ContinuousEffect
	if len(ces) > 0 {
		base = &ces[0]
	}
	s := &e.charsSum
	if s.valid && s.seq == e.activeBuildSeq && s.base == base && s.n == len(ces) {
		if printedCharsVerify {
			nonSelf, src, renames := summarizeChars(ces, nil)
			if nonSelf != s.nonSelf || !slices.Equal(src, s.selfSrc) || renames != s.renames {
				panic(fmt.Sprintf("rules: chars summary at build %d disagrees with a rescan", s.seq))
			}
		}
		return s
	}
	s.nonSelf, s.selfSrc, s.renames = summarizeChars(ces, s.selfSrc[:0])
	s.valid, s.seq, s.base, s.n = true, e.activeBuildSeq, base, len(ces)
	return s
}

func summarizeChars(ces []ContinuousEffect, src []state.ObjID) (nonSelf bool, _ []state.ObjID, renames bool) {
	for i := range ces {
		ce := &ces[i]
		if ce.Layer == LText && ce.SetName != "" {
			renames = true
		}
		if nonSelf || (ce.Layer != LAbilities && ce.Layer != LPT && len(ce.CantHaveKeywords) == 0) {
			continue
		}
		if ce.Affects != "Card.Self" {
			nonSelf = true
			continue
		}
		src = append(src, ce.Source)
	}
	if nonSelf {
		src = src[:0]
	}
	return nonSelf, src, renames
}

// printedCharacteristics answers Characteristics without the layer walk when
// the argument above proves no continuous effect can reach id; ok is false
// whenever the full derivation must run instead.
//
// The keyword list is the face's own printed list (capped at its length, so
// a reader's append can never reach the shared face) whenever the object
// carries no intrinsic, marker-counter or status keyword -- the base list is
// then exactly the printed one -- and is never written to engine scratch, so
// it stays valid across any later derivation. Otherwise, with scratchOK, it
// is built into the derivedKW scratch exactly as derivedCompute builds it
// (Characteristics' documented aliasing); without scratchOK the call
// declines.
func (e *Engine) printedCharacteristics(id state.ObjID, scratchOK bool) (power, toughness int32, keywords []string, ok bool) {
	o, f := e.printedReach(id)
	if f == nil {
		return 0, 0, nil, false
	}
	kw, own := printedKeywords(o, f)
	if !own {
		if !scratchOK {
			return 0, 0, nil, false
		}
		kw = derivedBaseKeywords(e.derivedKW, o, f, false)
		if kw == nil {
			kw = []string{}
		}
		e.derivedKW = kw
	}
	dp, dt := o.CounterPTTotals()
	power, toughness = int32(f.Power())+dp, int32(f.Toughness())+dt
	if printedCharsVerify {
		got := append([]string{}, kw...)
		e.verifyPrintedChars(id, power, toughness, got, "")
		if !own {
			// The recompute rewrote the scratch; rebuild the answer into it.
			kw = derivedBaseKeywords(e.derivedKW, o, f, false)
			if kw == nil {
				kw = []string{}
			}
			e.derivedKW = kw
		}
	}
	return power, toughness, kw, true
}

// printedReach returns id's object and face when the printed fast path may
// answer for it (see above), else a nil face.
func (e *Engine) printedReach(id state.ObjID) (*state.Object, *cards.Face) {
	if !e.derivedMemoUsable() || e.activeEpoch != len(e.L.Events) || e.activeVersion != e.continuousVersion || e.activeStaticSeq != e.staticBuildSeq {
		return nil, nil
	}
	o := e.G.Obj(id)
	if o == nil {
		return nil, nil
	}
	f := o.Face()
	if f == nil || (o.FaceDown && o.Zone == state.ZBattlefield) {
		return nil, nil
	}
	s := e.charsSummaryOf(e.activeBuf)
	if s.nonSelf || slices.Contains(s.selfSrc, id) || faceHasCDAStatic(o) {
		return nil, nil
	}
	return o, f
}

// printedKeywords is derivedBaseKeywords for an object that adds nothing to
// its face's printed list: that list itself, capped, or a non-nil empty list
// (the derivation's bound empty answer). own is false when the object
// carries an intrinsic, marker-counter or status keyword (a face-down cloak
// never reaches here).
func printedKeywords(o *state.Object, f *cards.Face) (kw []string, own bool) {
	if len(o.IntrinsicKeywords) != 0 || o.Suspected || o.SuspendGranted {
		return nil, false
	}
	for i := range o.Counters {
		if c := &o.Counters[i]; c.N > 0 {
			if _, isKW := cards.CounterKeyword(c.Kind); isKW {
				return nil, false
			}
		}
	}
	if n := len(f.Keywords); n > 0 {
		return f.Keywords[:n:n], true
	}
	return []string{}, true
}

// printedKeywordsOnly is Keywords' fast path: the printed answer when it
// needs no scratch.
func (e *Engine) printedKeywordsOnly(id state.ObjID) ([]string, bool) {
	o, f := e.printedReach(id)
	if f == nil {
		return nil, false
	}
	kw, own := printedKeywords(o, f)
	if own && printedCharsVerify {
		dp, dt := o.CounterPTTotals()
		e.verifyPrintedChars(id, int32(f.Power())+dp, int32(f.Toughness())+dt, kw, "")
	}
	return kw, own
}

// printedViewCharacteristics is ViewCharacteristics' fast path: the printed
// answer when it needs no scratch and no layer-3 SetName$ effect exists (the
// derived name is then the face's).
func (e *Engine) printedViewCharacteristics(id state.ObjID) (name string, keywords []string, power, toughness int32, ok bool) {
	o, f := e.printedReach(id)
	if f == nil || e.charsSum.renames {
		return "", nil, 0, 0, false
	}
	kw, own := printedKeywords(o, f)
	if !own {
		return "", nil, 0, 0, false
	}
	dp, dt := o.CounterPTTotals()
	power, toughness = int32(f.Power())+dp, int32(f.Toughness())+dt
	if printedCharsVerify {
		e.verifyPrintedChars(id, power, toughness, kw, f.Name)
	}
	return f.Name, kw, power, toughness, true
}

func (e *Engine) verifyPrintedChars(id state.ObjID, power, toughness int32, got []string, name string) {
	want := e.derivedCompute(id, 0)
	if want.Power != power || want.Toughness != toughness || !slices.Equal(want.Keywords, got) || want.Keywords == nil ||
		(name != "" && want.Name != name) {
		msg := fmt.Sprintf("rules: printed characteristics for obj %d: fast %d/%d %q %q, full %d/%d %q %q",
			id, power, toughness, got, name, want.Power, want.Toughness, want.Keywords, want.Name)
		// A searcher recovers a simulation's panic into a discarded world, so
		// say it on stderr too: a verify run must not pass silently.
		fmt.Fprintln(os.Stderr, msg)
		panic(msg)
	}
}
