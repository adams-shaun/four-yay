package resolve

import "sync/atomic"

// Stats are process-wide observation counters: never game state, never read
// by the kernel's decisions.
type Stats struct {
	Checkpoints  int64 // resolution-starting passes that took S0
	NoAsk        int64 // ... whose resolution finished without a tape ask (S0 dropped)
	LegacySwitch int64 // ... that met a legacy ask first and went legacy in place
	Posed        int64 // stop-asks posed with the tape exhausted (unwinds)
	Reruns       int64 // re-executions from S0
	Served       int64 // answers served from the tape across all re-runs
	Aborts       int64 // re-runs that met a legacy ask: legacy replay fallback
	PrefixEvents int64 // recorded-prefix events re-executed and verified
	MaxK         int64 // the most answers one re-run served
	Exempt       int64 // resolution-starting passes run without S0 (predicate or answerer)
	Misses       int64 // ... whose resolution met a legacy ask anyway (fallback taken)
	Inline       int64 // converted asks answered by the synchronous answerer
}

// Sub is s - o, field by field: the counters one run of work moved.
func (s Stats) Sub(o Stats) Stats {
	return Stats{
		Checkpoints: s.Checkpoints - o.Checkpoints, NoAsk: s.NoAsk - o.NoAsk,
		LegacySwitch: s.LegacySwitch - o.LegacySwitch, Posed: s.Posed - o.Posed,
		Reruns: s.Reruns - o.Reruns, Served: s.Served - o.Served, Aborts: s.Aborts - o.Aborts,
		PrefixEvents: s.PrefixEvents - o.PrefixEvents, MaxK: s.MaxK,
		Exempt: s.Exempt - o.Exempt, Misses: s.Misses - o.Misses, Inline: s.Inline - o.Inline,
	}
}

type statsT struct {
	checkpoints, noAsk, legacySwitch, posed, reruns, served, aborts,
	prefixEvents, maxK, exempt, misses, inline atomic.Int64
}

var stats statsT

// noteK records a re-run that served k answers.
func (s *statsT) noteK(k int64) {
	for {
		cur := s.maxK.Load()
		if k <= cur || s.maxK.CompareAndSwap(cur, k) {
			return
		}
	}
}

// ReadStats snapshots the process-wide kernel counters.
func ReadStats() Stats {
	return Stats{
		Checkpoints: stats.checkpoints.Load(), NoAsk: stats.noAsk.Load(),
		LegacySwitch: stats.legacySwitch.Load(), Posed: stats.posed.Load(),
		Reruns: stats.reruns.Load(), Served: stats.served.Load(), Aborts: stats.aborts.Load(),
		PrefixEvents: stats.prefixEvents.Load(), MaxK: stats.maxK.Load(),
		Exempt: stats.exempt.Load(), Misses: stats.misses.Load(), Inline: stats.inline.Load(),
	}
}
