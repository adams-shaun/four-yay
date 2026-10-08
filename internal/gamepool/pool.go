//go:build gamepool

// Package gamepool is an optional, experimental games runner: it keeps M games
// in a pool and, for each seat, runs N goroutines that wait on a channel the
// games notify when it is that seat's turn. A decision with more than one
// legal option is immediately enqueued to a shared GPU batch; the enqueuing
// goroutine does NOT block on the GPU -- it schedules a callback that hands the
// served intent back to the waiting game. Trivial decisions (<= 1 option) are
// answered by the wrapped bot directly and never reach the batch.
//
// Build it with -tags gamepool. It is off by default so the default build,
// vet and the 32-bit gate never see it. The GPU scorer is a further tag,
// gamepool_gpu, so the scheduler itself stays pure Go.
//
// The shape, from the operator's spec:
//
//   - M games in a pool. Each game runs the whole rules engine locally.
//   - N goroutines per player/bot pull turn notifications from that seat's
//     channel. They never block forwarding to the GPU: they submit to the
//     batch with a callback and immediately wait for the next turn.
//   - The batch flushes when it holds MaxBatch requests or FlushEvery has
//     elapsed since the first straggler, whichever comes first.
//   - The scorer serves the whole batch in one call. The pass-through/local
//     scorer delegates to the wrapped bot, so the scheduling envelope can be
//     measured with no model cost; the flat GPU scorer runs one forward.
package gamepool

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// Enabled reports whether this build carries the real runner.
const Enabled = true

var dbgOn = os.Getenv("GAMEPOOL_DEBUG") != ""

func dbg(f string, a ...any) {
	if dbgOn {
		fmt.Fprintf(os.Stderr, "gamepool[dbg]: "+f+"\n", a...)
	}
}

// Request is one learner decision awaiting service. Inner is the wrapped seat
// the scheduler falls back to (and the local scorer uses); Intent is filled by
// the scorer and delivered to the game by the callback (done).
type Request struct {
	SeatIdx int
	Inner   seat.Seat
	View    view.View
	Decision decision.Decision
	Intent  decision.Intent

	Answer   chan decision.Intent
	done     func(decision.Intent)
	enqueued time.Time
}

// Scorer serves one packed batch of decisions in a single call. The local
// scorer delegates to each request's wrapped bot; a GPU scorer encodes the
// batch and runs one forward.
type Scorer interface {
	Serve(batch []*Request) error
	Name() string
}

// LocalScorer is the control scorer: it answers every request from its wrapped
// bot, so the pool exercises the full scheduling path with no model cost.
type LocalScorer struct{}

func (LocalScorer) Serve(batch []*Request) error {
	for _, r := range batch {
		in, err := r.Inner.Decide(context.Background(), r.View, r.Decision)
		if err != nil {
			return err
		}
		r.Intent = in
	}
	return nil
}

func (LocalScorer) Name() string { return "local" }

// Options configures a Pool.
type Options struct {
	// Workers is N: the goroutines per seat that pull turn notifications.
	Workers int
	// MaxBatch is B: the batch flush threshold.
	MaxBatch int
	// FlushEvery is X: flush a partial batch after this long. Zero flushes the
	// ready set immediately (width ~1 at the arrival instant).
	FlushEvery time.Duration
	// OnServe, when non-nil, sees every served batch's width.
	OnServe func(width int)
}

// Stats is the scheduling envelope observed.
type Stats struct {
	Served   int64
	States   int64
	WidthSum int64
	WidthMax int64
	ServeNS  int64
	QueueNS  int64
	Buckets  [9]int64
}

func (s Stats) MeanWidth() float64 {
	if s.Served == 0 {
		return 0
	}
	return float64(s.WidthSum) / float64(s.Served)
}

func (s Stats) String() string {
	return fmt.Sprintf("calls %d, states %d, mean width %.2f, max width %d",
		s.Served, s.States, s.MeanWidth(), s.WidthMax)
}

// Pool owns the batch and the per-seat consumer goroutines.
type Pool struct {
	opts   Options
	scorer Scorer
	turn   []chan *Request

	consWg  sync.WaitGroup
	batcher *batch

	served   atomic.Int64
	states   atomic.Int64
	widthSum atomic.Int64
	widthMax atomic.Int64
	serveNS  atomic.Int64
	queueNS  atomic.Int64
	buckets  [9]atomic.Int64
}

