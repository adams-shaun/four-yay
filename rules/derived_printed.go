package rules

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

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
// no layer-6/7/CantHaveKeywords entry of active() CAN match the object,
// nothing applies in either walk, whatever the bindings, and the result is
// the base keyword list and printed P/T plus counters. The two remaining
// inputs are excluded explicitly: a face-down battlefield object (CR 708.5's
// synthetic 2/2 basis) and a face carrying a characteristic-defining static
// (cdaSetPT evaluates an arbitrary amount) take the full derivation.
//
// "Cannot match" is decided by a gate per entry (charsGate): a set of
// NECESSARY conditions read off the entry's Affected$ spec when that spec is
// one plain conjunction -- a single alternative `Base.P1+P2+...` with no
// comma, EACH list, space, negation, comparison or cast-provenance token --
// so the filter admits the object only if every predicate holds. The gate
// keeps only predicates whose meaning is a plain field test, mirrored
// exactly from the filter (effects/filter.go's predicate table and
// numeric counters_ form): Self (the object is the source), Other (it is
// not), YouCtrl / OppCtrl (its controller is / is not the effect's
// controller, the match's You), EquippedBy / EnchantedBy (the source is on
// the battlefield attached to it) and counters_GE<n>_<KIND> with a literal
// n (o.Counter(KIND) >= n). One failed condition proves the match false.
// Every other predicate, and any spec outside the plain shape, is treated
// as satisfiable. The exact `Card.Self` spec is the gate {Self}: the same
// test matchesWithCharsPT's Card.Self early-out applies.
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

// charsSummary is a per-build digest of active() for the fast path: one gate
// per P/T- or keyword-bearing entry (layer 6, layer 7, or a
// CantHaveKeywords$ prohibition), and whether some layer-3 SetName$ entry
// exists. It is keyed like activeSummary: active()'s build count plus the
// list's backing pointer and length.
type charsSummary struct {
	valid bool
	seq   uint64
	base  *ContinuousEffect
	n     int
	gates []charsGate
	// open / ptOpen: some gate (some layer-7 gate) carries no condition at
	// all, so it may reach every object and the fast path never applies.
	open, ptOpen bool
	// renames: some entry is a layer-3 SetName$ effect, so a derived name
	// may differ from the printed one (ViewCharacteristics' fast path).
	renames bool
}

// charsGate is one entry's necessary match conditions (see above).
type charsGate struct {
	src  state.ObjID
	ctrl state.PlayerID
	// pt: a layer-7 entry, the only kind that can move P/T.
	pt bool
	gateSpec
}

// gateSpec is the src/controller-independent part of a gate: a pure
// function of the Affected$ text, memoised per spec (gateSpecFor).
type gateSpec struct {
	open                            bool // no usable condition
	self, other, you, opp, attached bool
	ctrKind                         string
	ctrMin                          int32
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
			var fresh charsSummary
			fresh.summarize(ces)
			if !slices.Equal(fresh.gates, s.gates) || fresh.open != s.open || fresh.ptOpen != s.ptOpen || fresh.renames != s.renames {
				panic(fmt.Sprintf("rules: chars summary at build %d disagrees with a rescan", s.seq))
			}
		}
		return s
	}
	s.summarize(ces)
	s.valid, s.seq, s.base, s.n = true, e.activeBuildSeq, base, len(ces)
	return s
}

func (s *charsSummary) summarize(ces []ContinuousEffect) {
	s.gates, s.open, s.ptOpen, s.renames = s.gates[:0], false, false, false
	for i := range ces {
		ce := &ces[i]
		if ce.Layer == LText && ce.SetName != "" {
			s.renames = true
		}
		if ce.Layer != LAbilities && ce.Layer != LPT && len(ce.CantHaveKeywords) == 0 {
			continue
		}
		g := charsGate{src: ce.Source, ctrl: ce.Controller, pt: ce.Layer == LPT, gateSpec: gateSpecFor(ce.Affects)}
		if g.open {
			s.open = true
			if g.pt {
				s.ptOpen = true
			}
		}
		s.gates = append(s.gates, g)
	}
}

