//go:build broker

// Package broker is an optional, experimental scheduler: it batches learner
// decisions from many independent gorge games into one inference call, with no
// all-games barrier.
//
// Build it with -tags broker. It is off by default so the default build, vet
// and the 32-bit gate never see it.
//
// Shape (mirroring mtg-kernel's src/async_rollout.rs, which states the rule
// this implements: "the broker snapshots whichever lanes are ready instead of
// imposing an all-lane barrier"):
//
//   - Each worker owns one game and runs the whole rules engine locally. The
//     opponent's moves never cross the broker boundary.
//   - Only a wrapped LEARNER seat's decisions reach the broker. Wrapping is
//     opt-in per seat, so a game can run one brokered seat against one direct
//     seat, and the control (no broker) is the same game with no wrap.
//   - The broker drains whatever is ready right now, packs the requests into
//     one ragged batch, and serves them with a single call. A game waits only
//     for its own request's batch, never for every other game.
//
// The inference call is pluggable via Scorer. The pass-through scorer (the
// default) is the control: it measures the scheduling envelope with no model
// cost, which is what the batch-width statistics are for. A GPU scorer can be
// dropped in without touching the scheduler.
package broker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// Enabled reports whether this build carries the real scheduler. A driver
// checks it once and falls back to running without batching.
const Enabled = true

// PassThrough is the control scorer: it serves the ready set without
// computing anything, so the scheduling envelope (width, service time) can be
// measured with no model cost. A real scorer computes the intents here.
type PassThrough struct{}

func (PassThrough) Serve([]*Request) error { return nil }
func (PassThrough) Name() string           { return "pass" }

// Request is one learner decision awaiting service.
type Request struct {
	SeatIdx  int
	View     view.View
	Decision decision.Decision
	// enqueued records when the request reached the broker, so queueing and
	// scoring latency are separable.
	enqueued time.Time
	answer   chan struct{}
}

// Scorer serves one packed batch of decisions. A real scorer computes the
// intents from the packed states; the pass-through scorer computes nothing,
// because the wrapped seat already made its choice and the broker is only
// deciding when to release it.
type Scorer interface {
	// Serve is called with a non-empty batch. It must return for every
	// request; the scheduler handles its errors by releasing the batch.
	Serve(batch []*Request) error
	// Name is the scorer's identity in the run report.
	Name() string
}

// Options configures a Broker.
type Options struct {
	// MaxBatch caps the number of requests served in one call.
	MaxBatch int
	// Wait is how long the broker lets stragglers join a batch before serving
	// the ready set. Zero serves whatever is ready immediately (the purest
	// no-barrier behaviour); a positive value trades latency for width.
	Wait time.Duration
	// OnServe, when non-nil, sees every served batch's width. Diagnostic only.
	OnServe func(width int)
}

// Stats is the scheduling envelope actually observed. The width statistics are
// the point: they say how much batching the no-barrier design buys at a given
// worker count and decision rate.
type Stats struct {
	Served   int64 // scorer calls
	States   int64 // requests served in total
	WidthSum int64
	WidthMax int64
	MaxBatch int64    // observed cap
	ServeNS  int64    // total wall time inside Serve
	QueueNS  int64    // total wall time a request waited to be served
	Buckets  [9]int64 // width 1,2,4,...,256
}

func (s Stats) MeanWidth() float64 {
	if s.Served == 0 {
		return 0
	}
	return float64(s.WidthSum) / float64(s.Served)
}

func (s Stats) MeanServeMicros() float64 {
	if s.Served == 0 {
		return 0
	}
	return float64(s.ServeNS) / float64(s.Served) / 1e3
}

func (s Stats) String() string {
	return fmt.Sprintf("calls %d, states %d, mean width %.2f, max width %d, mean serve %.1f us",
		s.Served, s.States, s.MeanWidth(), s.WidthMax, s.MeanServeMicros())
}

// Broker is the ready-set collector.
type Broker struct {
	opts   Options
	scorer Scorer
	in     chan *Request
	stop   chan struct{}
	done   chan struct{}

	stats atomic.Value // Stats, published on Stop

	served   atomic.Int64
	states   atomic.Int64
	widthSum atomic.Int64
	widthMax atomic.Int64
	serveNS  atomic.Int64
	queueNS  atomic.Int64
	buckets  [9]atomic.Int64
}

