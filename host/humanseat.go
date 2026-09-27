package host

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// HumanSeat is a seat.Seat that can hold one decision pending and be answered
// from outside (Task M2b-2's SubmitIntent). It is "just another seat.Seat":
// the engine hands it the same view.View and decision.Decision it hands any
// bot, and blocks on Decide until an intent is submitted, ctx is cancelled, or
// ThinkTimeout elapses. It carries a caretaker seat (Task M2b-3): the
// deterministic bot that would have occupied this slot in a pure-bot game, so
// an unanswered decision is turned by exactly the intent that bot would have
// produced and the logged game replays byte-identically (D3).
type HumanSeat struct {
	mu sync.Mutex
	// slot is the decision currently being answered, together with the recv
	// channel that submits to it; nil when Decide is not parked (or has
	// already returned). Task M2b-1's design decision D1: each Decide call
	// gets its own slot, so a recv channel belongs to exactly one decision
	// and cannot outlive her -- a late submit for an abandoned decision goes
	// to a channel nobody will ever read again, instead of poisoning the next
	// decision (the abandoned-slot leak this fix round is named for).
	slot *pendingSlot
	// timeout is the per-decision think budget in force (Options.ThinkTimeout
	// at the time play configured this seat). 0 means no timeout: the seat
	// waits for a submit or ctx cancel as it always did.
	timeout time.Duration
	// caretaker is the deterministic bot that answers a timed-out or
	// ctx-cancelled decision in the player's place. Configuring walk sets it
	// to the bot defaultSeats would have built for this slot.
	caretaker seat.Seat
	// caretakerFires counts how many decisions this seat has delegated to its
	// caretaker. Task M2b-5 reads it to assert positively that a human who
	// answered every decision was never substituted for. Without a counter it
	// would be unobservable: by design D3 a caretaker's intent is byte-identical
	// to the human's, so absence-of-something is the only signal available, and
	// a counter is that signal.
	caretakerFires uint64
}

// configure arms the seat with its caretaker and think budget. play calls it
// once per match, on the match goroutine, before any Decide, so it never
// races a parked Decide; the fields are copied under the mutex in Decide
// (M2b-2's copy-under-the-lock shape). A zero timeout still installs the
// caretaker so ctx cancellation falls back to it.
func (s *HumanSeat) configure(timeout time.Duration, caretaker seat.Seat) {
	s.mu.Lock()
	s.timeout = timeout
	s.caretaker = caretaker
	s.mu.Unlock()
}

// pendingSlot binds one decision to the recv channel that answers it. Keep
// dec by value so a mechanical copy has its own decision instance, and keep
// recv per-call (capacity 1) so a racing submit is never silently dropped.
type pendingSlot struct {
	dec  decision.Decision
	recv chan decision.Intent
}

// NewHumanSeat returns a HumanSeat with no decision pending.
func NewHumanSeat() *HumanSeat {
	return &HumanSeat{}
}

// Decide records d as the pending decision and blocks until a matching intent
// is submitted, ctx is done, or ThinkTimeout elapses. It satisfies seat.Seat.
// Task M2b-3 wires the two exit-but-still-playable paths (timeout and ctx
// cancel) to the caretaker: instead of erroring (which would crash the match
// and, worse, leave a disconnected human able to wedge play's blocked Decide
// forever -- FL-17), the seat answers with the deterministic bot's intent for
// that slot, which play submits like any other, so the logged game continues as
// if the slot had been a bot all along (D3). A human reconnect still works via
// SubmitIntent against the oncoming decisions (D2). With no caretaker (a bare
// NewHumanSeat, as today's unit tests build) or a zero ThinkTimeout, ctx
// cancellation alone is what unblocks Decide and returns ctx.Err().
//
// Every return path -- human answer, caretaker answer and cancellation alike
// -- clears the seat's slot, but only if it is still this call's own slot:
// identity comparison stops an earlier Decide from un-publishing the slot an
// overlapping later Decide installed (Decide is not called concurrently
// today, but defensive here costs nothing). With the slot gone, a later submit
// finds no current decision to answer and is rejected, and the next Decide
// parks on a brand-new channel. The caretaker path is no exception: it clears
// the slot through the same defer, so after a timeout the seat is clean for
// the next decision -- no leftover slot, no buffered intent.
// parking is the slate of one parked-but-not-yet-answered decision. park()
// installs the seat's slot (so Pending/SubmitIntent find and can answer it)
// and returns a parking whose await() blocks for the answer. Split that way,
// play() can make a decision answerable BEFORE it is ever published, which is
// exactly the ordering that closes the publish-before-park race: nothing may
// observe a pending decision before the seat that owns it is able to accept
// an answer for it. Between park returning and await being called the seat is
// already accept-ready.
type parking struct {
	s         *HumanSeat
	slot      *pendingSlot
	ctx       context.Context
	v         view.View
	d         decision.Decision
	timeout   time.Duration
	caretaker seat.Seat
	// undo is the match's undo-request channel (nil for the plain seat
	// API); await reports an undoRequest carrying the requesting seat when
	// it fires, so an undo can interrupt a parked human exactly as a
	// submitted intent would.
	undo <-chan state.PlayerID
}