// mayReach reports whether the gate's conditions all hold for o, i.e. the
// entry's match is not proven false.
func (g *charsGate) mayReach(game *state.Game, o *state.Object) bool {
	if g.open {
		return true
	}
	if (g.self && o.ID != g.src) || (g.other && o.ID == g.src) ||
		(g.you && o.Controller != g.ctrl) || (g.opp && o.Controller == g.ctrl) {
		return false
	}
	if g.attached {
		if s := game.Obj(g.src); s == nil || s.AttachedTo != o.ID || s.Zone != state.ZBattlefield {
			return false
		}
	}
	if g.ctrKind != "" && o.Counter(g.ctrKind) < g.ctrMin {
		return false
	}
	return true
}

// reached reports whether some gate (some layer-7 gate, with ptOnly) may
// reach o.
func (s *charsSummary) reached(game *state.Game, o *state.Object, ptOnly bool) bool {
	if s.ptOpen || (s.open && !ptOnly) {
		return true
	}
	for i := range s.gates {
		if g := &s.gates[i]; (g.pt || !ptOnly) && g.mayReach(game, o) {
			return true
		}
	}
	return false
}

// gateSpecMemo caches gateSpecParse per spec text: a direct-mapped table of
// immutable entries, shared across engines (specLocalMemo's shape).
var gateSpecMemo [256]atomic.Pointer[gateSpecEntry]

type gateSpecEntry struct {
	spec string
	g    gateSpec
}

func gateSpecFor(spec string) gateSpec {
	h := uint32(2166136261)
	for i := 0; i < len(spec); i++ {
		h = (h ^ uint32(spec[i])) * 16777619
	}
	slot := &gateSpecMemo[h%uint32(len(gateSpecMemo))]
	if en := slot.Load(); en != nil && en.spec == spec {
		return en.g
	}
	g := gateSpecParse(spec)
	slot.Store(&gateSpecEntry{spec: spec, g: g})
	return g
}

// gateSpecParse reads the necessary conditions of a plain conjunctive spec
// (see the file comment); anything else is open.
func gateSpecParse(spec string) gateSpec {
	open := gateSpec{open: true}
	if spec == "" || strings.ContainsAny(spec, ", &!<>=()") || strings.Contains(spec, "wasCast") ||
		strings.Contains(spec, "IsTargeting") {
		return open
	}
	base, preds, _ := strings.Cut(spec, ".")
	if !letterWord(base) || base == "CARDNAME" {
		return open
	}
	var g gateSpec
	any := false
	for p := range strings.SplitSeq(preds, "+") {
		switch p {
		case "Self":
			g.self, any = true, true
		case "Other":
			g.other, any = true, true
		case "YouCtrl":
			g.you, any = true, true
		case "OppCtrl":
			g.opp, any = true, true
		case "EquippedBy", "EnchantedBy":
			g.attached, any = true, true
		default:
			rest, ok := strings.CutPrefix(p, "counters_GE")
			if !ok || g.ctrKind != "" {
				continue
			}
			num, kind, ok := strings.Cut(rest, "_")
			if !ok || kind == "" || !literalAmount(num) || num[0] == '+' || num[0] == '-' {
				continue
			}
			n, err := strconv.Atoi(num)
			if err != nil || n <= 0 {
				continue
			}
			g.ctrKind, g.ctrMin, any = kind, int32(n), true
		}
	}
	if !any {
		return open
	}
	return g
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
	if e.charsSummaryOf(e.activeBuf).reached(e.G, o, false) || faceHasCDAStatic(o) {
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

// printedPT is derivedScalar's fast path: when no layer-7 entry of active
// (which must be active()'s current list) can match o, its P/T is the
// printed pair plus the 7d counter totals -- the layer-4/6 bindings a layer-7 match
// would read never matter. The same exclusions as printedReach apply.
func (e *Engine) printedPT(o *state.Object, f *cards.Face, active []ContinuousEffect) (power, toughness int32, ok bool) {
	if !e.derivedMemoUsable() || (o.FaceDown && o.Zone == state.ZBattlefield) {
		return 0, 0, false
	}
	if e.charsSummaryOf(active).reached(e.G, o, true) || faceHasCDAStatic(o) {
		return 0, 0, false
	}
	dp, dt := o.CounterPTTotals()
	power, toughness = int32(f.Power())+dp, int32(f.Toughness())+dt
	if printedCharsVerify {
		if want := e.derivedCompute(o.ID, 0); want.Power != power || want.Toughness != toughness {
			msg := fmt.Sprintf("rules: printed P/T for obj %d: fast %d/%d, full %d/%d", o.ID, power, toughness, want.Power, want.Toughness)
			fmt.Fprintln(os.Stderr, msg)
			panic(msg)
		}
	}
	return power, toughness, true
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
