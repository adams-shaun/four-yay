package rules

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The quiet serve (quiet-seat design
// docs/superpowers/specs/2026-10-08-quiet-seat-walk-skip-design.md, §3.2/§3.3,
// step Q2): priorityOptions serves a proved window's options without running
// the legal-action walk. The proof (rules/quiet_proof.go, seatQuiet) is an
// over-approximation: when it says quiet, the walk would offer nothing beyond
// the mana section's "activate" options, so the serve runs exactly
// legalWalk.manaSection() and legalWalk.postWalkTail() -- the same code the
// walk runs for those parts -- and nothing else. Byte-identity is the
// acceptance test (TestLegalWalkDigest), and quietVerifyOn hosts compare
// every served window with the real walk (§4.1).

// quietOff disables the quiet serve. Until Q2's default flip it follows the
// prio-memo flag pattern inverted: the serve is ON only when
// GORGE_QUIET_SKIP=1 is set; after the flip, GORGE_QUIET_SKIP=0 disables it.
var quietOff = os.Getenv("GORGE_QUIET_SKIP") != "1"

// quietServed counts the windows priorityOptions served from the proof
// instead of the walk. Observation only: it never reaches an event, an
// option, a view or a log.
var quietServed atomic.Uint64

// quietUsable reports whether the engine is at a plain priority ask the
// serve may answer: the shared preconditions of prioMemoUsable
// (rules/prio_memo.go) minus the memo-only items. The recorder-armed pair
// (potentialFullDemand && WalkRecDemand) is required false: on such an
// engine the walk's record feeds PotentialMana's membership lists and the
// decision's potential tail (walk_block_reuse.go), so a recorder-armed
// engine keeps the walk (design §6 Q2 "out of scope"). An inert hold-out is
// deliberately NOT required: the tail filter runs on the served list too.
func (e *Engine) quietUsable() bool {
	return e.hostAsking == 0 && e.cast == nil && !e.Suspended() && e.offStackMana == nil &&
		e.resolvingObj == 0 && !e.applyingReplacement && e.activeDepth == 0 &&
		!e.tape.Watching() &&
		!(e.potentialFullDemand && e.WalkRecDemand)
}

// quietOptions serves p's proved priority window: the walk's mana section
// (the proof's contract holds that nothing else can be offered) and the
// walk's post-walk tail, built the way legalActionsWalkWithWindow builds its
// forAsk result -- into the decision arena's option tail -- so the result
// needs neither the scratch copy nor the scratch's clear. Every per-walk side
// effect the walk makes is mirrored or skipped with a justification:
//
//   - legalActionWalks is NOT incremented: it counts legalActionsPriced
//     calls (engine_scratch.go), and a served window runs none; the
//     test-visible diagnostics that read it (payment_plan,
//     potential_walk_cache) must see a serve as zero walks.
//   - derived memo scope: mirrored (beginDerivedMemo/endDerivedMemo), so the
//     mana section memoizes its Derived reads exactly as the walk's do.
//   - scratch/arena buffers: mirrored (legalOptBuf borrow, arena tail).
//   - walkReuse: mirrored -- the walk discards an armed record when hyp is
//     nil, so the serve discards it too; the serve never installs one
//     (hyp is always nil) and never records (quietUsable required the
//     recorder unarmed, so w.rec stays nil and recordManaSection/ownManaMembers
//     record nothing).
//   - outHW: mirrored -- walkHW is the same max(len(out), outHW) read the
//     walk passes to the arena tail's commit.
func (e *Engine) quietOptions(p state.PlayerID) []decision.Option {
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	out := e.legalOptBuf[:0]
	e.legalOptBuf = nil
	var tail optTail
	var scratch []decision.Option
	if tail = e.arenaOptionsTail(); tail.buf != nil {
		scratch, out = out, tail.buf
	}
	w := &legalWalk{
		e:             e,
		p:             p,
		hyp:           nil,
		castsOnly:     false,
		sorcery:       e.sorcerySpeed(p),
		out:           out,
		costStatics:   costStaticSource{e: e},
		actionStatics: actionStaticSource{e: e},
	}
	// The walk's record-reuse consumption, mirrored (legal.go): an armed
	// record is discarded, and -- hyp nil -- nothing is installed from it.
	if e.walkReuse != nil {
		e.walkReuse = nil
	}
	_ = w.manaSection()
	out = w.out
	walkHW := max(len(out), w.outHW)
	res := w.postWalkTail(out, walkHW, tail, scratch, nil, true, false, nil)
	if quietVerifyOn() {
		e.quietServeCheck(p, res)
	}
	return res
}

// quietServeCheck is the serve's verify arm (design §4.1): under
// quietVerifyOn, every served window also runs the real priority walk and
// the whole option list is compared field by field (optionEqual), Index
// included. On a mismatch it panics with the §4.1 message, naming the first
// option the two lists disagree on.
func (e *Engine) quietServeCheck(p state.PlayerID, served []decision.Option) {
	walk := e.legalActionsWithWindow(p, nil)
	if len(served) == len(walk) {
		same := true
		for i := range served {
			if !optionEqual(&served[i], &walk[i]) {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	// Name the first disagreement: the walk's option at the first index the
	// two lists differ on (or either list's last option when one is longer).
	name := func(o *decision.Option) string {
		n := ""
		if o.Obj != 0 {
			if obj := e.G.Obj(o.Obj); obj != nil && obj.Face() != nil {
				n = obj.Face().Name
			}
		}
		return n
	}
	var wOpt, sOpt decision.Option
	switch {
	case len(walk) <= len(served):
		wOpt, sOpt = walk[len(walk)-1], served[len(walk)-1]
	default:
		wOpt, sOpt = walk[len(served)-1], served[len(served)-1]
	}
	for i := 0; i < len(walk) && i < len(served); i++ {
		if !optionEqual(&served[i], &walk[i]) {
			wOpt, sOpt = walk[i], served[i]
			break
		}
	}
	panic(fmt.Sprintf("rules: quiet proof wrong for seat %d (turn %d step %v stack %d): walk offered %s %q obj %d (%s), served %s %q obj %d",
		p, e.G.Turn, e.G.Step, len(e.G.Stack), wOpt.Kind, wOpt.Label, wOpt.Obj, name(&wOpt), sOpt.Kind, sOpt.Label, sOpt.Obj))
}

// quietServedCount reports the process-wide serve counter (test and stats).
func quietServedCount() uint64 { return quietServed.Load() }