// New starts the pool: one batch goroutine and opts.Workers consumer
// goroutines for each of numSeats seats.
func New(numSeats int, sc Scorer, opts Options) (*Pool, error) {
	if sc == nil {
		return nil, fmt.Errorf("gamepool: nil scorer")
	}
	if opts.Workers <= 0 {
		opts.Workers = 1
	}
	if opts.MaxBatch <= 0 {
		opts.MaxBatch = 64
	}
	b := &batch{
		in:   make(chan *Request, opts.MaxBatch*4),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	b.opts, b.scorer, b.pool = opts, sc, nil
	p := &Pool{opts: opts, scorer: sc, batcher: b}
	b.pool = p
	p.turn = make([]chan *Request, numSeats)
	for i := range p.turn {
		p.turn[i] = make(chan *Request)
	}
	go b.loop()
	if os.Getenv("GAMEPOOL_DEBUG") != "" {
		go func() {
			for {
				time.Sleep(time.Second)
				fmt.Fprintf(os.Stderr, "gamepool[dbg]: in=%d buf=%d\n", len(b.in), b.blen.Load())
			}
		}()
	}
	for _, ch := range p.turn {
		for w := 0; w < opts.Workers; w++ {
			p.consWg.Add(1)
			go func(ch chan *Request) {
				defer p.consWg.Done()
				for r := range ch {
					p.consume(r)
				}
			}(ch)
		}
	}
	return p, nil
}

// consume is one per-seat worker: a trivial decision is answered from the
// wrapped bot inline; a non-trivial one is enqueued to the batch, and the
// worker immediately returns to wait for the next turn (no GPU block).
func (p *Pool) consume(r *Request) {
	if len(r.Decision.Options) <= 1 {
		in, err := r.Inner.Decide(context.Background(), r.View, r.Decision)
		if err != nil {
			in = oneOptionIntent(r.Decision)
		}
		r.done(in)
		return
	}
	p.batcher.in <- r
}

// Wrap returns inner behind the pool: its decisions are published to the
// seat's turn channel and released with the served intent.
func (p *Pool) Wrap(inner seat.Seat, seatIdx int) seat.Seat {
	return &wrapped{pool: p, inner: inner, seatIdx: seatIdx}
}

// Stop waits for the batch to drain and returns the envelope. All games must
// have finished -- no wrapped seat may still call Decide -- before it runs: it
// closes the turn channels, lets every consumer drain and submit its last
// request (the batch is still serving), then stops the batch.
func (p *Pool) Stop() Stats {
	for _, ch := range p.turn {
		close(ch)
	}
	p.consWg.Wait()
	close(p.batcher.stop)
	<-p.batcher.done
	s := Stats{
		Served:   p.served.Load(),
		States:   p.states.Load(),
		WidthSum: p.widthSum.Load(),
		WidthMax: p.widthMax.Load(),
		ServeNS:  p.serveNS.Load(),
		QueueNS:  p.queueNS.Load(),
	}
	for i := range p.buckets {
		s.Buckets[i] = p.buckets[i].Load()
	}
	return s
}

func (p *Pool) observe(width int) {
	p.served.Add(1)
	p.states.Add(int64(width))
	p.widthSum.Add(int64(width))
	for {
		m := p.widthMax.Load()
		if int64(width) <= m || p.widthMax.CompareAndSwap(m, int64(width)) {
			break
		}
	}
	bit := 0
	for w := width; w > 1 && bit < len(p.buckets)-1; w >>= 1 {
		bit++
	}
	p.buckets[bit].Add(1)
	if p.opts.OnServe != nil {
		p.opts.OnServe(width)
	}
}

// batch collects requests and flushes at B or after X.
type batch struct {
	opts   Options
	scorer Scorer
	pool   *Pool
	in     chan *Request
	stop   chan struct{}
	done   chan struct{}

	blen atomic.Int64 // len(buf), for GAMEPOOL_DEBUG
}

func (b *batch) loop() {
	defer close(b.done)
	buf := make([]*Request, 0, b.opts.MaxBatch)
	var timer *time.Timer
	var tc <-chan time.Time

	flush := func() {
		if len(buf) == 0 {
			dbg("flush empty (timerNil=%v tcNil=%v)", timer == nil, tc == nil)
			return
		}
		dbg("flush %d", len(buf))
		t0 := time.Now()
		err := b.scorer.Serve(buf)
		b.pool.serveNS.Add(time.Since(t0).Nanoseconds())
		if err != nil {
			fmt.Printf("gamepool: scorer %s failed: %v\n", b.scorer.Name(), err)
			for _, r := range buf {
				if in, e := r.Inner.Decide(context.Background(), r.View, r.Decision); e == nil {
					r.Intent = in
				}
			}
		}
		b.pool.observe(len(buf))
		for _, r := range buf {
			b.pool.queueNS.Add(time.Since(r.enqueued).Nanoseconds())
			r.done(r.Intent)
		}
		buf = buf[:0]
		b.blen.Store(0)
		if timer != nil {
			timer.Stop()
			timer, tc = nil, nil
		}
	}

	for {
		select {
		case <-b.stop:
			// Drain whatever is queued, then serve it.
			for {
				select {
				case r := <-b.in:
					buf = append(buf, r)
					if len(buf) >= b.opts.MaxBatch {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case r := <-b.in:
			buf = append(buf, r)
			b.blen.Store(int64(len(buf)))
			if len(buf) >= b.opts.MaxBatch {
				flush()
				continue
			}
			if b.opts.FlushEvery > 0 {
				if timer == nil {
					timer = time.NewTimer(b.opts.FlushEvery)
					tc = timer.C
					dbg("arm buf=%d", len(buf))
				}
			} else {
				flush()
			}
		case <-tc:
			dbg("timer fired buf=%d timerNil=%v", len(buf), timer == nil)
			flush()
		}
	}
}

// wrapped is a seat whose decisions travel through the pool.
type wrapped struct {
	pool    *Pool
	inner   seat.Seat
	seatIdx int
}

func (s *wrapped) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	r := &Request{
		SeatIdx: s.seatIdx, Inner: s.inner, View: v, Decision: d,
		Answer: make(chan decision.Intent, 1), enqueued: time.Now(),
	}
	r.done = func(in decision.Intent) { r.Answer <- in }
	select {
	case s.pool.turn[s.seatIdx] <- r:
	case <-ctx.Done():
		return decision.Intent{}, ctx.Err()
	}
	select {
	case in := <-r.Answer:
		return in, nil
	case <-ctx.Done():
		return decision.Intent{}, ctx.Err()
	}
}

func (s *wrapped) WantsPaymentActions() bool {
	if c, ok := s.inner.(seat.PaymentPlanConsumer); ok {
		return c.WantsPaymentActions()
	}
	return false
}

var _ seat.Seat = (*wrapped)(nil)

// oneOptionIntent answers a decision that offers at most one option with that
// option, the only legal choice.
func oneOptionIntent(d decision.Decision) decision.Intent {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	if len(d.Options) == 1 {
		in.Choices = []int{d.Options[0].Index}
	}
	return in
}