// New starts a broker. It returns an error rather than panicking so a driver
// can fall back to running without batching.
func New(opts Options, sc Scorer) (*Broker, error) {
	if sc == nil {
		return nil, errors.New("broker: nil scorer")
	}
	if opts.MaxBatch <= 0 {
		opts.MaxBatch = 256
	}
	b := &Broker{
		opts:   opts,
		scorer: sc,
		in:     make(chan *Request, opts.MaxBatch*4),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go b.loop()
	return b, nil
}

// Stop drains the broker and returns the observed envelope. It is safe to call
// once; stopping with requests in flight serves them before returning.
func (b *Broker) Stop() Stats {
	close(b.stop)
	<-b.done
	s := Stats{
		Served:   b.served.Load(),
		States:   b.states.Load(),
		WidthSum: b.widthSum.Load(),
		WidthMax: b.widthMax.Load(),
		ServeNS:  b.serveNS.Load(),
		QueueNS:  b.queueNS.Load(),
	}
	for i := range b.buckets {
		s.Buckets[i] = b.buckets[i].Load()
	}
	return s
}

// Wrap returns inner behind the broker: its decisions are published for
// scheduling, then released with the intent the seat chose. Wrap is the whole
// opt-in -- an unwrapped seat is the control.
func (b *Broker) Wrap(inner seat.Seat, seatIdx int) seat.Seat {
	return &wrapped{inner: inner, br: b, seatIdx: seatIdx}
}

// submit publishes one request and waits for its release.
func (b *Broker) submit(ctx context.Context, r *Request) error {
	select {
	case b.in <- r:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-r.answer:
		b.queueNS.Add(time.Since(r.enqueued).Nanoseconds())
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Broker) observe(width int) {
	b.served.Add(1)
	b.states.Add(int64(width))
	b.widthSum.Add(int64(width))
	for {
		m := b.widthMax.Load()
		if int64(width) <= m || b.widthMax.CompareAndSwap(m, int64(width)) {
			break
		}
	}
	bit := 0
	for w := width; w > 1 && bit < len(b.buckets)-1; w >>= 1 {
		bit++
	}
	b.buckets[bit].Add(1)
	if b.opts.OnServe != nil {
		b.opts.OnServe(width)
	}
}

// loop is the scheduler. It never waits for a barrier; it serves the ready set
// (optionally after Wait for stragglers, to observe the width/latency trade).
func (b *Broker) loop() {
	defer close(b.done)
	batch := make([]*Request, 0, b.opts.MaxBatch)
	var timer *time.Timer
	var timeout <-chan time.Time

	flush := func() {
		if len(batch) == 0 {
			return
		}
		t0 := time.Now()
		err := b.scorer.Serve(batch)
		b.serveNS.Add(time.Since(t0).Nanoseconds())
		if err != nil {
			fmt.Printf("broker: scorer %s failed: %v\n", b.scorer.Name(), err)
		}
		b.observe(len(batch))
		for _, r := range batch {
			close(r.answer)
		}
		batch = batch[:0]
		if timer != nil {
			timer.Stop()
			timer, timeout = nil, nil
		}
	}

	for {
		select {
		case <-b.stop:
			// Serve everything still queued, so no game is left waiting.
			for {
				select {
				case r := <-b.in:
					batch = append(batch, r)
					if len(batch) >= b.opts.MaxBatch {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case r := <-b.in:
			batch = append(batch, r)
			if len(batch) >= b.opts.MaxBatch {
				flush()
				continue
			}
			if b.opts.Wait > 0 {
				// Let stragglers join; the timer serves the batch.
				if timeout == nil {
					timer = time.NewTimer(b.opts.Wait)
					timeout = timer.C
				}
			} else {
				// No wait: drain whatever else is already queued and serve the
				// ready set now. This is the purest no-barrier behaviour -- a
				// game waits only for the batch that formed at its arrival.
				for len(batch) < b.opts.MaxBatch {
					select {
					case r2 := <-b.in:
						batch = append(batch, r2)
					default:
						flush()
						goto drained
					}
				}
				flush()
			drained:
			}
		case <-timeout:
			flush()
		}
	}
}

// wrapped is a seat whose decisions travel through the broker.
type wrapped struct {
	inner   seat.Seat
	br      *Broker
	seatIdx int
}

func (s *wrapped) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	// The wrapped seat chooses; the broker schedules. A GPU scorer would move
	// the choice into Serve, and this call would return whatever it produced.
	in, err := s.inner.Decide(ctx, v, d)
	if err != nil {
		return in, err
	}
	r := &Request{SeatIdx: s.seatIdx, View: v, Decision: d,
		enqueued: time.Now(), answer: make(chan struct{})}
	if err := s.br.submit(ctx, r); err != nil {
		// A broker hiccup must never change a game: proceed with the seat's
		// own intent.
		return in, nil
	}
	return in, nil
}

// WantsPaymentActions forwards the lazy payment-action opt-in, so wrapping does
// not change a seat's communication contract.
func (s *wrapped) WantsPaymentActions() bool {
	if c, ok := s.inner.(seat.PaymentPlanConsumer); ok {
		return c.WantsPaymentActions()
	}
	return false
}

// Ensure the wrapped seat satisfies the interfaces a host may demand.
var (
	_ seat.Seat                = (*wrapped)(nil)
	_ seat.PaymentPlanConsumer = (*wrapped)(nil)
	_ sync.Locker              = (*noopLocker)(nil)
)

type noopLocker struct{}

func (*noopLocker) Lock()   {}
func (*noopLocker) Unlock() {}