// park installs d as the seat's pending decision without blocking, returning
// a handle whose await() blocks for the answer. It snapshots the think budget
// and caretaker under the same mutex Decide took, so await behaves exactly as
// the old one-shot Decide did. undo, when non-nil, is the match's undo-request
// channel: await reports the undoRequest on it instead of an intent, so an
// undo can interrupt a parked human. The plain seat API (Decide) passes nil —
// a nil channel never fires.
func (s *HumanSeat) park(ctx context.Context, v view.View, d decision.Decision, undo <-chan state.PlayerID) *parking {
	owned := *d.Clone()
	slot := &pendingSlot{dec: owned, recv: make(chan decision.Intent, 1)}
	s.mu.Lock()
	s.slot = slot
	timeout := s.timeout
	caretaker := s.caretaker
	s.mu.Unlock()
	return &parking{s: s, slot: slot, ctx: ctx, v: v, d: owned, timeout: timeout, caretaker: caretaker, undo: undo}
}

// Decide records d as the pending decision and blocks until a matching intent
// is submitted, ctx is done, or ThinkTimeout elapses — park immediately
// followed by await. It satisfies seat.Seat. The plain seat API never sees an
// undo channel, so Decide's parking carries a nil one (a nil channel never
// fires).
func (s *HumanSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return s.park(ctx, v, d, nil).await()
}

// abandon clears the seat's slot if it is still this parking's — the same
// identity guard await's own defer uses. play's undo path calls it on a
// decision an undo discards, so the abandoned decision stops being
// answerable before the match is rewound out from under it.
func (p *parking) abandon() {
	s := p.s
	s.mu.Lock()
	if s.slot == p.slot {
		s.slot = nil
	}
	s.mu.Unlock()
}

// await is the blocking second half of Decide: it waits until the parked
// decision is answered by a SubmitIntent, ctx is done, or ThinkTimeout
// elapses (falling back to the caretaker), with the same slot-clear-on-return
// discipline as the old Decide. Every return path clears the seat's slot, but
// only if it is still this parking's own slot, so a mistaken re-use cannot
// un-publish a later decision; and the caretaker path shares the same
// clearing, so after a timeout or cancellation the seat is clean for the next
// decision.
func (p *parking) await() (decision.Intent, error) {
	s := p.s
	defer func() {
		s.mu.Lock()
		if s.slot == p.slot {
			s.slot = nil
		}
		s.mu.Unlock()
	}()

	if p.timeout > 0 {
		timer := time.NewTimer(p.timeout)
		defer timer.Stop()
		select {
		case in := <-p.slot.recv:
			return in, nil
		case <-p.ctx.Done():
			if p.caretaker != nil {
				return s.viaCaretaker(p.ctx, p.v, p.d, p.caretaker)
			}
			return decision.Intent{}, p.ctx.Err()
		case req := <-p.undo:
			// The undo signal won the race against any intent: the decision
			// this parking was installed for is about to be rewound away.
			// The payload is the requesting seat; play consumes it.
			return decision.Intent{}, undoRequest{player: req}
		case <-timer.C:
			if p.caretaker != nil {
				return s.viaCaretaker(p.ctx, p.v, p.d, p.caretaker)
			}
			// A timeout configured but no caretaker to fall back to is an
			// unarmed seat (unreachable when play configured it); rather than
			// error a decision nobody asked us to abandon, drop through to the
			// blocking select and keep waiting on submit/ctx/undo.
		}
	}
	select {
	case in := <-p.slot.recv:
		return in, nil
	case <-p.ctx.Done():
		if p.caretaker != nil {
			return s.viaCaretaker(p.ctx, p.v, p.d, p.caretaker)
		}
		return decision.Intent{}, p.ctx.Err()
	case req := <-p.undo:
		return decision.Intent{}, undoRequest{player: req}
	}
}

