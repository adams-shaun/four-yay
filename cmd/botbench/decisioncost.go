package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// -decision-cost: wall-clock time each named policy's seat spends inside
// its own Decide/DecideBoard, per decision kind (mean, p50, p95, p99, max).
// Only *seat.Bot seats are wrapped (the wrapper forwards the BoardSeat and
// PaymentPlanConsumer contracts the driver type-asserts, so the seat is
// driven exactly as unwrapped). The clock is read around the call and
// never reaches an answer, so a timed run's results are byte-identical to
// an untimed one; only the appended report (stderr under -out json) varies.

var decisionCostEnabled bool

var decisionCost = struct {
	sync.Mutex
	ns map[string][]int64 // "policy\tkind" -> per-decision nanoseconds
}{ns: map[string][]int64{}}

func recordDecisionCost(policy string, kind decision.Kind, d time.Duration) {
	decisionCost.Lock()
	k := policy + "\t" + string(kind)
	decisionCost.ns[k] = append(decisionCost.ns[k], d.Nanoseconds())
	all := policy + "\t(all)"
	decisionCost.ns[all] = append(decisionCost.ns[all], d.Nanoseconds())
	decisionCost.Unlock()
}

type timedBot struct {
	policy string
	b      *seat.Bot
}

func (t *timedBot) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	t0 := time.Now()
	in, err := t.b.Decide(ctx, v, d)
	recordDecisionCost(t.policy, d.Kind, time.Since(t0))
	return in, err
}

func (t *timedBot) DecideBoard(ctx context.Context, brd botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	t0 := time.Now()
	in, err := t.b.DecideBoard(ctx, brd, d)
	recordDecisionCost(t.policy, d.Kind, time.Since(t0))
	return in, err
}

func (t *timedBot) WantsPaymentActions() bool { return t.b.WantsPaymentActions() }

var (
	_ seat.Seat                = (*timedBot)(nil)
	_ seat.BoardSeat           = (*timedBot)(nil)
	_ seat.PaymentPlanConsumer = (*timedBot)(nil)
)

// wrapDecisionCost replaces the named policies' constructors with timed
// ones (a *seat.Bot result is wrapped; anything else is returned as is).
// Called once before any game starts.
func wrapDecisionCost(names ...string) {
	done := map[string]bool{}
	for _, n := range names {
		c, ok := policies[n]
		if !ok || done[n] {
			continue
		}
		done[n] = true
		name := n
		policies[n] = func(seed uint64) seat.Seat {
			s := c(seed)
			if b, ok := s.(*seat.Bot); ok {
				return &timedBot{policy: name, b: b}
			}
			return s
		}
	}
}

func writeDecisionCost(w io.Writer) {
	decisionCost.Lock()
	defer decisionCost.Unlock()
	keys := make([]string, 0, len(decisionCost.ns))
	for k := range decisionCost.ns {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintln(w, "decision cost (ms per decision, policy time only):")
	fmt.Fprintf(w, "  %-28s %-22s %9s %9s %9s %9s %9s %9s\n", "policy", "kind", "count", "mean", "p50", "p95", "p99", "max")
	for _, k := range keys {
		xs := append([]int64(nil), decisionCost.ns[k]...)
		sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
		var sum int64
		for _, x := range xs {
			sum += x
		}
		q := func(p float64) float64 { return float64(xs[int(p*float64(len(xs)-1))]) / 1e6 }
		var pol, kind string
		for i := range k {
			if k[i] == '\t' {
				pol, kind = k[:i], k[i+1:]
				break
			}
		}
		fmt.Fprintf(w, "  %-28s %-22s %9d %9.4f %9.4f %9.4f %9.4f %9.3f\n", pol, kind, len(xs),
			float64(sum)/float64(len(xs))/1e6, q(0.5), q(0.95), q(0.99), float64(xs[len(xs)-1])/1e6)
	}
}
