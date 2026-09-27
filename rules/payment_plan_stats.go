package rules

import (
	"fmt"
	"io"
	"sort"
)

// PaymentPlanStats is the engine-side auto-pay diagnostics sink (spec §3.2
// "eligibility and fallback diagnostics must explain which of these cases
// occurred", §5 Diagnostics). A tool or test attaches one with
// SetPaymentPlanStats and reads the plain counters afterwards; the zero value
// is ready to use.
//
// It is an observer only: recording emits no event, draws no RNG and never
// changes an offer, so an engine with a sink attached produces the same
// events, chain head and PaymentActions as one without. The reasons and
// details it holds can reveal the acting seat's hand (spec §7), so it stays
// engine/tool side and is never put on the wire to a seat.
//
// Counting rules:
//
//   - DecisionsBuilt counts runs of the offer builder
//     (EnsurePaymentActions builds once per priority decision and caches the
//     result; each PaymentActionsForPriority call is a fresh build). A build
//     on a finished game is not counted.
//   - PoolDeclined counts builds the builder declined wholesale because the
//     player's floating pool holds mana the planner cannot account for
//     (paymentPlanPoolOK: snow, persistent, restricted or unit-tagged mana).
//     Such a build walks no candidate, so it adds nothing to Candidates,
//     ByReason or ByDetail: it is counted once per build, here only.
//   - Candidates is the number of ordinary plain casts the builder planned;
//     each lands in exactly one ByReason bucket. ByReason is the planner's
//     Reason with "" (a complete plan, no limit) spelled "ready";
//     "search_limit" covers both a limit hit that still returned a best plan
//     (which is offered) and one that found none.
//   - ByDetail counts each non-empty Detail (the shape:, cost:, source: and
//     global-effect vocabulary) independently of its Reason.
//   - Nodes and MaxNodes are the planner's own PaymentPlanOutcome.Nodes, summed
//     and maximised over candidates; an outcome declined before the search
//     (unsupported) reports 0 nodes. SearchLimitHits counts candidates whose
//     Reason is "search_limit".
//   - ActionsOffered and PlansOffered count what the builder returned (V1:
//     one plan per action, so the two are equal today).
//   - PlannedSubmissions counts accepted Submits that carried a payment
//     selection; Fallbacks counts paymentPlanFallback by its spec §6 reason.
type PaymentPlanStats struct {
	DecisionsBuilt     int
	PoolDeclined       int
	Candidates         int
	ActionsOffered     int
	PlansOffered       int
	ByReason           map[string]int
	ByDetail           map[string]int
	Nodes              int
	MaxNodes           int
	SearchLimitHits    int
	PlannedSubmissions int
	Fallbacks          map[string]int
}

// SetPaymentPlanStats attaches s as this engine's auto-pay diagnostics sink;
// nil (the default) detaches it and costs nothing. The sink belongs to this
// engine alone: Clone does not carry it, and it must not be shared between
// engines driven on different goroutines.
func (e *Engine) SetPaymentPlanStats(s *PaymentPlanStats) { e.paymentStats = s }

// PaymentPlanStats returns the attached sink, or nil.
func (e *Engine) PaymentPlanStats() *PaymentPlanStats { return e.paymentStats }

func bumpPaymentStat(m *map[string]int, key string, n int) {
	if *m == nil {
		*m = map[string]int{}
	}
	(*m)[key] += n
}

func (s *PaymentPlanStats) recordBuild(poolDeclined bool) {
	if s == nil {
		return
	}
	s.DecisionsBuilt++
	if poolDeclined {
		s.PoolDeclined++
	}
}

func (s *PaymentPlanStats) recordOutcome(got PaymentPlanOutcome) {
	if s == nil {
		return
	}
	s.Candidates++
	reason := got.Reason
	if reason == "" {
		reason = "ready"
	}
	bumpPaymentStat(&s.ByReason, reason, 1)
	if got.Detail != "" {
		bumpPaymentStat(&s.ByDetail, got.Detail, 1)
	}
	s.Nodes += got.Nodes
	if got.Nodes > s.MaxNodes {
		s.MaxNodes = got.Nodes
	}
	if got.Reason == "search_limit" {
		s.SearchLimitHits++
	}
}

func (s *PaymentPlanStats) recordOffered(plans int) {
	if s == nil {
		return
	}
	s.ActionsOffered++
	s.PlansOffered += plans
}

func (s *PaymentPlanStats) recordPlannedSubmission() {
	if s == nil {
		return
	}
	s.PlannedSubmissions++
}

func (s *PaymentPlanStats) recordFallback(reason string) {
	if s == nil {
		return
	}
	bumpPaymentStat(&s.Fallbacks, reason, 1)
}

// Merge adds o's counters into s (MaxNodes takes the larger). A nil o is a
// no-op. It is how a harness aggregates the per-engine sinks of many games.
func (s *PaymentPlanStats) Merge(o *PaymentPlanStats) {
	if s == nil || o == nil {
		return
	}
	s.DecisionsBuilt += o.DecisionsBuilt
	s.PoolDeclined += o.PoolDeclined
	s.Candidates += o.Candidates
	s.ActionsOffered += o.ActionsOffered
	s.PlansOffered += o.PlansOffered
	s.Nodes += o.Nodes
	if o.MaxNodes > s.MaxNodes {
		s.MaxNodes = o.MaxNodes
	}
	s.SearchLimitHits += o.SearchLimitHits
	s.PlannedSubmissions += o.PlannedSubmissions
	for _, h := range []struct{ dst, src *map[string]int }{
		{&s.ByReason, &o.ByReason}, {&s.ByDetail, &o.ByDetail}, {&s.Fallbacks, &o.Fallbacks},
	} {
		for k, n := range *h.src {
			bumpPaymentStat(h.dst, k, n)
		}
	}
}

// WriteText prints s as a stable, sorted text report (histogram keys in
// byte order), so two runs with equal counters print identical bytes.
func (s *PaymentPlanStats) WriteText(w io.Writer) error {
	if s == nil {
		s = &PaymentPlanStats{}
	}
	var err error
	p := func(format string, args ...any) {
		if err == nil {
			_, err = fmt.Fprintf(w, format, args...)
		}
	}
	hist := func(name string, m map[string]int) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		p("  %s:", name)
		if len(keys) == 0 {
			p(" (none)\n")
			return
		}
		p("\n")
		for _, k := range keys {
			p("    %-40s %d\n", k, m[k])
		}
	}
	p("payment plan stats:\n")
	p("  decisions built:       %d\n", s.DecisionsBuilt)
	p("  pool declined builds:  %d\n", s.PoolDeclined)
	p("  cast candidates:       %d\n", s.Candidates)
	p("  actions offered:       %d\n", s.ActionsOffered)
	p("  plans offered:         %d\n", s.PlansOffered)
	p("  search nodes:          %d (max %d)\n", s.Nodes, s.MaxNodes)
	p("  search_limit hits:     %d\n", s.SearchLimitHits)
	p("  planned submissions:   %d\n", s.PlannedSubmissions)
	hist("outcomes by reason", s.ByReason)
	hist("outcomes by detail", s.ByDetail)
	hist("fallbacks by reason", s.Fallbacks)
	return err
}