// viaCaretaker records a caretaker substitution and performs it. The caretaker
// argument is the copy the caller took under the mutex (and validated non-nil
// on the same copy), so this helper performs the substitution against that
// validated copy rather than re-reading the field -- the nil guard and the
// dereference can never diverge. The counter is Task M2b-5's positive signal
// that a human answered every decision: it is incremented under the same mutex
// that guards the seat's other fields, before the (deterministic) caretaker
// intent is produced, so a test can read it only after Decide has returned
// (the match goroutine is the sole writer). The counter increment is the only
// field access this helper makes.
func (s *HumanSeat) viaCaretaker(ctx context.Context, v view.View, d decision.Decision, caretaker seat.Seat) (decision.Intent, error) {
	s.mu.Lock()
	s.caretakerFires++
	s.mu.Unlock()
	return caretaker.Decide(ctx, v, d)
}

// caretakerCount reports how many decisions the caretaker has answered in
// this seat's place. 0 after a full match means every decision was answered
// by a human submit. Package-internal; Task M2b-5 reads it to assert the
// caretaker never fired.
func (s *HumanSeat) caretakerCount() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caretakerFires
}

// pending returns whether a decision is currently being answered, and the
// decision itself. Package-internal; Task M2b-2's Pending/SubmitIntent use it.
// (Signature is (bool, *decision.Decision) -- ok first -- to match the task's
// step-1 test `if ok, got := s.pending(); !ok || got.Seq != 3`.)
func (s *HumanSeat) pending() (bool, *decision.Decision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.slot == nil {
		return false, nil
	}
	return true, s.slot.dec.Clone()
}

// submit delivers an intent to a parked Decide, refusing anything that does not
// match the pending decision. It runs decision.Decision.Validate so only valid
// intents wake the seat: wrong Seq, wrong player, wrong option indices,
// wrong min-max, or duplicates are all rejected without unblocking Decide
// (the engine is free to have moved on; a stale answer must not clobber it).
// The slot pointer is copied under the mutex but the send happens OUTSIDE it,
// so a send can never block a holder of s.mu (a leaked per-decision send would
// otherwise deadlock pending() and the next Decide).
func (s *HumanSeat) submit(in decision.Intent) error {
	return s.submitAdmitted(in, nil)
}

// submitAdmitted is submit with an optional host admission check.  The check
// runs while slot is still protected, after the wire-level decision validation
// and before the intent is accepted into the slot's channel.  This lets the
// host reject an offered-looking payment witness which the rules engine can no
// longer execute, without waking the match goroutine into a crash.
//
// The channel send remains outside s.mu: a receiver may be delayed and must
// never block Pending or another stale-answer rejection while holding the seat
// mutex.  Clone before admission so the exact value admitted is the one sent.
func (s *HumanSeat) submitAdmitted(in decision.Intent, admit func(decision.Decision, decision.Intent) error) error {
	in = decision.CloneIntent(in)
	s.mu.Lock()
	if s.slot == nil {
		s.mu.Unlock()
		return fmt.Errorf("no pending decision")
	}
	slot := s.slot
	if err := slot.dec.Validate(in); err != nil {
		s.mu.Unlock()
		return err
	}
	if admit != nil {
		if err := admit(*slot.dec.Clone(), in); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.mu.Unlock()
	slot.recv <- in
	return nil
}
