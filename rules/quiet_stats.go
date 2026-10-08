package rules

import (
	"fmt"
	"sync/atomic"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// quietStatsFlag is a link-time switch for the quiet-seat counters:
//
//	go build -ldflags "-X github.com/adams-shaun/gorge/rules.quietStatsFlag=1"
//
// When it is empty AND verify mode is off, the observer in priorityOptions
// does not run at all, so the default build pays one predictable branch.
var quietStatsFlag string

// The quiet-seat counters. Every one is process-wide and atomic; the proof
// itself is a pure read, and the counters are observation only -- they never
// reach an event, an option, a view or a log.
var (
	quietWindows        atomic.Uint64
	quietProved         atomic.Uint64
	quietProvedPassOnly atomic.Uint64
	quietProvedBareMana atomic.Uint64
	quietUnprovedPass   atomic.Uint64
	quietSoleBlocker    [qbCount]atomic.Uint64
	quietProofNs        atomic.Uint64
	quietWalkNs         atomic.Uint64
	quietMismatches     atomic.Uint64
)

// QuietStatsSnapshot is a consistent-enough read of the quiet counters for
// printing. Counts are read independently, so a concurrent run may see a
// window counted in one and not the other; the callers (enginebench rows) run
// single-threaded.
type QuietStatsSnapshot struct {
	Windows        uint64
	Proved         uint64
	ProvedPassOnly uint64
	ProvedBareMana uint64
	UnprovedPass   uint64
	SoleBlocker    [qbCount]uint64
	ProofNs        uint64
	WalkNs         uint64
	Mismatches     uint64
}

// QuietStats returns a snapshot of the quiet-seat counters.
func QuietStats() QuietStatsSnapshot {
	var s QuietStatsSnapshot
	s.Windows = quietWindows.Load()
	s.Proved = quietProved.Load()
	s.ProvedPassOnly = quietProvedPassOnly.Load()
	s.ProvedBareMana = quietProvedBareMana.Load()
	s.UnprovedPass = quietUnprovedPass.Load()
	for i := range s.SoleBlocker {
		s.SoleBlocker[i] = quietSoleBlocker[i].Load()
	}
	s.ProofNs = quietProofNs.Load()
	s.WalkNs = quietWalkNs.Load()
	s.Mismatches = quietMismatches.Load()
	return s
}

// quietClock is the nanosecond clock the quiet stats use, or nil when no
// host binary registered one. rules may not import time (archtest
// TestTimeIsImportedOnlyByTheHost), so the clock is injected by the caller
// (cmd/enginebench -quietstats); with no clock the proof counters still
// count but report zero ns.
var quietClock func() int64

// SetQuietClock registers the proof-path nanosecond clock. It must be called
// before the engine runs (once at process start).
func SetQuietClock(f func() int64) { quietClock = f }

// quietNow returns the current clock reading, or 0 with no clock registered.
func quietNow() int64 {
	if quietClock == nil {
		return 0
	}
	return quietClock()
}
func quietStatsEnabled() bool {
	return quietVerify || quietStatsFlag != ""
}

// QuietStatsLinked reports whether the link-time stats flag was set, so a
// caller (enginebench -quietstats) can error clearly instead of printing an
// all-zero table.
func QuietStatsLinked() bool { return quietStatsFlag != "" }

// quietObserve is the verify/stats arm of priorityOptions. It runs the proof
// alongside the normal path (which already produced opts -- the walk), and
// tallies the outcome. It never changes opts.
//
// It is a pure read apart from the counters and, in verify mode, a panic on
// a proof that says quiet while the walk offered a non-mana option. The two
// time reads are observation only: they feed the counters, never an event,
// option, view or log, so determinism is untouched (rules' determinism rule
// bans a clock whose value can reach an event).
func (e *Engine) quietObserve(p state.PlayerID, opts []decision.Option, totalNs uint64) {
	quietWindows.Add(1)
	var proofNs uint64
	if quietClock != nil {
		t0 := quietClock()
		blocker := e.quietBlocker(p)
		proofNs = uint64(quietClock() - t0)
		quietProofNs.Add(proofNs)
		if totalNs > proofNs {
			quietWalkNs.Add(totalNs - proofNs)
		}
		e.quietTally(p, opts, blocker)
		return
	}
	e.quietTally(p, opts, e.quietBlocker(p))
}

// quietTally records one observed window's outcome and, in verify mode,
// panics when the proof says quiet while the walk offered a non-mana option.
// It is the shared arm of quietObserve for both clock and no-clock hosts.
func (e *Engine) quietTally(p state.PlayerID, opts []decision.Option, blocker quietBlockerID) {
	nonMana := false
	anyActivate := false
	for i := range opts {
		switch opts[i].Kind {
		case "activate":
			anyActivate = true
		case "pass", "concede":
		default:
			nonMana = true
		}
	}
	passOnly := !nonMana && !anyActivate

	if blocker == qbNone {
		quietProved.Add(1)
		if passOnly {
			quietProvedPassOnly.Add(1)
		} else if !nonMana {
			quietProvedBareMana.Add(1)
		}
		if nonMana {
			quietMismatches.Add(1)
			if quietVerify {
				o := opts[firstNonManaOption(opts)]
				name := ""
				if obj := e.G.Obj(o.Obj); obj != nil && obj.Face() != nil {
					name = obj.Face().Name
				}
				panic(fmt.Sprintf("rules: quiet proof wrong for seat %d (turn %d step %v stack %d): walk offered %s %q obj %d (%s)",
					p, e.G.Turn, e.G.Step, len(e.G.Stack), o.Kind, o.Label, o.Obj, name))
			}
		}
	} else {
		if passOnly {
			quietUnprovedPass.Add(1)
			quietSoleBlocker[blocker].Add(1)
		}
	}
}

// firstNonManaOption returns the index of the first option that is neither an
// activate nor pass/concede. Callers have already seen one.
func firstNonManaOption(opts []decision.Option) int {
	for i := range opts {
		switch opts[i].Kind {
		case "activate", "pass", "concede":
		default:
			return i
		}
	}
	return 0
}

// QuietStatsPrint renders the -quietstats table.
func QuietStatsPrint(s QuietStatsSnapshot) string {
	pct := func(n, d uint64) float64 {
		if d == 0 {
			return 0
		}
		return float64(n) * 100 / float64(d)
	}
	out := fmt.Sprintf("quietstats: windows %d; proved %d (%.1f%%), pass-only proved %d (%.1f%% of proved), bare-mana proved %d (%.1f%% of proved)\n",
		s.Windows, s.Proved, pct(s.Proved, s.Windows), s.ProvedPassOnly, pct(s.ProvedPassOnly, s.Proved),
		s.ProvedBareMana, pct(s.ProvedBareMana, s.Proved))
	out += fmt.Sprintf("quietstats: pass-only windows %d; proved %d (%.1f%% of pass-only); unproved %d\n",
		s.ProvedPassOnly+s.UnprovedPass, s.ProvedPassOnly, pct(s.ProvedPassOnly, s.ProvedPassOnly+s.UnprovedPass), s.UnprovedPass)
	var meanProof, meanWalk float64
	if s.Windows > 0 {
		meanProof = float64(s.ProofNs) / float64(s.Windows)
		meanWalk = float64(s.WalkNs) / float64(s.Windows)
	}
	out += fmt.Sprintf("quietstats: proof %.0f ns/window; walk %.0f ns/window; mismatches %d\n", meanProof, meanWalk, s.Mismatches)
	out += "quietstats: unproved pass-only by first blocker:\n"
	for i := 0; i < int(qbCount); i++ {
		if s.SoleBlocker[i] == 0 {
			continue
		}
		out += fmt.Sprintf("quietstats:   %-34s %8d %6.1f%%\n", quietBlockerNames[i], s.SoleBlocker[i], pct(s.SoleBlocker[i], s.UnprovedPass))
	}
	return out
}
